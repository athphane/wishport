package forge

import (
	"context"
	"errors"
	api "github.com/athphane/wishport/internal/forge"
	"github.com/athphane/wishport/internal/inventory"
	"strings"
	"testing"
	"time"
)

type fakeClient struct {
	rows  []inventory.Connection
	err   error
	calls int
}

func (f *fakeClient) Inventory(context.Context) ([]inventory.Connection, error) {
	f.calls++
	return f.rows, f.err
}
func TestFailedRefreshKeepsGoodCache(t *testing.T) {
	c := &api.Cache{Dir: t.TempDir(), Partition: api.Partition("api", "token")}
	good := []inventory.Connection{{ID: "good"}}
	if e := c.Save(good); e != nil {
		t.Fatal(e)
	}
	client := &fakeClient{rows: []inventory.Connection{{ID: "partial"}}, err: errors.New("SECRET")}
	p := &Provider{Client: client, Cache: c, TTL: time.Hour}
	rows, e := p.List(context.Background(), true)
	if e == nil || rows[0].ID != "good" {
		t.Fatalf("%v %v", rows, e)
	}
	rows, _, _, e = p.Cached(context.Background())
	if e != nil || rows[0].ID != "good" {
		t.Fatal("cache changed after failed refresh")
	}
	if _, e = p.List(context.Background(), false); e != nil || client.calls != 1 {
		t.Fatal("cache read made network request")
	}
}
func TestStaleCacheRefreshAndPartialFailure(t *testing.T) {
	now := time.Now()
	c := &api.Cache{Dir: t.TempDir(), Partition: api.Partition("api", "token"), Now: func() time.Time { return now }}
	if e := c.Save([]inventory.Connection{{ID: "old"}}); e != nil {
		t.Fatal(e)
	}
	now = now.Add(2 * time.Hour)
	client := &fakeClient{rows: []inventory.Connection{{ID: "new"}}}
	p := &Provider{Client: client, Cache: c, TTL: time.Hour}
	rows, e := p.List(context.Background(), false)
	if e != nil || rows[0].ID != "new" || client.calls != 1 {
		t.Fatalf("stale cache did not refresh: %v %v", rows, e)
	}
	client.err = errors.New("Forge token rejected (HTTP 401)")
	if e = c.Forget(); e != nil {
		t.Fatal(e)
	}
	rows, e = p.List(context.Background(), true)
	if e == nil || len(rows) != 1 || rows[0].ID != "new" {
		t.Fatalf("partial result was discarded: %v %v", rows, e)
	}
	if !strings.Contains(e.Error(), "401") {
		t.Fatalf("actionable API error lost: %v", e)
	}
}
