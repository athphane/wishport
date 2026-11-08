package wishlist

import (
	"context"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/ssh"
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLAllOptions(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	fixture(t, file, `listen: 127.0.0.1
port: 2223
users: [{name: user, public-keys: [ignored]}]
metrics: {enabled: true}
endpoints:
 - name: production
   address: '[2001:db8::1]:2222'
   user: deploy
   description: A server
   link: {name: Docs, url: 'https://example.com'}
   remote_command: 'printf hello; uptime'
   forward_agent: true
   request_tty: false
   proxy_jump: 'jump@host:22,other@[2001:db8::2]:23'
   identity_files: ['~/.ssh/one', '/tmp/two']
   send_env: [APP_*]
   set_env: ['FOO=two words']
   preferred_authentications: [publickey, password]
   connect_timeout: 1500ms
 - name: production
   address: example.com:22
`)
	rows, err := (Provider{Path: file}).List(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID == rows[1].ID || rows[0].Host != "2001:db8::1" {
		t.Fatalf("wrong rows: %+v", rows)
	}
	cmd, err := ssh.Build(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, "\n")
	for _, want := range []string{"ForwardAgent=yes", "-T", "ProxyJump=jump@host:22,other@[2001:db8::2]:23", "SetEnv=\"FOO=two words\"", "ConnectTimeout=2", "~/.ssh/one", "/tmp/two", "printf hello; uptime"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q from %q", want, cmd.Args)
		}
	}
}
func TestHintsOrderedAndForgeImmutable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	fixture(t, file, `hints:
 - match: '*.local'
   user: first
   port: 2222
   forward_agent: true
 - match: '*.local'
   user: second
   remote_command: uptime
   forward_agent: false
`)
	rows := []inventory.Connection{{ID: "mdns:one", Source: inventory.SourceMDNS, Name: "one.local", Host: "192.0.2.1", Port: 22}, {ID: "forge:one", Source: inventory.SourceForge, Name: "one.local", Host: "192.0.2.2", User: "forge", Port: 22}}
	out, err := (Provider{Path: file}).ApplyHints(rows)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].User != "second" || out[0].Port != 2222 || out[0].ID != rows[0].ID || out[0].Source != rows[0].Source || *out[0].SSHOptions.ForwardAgent || out[1].User != "forge" || rows[0].User != "" {
		t.Fatalf("unexpected hints: %+v", out)
	}
}
func TestYAMLRejectsInvalidWithoutSecrets(t *testing.T) {
	for _, content := range []string{
		"endpoints:\n - name: private-secret\n   address: host:22\n   user: bad;user\n",
		"endpoints:\n - name: private-secret\n   address: host:22\n   address: other:22\n",
		"endpoints:\n - name: private-secret\n   address: host:22\n   proxy_jump: 'host -oProxyCommand=private-secret'\n",
		"endpoints:\n - name: private-secret\n   address: host:22\n   unknown: private-secret\n",
		"endpoints: []\n---\nendpoints: []\n",
	} {
		t.Run(content, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yaml")
			fixture(t, file, content)
			_, err := (Provider{Path: file}).List(context.Background(), false)
			if err == nil || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("unsafe or absent error: %v", err)
			}
		})
	}
}

func TestOpenSSHDiscoveryHints(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	fixture(t, file, "Host *.local\n User configured\n ProxyJump jump.example\n HostName wrong.example\n")
	original := inventory.Connection{ID: "mdns:one", Source: inventory.SourceMDNS, Name: "one.local", Host: "192.0.2.1", Port: 2222, User: "original"}
	out, err := (Provider{Path: file}).ApplyHints([]inventory.Connection{original})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ID != original.ID || out[0].Source != original.Source || out[0].Host != original.Host {
		t.Fatalf("changed discovery: %+v", out[0])
	}
	cmd, err := ssh.Build(out[0])
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"-F " + file, "HostName=192.0.2.1", "-l original", "-p 2222", "-- one.local"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}
