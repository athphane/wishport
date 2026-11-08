// Package secrets accesses the Linux Secret Service without placing tokens in arguments.
package secrets

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("system keyring is unavailable; set FORGE_API_TOKEN instead")
var ErrNotFound = errors.New("forge token is not configured")

type Runner interface {
	Run(context.Context, string, []string, []byte) ([]byte, error)
}
type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	return cmd.Output()
}

type Manager struct {
	Runner Runner
	Getenv func(string) string
	OS     string
	// Namespace selects a specific credential store for cleanup. Empty uses
	// Wishport first, then the original development namespace for reads.
	Namespace string
}

func (m Manager) runner() Runner {
	if m.Runner != nil {
		return m.Runner
	}
	return commandRunner{}
}
func (m Manager) supported() bool {
	platform := m.OS
	if platform == "" {
		platform = runtime.GOOS
	}
	return platform == "linux"
}
func (m Manager) Token(ctx context.Context) (string, error) {
	getenv := m.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if token := strings.TrimSpace(getenv("FORGE_API_TOKEN")); token != "" {
		return token, nil
	}
	if !m.supported() {
		return "", ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	namespaces := []string{"wishport", "sshdir"}
	if m.Namespace != "" {
		namespaces = []string{m.Namespace}
	}
	var failed bool
	for _, namespace := range namespaces {
		out, err := m.runner().Run(ctx, "secret-tool", []string{"lookup", "service", namespace, "account", "forge"}, nil)
		if err != nil {
			// secret-tool reports an absent item with exit 1 and no diagnostic.
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || len(exitErr.Stderr) != 0 {
				failed = true
			}
			continue
		}
		if token := strings.TrimSpace(string(out)); token != "" {
			return token, nil
		}
	}
	if failed {
		return "", ErrUnavailable
	}
	return "", ErrNotFound
}
func (m Manager) Store(ctx context.Context, token string) error {
	if !m.supported() {
		return ErrUnavailable
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n\x00") {
		return errors.New("invalid Forge token")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, e := m.runner().Run(ctx, "secret-tool", []string{"store", "--label=wishport Forge API token", "service", "wishport", "account", "forge"}, []byte(token))
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func (m Manager) Forget(ctx context.Context) error {
	if !m.supported() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	namespaces := []string{"wishport", "sshdir"}
	if m.Namespace != "" {
		namespaces = []string{m.Namespace}
	}
	var failed bool
	for _, namespace := range namespaces {
		if _, err := m.runner().Run(ctx, "secret-tool", []string{"clear", "service", namespace, "account", "forge"}, nil); err != nil {
			failed = true
		}
	}
	if failed {
		return ErrUnavailable
	}
	return nil
}
func Token(ctx context.Context) (string, error)     { return (Manager{}).Token(ctx) }
func Store(ctx context.Context, token string) error { return (Manager{}).Store(ctx, token) }
func Forget(ctx context.Context) error              { return (Manager{}).Forget(ctx) }
