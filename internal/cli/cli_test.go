package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONInventoryAndCommand(t *testing.T) {
	t.Setenv("FORGE_API_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nsources:\n  ssh_config: false\nconnections:\n  - id: test\n    name: test host\n    host: 192.0.2.1\n    user: deploy\n    directory: /srv/a'b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list", "--json", "--config", path}, {"--config", path, "command", "--target", "yaml:test", "--json"}} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, os.Stdin, &out, &stderr); code != 0 {
			t.Fatalf("code=%d err=%s", code, stderr.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc["schema_version"] != float64(1) {
			t.Fatal(doc)
		}
	}
}

func TestMissingDefaultAndMalformedConfig(t *testing.T) {
	t.Setenv("FORGE_API_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	// Keep this test independent of the machine's system SSH configuration.
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"list", "--json"}, os.Stdin, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host surviving\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "bad.yaml")
	_ = os.WriteFile(path, []byte("version: 1\ntoken: SENSITIVE\n"), 0600)
	out.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"list", "--json", "--config", path}, os.Stdin, &out, &stderr); code != 1 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "ssh:surviving") || strings.Contains(out.String()+stderr.String(), "SENSITIVE") {
		t.Fatal(out.String(), stderr.String())
	}
}
