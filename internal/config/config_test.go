package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadText(t *testing.T, s string) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
	return Load(p)
}
func TestDefaultsAndConnections(t *testing.T) {
	c, e := loadText(t, `version: 1
connections:
  - id: example
    name: Example
    host: example.com
    user: deploy
    port: 2222
    directory: /var/www/example
`)
	if e != nil {
		t.Fatal(e)
	}
	if !c.Sources.SSHConfig || !c.Sources.Wishlist || !c.Forge.Enabled || c.Forge.CacheTTL != 168*time.Hour || c.Forge.KeyInstall != "ask" {
		t.Fatalf("incorrect defaults: %+v", c)
	}
	if c.Connections[0].ID != "yaml:example" {
		t.Fatal(c.Connections)
	}
}
func TestStrictErrors(t *testing.T) {
	for _, s := range []string{"version: 2", "unknown: secret-value", "version: 1\nversion: 1", "forge:\n  enabled: true\n  enabled: false", "sources:\n  ssh_config: yes", "forge:\n  cache_ttl: invalid-secret", "forge:\n  key_install: invalid-secret", "connections:\n- id: sample\n  host: '-oProxyCommand=secret-value'", "connections:\n- id: sample\n  host: valid\n  port: 65536", "connections:\n- id: sample\n  host: valid\n- id: sample\n  host: other", "connections:\n- id: sample\n  host: valid\n  unexpected: secret-value", "version: 1\n---\nversion: 1"} {
		t.Run(s, func(t *testing.T) {
			_, e := loadText(t, s)
			if e == nil {
				t.Fatal("accepted invalid configuration")
			}
			if strings.Contains(e.Error(), "secret-value") || strings.Contains(e.Error(), "invalid-secret") {
				t.Fatal("error leaked value")
			}
		})
	}
}
func TestMissingConfigReturnsDefaults(t *testing.T) {
	c, e := Load(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(e, os.ErrNotExist) || !c.Forge.Enabled {
		t.Fatalf("%+v %v", c, e)
	}
}

func TestDefaultPathRenameCompatibility(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	current := filepath.Join(dir, "wishport", "config.yaml")
	legacy := filepath.Join(dir, "sshdir", "config.yaml")
	assertPath := func(want string) {
		t.Helper()
		got, err := DefaultPath()
		if err != nil || got != want {
			t.Fatalf("default path = %q, %v; want %q", got, err, want)
		}
	}
	assertPath(current)
	for _, path := range []string{legacy, current} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("version: 1"), 0600); err != nil {
			t.Fatal(err)
		}
		assertPath(path)
	}
}
