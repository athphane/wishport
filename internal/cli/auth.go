package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/app"
	"github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/secrets"
	"github.com/athphane/wishport/internal/tui"
	"github.com/charmbracelet/x/term"
)

func login(ctx context.Context, a *app.App, session bool, in *os.File, out, errout io.Writer) error {
	if !term.IsTerminal(in.Fd()) {
		return errors.New("login requires a terminal; for automation supply FORGE_API_TOKEN through your secret manager")
	}
	fmt.Fprintln(errout, "Create a Forge API token with organization:view and server:view; add server:create-keys only for optional key preparation.")
	fmt.Fprint(errout, "Forge API token (hidden): ")
	bytes, err := term.ReadPassword(in.Fd())
	fmt.Fprintln(errout)
	if err != nil {
		return errors.New("could not read token")
	}
	token := strings.TrimSpace(string(bytes))
	clear(bytes)
	if token == "" || strings.ContainsAny(token, "\x00\r\n") {
		return errors.New("invalid token")
	}
	if !session {
		if err := secrets.Store(ctx, token); err == nil {
			fmt.Fprintln(out, "Token saved in the desktop keyring.")
			return nil
		}
		fmt.Fprint(errout, "Desktop keyring unavailable. Use token for this directory session only? [y/N] ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			return errors.New("token was not saved")
		}
	}
	a.Token = func(context.Context) (string, error) { return token, nil }
	_, err = tea.NewProgram(tui.New(ctx, a), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}

func forget(ctx context.Context) error {
	// Look up the stored token independently of the environment override, so both
	// accounts' matching snapshots are removed without exposing either token.
	var errs []error
	tokens := []string{strings.TrimSpace(os.Getenv("FORGE_API_TOKEN"))}
	for _, namespace := range []string{"wishport", "sshdir"} {
		manager := secrets.Manager{Namespace: namespace, Getenv: func(string) string { return "" }}
		stored, lookupErr := manager.Token(ctx)
		if stored != "" {
			tokens = append(tokens, stored)
			if err := manager.Forget(ctx); err != nil {
				errs = append(errs, err)
			}
		} else if lookupErr != nil && !errors.Is(lookupErr, secrets.ErrNotFound) {
			errs = append(errs, errors.New("desktop token could not be checked or cleared"))
		}
	}
	for _, token := range tokens {
		if token == "" {
			continue
		}
		cache, err := forge.NewCache(forge.Partition(forge.DefaultBaseURL, token))
		if err == nil {
			err = cache.Forget()
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
