package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/charmbracelet/x/ansi"
)

func TestTableScrollingAndResponsiveColumns(t *testing.T) {
	m, _ := setup(t)
	m.snapshot.Connections = nil
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("node-%03d", i)
		m.snapshot.Connections = append(m.snapshot.Connections, inventory.Connection{ID: name, Name: name, Source: inventory.SourceForge, Kind: inventory.KindForgeServer, ServerName: name, OrganizationName: "Example", User: "deploy", Host: "192.0.2.1"})
	}
	m.filter("")
	m.Update(key("t"))
	for _, width := range []int{60, 92, 124} {
		for _, selected := range []int{0, 5, 50, 99} {
			m.selected = selected
			view, position := m.directoryView(width, 8)
			plain := ansi.Strip(view)
			if !strings.Contains(plain, fmt.Sprintf("node-%03d", selected)) || !strings.Contains(plain, "SSH target") {
				t.Fatalf("selected row or header hidden at width %d, row %d: %s", width, selected, plain)
			}
			if lipgloss.Width(view) > width || lipgloss.Height(view) > 8 {
				t.Fatalf("table overflow: %dx%d at width %d", lipgloss.Width(view), lipgloss.Height(view), width)
			}
			if !strings.Contains(ansi.Strip(position), fmt.Sprintf("%d / 100", selected+1)) {
				t.Fatal("incorrect position")
			}
			if width == 124 && !strings.Contains(plain, "Organization") {
				t.Fatal("wide table missing organization")
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	view, _ := m.directoryView(m.contentWidth(), 4)
	if strings.Contains(ansi.Strip(view), "SSH target") || !strings.Contains(ansi.Strip(view), "node-099") {
		t.Fatal("small terminal should retain selection in list fallback")
	}
}

func TestTableToggleKeepsSelectionAndDoesNotCaptureSearch(t *testing.T) {
	m := tabFixture(t)
	m.Update(special(tea.KeyTab))
	m.Update(key("j"))
	id := m.selectedID()
	m.Update(key("t"))
	if !m.tableMode || m.selectedID() != id || m.tab != tabLocal {
		t.Fatal("toggle changed navigation state")
	}
	m.Update(loaded{generation: m.generation, snapshot: m.snapshot})
	if !m.tableMode || m.selectedID() != id {
		t.Fatal("refresh reset table or selection")
	}
	m.Update(key("/"))
	m.Update(key("t"))
	if !m.tableMode || m.input.Value() != "t" {
		t.Fatal("table shortcut intercepted search")
	}
	m.Update(special(tea.KeyEscape))
	m.preparing = true
	m.Update(key("t"))
	if !m.tableMode {
		t.Fatal("changed view while preparing")
	}
	m.preparing = false
	m.Update(key("t"))
	if m.tableMode {
		t.Fatal("cannot return to list")
	}
}

func TestPreparationBarReportsStagesWithoutTimeEstimates(t *testing.T) {
	m, _ := setup(t)
	m.preparing = true
	for _, tc := range []struct{ stage, label string }{
		{interaction.StageAuthentication, "Step 1/7"},
		{interaction.StageInstallation, "Step 6/7"},
		{interaction.StageRetry, "Step 7/7"},
	} {
		m.stage = tc.stage
		bar := ansi.Strip(m.preparationProgress(40))
		if !strings.Contains(bar, tc.label) || strings.Contains(bar, "%") || lipgloss.Width(bar) > 40 {
			t.Fatalf("bad progress: %q", bar)
		}
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		if !strings.Contains(ansi.Strip(m.View().Content), tc.label) {
			t.Fatal("bar absent during preparation")
		}
	}
	m.stage = "Refreshing inventory"
	if m.preparationProgress(40) != "" {
		t.Fatal("unknown-duration work invented progress")
	}
	m.consent = &consentMsg{}
	if !strings.Contains(ansi.Strip(m.preparationProgress(40)), "Step 4/7") {
		t.Fatal("consent must report approval stage")
	}
	m.consent = nil
	m.stop()
	if strings.Contains(ansi.Strip(m.View().Content), "Step ") {
		t.Fatal("progress remained after stopping")
	}
}
