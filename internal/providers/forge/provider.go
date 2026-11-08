package forge

import (
	"context"
	"fmt"
	api "github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/inventory"
	"sync"
	"time"
)

type Client interface {
	Inventory(context.Context) ([]inventory.Connection, error)
}
type Provider struct {
	Client Client
	Cache  *api.Cache
	TTL    time.Duration
	mu     sync.Mutex
}

func (*Provider) Name() string { return "forge" }
func (p *Provider) Cached(ctx context.Context) ([]inventory.Connection, string, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cached(ctx)
}
func (p *Provider) cached(ctx context.Context) ([]inventory.Connection, string, bool, error) {
	if e := ctx.Err(); e != nil {
		return nil, "", false, e
	}
	if p.Cache == nil {
		return nil, "", false, fmt.Errorf("forge cache is unavailable")
	}
	rows, at, stale, e := p.Cache.Load(p.TTL)
	if e != nil {
		return nil, "", false, e
	}
	status := "Cached " + at.UTC().Format(time.RFC3339)
	if stale {
		status = "Stale cache " + at.UTC().Format(time.RFC3339)
	}
	age := time.Since(at)
	if age < 0 {
		age = 0
	}
	status += fmt.Sprintf(" (%s old)", age.Round(time.Minute))
	return rows, status, stale, nil
}
func (p *Provider) List(ctx context.Context, refresh bool) ([]inventory.Connection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !refresh {
		rows, _, stale, e := p.cached(ctx)
		if e == nil && !stale {
			return rows, nil
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if p.Client == nil {
		return nil, fmt.Errorf("forge client is unavailable")
	}
	rows, e := p.Client.Inventory(ctx)
	if e != nil {
		cached, _, _, ce := p.cached(ctx)
		if ce == nil {
			return cached, fmt.Errorf("forge refresh failed; showing cached inventory: %w", e)
		}
		return rows, fmt.Errorf("forge refresh failed; no cached inventory available: %w", e)
	}
	if p.Cache != nil {
		if e = p.Cache.Save(rows); e != nil {
			return rows, e
		}
	}
	return rows, nil
}
