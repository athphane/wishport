package app

import (
	"context"
	"errors"
	"os/exec"

	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/prepare"
	sshclient "github.com/athphane/wishport/internal/ssh"
)

func (a *App) Prepare(ctx context.Context, c inventory.Connection, keyPath string, confirm interaction.Confirm, progress interaction.Progress) (prepare.Result, error) {
	if c.Source != inventory.SourceForge {
		return prepare.Result{}, errors.New("key preparation is only available for Forge targets")
	}
	if !a.Config.Forge.Enabled {
		return prepare.Result{}, errors.New("forge is disabled in configuration")
	}
	if err := a.initForge(ctx); err != nil {
		return prepare.Result{}, err
	}
	if keyPath == "" {
		keyPath = a.Config.Forge.PublicKey
	}
	svc := prepare.Service{Backend: a.ForgeClient, Runner: a.Runner, KeyPath: keyPath, Disabled: a.Config.Forge.KeyInstall == "disabled"}
	return svc.Prepare(ctx, c, confirm, progress)
}

func (a *App) Connect(ctx context.Context, c inventory.Connection, confirm interaction.Confirm, progress interaction.Progress) (*exec.Cmd, error) {
	if c.Source != inventory.SourceForge || a.Config.Forge.KeyInstall == "disabled" {
		return a.Command(ctx, c)
	}
	result, err := a.Prepare(ctx, c, "", confirm, progress)
	if err != nil {
		var probe *prepare.ProbeError
		if errors.As(err, &probe) && probe.Kind == sshclient.ProbeUnknownHost {
			// Only OpenSSH's own interactive prompt may establish host trust. This
			// attempt performs no Forge write; select again after verifying the key.
			return a.Command(ctx, c)
		}
		return nil, err
	}
	if result.Identity != "" {
		c.Identity = result.Identity
	}
	return a.Command(ctx, c)
}
