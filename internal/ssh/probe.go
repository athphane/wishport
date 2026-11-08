package ssh

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/athphane/wishport/internal/inventory"
)

type ProbeStatus string

const (
	ProbeAccepted    ProbeStatus = "accepted"
	ProbeRejected    ProbeStatus = "publickey_rejected"
	ProbeUnknownHost ProbeStatus = "unknown_host"
	ProbeChangedHost ProbeStatus = "changed_host"
	ProbeNetwork     ProbeStatus = "network"
	ProbeLocked      ProbeStatus = "locked"
	ProbeAmbiguous   ProbeStatus = "ambiguous"
)

// Probe authenticates only against the explicit IP, user and port.
// HostName, port and user are pinned while trusted user proxy and identity settings
// remain available. A selected key is considered rejected only if OpenSSH
// reports that it actually offered that fingerprint.
func Probe(ctx context.Context, runner Runner, c inventory.Connection, identity string, fingerprint ...string) (ProbeStatus, error) {
	if runner == nil {
		return ProbeAmbiguous, errors.New("SSH runner is required")
	}
	if net.ParseIP(c.Host) == nil || c.User == "" || c.Port < 1 || c.Port > 65535 || c.SSHAlias != "" {
		return ProbeAmbiguous, errors.New("invalid explicit SSH target")
	}
	if err := Validate(c); err != nil {
		return ProbeAmbiguous, err
	}
	if strings.ContainsAny(identity, "\x00\r\n%$") {
		return ProbeAmbiguous, errors.New("invalid SSH identity")
	}
	args := []string{"-v", "-T", "-o", "HostName=" + c.Host, "-o", "CanonicalizeHostname=no", "-o", "BatchMode=yes", "-o", "PreferredAuthentications=publickey", "-o", "PubkeyAuthentication=yes", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=5", "-o", "ConnectionAttempts=1", "-o", "ClearAllForwardings=yes", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "SessionType=default", "-o", "RemoteCommand=none", "-o", "ForkAfterAuthentication=no", "-o", "StdinNull=yes", "-l", c.User, "-p", strconv.Itoa(c.Port)}
	if identity != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", identity)
	}
	args = append(args, "--", c.Host, "true")
	out, err := runner.Output(ctx, "ssh", args...)
	if ctx.Err() != nil {
		return ProbeAmbiguous, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		out = append(out, exit.Stderr...)
	}
	msg := strings.ToLower(string(out))
	lastAccepted := ""
	authenticated := false
	offered := false
	exhausted := false
	publicOnly := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "debug1: Server accepts key:") {
			lastAccepted = line
		}
		if strings.HasPrefix(line, "Authenticated to ") && strings.Contains(line, `using "publickey"`) {
			authenticated = true
		}
		if strings.HasPrefix(line, "debug1: Offering public key:") && len(fingerprint) > 0 && strings.Contains(line, fingerprint[0]) {
			offered = true
		}
		if line == "debug1: No more authentication methods to try." {
			exhausted = true
		}
		if line == "debug1: Authentications that can continue: publickey" {
			publicOnly = true
		}
	}
	if err == nil {
		if !authenticated {
			return ProbeAmbiguous, nil
		}
		if identity != "" && (len(fingerprint) == 0 || fingerprint[0] == "" || !strings.Contains(lastAccepted, fingerprint[0])) {
			return ProbeAmbiguous, nil
		}
		return ProbeAccepted, nil
	}
	// An authenticated session whose command failed is never a missing-key case.
	if authenticated || lastAccepted != "" {
		return ProbeAmbiguous, nil
	}
	switch {
	case strings.Contains(msg, "remote host identification has changed") || strings.Contains(msg, "host key verification failed") && strings.Contains(msg, "offending"):
		return ProbeChangedHost, nil
	case strings.Contains(msg, "no host key is known") || strings.Contains(msg, "no matching host key is known") || strings.Contains(msg, "host key is known") && strings.Contains(msg, "no "):
		return ProbeUnknownHost, nil
	case strings.Contains(msg, "agent refused operation") || strings.Contains(msg, "agent refused signing") || strings.Contains(msg, "sign_and_send_pubkey: signing failed") || strings.Contains(msg, "load key") && strings.Contains(msg, "passphrase"):
		return ProbeLocked, nil
	case strings.Contains(msg, "server accepts key:") && (strings.Contains(msg, "signing failed") || strings.Contains(msg, "agent refused")):
		return ProbeLocked, nil
	case strings.Contains(msg, "permission denied (publickey)"):
		if !exhausted || !publicOnly {
			return ProbeAmbiguous, nil
		}
		if identity != "" {
			if len(fingerprint) == 0 || fingerprint[0] == "" {
				return ProbeAmbiguous, nil
			}
			if !offered {
				return ProbeAmbiguous, nil
			}
		}
		return ProbeRejected, nil
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "connection timed out") || strings.Contains(msg, "no route to host") || strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "connection closed") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "could not resolve hostname"):
		return ProbeNetwork, nil
	default:
		return ProbeAmbiguous, nil
	}
}

// ProbeKey verifies the selected public-key fingerprint was actually offered.
func ProbeKey(ctx context.Context, runner Runner, c inventory.Connection, key Key) (ProbeStatus, error) {
	return Probe(ctx, runner, c, key.Identity, key.Fingerprint)
}
