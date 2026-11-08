package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/athphane/wishport/internal/app"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/server"
	"github.com/charmbracelet/x/term"
)

type stringFlags []string

func (s *stringFlags) String() string { return strings.Join(*s, ",") }
func (s *stringFlags) Set(v string) error {
	if len(*s) >= 32 || strings.TrimSpace(v) == "" {
		return errors.New("provide at most 32 nonempty domains")
	}
	*s = append(*s, v)
	return nil
}

type extraOptions struct {
	wishlist, listen, hostKey, authorizedKeys, authorizedUsers, metrics, mdnsDomain, tailnet string
	mdns                                                                                     bool
	srvDomains                                                                               stringFlags
	mdnsTimeout, refreshInterval                                                             time.Duration
}

func registerExtraFlags(fs *flag.FlagSet) *extraOptions {
	o := &extraOptions{}
	fs.StringVar(&o.wishlist, "wishlist-config", "", "explicit Wishlist YAML or SSH config file")
	fs.BoolVar(&o.mdns, "mdns", false, "discover local _ssh._tcp services (opt-in)")
	fs.StringVar(&o.mdnsDomain, "mdns-domain", "local", "mDNS discovery domain")
	fs.DurationVar(&o.mdnsTimeout, "mdns-timeout", 2*time.Second, "bounded mDNS browsing duration")
	fs.Var(&o.srvDomains, "srv-domain", "DNS domain to discover via SSH SRV records (repeatable)")
	fs.StringVar(&o.tailnet, "tailscale-net", "", "Tailscale tailnet; credentials from environment only")
	fs.DurationVar(&o.refreshInterval, "refresh-interval", 0, "automatic inventory refresh interval, 0 disables")
	fs.StringVar(&o.listen, "listen", server.DefaultListen, "hosted directory listen address (serve only)")
	fs.StringVar(&o.hostKey, "host-key", "", "server host key path; generated on explicit serve startup if missing")
	fs.StringVar(&o.authorizedKeys, "authorized-keys", "", "server authorized public keys file (serve only)")
	fs.StringVar(&o.authorizedUsers, "authorized-users", "", "Wishlist-style YAML users with per-user public keys (serve only)")
	fs.StringVar(&o.metrics, "metrics-listen", "", "optional loopback Prometheus listener (serve only)")
	return o
}
func valueFlag(s string) bool {
	if !strings.HasPrefix(s, "-") {
		return false
	}
	s = "--" + strings.TrimLeft(s, "-")
	switch s {
	case "--config", "--target", "--public-key", "--wishlist-config", "--mdns-domain", "--mdns-timeout", "--srv-domain", "--tailscale-net", "--refresh-interval", "--listen", "--host-key", "--authorized-keys", "--authorized-users", "--metrics-listen":
		return true
	}
	return false
}
func (o *extraOptions) apply(a *app.App, fs *flag.FlagSet) error {
	if o.refreshInterval < 0 || o.refreshInterval > 0 && o.refreshInterval < time.Second {
		return errors.New("refresh interval must be zero or at least 1s")
	}
	if o.mdnsTimeout <= 0 || o.mdnsTimeout > time.Minute {
		return errors.New("mDNS timeout must be between 0 and 1m")
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "wishlist-config":
			a.Config.Sources.Wishlist, a.Config.Sources.WishlistConfig = true, o.wishlist
		case "mdns":
			a.Config.Discovery.MDNS.Enabled = o.mdns
		case "mdns-domain":
			a.Config.Discovery.MDNS.Domain = o.mdnsDomain
		case "mdns-timeout":
			a.Config.Discovery.MDNS.Timeout = o.mdnsTimeout
		case "srv-domain":
			a.Config.Discovery.SRV.Domains = append([]string(nil), o.srvDomains...)
		case "tailscale-net":
			a.Config.Discovery.Tailscale.Tailnet = o.tailnet
		}
	})
	a.ConfigureProviders()
	return nil
}
func (o *extraOptions) serve(ctx context.Context, a *app.App) error {
	for _, warning := range a.Warnings {
		if warning.Source == "config" {
			return errors.New("fix configuration errors before starting the hosted directory")
		}
	}
	defaultPath, err := defaultServerDirectory()
	if err != nil {
		return err
	}
	if o.authorizedKeys == "" && o.authorizedUsers == "" {
		o.authorizedKeys = filepath.Join(defaultPath, "authorized_keys")
	}
	for _, path := range []string{o.authorizedKeys, o.authorizedUsers} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return errors.New("serve requires an existing authorization file; use --authorized-keys or --authorized-users PATH")
		}
	}
	if o.hostKey == "" {
		o.hostKey = filepath.Join(defaultPath, "server_ed25519")
	}
	if err := os.MkdirAll(filepath.Dir(o.hostKey), 0700); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	path := ""
	if _, err := os.Stat(a.ConfigPath); err == nil {
		path, err = filepath.Abs(a.ConfigPath)
		if err != nil {
			return err
		}
	}
	args := []string{"--refresh-interval", o.refreshInterval.String()}
	if a.Config.Sources.WishlistConfig != "" {
		args = append(args, "--wishlist-config", a.Config.Sources.WishlistConfig)
	}
	d := a.Config.Discovery
	args = append(args, fmt.Sprintf("--mdns=%t", d.MDNS.Enabled), "--mdns-domain", d.MDNS.Domain, "--mdns-timeout", d.MDNS.Timeout.String())
	for _, domain := range d.SRV.Domains {
		args = append(args, "--srv-domain", domain)
	}
	if d.Tailscale.Tailnet != "" {
		args = append(args, "--tailscale-net", d.Tailscale.Tailnet)
	}
	return server.Serve(ctx, server.Config{Listen: o.listen, HostKeyPath: o.hostKey, AuthorizedKeysPath: o.authorizedKeys, AuthorizedUsersPath: o.authorizedUsers, Executable: executable, ConfigPath: path, Args: args, MetricsListen: o.metrics})
}
func defaultServerDirectory() (string, error) {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(home, ".config")
	}
	return filepath.Join(d, "wishport"), nil
}

func keyConfirmation(in *os.File, out io.Writer, yes bool) interaction.Confirm {
	return func(ctx context.Context, consent interaction.Consent) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if yes {
			return true, nil
		}
		if !term.IsTerminal(in.Fd()) {
			return false, errors.New("key upload requires interactive consent; run wishport prepare --yes separately for unattended preparation")
		}
		c := consent.Target
		fmt.Fprintf(out, "Install public key?\nOrganization: %s\nServer: %s (%s:%d)\nSSH user: %s\nPublic key: %s\nFingerprint: %s\nType yes to approve: ", inventory.DisplayText(c.Organization), inventory.DisplayText(c.ServerName), inventory.DisplayText(c.Host), c.Port, inventory.DisplayText(c.User), inventory.DisplayText(consent.PublicKeyPath), inventory.DisplayText(consent.Fingerprint))
		answer, err := bufio.NewReader(in).ReadString('\n')
		return strings.TrimSpace(answer) == "yes", err
	}
}
