package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func finishLaunch(t *testing.T, m *Model) tea.Cmd {
	t.Helper()
	if m.pendingSSH == nil || m.stage != stageSSHLaunch {
		t.Fatal("OpenSSH launch was not queued")
	}
	var cmd tea.Cmd
	for i := 0; i < launchFrames; i++ {
		_, cmd = m.Update(launchTick{m.generation})
		if cmd == nil {
			t.Fatal("launch stopped before handoff")
		}
		if i < launchFrames-1 && m.pendingSSH == nil {
			t.Fatal("handoff happened before animation finished")
		}
	}
	if m.pendingSSH != nil || !m.preparing {
		t.Fatal("handoff failed to preserve connection state")
	}
	return cmd
}

func TestQuickConnectionLaunchProgress(t *testing.T) {
	m, _ := setup(t)
	_, prepare := m.Update(special(tea.KeyEnter))
	if m.stage != stageSSHPrepare || m.preparationProgress(40) == "" {
		t.Fatal("connection has no initial progress")
	}
	_, tick := m.Update(prepare())
	if tick == nil || m.pendingSSH == nil {
		t.Fatal("missing launch animation")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Opening OpenSSH") || strings.Contains(view, "100%") {
		t.Fatal("incorrect launch status")
	}
	start := m.preparationProgress(40)
	m.Update(launchTick{m.generation})
	if m.preparationProgress(40) == start {
		t.Fatal("launch bar did not advance")
	}
	m.launchFrame = 0
	finishLaunch(t, m)
	m.Update(returned{})
	if m.preparing || m.pendingSSH != nil {
		t.Fatal("connection return left pending launch state")
	}
}

func TestCancelLaunchPreventsLateHandoff(t *testing.T) {
	m, svc := setup(t)
	_, prepare := m.Update(special(tea.KeyEnter))
	ready := prepare()
	m.Update(ready)
	oldGeneration := m.generation
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if svc.ctx.Err() == nil || m.pendingSSH != nil || m.preparing {
		t.Fatal("launch cancellation failed")
	}
	if _, cmd := m.Update(launchTick{oldGeneration}); cmd != nil {
		t.Fatal("late timer launched cancelled connection")
	}
	if _, cmd := m.Update(ready); cmd != nil {
		t.Fatal("late preparation restarted cancelled connection")
	}
	_, next := m.Update(special(tea.KeyEnter))
	m.Update(next())
	if _, cmd := m.Update(launchTick{oldGeneration}); cmd != nil || m.launchFrame != 0 {
		t.Fatal("old timer advanced new connection")
	}
	finishLaunch(t, m)
}
