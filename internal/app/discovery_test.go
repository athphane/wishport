package app

import (
	"context"
	"errors"
	"testing"

	"github.com/athphane/wishport/internal/config"
	"github.com/athphane/wishport/internal/inventory"
)

type discoveredStub struct {
	calls int
	rows  []inventory.Connection
	err   error
}

func (p *discoveredStub) Name() string { return "mdns" }
func (p *discoveredStub) List(context.Context, bool) ([]inventory.Connection, error) {
	p.calls++
	return p.rows, p.err
}

func TestDiscoveryBackgroundAndFailedRefreshPreservesInventory(t *testing.T) {
	p := &discoveredStub{rows: []inventory.Connection{{ID: "mdns:one", Source: inventory.SourceMDNS, Name: "device", Host: "192.0.2.1"}}}
	cfg := config.Defaults()
	cfg.Forge.Enabled = false
	a := &App{Config: cfg, DiscoveryProviders: []inventory.Provider{p}}
	s, more := a.LoadInitial(context.Background())
	if p.calls != 0 || !more || len(s.Connections) != 0 {
		t.Fatal("initial load must not wait for discovery")
	}
	s = a.Load(context.Background(), false)
	if p.calls != 1 || len(s.Connections) != 1 {
		t.Fatal("discovery not loaded")
	}
	p.err = errors.New("offline")
	p.rows = nil
	s = a.Load(context.Background(), true)
	if len(s.Connections) != 1 || len(s.Warnings) != 1 {
		t.Fatal("failed refresh dropped prior discovered hosts")
	}
}

func TestFindTargetRejectsAmbiguousNames(t *testing.T) {
	s := inventory.Snapshot{Connections: []inventory.Connection{{ID: "ssh:one", Name: "same"}, {ID: "wishlist:one", Name: "same"}}}
	if _, err := FindTarget(s, "same"); err == nil {
		t.Fatal("ambiguous name silently selected")
	}
	if c, err := FindTarget(s, "ssh:one"); err != nil || c.ID != "ssh:one" {
		t.Fatal(c, err)
	}
}
