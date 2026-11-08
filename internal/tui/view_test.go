package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
)

// Set WISHPORT_RENDER_DIR to inspect the actual ANSI views with synthetic data.
func TestDirectoryLayouts(t *testing.T) {
	rows := []inventory.Connection{
		{ID: "1", Name: "example.com", Source: inventory.SourceForge, Kind: inventory.KindForgeSite, Organization: "acme", OrganizationName: "Acme", ServerName: "production-01", Host: "192.0.2.18", Port: 22, User: "acme", Directory: "/home/acme/example.com"},
		{ID: "2", Name: "production-01", Source: inventory.SourceForge, Kind: inventory.KindForgeServer, Organization: "acme", OrganizationName: "Acme", ServerName: "production-01", Host: "192.0.2.18", User: "forge"},
		{ID: "3", Name: "personal.example.com", Source: inventory.SourceForge, Kind: inventory.KindForgeSite, Organization: "personal", OrganizationName: "Personal", ServerName: "personal-01", Host: "192.0.2.80", User: "demo"},
		{ID: "4", Name: "raspberry-pi", Source: inventory.SourceSSHConfig, SSHAlias: "raspberry-pi"},
		{ID: "5", Name: "staging", Source: inventory.SourceYAML, Description: "Preview environment", Host: "192.0.2.20", User: "deploy"},
		{ID: "6", Name: "worker-01", Source: inventory.SourceForge, Kind: inventory.KindForgeServer, Organization: "acme", OrganizationName: "Acme", Host: "192.0.2.40", User: "forge"},
	}
	for _, tc := range []struct {
		name          string
		w, h          int
		dark, consent bool
	}{{"directory", 100, 30, true, false}, {"compact", 80, 24, true, false}, {"narrow", 40, 12, true, false}, {"light", 100, 30, false, false}, {"consent", 100, 30, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{snapshot: inventory.Snapshot{Connections: rows, Status: "Forge cache · updated 2 hours ago"}}
			m := New(context.Background(), svc)
			m.Update(m.Init()())
			m.Update(tea.WindowSizeMsg{Width: tc.w, Height: tc.h})
			m.setTheme(tc.dark)
			// Exercise both renderers at every supported size and theme.
			m.tableMode = true
			tableView := m.View().Content
			if lipgloss.Height(tableView) > tc.h || lipgloss.Width(tableView) > tc.w {
				t.Fatal("table layout overflows terminal")
			}
			m.tableMode = false
			if tc.consent {
				m.consent = &consentMsg{consent: interaction.Consent{Target: rows[0], PublicKeyPath: "/home/demo/.ssh/id_ed25519.pub", Fingerprint: "SHA256:sqWDn72GXfP3Y1rkPzGgCsTa3mq94cO8aLZrkw7R42A"}}
				if !m.consentFits() {
					t.Fatal("consent should fit ordinary terminal")
				}
			}
			view := m.View().Content
			if lipgloss.Height(view) > tc.h {
				t.Fatalf("view too tall: %d", lipgloss.Height(view))
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > tc.w {
					t.Fatal("horizontal overflow")
				}
			}
			if dir := os.Getenv("WISHPORT_RENDER_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, tc.name+".ansi"), []byte(view), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
