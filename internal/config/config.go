// Package config loads the deliberately small, strict wishport configuration.
package config

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/athphane/wishport/internal/inventory"
	sshclient "github.com/athphane/wishport/internal/ssh"
	"go.yaml.in/yaml/v3"
)

type Sources struct {
	SSHConfig      bool   `yaml:"ssh_config"`
	Wishlist       bool   `yaml:"wishlist"`
	WishlistConfig string `yaml:"wishlist_config"`
}

type Discovery struct {
	MDNS      MDNS
	SRV       SRV
	Tailscale Tailscale
}
type MDNS struct {
	Enabled bool
	Domain  string
	Timeout time.Duration
}
type SRV struct {
	Domains []string
	Timeout time.Duration
}
type Tailscale struct {
	Tailnet string
	Timeout time.Duration
}
type Forge struct {
	Enabled    bool          `yaml:"enabled"`
	CacheTTL   time.Duration `yaml:"-"`
	KeyInstall string        `yaml:"key_install"`
	PublicKey  string        `yaml:"public_key"`
}
type Config struct {
	Version     int
	Sources     Sources
	Forge       Forge
	Connections []inventory.Connection
	Discovery   Discovery
}

func Defaults() Config {
	return Config{Version: 1, Sources: Sources{SSHConfig: true, Wishlist: true}, Forge: Forge{Enabled: true, CacheTTL: 168 * time.Hour, KeyInstall: "ask"}, Discovery: Discovery{MDNS: MDNS{Domain: "local", Timeout: 2 * time.Second}, SRV: SRV{Timeout: 5 * time.Second}, Tailscale: Tailscale{Timeout: 15 * time.Second}}}
}
func DefaultPath() (string, error) {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", fmt.Errorf("find configuration directory: %w", e)
		}
		d = filepath.Join(home, ".config")
	}
	path := filepath.Join(d, "wishport", "config.yaml")
	// Preserve development installations without moving or rewriting user files.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		legacy := filepath.Join(d, "sshdir", "config.yaml")
		if _, err := os.Stat(legacy); err == nil {
			return legacy, nil
		}
	}
	return path, nil
}

// Load returns defaults together with os.ErrNotExist for a missing file. Callers
// may ignore that error for the default path, but should report explicit paths.
func Load(path string) (Config, error) {
	c := Defaults()
	f, e := os.Open(path)
	if e != nil {
		return c, fmt.Errorf("open configuration: %w", e)
	}
	defer f.Close()
	var root yaml.Node
	dec := yaml.NewDecoder(io.LimitReader(f, 4<<20))
	if e = dec.Decode(&root); e != nil {
		if e == io.EOF {
			return c, nil
		}
		return c, fmt.Errorf("invalid YAML configuration (check syntax)")
	}
	var extra yaml.Node
	if e = dec.Decode(&extra); e != io.EOF {
		return c, fmt.Errorf("configuration must contain one YAML document")
	}
	if len(root.Content) == 0 {
		return c, nil
	}
	n := root.Content[0]
	fields, e := mapping(n, map[string]bool{"version": true, "sources": true, "forge": true, "connections": true, "discovery": true})
	if e != nil {
		return c, e
	}
	if n := fields["version"]; n != nil {
		if n.Tag != "!!int" || n.Decode(&c.Version) != nil || c.Version != 1 {
			return c, fmt.Errorf("version must be 1")
		}
	}
	if n := fields["sources"]; n != nil {
		m, e := mapping(n, map[string]bool{"ssh_config": true, "wishlist": true, "wishlist_config": true})
		if e != nil {
			return c, e
		}
		if n := m["ssh_config"]; n != nil {
			if n.Tag != "!!bool" || n.Decode(&c.Sources.SSHConfig) != nil {
				return c, fmt.Errorf("sources.ssh_config must be a boolean")
			}
		}
		if n := m["wishlist"]; n != nil {
			if n.Tag != "!!bool" || n.Decode(&c.Sources.Wishlist) != nil {
				return c, fmt.Errorf("sources.wishlist must be a boolean")
			}
		}
		if n := m["wishlist_config"]; n != nil {
			if stringValue(n, &c.Sources.WishlistConfig) != nil || badText(c.Sources.WishlistConfig) {
				return c, fmt.Errorf("sources.wishlist_config must be a file path")
			}
		}
	}
	if n := fields["discovery"]; n != nil {
		if err := loadDiscovery(n, &c.Discovery); err != nil {
			return c, err
		}
	}
	if n := fields["forge"]; n != nil {
		m, e := mapping(n, map[string]bool{"enabled": true, "cache_ttl": true, "key_install": true, "public_key": true})
		if e != nil {
			return c, e
		}
		if n := m["enabled"]; n != nil {
			if n.Tag != "!!bool" || n.Decode(&c.Forge.Enabled) != nil {
				return c, fmt.Errorf("forge.enabled must be a boolean")
			}
		}
		if n := m["cache_ttl"]; n != nil {
			v, e := time.ParseDuration(n.Value)
			if n.Tag != "!!str" || e != nil || v <= 0 {
				return c, fmt.Errorf("forge.cache_ttl must be a positive duration (for example 168h)")
			}
			c.Forge.CacheTTL = v
		}
		for key, dst := range map[string]*string{"key_install": &c.Forge.KeyInstall, "public_key": &c.Forge.PublicKey} {
			if n := m[key]; n != nil {
				if e := stringValue(n, dst); e != nil {
					return c, fmt.Errorf("forge.%s must be a string", key)
				}
			}
		}
		if c.Forge.KeyInstall != "ask" && c.Forge.KeyInstall != "disabled" {
			return c, fmt.Errorf("forge.key_install must be ask or disabled")
		}
		if badText(c.Forge.PublicKey) {
			return c, fmt.Errorf("forge.public_key contains control characters")
		}
	}
	if n := fields["connections"]; n != nil {
		if n.Kind != yaml.SequenceNode {
			return c, fmt.Errorf("connections must be a list")
		}
		seen := map[string]bool{}
		for i, item := range n.Content {
			conn, e := connection(item)
			if e != nil {
				return c, fmt.Errorf("connection %d: %w", i+1, e)
			}
			if seen[conn.ID] {
				return c, fmt.Errorf("connection %d: duplicate id", i+1)
			}
			seen[conn.ID] = true
			c.Connections = append(c.Connections, conn)
		}
	}
	return c, nil
}
func mapping(n *yaml.Node, allowed map[string]bool) (map[string]*yaml.Node, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	m := map[string]*yaml.Node{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Tag != "!!str" || !allowed[k.Value] {
			return nil, fmt.Errorf("line %d: unknown configuration field (see configuration reference)", k.Line)
		}
		if m[k.Value] != nil {
			return nil, fmt.Errorf("line %d: duplicate configuration field", k.Line)
		}
		m[k.Value] = v
	}
	return m, nil
}
func stringValue(n *yaml.Node, dst *string) error {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return fmt.Errorf("expected string")
	}
	*dst = n.Value
	return nil
}

var token = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:+%-]*$`)
var ident = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func badText(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) }) >= 0
}
func connection(n *yaml.Node) (inventory.Connection, error) {
	c := inventory.Connection{Source: inventory.SourceYAML, Kind: inventory.KindHost}
	m, e := mapping(n, map[string]bool{"id": true, "name": true, "description": true, "host": true, "ssh_alias": true, "port": true, "user": true, "identity": true, "directory": true})
	if e != nil {
		return c, e
	}
	var id string
	for k, d := range map[string]*string{"id": &id, "name": &c.Name, "description": &c.Description, "host": &c.Host, "ssh_alias": &c.SSHAlias, "user": &c.User, "identity": &c.Identity, "directory": &c.Directory} {
		if n := m[k]; n != nil {
			if stringValue(n, d) != nil {
				return c, fmt.Errorf("%s must be a string", k)
			}
		}
		if badText(*d) {
			return c, fmt.Errorf("%s contains control characters", k)
		}
	}
	if !ident.MatchString(id) {
		return c, fmt.Errorf("id is required and must contain letters, digits, dots, underscores or hyphens")
	}
	c.ID = "yaml:" + id
	if strings.TrimSpace(c.Name) == "" {
		c.Name = id
	}
	if c.Host == "" && c.SSHAlias == "" {
		return c, fmt.Errorf("host or ssh_alias is required")
	}
	for k, v := range map[string]string{"host": c.Host, "ssh_alias": c.SSHAlias, "user": c.User} {
		if v != "" && !token.MatchString(v) && !(k == "host" && net.ParseIP(strings.Trim(v, "[]")) != nil) {
			return c, fmt.Errorf("%s has invalid characters or starts with an option prefix", k)
		}
	}
	for k, v := range map[string]string{"identity": c.Identity, "directory": c.Directory} {
		if strings.HasPrefix(v, "-") {
			return c, fmt.Errorf("%s must not start with an option prefix", k)
		}
	}
	if n := m["port"]; n != nil {
		if n.Tag != "!!int" || n.Decode(&c.Port) != nil || c.Port < 1 || c.Port > 65535 {
			return c, fmt.Errorf("port must be an integer from 1 to 65535")
		}
	}
	if err := sshclient.Validate(c); err != nil {
		return c, fmt.Errorf("invalid connection host, user, port, identity or directory (see configuration reference)")
	}
	return c, nil
}
