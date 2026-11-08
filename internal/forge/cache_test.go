package forge

import (
	"github.com/athphane/wishport/internal/inventory"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCachePermissionsWhitelistAndTTL(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := &Cache{Partition: Partition("api", "token"), Dir: filepath.Join(t.TempDir(), "cache"), Now: func() time.Time { return now }}
	row := inventory.Connection{ID: "forge:id", Name: "server", Description: "SECRET", Identity: "SECRET", SearchTerms: []string{"SECRET"}, Host: "1.2.3.4"}
	if e := c.Save([]inventory.Connection{row}); e != nil {
		t.Fatal(e)
	}
	path, _ := c.path()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "token") {
		t.Fatal("cache leaked secret fields")
	}
	for p, want := range map[string]os.FileMode{c.Dir: 0700, path: 0600} {
		st, e := os.Stat(p)
		if e != nil || st.Mode().Perm() != want {
			t.Fatalf("permissions %s: %v %v", p, st, e)
		}
	}
	rows, at, stale, e := c.Load(time.Hour)
	if e != nil || stale || len(rows) != 1 || !at.Equal(now) {
		t.Fatalf("%v %v %v", rows, stale, e)
	}
	now = now.Add(2 * time.Hour)
	if _, _, stale, e = c.Load(time.Hour); e != nil || !stale {
		t.Fatal("expected stale cache")
	}
	if e = c.Forget(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("cache not removed")
	}
}
func TestCacheIsolationCorruptionAndAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	a := &Cache{Partition: Partition("api", "a"), Dir: dir}
	b := &Cache{Partition: Partition("api", "b"), Dir: dir}
	if a.Partition == b.Partition || a.Partition == Partition("other", "a") {
		t.Fatal("partition collision")
	}
	if e := a.Save([]inventory.Connection{{ID: "old"}}); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := b.Load(time.Hour); e == nil {
		t.Fatal("cross-token cache available")
	}
	if e := a.Save([]inventory.Connection{{ID: "new"}}); e != nil {
		t.Fatal(e)
	}
	rows, _, _, e := a.Load(time.Hour)
	if e != nil || rows[0].ID != "new" {
		t.Fatal("replacement failed")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("temporary file remains")
	}
	path, _ := a.path()
	os.WriteFile(path, []byte(`{"version":1,"raw_api":"SECRET"}`), 0600)
	if _, _, _, e = a.Load(time.Hour); e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatal("unsafe corruption handling")
	}
	if _, e = NewCache("../../unsafe"); e == nil {
		t.Fatal("unsafe partition accepted")
	}
}
func TestCachePreservesAvailabilityAndOrganization(t *testing.T) {
	c := &Cache{Partition: Partition("api", "token"), Dir: t.TempDir()}
	if e := c.Save([]inventory.Connection{{ID: "site", OrganizationName: "Example organization", Unavailable: "site root is unavailable"}}); e != nil {
		t.Fatal(e)
	}
	rows, _, _, e := c.Load(time.Hour)
	if e != nil || rows[0].Unavailable == "" || rows[0].OrganizationName != "Example organization" {
		t.Fatalf("cache lost safety or organization fields: %+v %v", rows, e)
	}
}
func TestCacheUsesXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	c, e := NewCache(Partition("api", "token"))
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.path()
	if e != nil || filepath.Dir(p) != filepath.Join(dir, "wishport") {
		t.Fatalf("%s %v", p, e)
	}
}

func TestCacheRenameCompatibility(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	partition := Partition("api", "token")
	legacy := &Cache{Partition: partition, Dir: filepath.Join(dir, "sshdir")}
	if err := legacy.Save([]inventory.Connection{{ID: "old"}}); err != nil {
		t.Fatal(err)
	}
	current, err := NewCache(partition)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _, err := current.Load(time.Hour)
	if err != nil || len(rows) != 1 || rows[0].ID != "old" {
		t.Fatalf("legacy fallback: %v %v", rows, err)
	}
	if err := current.Save([]inventory.Connection{{ID: "new"}}); err != nil {
		t.Fatal(err)
	}
	rows, _, _, err = current.Load(time.Hour)
	if err != nil || len(rows) != 1 || rows[0].ID != "new" {
		t.Fatalf("current precedence: %v %v", rows, err)
	}
	if err := current.Forget(); err != nil {
		t.Fatal(err)
	}
	for _, cache := range []*Cache{legacy, current} {
		path, _ := cache.path()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache remains: %v", err)
		}
	}
}
