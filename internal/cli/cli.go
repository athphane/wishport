package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/app"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/tui"
	"github.com/charmbracelet/x/term"
)

var Version = "0.1.0-dev"

type envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Data          any    `json:"data,omitempty"`
	Error         string `json:"error,omitempty"`
}

func Run(ctx context.Context, args []string, in *os.File, out, errout io.Writer) int {
	if handled, code := referenceCommand(args, out, errout); handled {
		return code
	}
	name := ""
	normalized := []string{}
	// Global options are accepted on either side of the subcommand.
	for i := 0; i < len(args); i++ {
		a := args[i]
		if valueFlag(a) {
			normalized = append(normalized, a)
			i++
			if i < len(args) {
				normalized = append(normalized, args[i])
			}
			continue
		}
		if !strings.HasPrefix(a, "-") && name == "" {
			name = a
			continue
		}
		normalized = append(normalized, a)
	}
	fs := flag.NewFlagSet("wishport", flag.ContinueOnError)
	fs.SetOutput(errout)
	path := fs.String("config", "", "configuration file")
	jsonMode := fs.Bool("json", false, "versioned JSON output")
	refresh := fs.Bool("refresh", false, "refresh inventory")
	target := fs.String("target", "", "stable inventory ID")
	version := fs.Bool("version", false, "print version")
	session := fs.Bool("session", false, "keep login token in memory for one TUI session")
	publicKey := fs.String("public-key", "", "existing .pub file to prepare for a Forge target")
	yes := fs.Bool("yes", false, "approve a safe Forge public-key installation (prepare only)")
	options := registerExtraFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(errout, "Usage: wishport [--config PATH] [--wishlist-config PATH]\n       wishport list|check [--refresh] [--json]\n       wishport command --target ID [--json]\n       wishport connect --target ID\n       wishport <name-or-id>\n       wishport serve [--listen ADDRESS] --authorized-keys PATH\n       wishport prepare --target ID [--public-key PATH] [--yes] [--json]\n       wishport login [--session]\n       wishport forget-token [--json]\n       wishport completion bash|zsh|fish\n       wishport man\n\nWishport — OpenSSH directory with optional discovery and hosting")
		fs.PrintDefaults()
	}
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	emit := func(v any) {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(envelope{SchemaVersion: 1, Data: v})
	}
	fail := func(err error) int {
		if *jsonMode {
			_ = json.NewEncoder(out).Encode(envelope{SchemaVersion: 1, Error: err.Error()})
		}
		fmt.Fprintln(errout, inventory.DisplayText(err.Error()))
		return 1
	}
	if fs.NArg() != 0 {
		return fail(errors.New("unexpected arguments"))
	}
	if *version {
		fmt.Fprintln(out, Version)
		return 0
	}
	if name == "server" || name == "s" {
		name = "serve"
	}
	if name != "" && name != "list" && name != "check" && name != "command" && name != "login" && name != "forget-token" && name != "prepare" && name != "connect" && name != "serve" {
		if *target != "" {
			return fail(errors.New("choose a positional target or --target, not both"))
		}
		*target, name = name, "connect"
	}
	a := app.New(*path)
	if err := options.apply(a, fs); err != nil {
		return fail(err)
	}
	if name == "serve" {
		if *jsonMode {
			return fail(errors.New("serve is interactive; omit --json"))
		}
		if err := options.serve(ctx, a); err != nil {
			return fail(err)
		}
		return 0
	}
	if name == "login" {
		if *jsonMode {
			return fail(errors.New("login is interactive; omit --json"))
		}
		if err := login(ctx, a, *session, in, out, errout); err != nil {
			return fail(err)
		}
		return 0
	}
	if name == "forget-token" {
		if err := forget(ctx); err != nil {
			return fail(err)
		}
		if *jsonMode {
			emit(map[string]bool{"forgotten": true})
		} else {
			fmt.Fprintln(out, "Stored token and matching cache removed. Unset FORGE_API_TOKEN in your shell if set.")
		}
		return 0
	}
	if name == "" {
		if *jsonMode {
			return fail(errors.New("choose list, check or command for JSON output"))
		}
		if !term.IsTerminal(in.Fd()) {
			return fail(errors.New("the directory UI needs a terminal; use wishport list --json"))
		}
		model := tui.New(ctx, a)
		model.SetRefreshInterval(options.refreshInterval)
		_, err := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out)).Run()
		if err != nil {
			return fail(err)
		}
		return 0
	}
	s := a.Load(ctx, *refresh)
	if s.NeedsRefresh {
		s = a.Load(ctx, true)
	}
	switch name {
	case "connect":
		if *jsonMode {
			return fail(errors.New("connect runs OpenSSH; use command --json to inspect the invocation"))
		}
		if *target == "" {
			return fail(errors.New("--target or a positional target is required"))
		}
		c, err := app.FindTarget(s, *target)
		if err != nil {
			return fail(err)
		}
		cmd, err := a.Connect(ctx, c, keyConfirmation(in, errout, false), func(stage string) { fmt.Fprintln(errout, inventory.DisplayText(stage)) })
		if err != nil {
			return fail(err)
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errout
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() > 0 {
				return exit.ExitCode()
			}
			return fail(errors.New("OpenSSH could not run"))
		}
		return 0
	case "prepare":
		if *target == "" {
			return fail(errors.New("--target is required"))
		}
		c, err := app.Find(s, *target)
		if err != nil {
			return fail(err)
		}
		confirm := keyConfirmation(in, errout, *yes)
		result, err := a.Prepare(ctx, c, *publicKey, confirm, func(stage string) { fmt.Fprintln(errout, stage) })
		if err != nil {
			return fail(err)
		}
		emit(result)
	case "list":
		if *jsonMode {
			emit(s)
		} else {
			for _, c := range s.Connections {
				fmt.Fprintf(out, "%s\t%s\t%s\n", inventory.DisplayText(c.ID), inventory.DisplayText(c.Name), c.Source)
			}
		}
	case "check":
		_, err := exec.LookPath("ssh")
		result := struct {
			OK           bool   `json:"ok"`
			ConfigPath   string `json:"config_path"`
			SSHAvailable bool   `json:"ssh_available"`
			Inventory    any    `json:"inventory"`
		}{err == nil && len(s.Warnings) == 0, a.ConfigPath, err == nil, s}
		if *jsonMode {
			emit(result)
		} else {
			fmt.Fprintf(out, "ssh available: %t\nconfig: %s\nentries: %d\n", result.SSHAvailable, a.ConfigPath, len(s.Connections))
		}
		if err != nil {
			fmt.Fprintln(errout, "OpenSSH ssh executable is required")
			return 1
		}
	case "command":
		if *target == "" {
			return fail(errors.New("--target is required"))
		}
		c, err := app.Find(s, *target)
		if err != nil {
			return fail(err)
		}
		r, err := a.Resolve(ctx, c)
		if err != nil {
			return fail(err)
		}
		if *jsonMode {
			emit(r)
		} else {
			emit(r.Command)
		}
	}
	for _, w := range s.Warnings {
		fmt.Fprintf(errout, "%s: %s\n", w.Source, w.Message)
	}
	if len(s.Warnings) > 0 {
		return 1
	}
	return 0
}
