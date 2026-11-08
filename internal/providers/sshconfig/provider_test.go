package sshconfig

import (
	"context"
	"github.com/athphane/wishport/internal/ssh"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIncludesAliasesAndCycles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ssh")
	if e := os.MkdirAll(filepath.Join(dir, "parts"), 0700); e != nil {
		t.Fatal(e)
	}
	write := func(p, s string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, p), []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("config", `# comment
Host alpha beta *.wild !excluded
 Include parts/*.conf
Host="quoted" # trailing comment
Host alpha
`)
	write("parts/a.conf", `Host included
Include config
`)
	write("parts/b.conf", `Host final
`)
	got, e := (Provider{}).List(context.Background(), false)
	if e == nil {
		t.Fatal("expected cycle warning")
	}
	want := []string{"alpha", "beta", "included", "final", "quoted"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, a := range want {
		if got[i].SSHAlias != a {
			t.Fatalf("%d: %+v", i, got[i])
		}
	}
}
func TestMissingAndCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, e := (Provider{}).List(context.Background(), false)
	if e != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = (Provider{}).List(ctx, false); e == nil {
		t.Fatal("expected cancellation")
	}
}
func TestDirective(t *testing.T) {
	key, args, e := directive(`Host = "hello" alias # comment`)
	if e != nil || key != "Host" || len(args) != 2 || args[0] != "hello" {
		t.Fatalf("%s %v %v", key, args, e)
	}
}

func TestSearchHintsRetainAliasAuthority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ssh")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	config := `Host * !excluded
 User defaultuser
Host app excluded
 HostName 192.0.2.42
 Include hints.conf
 Port 2222
 IdentityFile ~/.ssh/other
Host app
 User deploy
Match exec "must never execute"
 HostName should-not-be-a-hint.example
`
	if e := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "hints.conf"), []byte("User includeuser\n"), 0600); e != nil {
		t.Fatal(e)
	}
	rows, e := (Provider{}).List(context.Background(), false)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	terms := strings.Join(rows[0].SearchTerms, " ")
	for _, want := range []string{"192.0.2.42", "deploy", "defaultuser", "includeuser"} {
		if !strings.Contains(terms, want) {
			t.Fatalf("missing search hint %s in %q", want, terms)
		}
	}
	if strings.Contains(terms, "should-not") {
		t.Fatal("Match hint was attributed to Host")
	}
	if strings.Contains(strings.Join(rows[1].SearchTerms, " "), "defaultuser") {
		t.Fatal("negated wildcard hint attributed")
	}
	for _, r := range rows {
		if r.Host != "" || r.User != "" || r.Port != 0 || r.Identity != "" {
			t.Fatalf("search hints changed execution defaults: %+v", r)
		}
		cmd, e := ssh.Build(r)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(cmd.Args, []string{"--", r.SSHAlias}) {
			t.Fatalf("alias execution changed: %+v", cmd)
		}
	}
}
func TestInvalidAliasesAreSkippedWithPartialWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if e := os.WriteFile(path, []byte("Host safe bad;alias $bad user@host -option\n"), 0600); e != nil {
		t.Fatal(e)
	}
	rows, e := (Provider{Path: path}).List(context.Background(), false)
	if e == nil || len(rows) != 1 || rows[0].SSHAlias != "safe" {
		t.Fatalf("%+v %v", rows, e)
	}
	if strings.Contains(e.Error(), "bad;alias") {
		t.Fatal("warning leaked alias value")
	}
}
