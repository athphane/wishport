package ssh

import (
	"github.com/athphane/wishport/internal/inventory"
	"os/exec"
	"strings"
	"testing"
)

func TestWishlistOptionsTrustAndTTY(t *testing.T) {
	c := inventory.Connection{Source: inventory.SourceWishlist, Host: "host", SSHOptions: &inventory.SSHOptions{}}
	cmd, err := Build(c)
	if err != nil || !strings.Contains(strings.Join(cmd.Args, " "), "-t") {
		t.Fatalf("shell TTY: %+v %v", cmd, err)
	}
	c.Source = inventory.SourceYAML
	if _, err := Build(c); err == nil {
		t.Fatal("untrusted source accepted SSH options")
	}
	c.Source = inventory.SourceForge
	if _, err := Build(c); err == nil {
		t.Fatal("Forge accepted SSH options")
	}
}
func TestRejectOptionsInjection(t *testing.T) {
	for _, o := range []inventory.SSHOptions{{ProxyJump: "host -oProxyCommand=touch"}, {ProxyJump: "user@-bad"}, {SendEnv: []string{"LANG\nProxyCommand=x"}}, {SetEnv: []string{"FOO=x\nProxyCommand=x"}}, {IdentityFiles: []string{"$(touch x)"}}, {RemoteCommand: "bad\x00"}, {PreferredAuthentications: []string{"publickey -oProxyCommand=x"}}} {
		if _, err := Build(inventory.Connection{Source: inventory.SourceWishlist, Host: "host", SSHOptions: &o}); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
}

func TestEnvironmentNativeOpenSSH(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH not installed")
	}
	o := &inventory.SSHOptions{SetEnv: []string{"FOO=old", "FOO=two words", "PERCENT=100%", "DOLLAR=$literal"}}
	args := append([]string{"-G"}, optionsArgs(o)...)
	args = append(args, "--", "example.com")
	out, err := exec.Command("ssh", args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"setenv FOO=two words\n", "setenv PERCENT=100%\n", "setenv DOLLAR=$literal\n"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("missing %q", want)
		}
	}
}
