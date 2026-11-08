package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	cryptossh "golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type keyRunner struct {
	agent, derived []byte
	locked         bool
	calls          []string
}

func (r *keyRunner) Output(_ context.Context, p string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, p+" "+strings.Join(args, " "))
	if p == "ssh-add" {
		return r.agent, nil
	}
	if r.locked {
		return []byte("PRIVATE SECRET"), errors.New("PRIVATE SECRET")
	}
	return r.derived, nil
}
func publicFixture(t *testing.T) []byte {
	t.Helper()
	pub, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	key, e := cryptossh.NewPublicKey(pub)
	if e != nil {
		t.Fatal(e)
	}
	return cryptossh.MarshalAuthorizedKey(key)
}
func writePublic(t *testing.T, path string, data []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestLoadKeyAgentAndNormalizedUpload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.pub")
	public := publicFixture(t)
	writePublic(t, path, []byte(strings.TrimSpace(string(public))+" unsafe-comment\n"))
	r := &keyRunner{agent: public}
	k, e := LoadKey(context.Background(), r, path)
	if e != nil {
		t.Fatal(e)
	}
	if k.Identity != path || strings.Contains(k.Public, "comment") || !strings.HasPrefix(k.Fingerprint, "SHA256:") || !strings.HasPrefix(k.Name, "sshdir-") || len(r.calls) != 1 {
		t.Fatalf("bad key: %+v %v", k, r.calls)
	}
}
func TestLoadKeyPrivateSiblingLockedAndMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.pub")
	public := publicFixture(t)
	writePublic(t, path, public)
	r := &keyRunner{derived: public}
	k, e := LoadKey(context.Background(), r, path)
	if e != nil || k.Identity != strings.TrimSuffix(path, ".pub") {
		t.Fatalf("%+v %v", k, e)
	}
	if !strings.Contains(r.calls[1], "-P  -f ") {
		t.Fatal("missing noninteractive empty passphrase")
	}
	r.locked = true
	if _, e = LoadKey(context.Background(), r, path); e == nil || !strings.Contains(e.Error(), "ssh-add") || strings.Contains(e.Error(), "SECRET") {
		t.Fatal(e)
	}
	r.locked = false
	r.derived = publicFixture(t)
	if _, e = LoadKey(context.Background(), r, path); e == nil || !strings.Contains(e.Error(), "match") {
		t.Fatal(e)
	}
}
func TestLoadKeyAmbiguousAndInvalidFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	public := publicFixture(t)
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
		writePublic(t, filepath.Join(home, ".ssh", name), public)
	}
	if _, e := LoadKey(context.Background(), &keyRunner{}, ""); e == nil || !strings.Contains(e.Error(), "choose") {
		t.Fatal(e)
	}
	for _, data := range [][]byte{append(append([]byte{}, public...), public...), []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nSECRET"), append([]byte(`command="evil" `), public...)} {
		path := filepath.Join(t.TempDir(), "key.pub")
		writePublic(t, path, data)
		if _, e := LoadKey(context.Background(), &keyRunner{}, path); e == nil {
			t.Fatal("invalid key accepted")
		}
	}
}
