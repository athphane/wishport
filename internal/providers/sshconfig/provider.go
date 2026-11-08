// Package sshconfig enumerates literal aliases without resolving or executing SSH.
package sshconfig

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/ssh"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type Provider struct{ Path string }

func (Provider) Name() string { return "ssh_config" }
func (p Provider) List(ctx context.Context, _ bool) ([]inventory.Connection, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, fmt.Errorf("find SSH configuration directory: %w", e)
	}
	path := p.Path
	if path == "" {
		path = filepath.Join(home, ".ssh", "config")
	}
	aliases := map[string]bool{}
	active := map[string]bool{}
	var result []inventory.Connection
	var issues []error
	files := 0
	// These are raw search hints, not an evaluation of OpenSSH precedence or Match.
	type hint struct {
		patterns []string
		value    string
	}
	var hints []hint
	patterns := []string{"*"}
	var visit func(string, int, bool)
	visit = func(path string, depth int, root bool) {
		if e := ctx.Err(); e != nil {
			issues = append(issues, e)
			return
		}
		if depth > 32 || files >= 4096 {
			issues = append(issues, fmt.Errorf("SSH Include limit reached"))
			return
		}
		abs, e := filepath.Abs(path)
		if e != nil {
			issues = append(issues, fmt.Errorf("invalid SSH Include path"))
			return
		}
		real, e := filepath.EvalSymlinks(abs)
		if e == nil {
			abs = real
		}
		if active[abs] {
			issues = append(issues, fmt.Errorf("SSH Include cycle detected"))
			return
		}
		f, e := os.Open(path)
		if e != nil {
			if root && os.IsNotExist(e) {
				return
			}
			issues = append(issues, fmt.Errorf("cannot read SSH configuration include"))
			return
		}
		defer f.Close()
		files++
		active[abs] = true
		defer delete(active, abs)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if ctx.Err() != nil {
				issues = append(issues, ctx.Err())
				return
			}
			key, args, e := directive(scanner.Text())
			if e != nil {
				issues = append(issues, fmt.Errorf("SSH configuration line %d: invalid quoting", line))
				continue
			}
			switch strings.ToLower(key) {
			case "host":
				patterns = append([]string(nil), args...)
				for _, alias := range args {
					if alias == "" || strings.ContainsAny(alias, "*?![]") || strings.IndexFunc(alias, unicode.IsSpace) >= 0 || strings.IndexFunc(alias, unicode.IsControl) >= 0 {
						continue
					}
					if ssh.Validate(inventory.Connection{SSHAlias: alias}) != nil {
						issues = append(issues, fmt.Errorf("SSH configuration line %d: skipped invalid literal alias; use a simple SSH host alias", line))
						continue
					}
					if aliases[alias] {
						continue
					}
					aliases[alias] = true
					result = append(result, inventory.Connection{ID: "ssh:" + alias, Source: inventory.SourceSSHConfig, Kind: inventory.KindHost, Name: alias, SSHAlias: alias})
				}
			case "match":
				// Match can execute commands and depends on runtime state. Do not evaluate it.
				patterns = nil
			case "hostname", "user":
				if len(args) == 1 && len(patterns) > 0 && !strings.ContainsAny(args[0], "%$") {
					c := inventory.Connection{SSHAlias: "hint"}
					if strings.EqualFold(key, "hostname") {
						c.Host = args[0]
					} else {
						c.User = args[0]
					}
					if ssh.Validate(c) == nil {
						hints = append(hints, hint{append([]string(nil), patterns...), args[0]})
					}
				}
			case "include":
				for _, pattern := range args {
					if pattern == "~" {
						pattern = home
					} else if strings.HasPrefix(pattern, "~/") {
						pattern = filepath.Join(home, pattern[2:])
					} else if strings.HasPrefix(pattern, "~") {
						issues = append(issues, fmt.Errorf("SSH Include uses unsupported home expansion"))
						continue
					}
					if !filepath.IsAbs(pattern) {
						pattern = filepath.Join(home, ".ssh", pattern)
					}
					matches, e := filepath.Glob(pattern)
					if e != nil {
						issues = append(issues, fmt.Errorf("invalid SSH Include glob"))
						continue
					}
					for _, match := range matches {
						visit(match, depth+1, false)
					}
				}
			}
		}
		if scanner.Err() != nil {
			issues = append(issues, fmt.Errorf("cannot read SSH configuration contents"))
		}
	}
	visit(path, 0, true)
	for i := range result {
		seen := map[string]bool{}
		for _, h := range hints {
			if hintMatches(h.patterns, result[i].SSHAlias) && !seen[h.value] {
				result[i].SearchTerms = append(result[i].SearchTerms, h.value)
				seen[h.value] = true
			}
		}
	}
	return result, errors.Join(issues...)
}

// directive handles OpenSSH's optional equals separator and double quotes.
func directive(s string) (string, []string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s[0] == '#' {
		return "", nil, nil
	}
	i := 0
	for i < len(s) && s[i] != '=' && !unicode.IsSpace(rune(s[i])) {
		i++
	}
	key := s[:i]
	s = strings.TrimSpace(s[i:])
	if strings.HasPrefix(s, "=") {
		s = strings.TrimSpace(s[1:])
	}
	var args []string
	var b strings.Builder
	quoted, escape, started := false, false, false
	flush := func() {
		if started {
			args = append(args, b.String())
			b.Reset()
			started = false
		}
	}
	for _, r := range s {
		if escape {
			b.WriteRune(r)
			started = true
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			started = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			started = true
			continue
		}
		if r == '#' && !quoted {
			break
		}
		if unicode.IsSpace(r) && !quoted {
			flush()
			continue
		}
		b.WriteRune(r)
		started = true
	}
	if quoted || escape {
		return key, nil, fmt.Errorf("invalid quoting")
	}
	flush()
	return key, args, nil
}

// hintMatches deliberately implements only Host pattern matching for search hints.
func hintMatches(patterns []string, alias string) bool {
	matched := false
	for _, pattern := range patterns {
		negative := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		ok, e := filepath.Match(strings.ToLower(pattern), strings.ToLower(alias))
		if e != nil || !ok {
			continue
		}
		if negative {
			return false
		}
		matched = true
	}
	return matched
}
