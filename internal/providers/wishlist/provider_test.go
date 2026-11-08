package wishlist

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/ssh"
)

func fixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryAndOpenSSHResolution(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH not installed")
	}
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			root := filepath.Join(home, "custom config")
			t.Setenv("XDG_CONFIG_HOME", root)
			path := filepath.Join(root, "wishlist")
			if directory {
				path = filepath.Join(path, "config")
			}
			include := filepath.Join(home, ".ssh", "included")
			fixture(t, include, "Host included\n HostName 2001:db8::1\n User secondary\n")
			fixture(t, filepath.Join(home, ".ssh", "config"), "Host demo\n HostName wrong.example\n")
			fixture(t, path, "Include \""+include+"\"\nHost demo\n HostName 192.0.2.4\n User deploy\n Port 2222\n IdentityFile /tmp/test-identity\n ProxyJump jump.example\nHost demo\nHost * !excluded *.example\n ServerAliveInterval 17\n")
			rows, err := (Provider{}).List(context.Background(), false)
			if err != nil || len(rows) != 2 {
				t.Fatalf("rows=%+v err=%v", rows, err)
			}
			var demo inventory.Connection
			for _, row := range rows {
				if row.Source != inventory.SourceWishlist || row.ID != "wishlist:"+row.SSHAlias || row.SSHConfigFile != path {
					t.Fatalf("lost provenance: %+v", row)
				}
				if row.Name == "demo" {
					demo = row
				}
			}
			resolved, err := ssh.Resolve(context.Background(), ssh.OSRunner{}, demo)
			if err != nil {
				t.Fatal(err)
			}
			if c := resolved.Connection; c.Host != "192.0.2.4" || c.User != "deploy" || c.Port != 2222 || c.Identity != "/tmp/test-identity" {
				t.Fatalf("wrong effective options: %+v", c)
			}
			args := resolved.Command.Args
			if len(args) != 4 || args[0] != "-F" || args[1] != path || args[2] != "--" || args[3] != "demo" {
				t.Fatalf("wrong handoff: %q", args)
			}
			out, err := (ssh.OSRunner{}).Output(context.Background(), "ssh", append([]string{"-G"}, args...)...)
			if err != nil || !strings.Contains(string(out), "proxyjump jump.example") || !strings.Contains(string(out), "serveraliveinterval 17") {
				t.Fatalf("SSH options were lost: %v", err)
			}
		})
	}
}

func TestDefaultPathMissingAndUnsupported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	p := Provider{}
	if rows, err := p.List(context.Background(), false); len(rows) != 0 || err != nil {
		t.Fatal(rows, err)
	}
	path := filepath.Join(home, ".config", "wishlist")
	fixture(t, path, "Host default\n HostName 192.0.2.1\n")
	if rows, err := p.List(context.Background(), false); len(rows) != 1 || err != nil {
		t.Fatal(rows, err)
	}
	fixture(t, path, "# YAML\nendpoints:\n - name: secret-fixture\n")
	if _, err := p.List(context.Background(), false); err == nil || !strings.Contains(err.Error(), "YAML") || strings.Contains(err.Error(), "secret-fixture") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.List(ctx, false); err != context.Canceled {
		t.Fatal(err)
	}
}
