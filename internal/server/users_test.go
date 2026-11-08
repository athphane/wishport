package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

type metadata string

func (m metadata) User() string        { return string(m) }
func (metadata) SessionID() []byte     { return nil }
func (metadata) ClientVersion() []byte { return nil }
func (metadata) ServerVersion() []byte { return nil }
func (metadata) RemoteAddr() net.Addr  { return &net.TCPAddr{} }
func (metadata) LocalAddr() net.Addr   { return &net.TCPAddr{} }

func TestNamedUsersBindKeysToUsernames(t *testing.T) {
	dir := t.TempDir()
	key := signer(t)
	path := filepath.Join(dir, "users.yaml")
	data := fmt.Sprintf("users:\n - name: alice\n   public-keys:\n    - %q\n", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key.PublicKey()))))
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := serverConfig(Config{HostKeyPath: filepath.Join(dir, "host"), AuthorizedUsersPath: path, Executable: "/bin/true"}, &metrics{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.PublicKeyCallback(metadata("alice"), key.PublicKey()); err != nil {
		t.Fatal("authorized user rejected", err)
	}
	if _, err = cfg.PublicKeyCallback(metadata("bob"), key.PublicKey()); err == nil {
		t.Fatal("key accepted for wrong user")
	}
	if _, err = cfg.PublicKeyCallback(metadata("alice"), signer(t).PublicKey()); err == nil {
		t.Fatal("wrong key accepted")
	}
}
