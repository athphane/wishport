package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/athphane/wishport/internal/config"
	"github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/inventory"
	provider "github.com/athphane/wishport/internal/providers/forge"
	yamlprovider "github.com/athphane/wishport/internal/providers/yaml"
	"github.com/athphane/wishport/internal/secrets"
)

type apiStub struct {
	calls int
	rows  []inventory.Connection
	err   error
}

func (a *apiStub) Inventory(context.Context) ([]inventory.Connection, error) {
	a.calls++
	return a.rows, a.err
}

func TestLocalInitialNeverTouchesCredentials(t *testing.T) {
	a := &App{Config: config.Defaults(), Token: func(context.Context) (string, error) { t.Fatal("initial load accessed keyring"); return "", nil }, Providers: []inventory.Provider{yamlprovider.Provider{Connections: []inventory.Connection{{ID: "yaml:local", Name: "local"}}}}}
	s, background := a.LoadInitial(context.Background())
	if len(s.Connections) != 1 || !background {
		t.Fatal(s, background)
	}
	a.Token = func(context.Context) (string, error) { return "", secrets.ErrUnavailable }
	s = a.Load(context.Background(), false)
	if len(s.Connections) != 1 || len(s.Warnings) != 0 {
		t.Fatal(s)
	}
}

func TestStaleCacheShownBeforeRefreshAndRetainedOnFailure(t *testing.T) {
	cache, _ := forge.NewCache(forge.Partition(forge.DefaultBaseURL, "fixture"))
	cache.Dir = filepath.Join(t.TempDir(), "cache")
	cache.Now = func() time.Time { return time.Now().Add(-8 * 24 * time.Hour) }
	old := []inventory.Connection{{ID: "forge:fixture:org:server:1", Source: inventory.SourceForge, Name: "cached", Kind: inventory.KindForgeServer}}
	if err := cache.Save(old); err != nil {
		t.Fatal(err)
	}
	cache.Now = nil
	api := &apiStub{err: errors.New("fixture offline")}
	a := &App{Config: config.Defaults(), Forge: &provider.Provider{Client: api, Cache: cache}}
	s := a.Load(context.Background(), false)
	if !s.NeedsRefresh || api.calls != 0 || len(s.Connections) != 1 {
		t.Fatal(s, api.calls)
	}
	s = a.Load(context.Background(), true)
	if api.calls != 1 || len(s.Warnings) != 1 || len(s.Connections) != 1 {
		t.Fatal(s, api.calls)
	}
}

func TestFailedRefreshDoesNotClaimLiveInventory(t *testing.T) {
	api := &apiStub{err: errors.New("invalid Forge API response")}
	a := &App{Config: config.Defaults(), Forge: &provider.Provider{Client: api}}
	s := a.Load(context.Background(), true)
	if s.Status != "Forge unavailable · refresh failed" || len(s.Warnings) != 1 {
		t.Fatal(s)
	}
}

func TestWishlistAndSSHConfigLoadTogether(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for path, content := range map[string]string{
		".ssh/config":      "Host shared\n HostName 192.0.2.1\n",
		".config/wishlist": "Host shared\n HostName 192.0.2.2\n",
		"disabled.yaml":    "sources:\n  wishlist: false\nforge:\n  enabled: false\n",
	} {
		full := filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := New("").LoadInitial(context.Background())
	if len(s.Warnings) != 0 || len(s.Connections) != 2 || s.Connections[0].ID != "ssh:shared" || s.Connections[1].ID != "wishlist:shared" {
		t.Fatal(s)
	}
	s, _ = New(filepath.Join(home, "disabled.yaml")).LoadInitial(context.Background())
	if len(s.Warnings) != 0 || len(s.Connections) != 1 || s.Connections[0].ID != "ssh:shared" {
		t.Fatal(s)
	}
}
