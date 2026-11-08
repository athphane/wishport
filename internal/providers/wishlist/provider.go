// Package wishlist imports trusted local Wishlist configuration without running a server.
package wishlist

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/gobwas/glob"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/providers/sshconfig"
	"github.com/athphane/wishport/internal/ssh"
	"go.yaml.in/yaml/v3"
)

type Provider struct{ Path string }

func (Provider) Name() string { return "wishlist" }

type endpoint struct {
	Name                 string          `yaml:"name"`
	Address              string          `yaml:"address"`
	User                 string          `yaml:"user"`
	Description          string          `yaml:"description"`
	Link                 *inventory.Link `yaml:"link"`
	inventory.SSHOptions `yaml:",inline"`
}
type hint struct {
	Match    string `yaml:"match"`
	Port     string `yaml:"port"`
	endpoint `yaml:",inline"`
}
type config struct {
	Endpoints []endpoint `yaml:"endpoints"`
	Hints     []hint     `yaml:"hints"`
	Listen    string     `yaml:"listen"`
	Port      int64      `yaml:"port"`
	Users     yaml.Node  `yaml:"users"`
	Metrics   yaml.Node  `yaml:"metrics"`
}

func (p Provider) locate() (string, error) {
	candidates := []string{}
	if p.Path != "" {
		path := p.Path
		if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("cannot expand Wishlist configuration path")
			}
			path = filepath.Join(home, path[2:])
		}
		candidates = append(candidates, path)
	} else {
		candidates = append(candidates, ".wishlist")
		root := os.Getenv("XDG_CONFIG_HOME")
		if root == "" {
			home, e := os.UserHomeDir()
			if e != nil {
				return "", fmt.Errorf("find Wishlist home directory")
			}
			root = filepath.Join(home, ".config")
		}
		candidates = append(candidates, filepath.Join(root, "wishlist.yaml"), filepath.Join(root, "wishlist.yml"), filepath.Join(root, "wishlist"))
		if native, e := os.UserConfigDir(); e == nil && native != root {
			candidates = append(candidates, filepath.Join(native, "wishlist.yaml"), filepath.Join(native, "wishlist.yml"), filepath.Join(native, "wishlist"))
		}
	}
	for _, candidate := range candidates {
		info, e := os.Stat(candidate)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", fmt.Errorf("cannot access Wishlist configuration")
		}
		if info.IsDir() {
			found := false
			for _, n := range []string{"config.yaml", "config.yml", "config"} {
				c := filepath.Join(candidate, n)
				i, e := os.Stat(c)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return "", fmt.Errorf("cannot access Wishlist configuration")
				}
				candidate = c
				info = i
				found = true
				break
			}
			if !found {
				continue
			}
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("wishlist configuration must be a regular file")
		}
		return filepath.Abs(candidate)
	}
	if p.Path != "" {
		return "", fmt.Errorf("explicit Wishlist configuration does not exist or has no config file")
	}
	return "", nil
}
func (p Provider) read() (string, *config, error) {
	file, e := p.locate()
	if e != nil || file == "" {
		return file, nil, e
	}
	f, e := os.Open(file)
	if e != nil {
		return file, nil, fmt.Errorf("cannot read Wishlist configuration")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		return file, nil, fmt.Errorf("wishlist configuration exceeds 1 MiB or cannot be read")
	}
	isYAML := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		isYAML = line == "---" || strings.HasSuffix(fields[0], ":") || strings.HasPrefix(line, "{")
		break
	}
	if !isYAML {
		return file, nil, nil
	}
	var c config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if e := dec.Decode(&c); e != nil {
		return file, nil, fmt.Errorf("invalid Wishlist YAML: check field names, types and duplicate keys")
	}
	var extra yaml.Node
	if e := dec.Decode(&extra); e != io.EOF {
		return file, nil, fmt.Errorf("wishlist YAML must contain one document")
	}
	if len(c.Endpoints) > 10000 || len(c.Hints) > 10000 {
		return file, nil, fmt.Errorf("too many Wishlist endpoints or hints")
	}
	return file, &c, nil
}
func (e endpoint) connection() (inventory.Connection, error) {
	if e.Link != nil {
		u, err := url.Parse(e.Link.URL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || strings.IndexFunc(e.Link.Name+e.Link.URL, unicode.IsControl) >= 0 {
			return inventory.Connection{}, fmt.Errorf("invalid link: use an http or https URL")
		}
	}
	c := inventory.Connection{Source: inventory.SourceWishlist, Kind: inventory.KindHost, Name: e.Name, User: e.User, Description: e.Description, Link: e.Link, SSHOptions: &e.SSHOptions}
	if c.Name == "" {
		return c, fmt.Errorf("name is required")
	}
	host, port, err := net.SplitHostPort(e.Address)
	if err != nil {
		return c, fmt.Errorf("address must use host:port (IPv6 uses [address]:port)")
	}
	c.Host = host
	c.Port, err = strconv.Atoi(port)
	if err != nil || c.Port < 1 {
		return c, fmt.Errorf("invalid address port")
	}
	if err := ssh.Validate(c); err != nil {
		return c, err
	}
	return c, nil
}
func (p Provider) List(ctx context.Context, refresh bool) ([]inventory.Connection, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	file, c, e := p.read()
	if e != nil || file == "" {
		return nil, e
	}
	if c == nil {
		rows, e := (sshconfig.Provider{Path: file}).List(ctx, refresh)
		for i := range rows {
			rows[i].ID = "wishlist:" + rows[i].SSHAlias
			rows[i].Source = inventory.SourceWishlist
			rows[i].SSHConfigFile = file
		}
		return rows, e
	}
	rows := []inventory.Connection{}
	counts := map[string]int{}
	for i, e := range c.Endpoints {
		row, err := e.connection()
		if err != nil {
			return rows, fmt.Errorf("wishlist YAML endpoint %d: invalid configuration (%s)", i+1, safeError(err))
		}
		sum := sha256.Sum256([]byte(e.Name + "\x00" + e.Address + "\x00" + e.User))
		base := fmt.Sprintf("wishlist:yaml:%x", sum[:12])
		counts[base]++
		row.ID = base
		if counts[base] > 1 {
			row.ID += fmt.Sprintf(":%d", counts[base])
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func safeError(err error) string {
	s := err.Error()
	if strings.Contains(s, "host") || strings.Contains(s, "user") {
		return "invalid host, address or user"
	}
	return s
}

// ApplyHints applies local hints in order to discovered hosts only. Forge targets are immutable.
func (p Provider) ApplyHints(rows []inventory.Connection) ([]inventory.Connection, error) {
	file, cfg, err := p.read()
	if err != nil || file == "" {
		return rows, err
	}
	if cfg == nil {
		out := append([]inventory.Connection(nil), rows...)
		for i, c := range out {
			if c.Source != inventory.SourceTailscale && c.Source != inventory.SourceMDNS && c.Source != inventory.SourceSRV {
				continue
			}
			c.SSHAlias = c.Name
			c.SSHConfigFile = file
			if ssh.Validate(c) != nil {
				return rows, fmt.Errorf("wishlist SSH hints require a valid discovered host name")
			}
			out[i] = c
		}
		return out, nil
	}
	out := append([]inventory.Connection(nil), rows...)
	for n, h := range cfg.Hints {
		matcher, globErr := glob.Compile(h.Match)
		if globErr != nil || h.Match == "" {
			return rows, fmt.Errorf("wishlist YAML hint %d: invalid match glob", n+1)
		}
		for i, c := range out {
			if c.Source != inventory.SourceTailscale && c.Source != inventory.SourceMDNS && c.Source != inventory.SourceSRV {
				continue
			}
			matched := matcher.Match(c.Name)
			if !matched {
				continue
			}
			if h.Port != "" {
				v, e := strconv.Atoi(h.Port)
				if e != nil || v < 1 || v > 65535 {
					return rows, fmt.Errorf("wishlist YAML hint %d: invalid port", n+1)
				}
				c.Port = v
			}
			if h.User != "" {
				c.User = h.User
			}
			if h.Description != "" {
				c.Description = h.Description
			}
			if h.Link != nil {
				u, e := url.Parse(h.Link.URL)
				if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || strings.IndexFunc(h.Link.Name+h.Link.URL, unicode.IsControl) >= 0 {
					return rows, fmt.Errorf("wishlist hint %d: invalid link", n+1)
				}
				c.Link = h.Link
			}
			opts := inventory.SSHOptions{}
			if c.SSHOptions != nil {
				opts = *c.SSHOptions
			}
			mergeOptions(&opts, h.SSHOptions)
			c.SSHOptions = &opts
			if err := ssh.Validate(c); err != nil {
				return rows, fmt.Errorf("wishlist YAML hint %d: invalid SSH options", n+1)
			}
			out[i] = c
		}
	}
	return out, nil
}
func mergeOptions(a *inventory.SSHOptions, b inventory.SSHOptions) {
	if b.ForwardAgent != nil {
		a.ForwardAgent = b.ForwardAgent
	}
	if b.RequestTTY != nil {
		a.RequestTTY = b.RequestTTY
	}
	if b.RemoteCommand != "" {
		a.RemoteCommand = b.RemoteCommand
	}
	if b.ProxyJump != "" {
		a.ProxyJump = b.ProxyJump
	}
	if b.SendEnv != nil {
		a.SendEnv = append(append([]string(nil), a.SendEnv...), b.SendEnv...)
	}
	if b.SetEnv != nil {
		a.SetEnv = append(append([]string(nil), a.SetEnv...), b.SetEnv...)
	}
	if b.PreferredAuthentications != nil {
		a.PreferredAuthentications = append(append([]string(nil), a.PreferredAuthentications...), b.PreferredAuthentications...)
	}
	if b.IdentityFiles != nil {
		a.IdentityFiles = append(append([]string(nil), a.IdentityFiles...), b.IdentityFiles...)
	}
	if b.ConnectTimeout != 0 {
		a.ConnectTimeout = b.ConnectTimeout
	}
}
