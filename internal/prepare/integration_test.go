package prepare_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/prepare"
	"github.com/athphane/wishport/internal/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// integrationRunner emits the verbose evidence OpenSSH supplies without invoking
// SSH, consulting user configuration, or connecting to a real server.
type integrationRunner struct {
	t           *testing.T
	public      []byte
	fingerprint string
	outcomes    []string
	calls       int
}

func (r *integrationRunner) Output(_ context.Context, program string, args ...string) ([]byte, error) {
	if program == "ssh-add" {
		if !reflect.DeepEqual(args, []string{"-L"}) {
			r.t.Errorf("ssh-add args: %q", args)
		}
		return r.public, nil
	}
	if program != "ssh" {
		r.t.Fatalf("unexpected subprocess %s", program)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"HostName=192.0.2.42", "-l deploy", "-p 2222", "StrictHostKeyChecking=yes", "-- 192.0.2.42 true"} {
		if !strings.Contains(joined, want) {
			r.t.Errorf("SSH target lacks %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "app.example.test") {
		r.t.Error("SSH used the site DNS name")
	}
	if r.calls >= len(r.outcomes) {
		r.t.Fatal("unexpected extra SSH probe")
	}
	outcome := r.outcomes[r.calls]
	r.calls++
	switch outcome {
	case "accepted":
		return []byte("debug1: Server accepts key: ED25519 " + r.fingerprint + " explicit\nAuthenticated to 192.0.2.42 using \"publickey\".\n"), nil
	case "unknown":
		return []byte("No ED25519 host key is known for 192.0.2.42 and you have requested strict checking."), errors.New("exit 255")
	default:
		return []byte("debug1: Offering public key: ED25519 " + r.fingerprint + " explicit\ndebug1: Authentications that can continue: publickey\ndebug1: No more authentication methods to try.\ndeploy@192.0.2.42: Permission denied (publickey).\n"), errors.New("exit 255")
	}
}

type forgeFixture struct {
	t                                     *testing.T
	changed                               bool
	posts, listsAfterPost, gets, verifies int
	payload                               map[string]string
}

func resource(kind, id string, attrs map[string]any) map[string]any {
	return map[string]any{"type": kind, "id": id, "attributes": attrs}
}
func (f *forgeFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer fixture-token" {
		f.t.Error("missing fixture authorization")
	}
	if r.Header.Get("Accept") != "application/vnd.api+json" {
		f.t.Error("missing JSON:API accept header")
	}
	w.Header().Set("Content-Type", "application/vnd.api+json")
	server := resource("servers", "srv42", map[string]any{"name": "Production", "ip_address": "192.0.2.42", "ssh_port": 2222, "is_ready": true, "revoked": false})
	site := resource("sites", "site7", map[string]any{"name": "app.example.test", "user": "deploy", "root_directory": "/home/deploy/app/public"})
	site["relationships"] = map[string]any{"server": map[string]any{"data": map[string]any{"type": "servers", "id": "srv42"}}}
	key := func(status string) map[string]any {
		return resource("keys", "key9", map[string]any{"name": f.payload["name"], "user": "deploy", "status": status})
	}
	var data any
	page := false
	switch r.URL.Path {
	case "/api/orgs":
		data = []any{resource("organizations", "org1", map[string]any{"slug": "acme", "name": "Acme"})}
		page = true
	case "/api/orgs/acme/servers":
		data = []any{server}
		page = true
	case "/api/orgs/acme/sites":
		if r.URL.Query().Get("include") != "server" {
			f.t.Error("site discovery missing server include")
		}
		data = []any{site}
		page = true
	case "/api/orgs/acme/sites/site7":
		f.verifies++
		if f.changed {
			site["attributes"].(map[string]any)["user"] = "other"
		}
		data = site
	case "/api/orgs/acme/servers/srv42":
		data = server
	case "/api/orgs/acme/servers/srv42/ssh-keys":
		if r.Method == http.MethodPost {
			f.posts++
			if err := json.NewDecoder(r.Body).Decode(&f.payload); err != nil {
				f.t.Error(err)
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		page = true
		data = []any{}
		if f.posts > 0 {
			f.listsAfterPost++
			if f.listsAfterPost > 1 {
				data = []any{key("installing")}
			}
		}
	case "/api/orgs/acme/servers/srv42/ssh-keys/key9":
		f.gets++
		status := "installed"
		if f.gets == 1 {
			status = "installing"
		}
		data = key(status)
	default:
		f.t.Errorf("unexpected API endpoint %s %s", r.Method, r.URL)
		http.NotFound(w, r)
		return
	}
	body := map[string]any{"data": data}
	if page {
		body["meta"] = map[string]any{"next_cursor": nil}
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		f.t.Error(err)
	}
}
func integrationSetup(t *testing.T) (*forgeFixture, *forge.Client, inventory.Connection, *integrationRunner, string, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := cryptossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(key)))
	public := []byte(normalized + " fixture comment must not be submitted\n")
	path := filepath.Join(t.TempDir(), "deploy.pub")
	if err = os.WriteFile(path, public, 0600); err != nil {
		t.Fatal(err)
	}
	fixture := &forgeFixture{t: t}
	httpServer := httptest.NewServer(fixture)
	t.Cleanup(httpServer.Close)
	client, err := forge.NewClient(httpServer.URL+"/api", "fixture-token", httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	connections, err := client.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range connections {
		if c.Kind == inventory.KindForgeSite {
			if c.Unavailable != "" || c.Host != "192.0.2.42" || c.Port != 2222 || c.User != "deploy" || c.Directory != "/home/deploy/app/public" || c.ServerID != "srv42" || c.Organization != "acme" {
				t.Fatalf("discovered site: %+v", c)
			}
			return fixture, client, c, &integrationRunner{t: t, public: public, fingerprint: cryptossh.FingerprintSHA256(key)}, path, normalized
		}
	}
	t.Fatal("site not discovered")
	return nil, nil, inventory.Connection{}, nil, "", ""
}
func TestForgePrepareHTTPIntegration(t *testing.T) {
	f, client, c, runner, path, normalized := integrationSetup(t)
	runner.outcomes = []string{"rejected", "rejected", "rejected", "accepted"}
	waits, consents := 0, 0
	service := prepare.Service{Backend: client, Runner: runner, KeyPath: path, Wait: func(ctx context.Context, d time.Duration) error {
		waits++
		if d != 2*time.Second {
			t.Errorf("poll delay %s", d)
		}
		return ctx.Err()
	}}
	consent := func(_ context.Context, v interaction.Consent) (bool, error) {
		consents++
		if !reflect.DeepEqual(v.Target, c) || v.PublicKeyPath != path || v.Fingerprint != runner.fingerprint {
			t.Errorf("consent %+v", v)
		}
		return true, nil
	}
	result, err := service.Prepare(context.Background(), c, consent, nil)
	if err != nil || !result.Installed || result.Auth != ssh.ProbeAccepted {
		t.Fatalf("prepare %+v: %v", result, err)
	}
	parsed, _, _, _, _ := cryptossh.ParseAuthorizedKey(runner.public)
	keyHash := sha256.Sum256(parsed.Marshal())
	userHash := sha256.Sum256([]byte(c.User))
	want := map[string]string{"name": "sshdir-" + hex.EncodeToString(keyHash[:]) + "-" + hex.EncodeToString(userHash[:8]), "user": "deploy", "key": normalized}
	if !reflect.DeepEqual(f.payload, want) || f.posts != 1 || f.listsAfterPost < 2 || f.gets < 3 || waits < 3 || consents != 1 {
		t.Fatalf("payload=%v posts=%d lists=%d gets=%d waits=%d consents=%d", f.payload, f.posts, f.listsAfterPost, f.gets, waits, consents)
	}
	runner.calls = 0
	runner.outcomes = []string{"rejected", "rejected", "accepted"}
	result, err = service.Prepare(context.Background(), c, consent, nil)
	if err != nil || !result.Installed || result.Auth != ssh.ProbeAccepted || f.posts != 1 || consents != 2 {
		t.Fatalf("reuse %+v err=%v posts=%d consents=%d", result, err, f.posts, consents)
	}
}
func TestForgePrepareHTTPRefusesUnsafeTarget(t *testing.T) {
	for _, scenario := range []string{"changed live target", "unknown host"} {
		t.Run(scenario, func(t *testing.T) {
			f, client, c, runner, path, _ := integrationSetup(t)
			runner.outcomes = []string{"rejected"}
			f.changed = scenario == "changed live target"
			if scenario == "unknown host" {
				runner.outcomes = []string{"unknown"}
			}
			consents := 0
			service := prepare.Service{Backend: client, Runner: runner, KeyPath: path, Wait: func(context.Context, time.Duration) error { t.Fatal("unexpected wait"); return nil }}
			_, err := service.Prepare(context.Background(), c, func(context.Context, interaction.Consent) (bool, error) { consents++; return true, nil }, nil)
			if err == nil || consents != 0 || f.posts != 0 {
				t.Fatalf("err=%v consents=%d posts=%d", err, consents, f.posts)
			}
			if scenario == "unknown host" && f.verifies != 0 {
				t.Errorf("unknown host verified Forge %d times", f.verifies)
			}
		})
	}
}
