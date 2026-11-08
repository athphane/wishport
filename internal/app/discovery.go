package app

import (
	"context"
	"sync"

	"github.com/athphane/wishport/internal/inventory"
)

func (a *App) loadDiscovery(ctx context.Context, refresh bool, s *inventory.Snapshot) {
	type result struct {
		name string
		rows []inventory.Connection
		err  error
	}
	results := make([]result, len(a.DiscoveryProviders))
	var wg sync.WaitGroup
	for i, p := range a.DiscoveryProviders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := p.List(ctx, refresh)
			results[i] = result{p.Name(), rows, err}
		}()
	}
	wg.Wait()
	var rows []inventory.Connection
	a.mu.Lock()
	if a.discovered == nil {
		a.discovered = map[string][]inventory.Connection{}
	}
	for _, r := range results {
		if r.err == nil && ctx.Err() == nil {
			a.discovered[r.name] = append([]inventory.Connection(nil), r.rows...)
		} else if old, ok := a.discovered[r.name]; ok {
			r.rows = old
		}
		rows = append(rows, r.rows...)
		if r.err != nil {
			s.Warnings = append(s.Warnings, inventory.Warning{Source: r.name, Message: r.err.Error()})
		}
	}
	a.mu.Unlock()
	if a.Wishlist != nil && len(rows) > 0 {
		hinted, err := a.Wishlist.ApplyHints(rows)
		if err != nil {
			s.Warnings = append(s.Warnings, inventory.Warning{Source: "wishlist", Message: err.Error()})
			for i := range rows {
				rows[i].Unavailable = "Invalid discovery hints; fix the Wishlist configuration"
			}
		} else {
			rows = hinted
		}
	}
	s.Connections = append(s.Connections, rows...)
	inventory.Sort(s.Connections)
}
