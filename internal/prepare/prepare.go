// Package prepare coordinates explicit-consent Forge SSH key installation.
package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/ssh"
)

type Backend interface {
	Verify(context.Context, inventory.Connection) (inventory.Connection, error)
	ListKeys(context.Context, string, string) ([]forge.Key, error)
	CreateKey(context.Context, string, string, string, string, string) error
	GetKey(context.Context, string, string, string) (forge.Key, error)
}
type Service struct {
	Backend  Backend
	Runner   ssh.Runner
	KeyPath  string
	Disabled bool
	Wait     func(context.Context, time.Duration) error
}
type Result struct {
	Auth        ssh.ProbeStatus `json:"auth"`
	Installed   bool            `json:"installed"`
	KeyPath     string          `json:"key_path,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Identity    string          `json:"identity,omitempty"`
}
type ProbeError struct{ Kind ssh.ProbeStatus }

func (e *ProbeError) Error() string {
	switch e.Kind {
	case ssh.ProbeUnknownHost:
		return "SSH host key is unknown; verify it with an interactive SSH connection before retrying"
	case ssh.ProbeChangedHost:
		return "SSH host key changed; resolve the host key mismatch before retrying"
	case ssh.ProbeNetwork:
		return "SSH connection failed; check network connectivity before retrying"
	case ssh.ProbeLocked:
		return "SSH key or agent is locked; unlock it before retrying"
	case ssh.ProbeRejected:
		return "SSH public key was rejected"
	default:
		return "SSH probe was inconclusive; key installation is not safe"
	}
}
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func same(a, b inventory.Connection) bool {
	return a.ID == b.ID && a.Source == b.Source && a.Kind == b.Kind && a.Organization == b.Organization && a.ServerID == b.ServerID && a.SiteID == b.SiteID && a.Host == b.Host && a.Port == b.Port && a.User == b.User && a.Directory == b.Directory && b.Unavailable == ""
}
func emit(p interaction.Progress, s string) {
	if p != nil {
		p(s)
	}
}
func (s *Service) Prepare(ctx context.Context, c inventory.Connection, confirm interaction.Confirm, progress interaction.Progress) (Result, error) {
	result := Result{}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if c.Source != inventory.SourceForge || (c.Kind != inventory.KindForgeServer && c.Kind != inventory.KindForgeSite) || c.Unavailable != "" || c.ID == "" || c.Organization == "" || c.ServerID == "" {
		return result, errors.New("forge target is unavailable or invalid")
	}
	emit(progress, interaction.StageAuthentication)
	status, err := ssh.Probe(ctx, s.Runner, c, "")
	result.Auth = status
	if err != nil {
		return result, err
	}
	if status == ssh.ProbeAccepted {
		return result, nil
	}
	if status != ssh.ProbeRejected {
		return result, &ProbeError{status}
	}
	if s.Disabled {
		return result, errors.New("automatic Forge key installation is disabled")
	}
	if s.Backend == nil {
		return result, errors.New("forge backend is unavailable")
	}
	emit(progress, interaction.StageTarget)
	live, err := s.Backend.Verify(ctx, c)
	if err != nil {
		return result, fmt.Errorf("verify Forge target: %w", err)
	}
	if !same(c, live) {
		return result, errors.New("forge target changed; refresh and select it again")
	}
	emit(progress, interaction.StageLocalKey)
	key, err := ssh.LoadKey(ctx, s.Runner, s.KeyPath)
	if err != nil {
		return result, err
	}
	result.KeyPath = key.Path
	result.Fingerprint = key.Fingerprint
	result.Identity = key.Identity
	status, err = ssh.ProbeKey(ctx, s.Runner, c, key)
	result.Auth = status
	if err != nil {
		return result, err
	}
	if status == ssh.ProbeAccepted {
		return result, nil
	}
	if status != ssh.ProbeRejected {
		return result, &ProbeError{status}
	}
	if confirm == nil {
		return result, errors.New("confirmation is required to install the SSH key")
	}
	emit(progress, interaction.StageApproval)
	yes, err := confirm(ctx, interaction.Consent{Target: c, PublicKeyPath: key.Path, Fingerprint: key.Fingerprint, Message: "Install this public key for the specified Forge user and server?"})
	if err != nil {
		return result, err
	}
	if !yes {
		return result, errors.New("SSH key installation declined")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	emit(progress, interaction.StageRecheck)
	live, err = s.Backend.Verify(ctx, c)
	if err != nil {
		return result, fmt.Errorf("verify Forge target: %w", err)
	}
	if !same(c, live) {
		return result, errors.New("forge target changed; refresh and select it again")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	sum := sha256.Sum256([]byte(c.User))
	scopedName := key.Name + "-" + hex.EncodeToString(sum[:8])
	emit(progress, interaction.StageExistingKeys)
	keys, err := s.Backend.ListKeys(ctx, c.Organization, c.ServerID)
	if err != nil {
		return result, fmt.Errorf("list Forge keys: %w", err)
	}
	existing, err := matching(keys, scopedName, c.User)
	if err != nil {
		return result, err
	}
	submitted := false
	if existing == nil {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		emit(progress, interaction.StageSubmitting)
		if err = ctx.Err(); err != nil {
			return result, err
		}
		// A failed HTTP request can have reached Forge; never issue a second POST here.
		if err = s.Backend.CreateKey(ctx, c.Organization, c.ServerID, scopedName, c.User, key.Public); err != nil {
			return result, fmt.Errorf("submit Forge key (the request may have completed): %w", err)
		}
		submitted = true
		emit(progress, interaction.StageSubmitted)
	}
	emit(progress, interaction.StageInstallation)
	delay := s.Wait
	if delay == nil {
		delay = wait
	}
	for attempt := 0; attempt < 12; attempt++ {
		if attempt > 0 {
			if err = delay(ctx, 2*time.Second); err != nil {
				return result, afterWrite(err, submitted)
			}
		}
		if existing == nil {
			keys, err = s.Backend.ListKeys(ctx, c.Organization, c.ServerID)
			if err != nil {
				return result, afterWrite(fmt.Errorf("list Forge keys: %w", err), submitted)
			}
			existing, err = matching(keys, scopedName, c.User)
			if err != nil {
				return result, afterWrite(err, submitted)
			}
			if existing == nil {
				continue
			}
		}
		current, e := s.Backend.GetKey(ctx, c.Organization, c.ServerID, existing.ID)
		if e != nil {
			return result, afterWrite(fmt.Errorf("read Forge key: %w", e), submitted)
		}
		if current.ID != existing.ID || current.Name != scopedName || current.User != c.User {
			return result, afterWrite(errors.New("forge key identity changed"), submitted)
		}
		switch strings.ToLower(current.Status) {
		case "installed":
			result.Installed = true
			emit(progress, interaction.StageRetry)
			status, err = ssh.ProbeKey(ctx, s.Runner, c, key)
			result.Auth = status
			if err != nil {
				return result, afterWrite(err, submitted)
			}
			if status == ssh.ProbeAccepted {
				return result, nil
			}
			if status != ssh.ProbeRejected {
				return result, afterWrite(&ProbeError{status}, submitted)
			}
		case "failed", "error":
			return result, afterWrite(errors.New("forge reported SSH key installation failed"), submitted)
		}
	}
	return result, afterWrite(errors.New("timed out waiting for Forge SSH key installation or authentication"), submitted)
}
func matching(keys []forge.Key, name, user string) (*forge.Key, error) {
	var found *forge.Key
	for i := range keys {
		if keys[i].Name != name {
			continue
		}
		if keys[i].User != user {
			return nil, errors.New("forge key name already belongs to a different user")
		}
		if found != nil || keys[i].ID == "" {
			return nil, errors.New("ambiguous Forge key records")
		}
		found = &keys[i]
	}
	return found, nil
}
func afterWrite(err error, submitted bool) error {
	if submitted {
		return fmt.Errorf("%w; Forge installation may still finish", err)
	}
	return err
}
