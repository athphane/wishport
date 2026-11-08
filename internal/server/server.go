// Package server provides an opt-in SSH frontend to the local directory.
package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

const DefaultListen = "127.0.0.1:2222"
const maxConnections = 32
const handshakeTimeout = 10 * time.Second

// Config identifies the explicitly trusted keys and the local program to run.
type Config struct {
	Listen              string
	HostKeyPath         string
	AuthorizedKeysPath  string
	AuthorizedUsersPath string
	Executable          string
	ConfigPath          string
	Args                []string
	MetricsListen       string
}

// Serve listens until ctx is cancelled. It creates a host key only as part of startup.
func Serve(ctx context.Context, cfg Config) error {
	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	return ServeListener(ctx, ln, cfg)
}

// ServeListener serves on an already bound listener, which it closes on return.
func ServeListener(ctx context.Context, ln net.Listener, cfg Config) error {
	defer ln.Close()
	stats := &metrics{}
	sc, err := serverConfig(cfg, stats)
	if err != nil {
		return err
	}
	if cfg.MetricsListen != "" {
		host, _, e := net.SplitHostPort(cfg.MetricsListen)
		if e != nil {
			return e
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("metrics listener must be loopback")
		}
		ml, e := net.Listen("tcp", cfg.MetricsListen)
		if e != nil {
			return e
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "wishport_connections_total %d\nwishport_auth_success_total %d\nwishport_auth_failure_total %d\nwishport_sessions_total %d\n", stats.connections.Load(), stats.authSuccess.Load(), stats.authFailure.Load(), stats.sessions.Load())
		})
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go srv.Serve(ml)
		defer srv.Close()
	}
	return serve(ctx, ln, cfg, sc, stats)
}

type metrics struct{ connections, authSuccess, authFailure, sessions atomic.Uint64 }

func serverConfig(cfg Config, stats *metrics) (*ssh.ServerConfig, error) {
	if cfg.HostKeyPath == "" || cfg.AuthorizedKeysPath == "" && cfg.AuthorizedUsersPath == "" || cfg.Executable == "" {
		return nil, errors.New("host key, authorized keys, and executable are required")
	}
	var data []byte
	var err error
	if cfg.AuthorizedKeysPath != "" {
		data, err = os.ReadFile(cfg.AuthorizedKeysPath)
		if err != nil {
			return nil, fmt.Errorf("read authorized keys: %w", err)
		}
	}
	keys := map[string]bool{}
	for len(strings.TrimSpace(string(data))) > 0 {
		key, _, options, rest, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse authorized keys: %w", err)
		}
		// OpenSSH key options are not interpreted here; accepting them would silently
		// discard restrictions such as from= and command=.
		if len(options) != 0 {
			return nil, errors.New("authorized key options are unsupported")
		}
		if _, certificate := key.(*ssh.Certificate); certificate {
			return nil, errors.New("SSH certificates are unsupported; authorize raw public keys")
		}
		keys[string(key.Marshal())] = true
		data = rest
	}
	users := map[string]map[string]bool{}
	if cfg.AuthorizedUsersPath != "" {
		users, err = authorizedUsers(cfg.AuthorizedUsersPath)
		if err != nil {
			return nil, err
		}
	}
	if len(keys) == 0 && len(users) == 0 {
		return nil, errors.New("authorized keys file contains no keys")
	}
	signer, err := hostKey(cfg.HostKeyPath)
	if err != nil {
		return nil, err
	}
	sc := &ssh.ServerConfig{MaxAuthTries: 3, PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if !keys[string(key.Marshal())] && !users[meta.User()][string(key.Marshal())] {
			stats.authFailure.Add(1)
			return nil, errors.New("unauthorized key")
		}
		stats.authSuccess.Add(1)
		return nil, nil
	}}
	sc.AddHostKey(signer)
	return sc, nil
}

func hostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		der, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return nil, e
		}
		data = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		// Link a complete owner-only file into place, never overwrite an existing key.
		f, e := os.CreateTemp(filepath.Dir(path), ".wishport-hostkey-*")
		if e != nil {
			return nil, fmt.Errorf("create host key: %w", e)
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		if e = f.Chmod(0600); e == nil {
			_, e = f.Write(data)
		}
		if e == nil {
			e = f.Sync()
		}
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			return nil, e
		}
		if e = os.Link(tmp, path); e != nil && !errors.Is(e, os.ErrExist) {
			return nil, fmt.Errorf("install host key: %w", e)
		}
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read host key: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("host key must be a regular owner-only file")
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse host key: %w", err)
	}
	return signer, nil
}

func serve(ctx context.Context, ln net.Listener, cfg Config, sc *ssh.ServerConfig, stats *metrics) error {
	defer ln.Close()
	var wg sync.WaitGroup
	var mu sync.Mutex
	conns := map[net.Conn]struct{}{}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ln.Close()
			mu.Lock()
			for c := range conns {
				c.Close()
			}
			mu.Unlock()
		case <-done:
		}
	}()
	defer func() {
		close(done)
		mu.Lock()
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	}()
	sem := make(chan struct{}, maxConnections)
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case sem <- struct{}{}:
			stats.connections.Add(1)
		default:
			c.Close()
			continue
		}
		mu.Lock()
		conns[c] = struct{}{}
		if ctx.Err() != nil {
			c.Close()
		}
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem; mu.Lock(); delete(conns, c); mu.Unlock(); c.Close() }()
			c.SetDeadline(time.Now().Add(handshakeTimeout))
			conn, chans, reqs, err := ssh.NewServerConn(c, sc)
			if err != nil {
				return
			}
			c.SetDeadline(time.Time{})
			defer conn.Close()
			go ssh.DiscardRequests(reqs)
			// One session per connection bounds resources and isolates forwarded agents.
			used := false
			var sessions sync.WaitGroup
			defer func() { conn.Close(); sessions.Wait() }()
			for ch := range chans {
				if ch.ChannelType() != "session" || used {
					ch.Reject(ssh.Prohibited, "only one session is allowed")
					continue
				}
				used = true
				channel, requests, err := ch.Accept()
				if err != nil {
					continue
				}
				sessions.Add(1)
				go func() { defer sessions.Done(); session(ctx, conn, channel, requests, cfg, stats) }()
			}
		}()
	}
}

type ptyRequest struct {
	Term                         string
	Columns, Rows, Width, Height uint32
	Modes                        string
}
type windowRequest struct{ Columns, Rows, Width, Height uint32 }

var targetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,511}$`)

func commandArgs(command string) ([]string, bool) {
	if command == "" || command == "menu" {
		return nil, true
	}
	fields := strings.Fields(command)
	if len(fields) == 3 && fields[0] == "connect" && fields[1] == "--target" && targetPattern.MatchString(fields[2]) {
		return fields, true
	}
	if len(fields) == 1 && fields[0] != "serve" && fields[0] != "server" && fields[0] != "s" && targetPattern.MatchString(fields[0]) {
		return []string{"connect", "--target", fields[0]}, true
	}
	return nil, false
}
func size(cols, rows uint32) *pty.Winsize {
	if cols == 0 || cols > 65535 {
		cols = 80
	}
	if rows == 0 || rows > 65535 {
		rows = 24
	}
	return &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
}
func session(ctx context.Context, conn *ssh.ServerConn, ch ssh.Channel, requests <-chan *ssh.Request, cfg Config, stats *metrics) {
	defer ch.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var term string
	var winsize *pty.Winsize
	var terminal *os.File
	var started, agentRequested bool
	var stopAgent func()
	defer func() {
		if terminal != nil {
			terminal.Close()
		}
		if stopAgent != nil {
			stopAgent()
		}
	}()
	exited := make(chan struct{})
	for {
		select {
		case <-ctx.Done():
			return
		case <-exited:
			return
		case req, ok := <-requests:
			if !ok {
				return
			}
			accepted := false
			switch req.Type {
			case "pty-req":
				var p ptyRequest
				if !started && winsize == nil && ssh.Unmarshal(req.Payload, &p) == nil && len(p.Term) <= 128 && regexp.MustCompile(`^[A-Za-z0-9._+-]*$`).MatchString(p.Term) {
					term = p.Term
					winsize = size(p.Columns, p.Rows)
					accepted = true
				}
			case "window-change":
				var w windowRequest
				if ssh.Unmarshal(req.Payload, &w) == nil && winsize != nil {
					winsize = size(w.Columns, w.Rows)
					if terminal != nil {
						_ = pty.Setsize(terminal, winsize)
					}
					accepted = true
				}
			case "auth-agent-req@openssh.com":
				if !started && !agentRequested {
					agentRequested = true
					accepted = true
				}
			case "shell", "exec":
				var command string
				valid := req.Type == "shell" && len(req.Payload) == 0
				if req.Type == "exec" {
					var p struct{ Command string }
					if ssh.Unmarshal(req.Payload, &p) == nil {
						command = p.Command
						valid = true
					}
				}
				args, allowed := commandArgs(command)
				if !started && winsize != nil && valid && allowed {
					args = append(append([]string{}, cfg.Args...), args...)
					if cfg.ConfigPath != "" {
						args = append([]string{"--config", cfg.ConfigPath}, args...)
					}
					cmd := exec.CommandContext(ctx, cfg.Executable, args...)
					cmd.Env = append(os.Environ(), "TERM="+term)
					// Never inherit an unrelated local agent into a remote session.
					cmd.Env = withoutEnv(cmd.Env, "SSH_AUTH_SOCK")
					if agentRequested {
						var sock string
						var err error
						sock, stopAgent, err = forwardAgent(ctx, conn)
						if err != nil {
							break
						}
						cmd.Env = append(cmd.Env, "SSH_AUTH_SOCK="+sock)
					}
					var err error
					terminal, err = pty.StartWithSize(cmd, winsize)
					if err != nil {
						break
					}
					started = true
					accepted = true
					stats.sessions.Add(1)
					go func() { io.Copy(terminal, ch) }()
					go func() {
						io.Copy(ch, terminal)
						err := cmd.Wait()
						status := uint32(0)
						if err != nil {
							status = 1
							var ee *exec.ExitError
							if errors.As(err, &ee) && ee.ExitCode() >= 0 {
								status = uint32(ee.ExitCode())
							}
						}
						_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
						close(exited)
					}()
				}
			}
			if req.WantReply {
				_ = req.Reply(accepted, nil)
			}
		}
	}
}
func withoutEnv(env []string, key string) []string {
	out := env[:0]
	for _, v := range env {
		if !strings.HasPrefix(v, key+"=") {
			out = append(out, v)
		}
	}
	return out
}

// forwardAgent exposes only the authenticated client's agent on a private socket.
func forwardAgent(ctx context.Context, conn *ssh.ServerConn) (string, func(), error) {
	dir, err := os.MkdirTemp("", "wishport-agent-*")
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	active := map[net.Conn]struct{}{}
	sem := make(chan struct{}, 16)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			select {
			case sem <- struct{}{}:
			default:
				c.Close()
				continue
			}
			mu.Lock()
			active[c] = struct{}{}
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem; mu.Lock(); delete(active, c); mu.Unlock(); c.Close() }()
				remote, reqs, e := conn.OpenChannel("auth-agent@openssh.com", nil)
				if e != nil {
					return
				}
				defer remote.Close()
				go ssh.DiscardRequests(reqs)
				done := make(chan struct{})
				go func() { io.Copy(remote, c); remote.CloseWrite(); close(done) }()
				io.Copy(c, remote)
				c.Close()
				<-done
			}()
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ln.Close()
			<-acceptDone
			mu.Lock()
			for c := range active {
				c.Close()
			}
			mu.Unlock()
			conn.Close()
			wg.Wait()
			os.RemoveAll(dir)
		})
	}
	_ = ctx
	return path, stop, nil
}
