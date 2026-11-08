package prepare

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/ssh"
	cryptossh "golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	c              inventory.Connection
	keys           []forge.Key
	created        int
	getStatus      string
	verifyCalls    int
	changeOnSecond bool
	err            error
}

func (b *fakeBackend) Verify(_ context.Context, c inventory.Connection) (inventory.Connection, error) {
	b.verifyCalls++
	if b.changeOnSecond && b.verifyCalls == 2 {
		c.Host = "192.0.2.99"
		return c, nil
	}
	if b.c.ID != "" {
		return b.c, b.err
	}
	return c, b.err
}
func (b *fakeBackend) ListKeys(context.Context, string, string) ([]forge.Key, error) {
	return b.keys, b.err
}
func (b *fakeBackend) CreateKey(_ context.Context, _, _, name, user, _ string) error {
	b.created++
	b.keys = append(b.keys, forge.Key{ID: "1", Name: name, User: user, Status: "pending"})
	return b.err
}
func (b *fakeBackend) GetKey(_ context.Context, _, _, _ string) (forge.Key, error) {
	if b.err != nil {
		return forge.Key{}, b.err
	}
	k := b.keys[0]
	k.Status = b.getStatus
	return k, nil
}

type runner struct {
	public []byte
	probes []string
	calls  int
}

func (r *runner) Output(_ context.Context, program string, _ ...string) ([]byte, error) {
	if program == "ssh-add" {
		return r.public, nil
	}
	if program == "ssh" {
		i := r.calls
		r.calls++
		if i >= len(r.probes) {
			i = len(r.probes) - 1
		}
		if r.probes[i] == "ok" {
			pub, _, _, _, _ := cryptossh.ParseAuthorizedKey(r.public)
			return []byte("debug1: Server accepts key: " + cryptossh.FingerprintSHA256(pub) + " explicit\nAuthenticated to 192.0.2.1 using \"publickey\".\n"), nil
		}
		msg := r.probes[i]
		if strings.Contains(msg, "Permission denied (publickey)") {
			pub, _, _, _, _ := cryptossh.ParseAuthorizedKey(r.public)
			msg = "debug1: Offering public key: " + cryptossh.FingerprintSHA256(pub) + " explicit\ndebug1: Authentications that can continue: publickey\ndebug1: No more authentication methods to try.\n" + msg
		}
		return []byte(msg), errors.New("exit")
	}
	return nil, errors.New("unexpected command")
}
func fixture(t *testing.T) (inventory.Connection, *runner, string) {
	t.Helper()
	p, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := cryptossh.NewPublicKey(p)
	if e != nil {
		t.Fatal(e)
	}
	data := cryptossh.MarshalAuthorizedKey(pub)
	path := filepath.Join(t.TempDir(), "id.pub")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	return inventory.Connection{ID: "id", Source: inventory.SourceForge, Kind: inventory.KindForgeSite, Organization: "team", ServerID: "1", SiteID: "2", Host: "192.0.2.1", Port: 22, User: "deploy", Directory: "/home/deploy/app"}, &runner{public: data}, path
}
func TestPrepareInstallAndNoDuplicate(t *testing.T) {
	c, r, path := fixture(t)
	r.probes = []string{"Permission denied (publickey).", "Permission denied (publickey).", "ok"}
	b := &fakeBackend{getStatus: "installed"}
	s := Service{Backend: b, Runner: r, KeyPath: path, Wait: func(context.Context, time.Duration) error { return nil }}
	var consent interaction.Consent
	var progress []string
	out, e := s.Prepare(context.Background(), c, func(_ context.Context, v interaction.Consent) (bool, error) { consent = v; return true, nil }, func(v string) { progress = append(progress, v) })
	if e != nil || !out.Installed || out.Auth != ssh.ProbeAccepted || b.created != 1 || consent.Target.User != "deploy" || consent.Fingerprint == "" || !strings.Contains(strings.Join(progress, ","), "Submitted") {
		t.Fatalf("result=%+v err=%v backend=%+v", out, e, b)
	}
	last := 0
	seen := map[int]bool{}
	for _, stage := range progress {
		step, total := interaction.PreparationStep(stage)
		if step < last || step == 0 || total != 7 {
			t.Fatalf("invalid progress sequence: %v", progress)
		}
		seen[step] = true
		last = step
	}
	if len(seen) != 7 {
		t.Fatalf("installation did not report all workflow stages: %v", progress)
	}
	r.probes = []string{"Permission denied (publickey).", "Permission denied (publickey).", "ok"}
	r.calls = 0
	if _, e = s.Prepare(context.Background(), c, func(context.Context, interaction.Consent) (bool, error) { return true, nil }, nil); e != nil || b.created != 1 {
		t.Fatalf("duplicate: %v writes %d", e, b.created)
	}
}
func TestPrepareNoWriteOnFailure(t *testing.T) {
	for _, kind := range []string{"accepted", "host", "decline", "mismatch", "403", "pending", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			c, r, path := fixture(t)
			r.probes = []string{"Permission denied (publickey).", "Permission denied (publickey)."}
			b := &fakeBackend{getStatus: "pending"}
			s := Service{Backend: b, Runner: r, KeyPath: path, Wait: func(context.Context, time.Duration) error { return context.Canceled }}
			confirm := func(context.Context, interaction.Consent) (bool, error) { return true, nil }
			switch kind {
			case "accepted":
				r.probes = []string{"ok"}
			case "host":
				r.probes = []string{"No ED25519 host key is known"}
			case "decline":
				confirm = func(context.Context, interaction.Consent) (bool, error) { return false, nil }
			case "mismatch":
				b.c = c
				b.c.User = "wrong"
			case "403":
				b.err = errors.New("HTTP 403")
			case "pending", "cancel":
			}
			_, e := s.Prepare(context.Background(), c, confirm, nil)
			if kind == "accepted" {
				if e != nil || b.created != 0 {
					t.Fatalf("%v", e)
				}
				return
			}
			if e == nil {
				t.Fatal("expected error")
			}
			if kind != "pending" && kind != "cancel" && b.created != 0 {
				t.Fatalf("unexpected write %d", b.created)
			}
			if (kind == "pending" || kind == "cancel") && !strings.Contains(e.Error(), "may still finish") {
				t.Fatalf("missing uncertain warning: %v", e)
			}
		})
	}
}

func TestExistingKeyAmbiguityAndPollTimeout(t *testing.T) {
	c, r, path := fixture(t)
	r.probes = []string{"Permission denied (publickey).", "Permission denied (publickey)."}
	k, err := ssh.LoadKey(context.Background(), r, path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(c.User))
	k.Name += "-" + hex.EncodeToString(sum[:8])
	for _, tc := range []struct {
		name   string
		keys   []forge.Key
		status string
	}{
		{"different user", []forge.Key{{ID: "1", Name: k.Name, User: "other", Status: "installed"}}, "installed"},
		{"duplicate", []forge.Key{{ID: "1", Name: k.Name, User: c.User, Status: "installed"}, {ID: "2", Name: k.Name, User: c.User, Status: "installed"}}, "installed"},
		{"pending", []forge.Key{{ID: "1", Name: k.Name, User: c.User, Status: "pending"}}, "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.calls = 0
			b := &fakeBackend{keys: tc.keys, getStatus: tc.status}
			s := Service{Backend: b, Runner: r, KeyPath: path, Wait: func(context.Context, time.Duration) error { return nil }}
			_, err := s.Prepare(context.Background(), c, func(context.Context, interaction.Consent) (bool, error) { return true, nil }, nil)
			if err == nil || b.created != 0 {
				t.Fatalf("err=%v creates=%d", err, b.created)
			}
		})
	}
}

func TestReverifyAfterConsentAndBeforeSubmission(t *testing.T) {
	c, r, path := fixture(t)
	r.probes = []string{"Permission denied (publickey).", "Permission denied (publickey)."}
	b := &fakeBackend{changeOnSecond: true}
	s := Service{Backend: b, Runner: r, KeyPath: path}
	_, err := s.Prepare(context.Background(), c, func(context.Context, interaction.Consent) (bool, error) { return true, nil }, nil)
	if err == nil || b.verifyCalls != 2 || b.created != 0 {
		t.Fatalf("err=%v verifies=%d writes=%d", err, b.verifyCalls, b.created)
	}
}
func TestSubmissionWarningBeforePost(t *testing.T) {
	c, r, path := fixture(t)
	r.probes = []string{"Permission denied (publickey).", "Permission denied (publickey)."}
	b := &fakeBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	s := Service{Backend: b, Runner: r, KeyPath: path}
	_, err := s.Prepare(ctx, c, func(context.Context, interaction.Consent) (bool, error) { return true, nil }, func(message string) {
		if strings.HasPrefix(message, "Submitted or pending") {
			cancel()
		}
	})
	if err == nil || b.created != 0 {
		t.Fatalf("error %v writes %d", err, b.created)
	}
}
