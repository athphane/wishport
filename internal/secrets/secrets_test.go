package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type call struct {
	name  string
	args  []string
	input []byte
}
type fakeRunner struct {
	calls []call
	out   []byte
	err   error
}

func (f *fakeRunner) Run(_ context.Context, n string, a []string, b []byte) ([]byte, error) {
	f.calls = append(f.calls, call{n, a, b})
	return f.out, f.err
}
func TestEnvironmentPriority(t *testing.T) {
	f := &fakeRunner{}
	m := Manager{Runner: f, OS: "linux", Getenv: func(string) string { return "env-token" }}
	token, e := m.Token(context.Background())
	if e != nil || token != "env-token" || len(f.calls) != 0 {
		t.Fatal("environment priority failed")
	}
}
func TestKeyringAndSafeFailures(t *testing.T) {
	f := &fakeRunner{out: []byte("stored-token\n")}
	m := Manager{Runner: f, OS: "linux", Getenv: func(string) string { return "" }}
	token, e := m.Token(context.Background())
	if e != nil || token != "stored-token" {
		t.Fatal(e)
	}
	if e = m.Store(context.Background(), "SECRET"); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(strings.Join(f.calls[1].args, " "), "SECRET") || string(f.calls[1].input) != "SECRET" {
		t.Fatal("token must be provided only on stdin")
	}
	f.err = errors.New("SECRET from stderr")
	if _, e = m.Token(context.Background()); e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatal("leaked subprocess error")
	}
	if e = m.Forget(context.Background()); e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatal("leaked subprocess error")
	}
}
func TestNonLinuxUsesEnvironmentOnly(t *testing.T) {
	m := Manager{OS: "darwin", Getenv: func(string) string { return "" }}
	if _, e := m.Token(context.Background()); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}

type namespaceRunner struct {
	calls  []call
	tokens map[string]string
}

func (f *namespaceRunner) Run(_ context.Context, name string, args []string, input []byte) ([]byte, error) {
	f.calls = append(f.calls, call{name, args, input})
	for i, arg := range args {
		if arg == "service" && i+1 < len(args) {
			namespace := args[i+1]
			switch args[0] {
			case "lookup":
				return []byte(f.tokens[namespace]), nil
			case "store":
				f.tokens[namespace] = string(input)
			case "clear":
				delete(f.tokens, namespace)
			}
			return nil, nil
		}
	}
	return nil, errors.New("missing namespace")
}

func TestTokenRenameCompatibility(t *testing.T) {
	f := &namespaceRunner{tokens: map[string]string{"sshdir": "legacy-token"}}
	m := Manager{Runner: f, OS: "linux", Getenv: func(string) string { return "" }}
	if got, err := m.Token(context.Background()); err != nil || got != "legacy-token" {
		t.Fatalf("legacy lookup: %v", err)
	}
	if err := m.Store(context.Background(), "current-token"); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Token(context.Background()); err != nil || got != "current-token" {
		t.Fatalf("current precedence: %v", err)
	}
	m.Namespace = "sshdir"
	if got, err := m.Token(context.Background()); err != nil || got != "legacy-token" {
		t.Fatalf("namespace lookup: %v", err)
	}
	m.Namespace = ""
	if err := m.Forget(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.tokens) != 0 {
		t.Fatal("credentials remain after forgetting")
	}
}
