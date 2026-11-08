package ssh

import (
	"context"
	"errors"
	"github.com/athphane/wishport/internal/inventory"
	"strings"
	"testing"
)

type probeRunner struct {
	args []string
	out  string
	err  error
}

func (r *probeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = args
	return []byte(r.out), r.err
}
func TestProbeClassifiesSafely(t *testing.T) {
	c := inventory.Connection{Host: "192.0.2.1", Port: 22, User: "forge"}
	for _, tc := range []struct {
		message string
		want    ProbeStatus
	}{{"debug1: Offering public key: SHA256:test explicit\ndebug1: Authentications that can continue: publickey\ndebug1: No more authentication methods to try.\nPermission denied (publickey).", ProbeRejected}, {"No ED25519 host key is known for 192.0.2.1", ProbeUnknownHost}, {"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!", ProbeChangedHost}, {"Connection timed out", ProbeNetwork}, {"agent refused operation\nPermission denied (publickey).", ProbeLocked}, {"Host key verification failed", ProbeAmbiguous}, {"other", ProbeAmbiguous}} {
		r := &probeRunner{out: tc.message, err: errors.New("exit")}
		got, err := Probe(context.Background(), r, c, "/tmp/my key", "SHA256:test")
		if err != nil || got != tc.want {
			t.Fatalf("%q: %s %v", tc.message, got, err)
		}
		a := strings.Join(r.args, "|")
		for _, v := range []string{"StrictHostKeyChecking=yes", "ControlPath=none", "BatchMode=yes", "IdentitiesOnly=yes", "/tmp/my key", "192.0.2.1"} {
			if !strings.Contains(a, v) {
				t.Fatalf("missing %s", v)
			}
		}
	}
}

func TestSelectedKeyRequiresOfferedFingerprint(t *testing.T) {
	c := inventory.Connection{Host: "192.0.2.1", Port: 22, User: "forge"}
	r := &probeRunner{out: "debug1: Offering public key: SHA256:other\nPermission denied (publickey).", err: errors.New("exit")}
	got, err := ProbeKey(context.Background(), r, c, Key{Identity: "/tmp/id", Fingerprint: "SHA256:selected"})
	if err != nil || got != ProbeAmbiguous {
		t.Fatalf("%s %v", got, err)
	}
	joined := strings.Join(r.args, "|")
	if strings.Contains(joined, "/dev/null") || !strings.Contains(joined, "HostName=192.0.2.1") || !strings.Contains(joined, "CanonicalizeHostname=no") {
		t.Fatalf("arguments %s", joined)
	}
}

func TestProbeRejectsAuthenticatedCommandFailureAndOtherKeySuccess(t *testing.T) {
	c := inventory.Connection{Host: "192.0.2.1", Port: 22, User: "forge"}
	for _, tc := range []struct {
		name, out string
		err       error
		want      ProbeStatus
	}{
		{"other identity", "debug1: Server accepts key: SHA256:other\nAuthenticated to 192.0.2.1 using \"publickey\".\n", nil, ProbeAmbiguous},
		{"selected identity", "debug1: Server accepts key: SHA256:selected\nAuthenticated to 192.0.2.1 using \"publickey\".\n", nil, ProbeAccepted},
		{"remote command failure", "debug1: Server accepts key: SHA256:selected\nAuthenticated to 192.0.2.1 using \"publickey\".\nPermission denied (publickey).", errors.New("exit"), ProbeAmbiguous},
		{"forged failure text", "Permission denied (publickey).", errors.New("exit"), ProbeAmbiguous},
		{"session without authentication", "", nil, ProbeAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &probeRunner{out: tc.out, err: tc.err}
			got, err := ProbeKey(context.Background(), r, c, Key{Identity: "/tmp/id", Fingerprint: "SHA256:selected"})
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
			args := strings.Join(r.args, "|")
			for _, option := range []string{"SessionType=default", "RemoteCommand=none", "ForkAfterAuthentication=no"} {
				if !strings.Contains(args, option) {
					t.Fatal(args)
				}
			}
		})
	}
}
