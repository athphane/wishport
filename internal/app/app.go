package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/athphane/wishport/internal/config"
	"github.com/athphane/wishport/internal/discovery"
	"github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/inventory"
	forgeprovider "github.com/athphane/wishport/internal/providers/forge"
	"github.com/athphane/wishport/internal/providers/sshconfig"
	"github.com/athphane/wishport/internal/providers/wishlist"
	yamlprovider "github.com/athphane/wishport/internal/providers/yaml"
	"github.com/athphane/wishport/internal/secrets"
	sshclient "github.com/athphane/wishport/internal/ssh"
)

type App struct {
	Config             config.Config
	ConfigPath         string
	Providers          []inventory.Provider
	DiscoveryProviders []inventory.Provider
	Wishlist           *wishlist.Provider
	discovered         map[string][]inventory.Connection
	Runner             sshclient.Runner
	Warnings           []inventory.Warning
	Forge              *forgeprovider.Provider
	ForgeClient        *forge.Client
	Token              func(context.Context) (string, error)
	mu                 sync.Mutex
}

func New(path string) *App {
	a := &App{Runner: sshclient.OSRunner{}, Token: secrets.Token}
	explicit := path != ""
	var err error
	if path == "" {
		path, err = config.DefaultPath()
	}
	a.ConfigPath = path
	if err == nil {
		a.Config, err = config.Load(path)
	}
	if err != nil {
		a.Config = config.Defaults()
		if explicit || !errors.Is(err, os.ErrNotExist) {
			a.Warnings = append(a.Warnings, inventory.Warning{Source: "config", Message: err.Error()})
		}
	}
	a.ConfigureProviders()
	return a
}

// ConfigureProviders is called during startup, after CLI overrides are applied.
func (a *App) ConfigureProviders() {
	a.Providers = nil
	a.DiscoveryProviders = nil
	a.Wishlist = nil
	if a.Config.Sources.SSHConfig {
		home, e := os.UserHomeDir()
		if e != nil {
			a.Warnings = append(a.Warnings, inventory.Warning{Source: "ssh_config", Message: e.Error()})
		} else {
			path := filepath.Join(home, ".ssh", "config")
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				path = "/etc/ssh/ssh_config"
			}
			a.Providers = append(a.Providers, sshconfig.Provider{Path: path})
		}
	}
	if a.Config.Sources.Wishlist {
		a.Wishlist = &wishlist.Provider{Path: a.Config.Sources.WishlistConfig}
		a.Providers = append(a.Providers, a.Wishlist)
	}
	a.Providers = append(a.Providers, yamlprovider.Provider{Connections: a.Config.Connections})
	d := a.Config.Discovery
	if d.MDNS.Enabled {
		a.DiscoveryProviders = append(a.DiscoveryProviders, &discovery.MDNS{Domain: d.MDNS.Domain, Timeout: d.MDNS.Timeout})
	}
	if len(d.SRV.Domains) > 0 {
		a.DiscoveryProviders = append(a.DiscoveryProviders, &discovery.SRV{Domains: d.SRV.Domains, Timeout: d.SRV.Timeout})
	}
	if d.Tailscale.Tailnet != "" {
		a.DiscoveryProviders = append(a.DiscoveryProviders, &discovery.Tailscale{Tailnet: d.Tailscale.Tailnet, Timeout: d.Tailscale.Timeout})
	}
}

func (a *App) DiscoveryEnabled() bool { return len(a.DiscoveryProviders) > 0 }

func (a *App) local(ctx context.Context, refresh bool) inventory.Snapshot {
	s := inventory.Snapshot{Connections: []inventory.Connection{}, Warnings: append([]inventory.Warning{}, a.Warnings...)}
	for _, p := range a.Providers {
		rows, err := p.List(ctx, refresh)
		s.Connections = append(s.Connections, rows...)
		if err != nil {
			s.Warnings = append(s.Warnings, inventory.Warning{Source: p.Name(), Message: err.Error()})
		}
	}
	inventory.Sort(s.Connections)
	return s
}

// LoadInitial never waits for the keyring or the network.
func (a *App) LoadInitial(ctx context.Context) (inventory.Snapshot, bool) {
	return a.local(ctx, false), a.Config.Forge.Enabled || a.DiscoveryEnabled()
}

func (a *App) initForge(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Forge != nil {
		return nil
	}
	lookup := a.Token
	if lookup == nil {
		lookup = secrets.Token
	}
	token, err := lookup(ctx)
	if err != nil {
		return err
	}
	client, err := forge.NewClient(forge.DefaultBaseURL, token, nil)
	if err != nil {
		return err
	}
	cache, err := forge.NewCache(forge.Partition(forge.DefaultBaseURL, token))
	if err != nil {
		return err
	}
	a.ForgeClient = client
	a.Forge = &forgeprovider.Provider{Client: client, Cache: cache, TTL: a.Config.Forge.CacheTTL}
	return nil
}

func (a *App) Load(ctx context.Context, refresh bool) inventory.Snapshot {
	s := a.local(ctx, refresh)
	a.loadDiscovery(ctx, refresh, &s)
	if !a.Config.Forge.Enabled {
		return s
	}
	if err := a.initForge(ctx); err != nil {
		s.Status = "Forge not connected · use wishport login or FORGE_API_TOKEN"
		if !errors.Is(err, secrets.ErrNotFound) && !errors.Is(err, secrets.ErrUnavailable) {
			s.Warnings = append(s.Warnings, inventory.Warning{Source: "forge", Message: err.Error()})
		}
		return s
	}
	if !refresh {
		rows, status, stale, err := a.Forge.Cached(ctx)
		if err == nil {
			s.Connections = append(s.Connections, rows...)
			inventory.Sort(s.Connections)
			s.Status = "Forge: " + status
			s.NeedsRefresh = stale
			return s
		}
	}
	rows, err := a.Forge.List(ctx, true)
	s.Connections = append(s.Connections, rows...)
	if err != nil {
		s.Warnings = append(s.Warnings, inventory.Warning{Source: "forge", Message: err.Error()})
	}
	_, status, _, cacheErr := a.Forge.Cached(ctx)
	if cacheErr == nil {
		s.Status = "Forge: " + status
	} else if err != nil {
		s.Status = "Forge unavailable · refresh failed"
		if len(rows) > 0 {
			s.Status = "Forge partial inventory · refresh failed"
		}
	} else {
		s.Status = "Forge: live inventory (cache unavailable)"
	}
	inventory.Sort(s.Connections)
	return s
}

func (a *App) Resolve(ctx context.Context, c inventory.Connection) (sshclient.Resolution, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return sshclient.Resolve(ctx, a.Runner, c)
}

func (a *App) Command(ctx context.Context, c inventory.Connection) (*exec.Cmd, error) {
	r, err := a.Resolve(ctx, c)
	if err != nil {
		return nil, err
	}
	return r.Command.Exec(ctx), nil
}

func (a *App) Address(ctx context.Context, c inventory.Connection) (string, error) {
	if c.SSHAlias == "" && c.Host != "" {
		return c.Host, nil
	}
	r, err := a.Resolve(ctx, c)
	if err != nil {
		return "", err
	}
	return r.Connection.Host, nil
}

func Find(s inventory.Snapshot, id string) (inventory.Connection, error) {
	for _, c := range s.Connections {
		if c.ID == id {
			return c, nil
		}
	}
	return inventory.Connection{}, fmt.Errorf("target ID not found; use wishport list --json to choose a target")
}

// FindTarget accepts exact IDs first, then a unique name or SSH alias.
func FindTarget(s inventory.Snapshot, target string) (inventory.Connection, error) {
	if c, err := Find(s, target); err == nil {
		return c, nil
	}
	var matches []inventory.Connection
	for _, c := range s.Connections {
		if c.Name == target || c.SSHAlias != "" && c.SSHAlias == target {
			matches = append(matches, c)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return inventory.Connection{}, errors.New("target name is ambiguous; use its stable ID from wishport list --json")
	}
	return inventory.Connection{}, errors.New("target not found; use wishport list --json")
}
