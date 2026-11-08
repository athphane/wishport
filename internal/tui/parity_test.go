package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/inventory"
)

func TestDiscoveryTabAndWishlistNavigation(t *testing.T) {
	m := tabFixture(t)
	m.snapshot.Connections = append(m.snapshot.Connections, inventory.Connection{ID: "mdns:one", Source: inventory.SourceMDNS, Name: "discovered", Host: "192.0.2.1"})
	m.filter("")
	m.Update(special(tea.KeyLeft))
	if m.tab != tabDiscovered || len(m.visible) != 1 {
		t.Fatal("discovery missing from tab cycle")
	}
	_, copy := m.Update(key("y"))
	if copy == nil {
		t.Fatal("copy shortcut missing")
	}
	_, clipboard := m.Update(copy())
	if clipboard == nil {
		t.Fatal("copy did not emit clipboard command")
	}
	m.Update(special(tea.KeyTab))
	m.Update(key("G"))
	if m.selectedID() != "mdns:one" {
		t.Fatal("last item shortcut failed")
	}
	m.Update(key("g"))
	if m.selected != 0 {
		t.Fatal("first item shortcut failed")
	}
	m.Update(special(tea.KeyPgDown))
	if m.selected == 0 {
		t.Fatal("page down failed")
	}
	m.Update(key("/"))
	m.Update(key("y"))
	if m.input.Value() != "y" {
		t.Fatal("copy intercepted typing")
	}
	m.Update(special(tea.KeyEscape))
	_, connect := m.Update(key("o"))
	if connect == nil || !m.preparing {
		t.Fatal("o did not connect")
	}
}

func TestPeriodicRefreshDoesNotInterruptPreparation(t *testing.T) {
	m, _ := setup(t)
	m.SetRefreshInterval(time.Second)
	_, cmd := m.Update(refreshTick{})
	if cmd == nil || !m.loading {
		t.Fatal("periodic refresh missing")
	}
	m.stop()
	m.preparing = true
	generation := m.generation
	m.Update(refreshTick{})
	if !m.preparing || m.generation != generation {
		t.Fatal("refresh interrupted preparation")
	}
	m.SetRefreshInterval(0)
	if _, cmd := m.Update(refreshTick{}); cmd != nil {
		t.Fatal("disabled refresh kept running")
	}
	// Opt-in scheduling must not change ordinary Init's directly usable command.
	n := New(context.Background(), &fakeService{})
	if _, ok := n.Init()().(loaded); !ok {
		t.Fatal("ordinary startup unexpectedly schedules timers")
	}
}
