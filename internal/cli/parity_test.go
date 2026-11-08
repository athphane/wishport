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

func localCLI(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("PATH", home)
	t.Setenv("FORGE_API_TOKEN", "")
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("sources: {ssh_config: false, wishlist: false}\nforge: {enabled: false}\nconnections:\n - id: one\n   name: demo\n   host: 192.0.2.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return home, path
}

func TestWishlistConfigFlagAndDirectConnection(t *testing.T) {
	home, path := localCLI(t)
	wishlist := filepath.Join(home, "wishlist.yaml")
	if err := os.WriteFile(wishlist, []byte("endpoints:\n - name: external\n   address: 192.0.2.2:2222\n   user: deploy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--wishlist-config", wishlist, "list", "--config", path, "--json"}, os.Stdin, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || !strings.Contains(out.String(), "external") || !strings.Contains(out.String(), "yaml:one") {
		t.Fatal(out.String())
	}
	if err := os.WriteFile(filepath.Join(home, "ssh"), []byte("#!/bin/sh\nprintf 'SSH-DEST:%s\\n' \"$*\"\nexit 17\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, target := range [][]string{{"connect", "--target", "yaml:one"}, {"demo"}} {
		out.Reset()
		stderr.Reset()
		args := append([]string{"--config", path}, target...)
		code := Run(context.Background(), args, os.Stdin, &out, &stderr)
		if code != 17 || !strings.Contains(out.String(), "192.0.2.1") {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
}

func TestServeAndRefreshValidationDoNotStartServices(t *testing.T) {
	home, path := localCLI(t)
	for _, args := range [][]string{
		{"serve", "--authorized-keys", filepath.Join(home, "missing")},
		{"serve", "--json"},
		{"list", "--json", "--refresh-interval", "10ms"},
		{"list", "--json", "--mdns-timeout", "-1s"},
	} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), append([]string{"--config", path}, args...), os.Stdin, &out, &stderr); code == 0 {
			t.Fatal("invalid request accepted", args)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "wishport", "server_ed25519")); !os.IsNotExist(err) {
		t.Fatal("validation created server key")
	}
}
