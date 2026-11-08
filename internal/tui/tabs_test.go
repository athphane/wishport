package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/inventory"
)

func tabFixture(t *testing.T) *Model {
	t.Helper()
	svc := &fakeService{snapshot: inventory.Snapshot{Connections: []inventory.Connection{
		{ID: "ssh", Name: "prod-ssh", Source: inventory.SourceSSHConfig, Kind: inventory.KindHost},
		{ID: "wishlist", Name: "prod-wishlist", Source: inventory.SourceWishlist, Kind: inventory.KindHost},
		{ID: "yaml", Name: "prod-yaml", Source: inventory.SourceYAML, Kind: inventory.KindHost},
		{ID: "server", Name: "prod-server", Source: inventory.SourceForge, Kind: inventory.KindForgeServer},
		{ID: "site", Name: "prod.example", Source: inventory.SourceForge, Kind: inventory.KindForgeSite},
	}}}
	m := New(context.Background(), svc)
	m.Update(m.Init()())
	return m
}

func visibleIDs(m *Model) []string {
	var ids []string
	for _, c := range m.visible {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestTabSourcesNavigationAndRememberedSelection(t *testing.T) {
	m := tabFixture(t)
	m.Update(special(tea.KeyTab))
	if m.tab != tabLocal || !reflect.DeepEqual(visibleIDs(m), []string{"ssh", "wishlist", "yaml"}) {
		t.Fatal("Local must include all three local providers", visibleIDs(m))
	}
	if !strings.Contains(m.View().Content, "3 connections") {
		t.Fatal("count must reflect active tab")
	}
	m.Update(key("j"))
	m.Update(special(tea.KeyRight))
	if m.tab != tabForge || !reflect.DeepEqual(visibleIDs(m), []string{"server", "site"}) {
		t.Fatal("Forge must include servers and sites", visibleIDs(m))
	}
	m.Update(special(tea.KeyLeft))
	if m.tab != tabLocal || m.selectedID() != "wishlist" {
		t.Fatal("Local selection was lost")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.tab != tabAll || len(m.visible) != 5 {
		t.Fatal("Shift+Tab did not select All")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.tab != tabForge {
		t.Fatal("backward wrap failed")
	}
	m.Update(special(tea.KeyTab))
	if m.tab != tabAll {
		t.Fatal("forward wrap failed")
	}
}

func TestTabsComposeWithSearchAndSurviveRefresh(t *testing.T) {
	m := tabFixture(t)
	m.Update(key("/"))
	for _, r := range "server:prd" {
		m.Update(key(string(r)))
	}
	m.Update(special(tea.KeyTab))
	if m.tab != tabLocal || len(m.visible) != 3 || !m.input.Focused() {
		t.Fatal("focused tab switch failed")
	}
	m.Update(special(tea.KeyLeft))
	if m.tab != tabLocal {
		t.Fatal("input cursor navigation switched tabs")
	}
	m.Update(special(tea.KeyTab))
	if m.tab != tabForge || !reflect.DeepEqual(visibleIDs(m), []string{"server"}) {
		t.Fatal("server scope lost", visibleIDs(m))
	}
	m.Update(loaded{generation: m.generation, snapshot: m.snapshot})
	if m.tab != tabForge || m.input.Value() != "server:prd" || m.selectedID() != "server" {
		t.Fatal("refresh lost tab, query, or selection")
	}
	m.Update(special(tea.KeyEscape))
	if m.tab != tabForge || len(m.visible) != 2 || m.input.Focused() {
		t.Fatal("escape should clear search within current tab")
	}
}

func TestTabsEmptyAndPreparationFreeze(t *testing.T) {
	m, _ := setup(t)
	m.Update(special(tea.KeyLeft))
	if m.tab != tabForge || len(m.visible) != 0 || !strings.Contains(m.View().Content, "No Forge connections") {
		t.Fatal("missing empty Forge state")
	}
	m.preparing = true
	m.Update(special(tea.KeyTab))
	m.Update(special(tea.KeyRight))
	if m.tab != tabForge {
		t.Fatal("tab changed while preparing")
	}
	m.preparing = false
	m.Update(special(tea.KeyTab))
	if m.tab != tabAll || len(m.visible) != 2 {
		t.Fatal("empty tab prevented navigation")
	}
}
