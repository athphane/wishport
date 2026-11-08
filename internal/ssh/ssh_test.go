package ssh

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/athphane/wishport/internal/inventory"
)

type fakeRunner struct {
	program string
	args    []string
	out     []byte
	err     error
}

func (r *fakeRunner) Output(_ context.Context, p string, args ...string) ([]byte, error) {
	r.program = p
	r.args = args
	return r.out, r.err
}
func TestBuild(t *testing.T) {
	c := inventory.Connection{Host: "2001:db8::1", User: "alice", Port: 2222, Identity: "/tmp/my key", Directory: "/home/alice/it's here; $HOME"}
	got, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-l", "alice", "-p", "2222", "-i", "/tmp/my key", "-t", "--", "2001:db8::1", `cd -- '/home/alice/it'\''s here; $HOME' && exec "${SHELL:-/bin/sh}" -l`}
	if got.Program != "ssh" || !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("got %#v", got)
	}
	if cmd := got.Exec(context.Background()); cmd.Path == "" || !reflect.DeepEqual(cmd.Args[1:], want) {
		t.Fatalf("exec: %#v", cmd)
	}
}
func TestResolveAlias(t *testing.T) {
	r := &fakeRunner{out: []byte("hostname server.example\nuser configuser\nport 22\nidentityfile %d/.ssh/id_ed25519\nidentityfile %d/.ssh/id_rsa\n")}
	c := inventory.Connection{SSHAlias: "prod", User: "override", Identity: "/tmp/key file"}
	got, err := Resolve(context.Background(), r, c)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-G", "-l", "override", "-i", "/tmp/key file", "--", "prod"}
	if r.program != "ssh" || !reflect.DeepEqual(r.args, want) {
		t.Fatalf("invocation %s %q", r.program, r.args)
	}
	if got.Connection.Host != "server.example" || got.Connection.Port != 22 || got.Connection.Identity != "%d/.ssh/id_ed25519" {
		t.Fatalf("diagnostics: %#v", got.Connection)
	}
	if !reflect.DeepEqual(got.Command.Args, want[1:]) {
		t.Fatalf("command: %#v", got.Command)
	}
}
func TestValidate(t *testing.T) {
	for _, c := range []inventory.Connection{{Host: "-oProxyCommand=bad"}, {Host: "bad;command"}, {Host: "%h"}, {Host: "$HOST"}, {Host: "good", User: "-x"}, {Host: "good", Port: 65536}, {Host: "good", Identity: "%d/key"}, {Host: "good", Directory: "a\nb"}} {
		if Validate(c) == nil {
			t.Errorf("accepted %#v", c)
		}
	}
}
func TestResolveError(t *testing.T) {
	for _, err := range []error{context.Canceled, errors.New("exit status 255")} {
		_, got := Resolve(context.Background(), &fakeRunner{err: err}, inventory.Connection{SSHAlias: "prod"})
		if !errors.Is(got, err) {
			t.Fatalf("got %v", got)
		}
	}
}

func TestWishlistConfigPassedToResolveAndConnect(t *testing.T) {
	c := inventory.Connection{Source: inventory.SourceWishlist, SSHAlias: "prod", SSHConfigFile: "/tmp/config with spaces", User: "override"}
	r := &fakeRunner{out: []byte("hostname 192.0.2.1\nuser override\nport 2222\n")}
	got, err := Resolve(context.Background(), r, c)
	want := []string{"-G", "-F", c.SSHConfigFile, "-l", "override", "--", "prod"}
	if err != nil || !reflect.DeepEqual(r.args, want) || !reflect.DeepEqual(got.Command.Args, want[1:]) {
		t.Fatalf("resolve=%+v args=%q err=%v", got, r.args, err)
	}
	for _, path := range []string{"relative", "none", "/tmp/bad\nconfig"} {
		c.SSHConfigFile = path
		if _, err := Build(c); err == nil {
			t.Fatalf("accepted config path %q", path)
		}
	}
	c.SSHConfigFile = "/tmp/config"
	c.Source = inventory.SourceForge
	if _, err := Build(c); err == nil {
		t.Fatal("Forge must not override its SSH configuration file")
	}
}
