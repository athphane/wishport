package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func signer(t *testing.T) ssh.Signer {
	t.Helper()
	_, k, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	s, e := ssh.NewSignerFromKey(k)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func startTestServer(t *testing.T) (string, ssh.Signer, string) {
	return startTestServerWith(t, "#!/bin/sh\nprintf 'child-started\\n'\nstty size\nexit 7\n")
}
func startTestServerWith(t *testing.T, child string) (string, ssh.Signer, string) {
	t.Helper()
	dir := t.TempDir()
	key := signer(t)
	auth := filepath.Join(dir, "authorized_keys")
	host := filepath.Join(dir, "host_key")
	if err := os.WriteFile(auth, ssh.MarshalAuthorizedKey(key.PublicKey()), 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "child")
	if err := os.WriteFile(script, []byte(child), 0700); err != nil {
		t.Fatal(err)
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeListener(ctx, ln, Config{HostKeyPath: host, AuthorizedKeysPath: auth, Executable: script})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(3 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return ln.Addr().String(), key, host
}

func TestForwardedAgentIsIsolatedAndCleanedUp(t *testing.T) {
	if _, err := exec.LookPath("ssh-add"); err != nil {
		t.Skip("ssh-add required")
	}
	t.Setenv("SSH_AUTH_SOCK", "/unrelated/local/agent")
	addr, key, _ := startTestServerWith(t, "#!/bin/sh\nprintf 'SOCKET=%s\\n' \"$SSH_AUTH_SOCK\"\nssh-add -l\n")
	c, err := client(t, addr, key)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ring := agent.NewKeyring()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = ring.Add(agent.AddedKey{PrivateKey: private, Comment: "forwarded-fixture"}); err != nil {
		t.Fatal(err)
	}
	if err = agent.ForwardToAgent(c, ring); err != nil {
		t.Fatal(err)
	}
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = agent.RequestAgentForwarding(s); err != nil {
		t.Fatal(err)
	}
	if err = s.RequestPty("xterm", 24, 80, nil); err != nil {
		t.Fatal(err)
	}
	out, err := s.CombinedOutput("menu")
	if err != nil || !strings.Contains(string(out), "forwarded-fixture") || strings.Contains(string(out), "/unrelated/local/agent") {
		t.Fatalf("agent forwarding: %v %s", err, out)
	}
	var socket string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "SOCKET=") {
			socket = strings.TrimSpace(strings.TrimPrefix(line, "SOCKET="))
		}
	}
	if socket == "" {
		t.Fatal("agent socket missing")
	}
	c.Close()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent socket was not cleaned up")
		}
		time.Sleep(time.Millisecond)
	}
}
func client(t *testing.T, address string, key ssh.Signer) (*ssh.Client, error) {
	t.Helper()
	return ssh.Dial("tcp", address, &ssh.ClientConfig{User: "user", Auth: []ssh.AuthMethod{ssh.PublicKeys(key)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 3 * time.Second})
}
func TestSessionAndAuthentication(t *testing.T) {
	addr, key, host := startTestServer(t)
	if c, e := client(t, addr, signer(t)); e == nil {
		c.Close()
		t.Fatal("unauthorized key accepted")
	}
	c, e := client(t, addr, key)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	s, e := c.NewSession()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.RequestPty("xterm", 35, 100, ssh.TerminalModes{}); e != nil {
		t.Fatal(e)
	}
	out, e := s.CombinedOutput("menu")
	if e == nil {
		t.Fatal("expected exit status 7")
	}
	exit, ok := e.(*ssh.ExitError)
	if !ok || exit.ExitStatus() != 7 {
		t.Fatalf("exit = %v", e)
	}
	if !strings.Contains(string(out), "child-started") || !strings.Contains(string(out), "35 100") {
		t.Fatalf("unexpected output %q", out)
	}
	info, e := os.Stat(host)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("host key permissions: %v %v", info, e)
	}
}
func TestRejectCommandsAndForwarding(t *testing.T) {
	addr, key, _ := startTestServer(t)
	c, e := client(t, addr, key)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, _, e = c.OpenChannel("direct-tcpip", nil); e == nil {
		t.Fatal("forwarding accepted")
	}
	s, e := c.NewSession()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.RequestPty("xterm", 24, 80, nil); e != nil {
		t.Fatal(e)
	}
	if e = s.Start("sh -c id"); e == nil {
		t.Fatal("arbitrary command accepted")
	}
}
func TestStartupRejectsUnsafeKeys(t *testing.T) {
	dir := t.TempDir()
	k := signer(t)
	auth := filepath.Join(dir, "auth")
	host := filepath.Join(dir, "host")
	data := append([]byte("command=\"id\" "), ssh.MarshalAuthorizedKey(k.PublicKey())...)
	os.WriteFile(auth, data, 0600)
	_, e := serverConfig(Config{HostKeyPath: host, AuthorizedKeysPath: auth, Executable: "/bin/true"}, &metrics{})
	if e == nil {
		t.Fatal("key options accepted")
	}
	if _, e = os.Stat(host); !os.IsNotExist(e) {
		t.Fatal("created host key despite invalid authorized keys")
	}
}
func TestCommandArgs(t *testing.T) {
	for _, tt := range []struct {
		cmd  string
		want bool
	}{{"", true}, {"menu", true}, {"production", true}, {"connect --target forge:123", true}, {"connect --target x;whoami", false}, {"serve", false}} {
		_, got := commandArgs(tt.cmd)
		if got != tt.want {
			t.Errorf("%q: got %v", tt.cmd, got)
		}
	}
}
