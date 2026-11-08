// Package ssh constructs OpenSSH commands without invoking a local shell.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/athphane/wishport/internal/inventory"
)

type Command struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}
type OSRunner struct{}

func (OSRunner) Output(ctx context.Context, program string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &boundedOutput{}, &boundedOutput{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("subprocess output exceeded safety limit")
	}
	// Verbose SSH diagnostics are needed even when authentication succeeds.
	// Keep remote stdout out of the probe classifier entirely.
	if program == "ssh" {
		for _, arg := range args {
			if arg == "-v" {
				return stderr.Bytes(), err
			}
		}
	}
	if exit, ok := err.(*exec.ExitError); ok {
		exit.Stderr = stderr.Bytes()
	}
	return stdout.Bytes(), err
}

type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func (c Command) Exec(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, c.Program, c.Args...)
}

// Validate checks values passed to OpenSSH or interpolated into the remote command.
func Validate(c inventory.Connection) error {
	if c.SSHConfigFile != "" {
		if !trustedOptionsSource(c.Source) || c.SSHAlias == "" || !filepath.IsAbs(c.SSHConfigFile) || strings.IndexFunc(c.SSHConfigFile, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid SSH configuration file")
		}
	}
	if err := validateOptions(c); err != nil {
		return err
	}
	if c.Unavailable != "" {
		return fmt.Errorf("connection unavailable: %s", c.Unavailable)
	}
	target := c.Host
	if c.SSHAlias != "" {
		target = c.SSHAlias
	}
	if target == "" {
		return fmt.Errorf("ssh host or alias is required")
	}
	if !validTarget(target) {
		return fmt.Errorf("invalid ssh host or alias %q", target)
	}
	if c.Host != "" && !validTarget(c.Host) {
		return fmt.Errorf("invalid ssh host %q", c.Host)
	}
	if c.User != "" && !validName(c.User) {
		return fmt.Errorf("invalid ssh user %q", c.User)
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid ssh port %d", c.Port)
	}
	if strings.ContainsAny(c.Identity, "\x00\r\n%$") {
		return fmt.Errorf("invalid identity path")
	}
	if strings.IndexFunc(c.Directory, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid remote directory")
	}
	return nil
}
func validName(s string) bool {
	if s == "" || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
func validTarget(s string) bool {
	if strings.Contains(s, ":") {
		return net.ParseIP(s) != nil && !strings.HasPrefix(s, "-")
	}
	return validName(s)
}

// Build preserves aliases as OpenSSH destinations, leaving their configuration to OpenSSH.
// Explicit user, port, and identity values are passed as overrides.
func Build(c inventory.Connection) (Command, error) {
	if err := Validate(c); err != nil {
		return Command{}, err
	}
	args := []string{}
	if c.SSHConfigFile != "" {
		args = append(args, "-F", c.SSHConfigFile)
	}
	if c.SSHConfigFile != "" && c.Source != inventory.SourceWishlist && c.Host != "" {
		args = append(args, "-o", "HostName="+c.Host, "-o", "CanonicalizeHostname=no")
	}
	target := c.SSHAlias
	if target == "" {
		target = c.Host
	}
	if c.Source == inventory.SourceForge {
		if c.SSHAlias != "" || net.ParseIP(c.Host) == nil || c.User == "" || c.Port < 1 {
			return Command{}, fmt.Errorf("forge connection requires its original server IP, port and user")
		}
		args = append(args, "-o", "HostName="+c.Host, "-o", "CanonicalizeHostname=no", "-o", "StrictHostKeyChecking=ask")
	}
	if c.User != "" {
		args = append(args, "-l", c.User)
	}
	if c.Port != 0 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	if c.Identity != "" {
		args = append(args, "-i", c.Identity)
	}
	if c.SSHOptions != nil {
		args = append(args, optionsArgs(c.SSHOptions)...)
	} else if c.Directory != "" {
		args = append(args, "-t")
	}
	args = append(args, "--", target)
	if c.SSHOptions != nil && c.SSHOptions.RemoteCommand != "" {
		args = append(args, c.SSHOptions.RemoteCommand)
	}
	if c.Directory != "" {
		args = append(args, "cd -- "+quote(c.Directory)+" && exec \"${SHELL:-/bin/sh}\" -l")
	}
	return Command{Program: "ssh", Args: args}, nil
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// Resolution keeps the executable command separate from diagnostic values.
type Resolution struct {
	Connection inventory.Connection `json:"connection"`
	Command    Command              `json:"command"`
}

// Resolve asks OpenSSH for an alias's effective configuration while preserving
// the original explicit overrides in Command.
func Resolve(ctx context.Context, runner Runner, c inventory.Connection) (Resolution, error) {
	cmd, err := Build(c)
	if err != nil {
		return Resolution{}, err
	}
	result := Resolution{Connection: c, Command: cmd}
	if c.SSHAlias == "" {
		return result, nil
	}
	if runner == nil {
		return result, fmt.Errorf("ssh runner is required")
	}
	args := []string{"-G"}
	for i, arg := range cmd.Args {
		if arg == "--" {
			args = append(args, cmd.Args[:i]...)
			break
		}
	}
	args = append(args, "--", c.SSHAlias)
	out, err := runner.Output(ctx, "ssh", args...)
	if err != nil {
		return result, fmt.Errorf("resolve ssh alias %q: %w", c.SSHAlias, err)
	}
	var identity string
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "hostname":
			c.Host = value
		case "user":
			c.User = value
		case "port":
			p, e := strconv.Atoi(value)
			if e != nil || p < 1 || p > 65535 {
				return result, fmt.Errorf("invalid resolved ssh port %q", value)
			}
			c.Port = p
		case "identityfile":
			if identity == "" && value != "none" {
				identity = value
			}
		}
	}
	c.Identity = identity
	// IdentityFile values in ssh -G may contain OpenSSH's own percent tokens.
	check := c
	check.Identity = ""
	if err := Validate(check); err != nil {
		return result, fmt.Errorf("invalid resolved ssh configuration: %w", err)
	}
	result.Connection = c
	return result, nil
}
