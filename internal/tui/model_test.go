package tui

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/interaction"
	"github.com/athphane/wishport/internal/inventory"
)

type fakeService struct {
	snapshot   inventory.Snapshot
	refresh    bool
	commandErr error
	ctx        context.Context
}

func (s *fakeService) Load(ctx context.Context, refresh bool) inventory.Snapshot {
	s.refresh = refresh
	s.ctx = ctx
	return s.snapshot
}
func (s *fakeService) Command(ctx context.Context, c inventory.Connection) (*exec.Cmd, error) {
	s.ctx = ctx
	return exec.Command("true"), s.commandErr
}
func key(text string) tea.KeyPressMsg   { return tea.KeyPressMsg{Code: []rune(text)[0], Text: text} }
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestConsentCannotApproveClippedDetails(t *testing.T) {
	m, _ := setup(t)
	m.preparing = true
	m.consent = &consentMsg{generation: m.generation, consent: interaction.Consent{PublicKeyPath: "/keys/id.pub", Fingerprint: "SHA256:full-fingerprint"}, reply: make(chan bool, 1)}
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	reply := m.consent.reply
	m.Update(key("y"))
	if len(reply) != 0 || m.consent == nil {
		t.Fatal("approved unseen details")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.Update(key("y"))
	if len(reply) != 1 {
		t.Fatal("full-size consent could not be approved")
	}
}

func TestStaleSnapshotSchedulesBackgroundRefresh(t *testing.T) {
	m, _ := setup(t)
	_, cmd := m.Update(loaded{generation: m.generation, snapshot: inventory.Snapshot{Connections: m.snapshot.Connections, NeedsRefresh: true}})
	if cmd == nil || !m.loading || len(m.visible) != 2 {
		t.Fatal("stale cached rows must remain available during refresh")
	}
}
func setup(t *testing.T) (*Model, *fakeService) {
	t.Helper()
	svc := &fakeService{snapshot: inventory.Snapshot{Connections: []inventory.Connection{{ID: "a", Name: "Alpha", Source: inventory.SourceSSHConfig}, {ID: "b", Name: "Beta", Source: inventory.SourceYAML}}}}
	m := New(context.Background(), svc)
	m.Update(m.Init()())
	return m, svc
}
func TestNavigationFilterAndTyping(t *testing.T) {
	m, _ := setup(t)
	m.Update(key("j"))
	if m.selectedID() != "b" {
		t.Fatal("navigation failed")
	}
	m.Update(key("/"))
	m.Update(key("q"))
	if m.input.Value() != "q" {
		t.Fatal("q intercepted while typing")
	}
	m.Update(special(tea.KeyEscape))
	if m.input.Focused() || len(m.visible) != 2 {
		t.Fatal("escape should clear and blur")
	}
	m.Update(key("/"))
	m.Update(key("B"))
	m.Update(key("t"))
	if len(m.visible) != 1 || m.visible[0].ID != "b" {
		t.Fatal("fuzzy filter failed")
	}
}

func TestServerSearchScope(t *testing.T) {
	rows := []inventory.Connection{
		{ID: "server", Source: inventory.SourceForge, Kind: inventory.KindForgeServer, Name: "production-01", ServerName: "production-01"},
		{ID: "site", Source: inventory.SourceForge, Kind: inventory.KindForgeSite, Name: "production.example", ServerName: "production-01"},
		{ID: "wishlist", Source: inventory.SourceWishlist, Kind: inventory.KindHost, Name: "prod-local", SSHAlias: "prod-local"},
		{ID: "ssh", Source: inventory.SourceSSHConfig, Kind: inventory.KindHost, Name: "staging", SSHAlias: "staging"},
		{ID: "yaml", Source: inventory.SourceYAML, Kind: inventory.KindHost, Name: "Shortcut", SSHAlias: "prod-alias"},
		{ID: "metadata", Source: inventory.SourceYAML, Kind: inventory.KindHost, Name: "other", Host: "production.example", User: "production", Organization: "production", Description: "production", SearchTerms: []string{"production", "custom:tag"}},
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"server:prd", []string{"server", "wishlist", "yaml"}},
		{"  SERVER: PrD  ", []string{"server", "wishlist", "yaml"}},
		{"server:", []string{"server", "wishlist", "ssh", "yaml", "metadata"}},
		{"server:stg", []string{"ssh"}},
		{"server:missing", nil},
		{"production", []string{"server", "site", "metadata"}},
		{"custom:tag", []string{"metadata"}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			svc := &fakeService{snapshot: inventory.Snapshot{Connections: rows}}
			m := New(context.Background(), svc)
			m.Update(m.Init()())
			m.input.SetValue(tc.query)
			m.filter("")
			var got []string
			for _, c := range m.visible {
				got = append(got, c.ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServerPrefixTypingRefreshAndEscape(t *testing.T) {
	m, _ := setup(t)
	m.Update(key("/"))
	for _, r := range "server:Bt" {
		m.Update(key(string(r)))
	}
	if m.selectedID() != "b" || len(m.visible) != 1 || !m.input.Focused() {
		t.Fatal("server prefix was not applied while typing")
	}
	updated := inventory.Snapshot{Connections: []inventory.Connection{
		{ID: "site", Kind: inventory.KindForgeSite, Name: "Beta site", ServerName: "Beta"},
		{ID: "b", Kind: inventory.KindHost, Name: "Beta"},
		{ID: "a", Kind: inventory.KindHost, Name: "Alpha"},
	}}
	m.Update(loaded{generation: m.generation, snapshot: updated})
	if m.selectedID() != "b" || len(m.visible) != 1 || m.input.Value() != "server:Bt" {
		t.Fatal("refresh lost server scope or selection")
	}
	if !strings.Contains(m.View().Content, "server names only") {
		t.Fatal("scope is not visible")
	}
	m.Update(special(tea.KeyEscape))
	if len(m.visible) != 3 || m.input.Value() != "" {
		t.Fatal("escape did not clear server scope")
	}
}
func TestRefreshSelectionAndCancellation(t *testing.T) {
	m, svc := setup(t)
	m.Update(key("j"))
	_, cmd := m.Update(key("R"))
	if !m.loading {
		t.Fatal("expected loading")
	}
	message := cmd()
	if !svc.refresh {
		t.Fatal("refresh flag missing")
	}
	m.Update(message)
	if m.selectedID() != "b" {
		t.Fatal("selection not retained")
	}
	_, cmd = m.Update(key("R"))
	stale := cmd()
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m.Update(stale)
	if m.loading || m.status != "Cancelled" {
		t.Fatal("stale result overwrote cancellation")
	}
	if svc.ctx.Err() != context.Canceled {
		t.Fatal("load context not cancelled")
	}
}

func TestControlRefreshWhileSearching(t *testing.T) {
	m, svc := setup(t)
	m.Update(special(tea.KeyTab))
	m.Update(key("t"))
	m.Update(key("/"))
	for _, r := range "server:Bt" {
		m.Update(key(string(r)))
	}
	id := m.selectedID()
	_, refresh := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if refresh == nil || !m.loading {
		t.Fatal("Ctrl+R did not start refresh")
	}
	m.Update(refresh())
	if !svc.refresh || m.input.Value() != "server:Bt" || !m.input.Focused() || m.selectedID() != id || m.tab != tabLocal || !m.tableMode {
		t.Fatal("refresh must bypass cache and preserve search, tab, view, and selection")
	}
	if !strings.Contains(m.helpView(90), "refresh Forge") {
		t.Fatal("refresh shortcut missing from help")
	}
	m.preparing = true
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("refresh interrupted connection preparation")
	}
}
func TestPrepareErrorReturnAndFreeze(t *testing.T) {
	m, svc := setup(t)
	svc.commandErr = errors.New("no credentials")
	_, cmd := m.Update(special(tea.KeyEnter))
	m.Update(key("j"))
	if m.selectedID() != "a" {
		t.Fatal("moved while preparing")
	}
	m.Update(cmd())
	if m.preparing || !strings.Contains(m.status, "no credentials") {
		t.Fatal("preparation error absent")
	}
	svc.commandErr = nil
	_, cmd = m.Update(special(tea.KeyEnter))
	_, run := m.Update(cmd())
	if run == nil || !m.preparing {
		t.Fatal("expected OpenSSH launch animation")
	}
	finishLaunch(t, m)
	m.Update(returned{errors.New("exit status 1")})
	if m.preparing || !strings.Contains(m.status, "exit status 1") {
		t.Fatal("return not handled")
	}
}
func TestResponsiveWarningsAndSanitization(t *testing.T) {
	m, _ := setup(t)
	m.snapshot.Warnings = []inventory.Warning{{Source: "forge", Message: "failed\x1b[2J\n"}}
	m.Update(tea.WindowSizeMsg{Width: 35, Height: 10})
	content := m.View().Content
	if !strings.Contains(content, "Warning") || strings.Contains(content, "\x1b[2J") {
		t.Fatalf("unsafe or missing warning: %q", content)
	}
	m.snapshot.Connections = nil
	m.filter("")
	if !strings.Contains(m.View().Content, "No connections") {
		t.Fatal("empty state missing")
	}
	m.loading = true
	if !strings.Contains(m.View().Content, "Loading") {
		t.Fatal("loading state missing")
	}
}

type progressiveFake struct {
	fakeService
	initial      inventory.Snapshot
	needsRefresh bool
}

func (s *progressiveFake) LoadInitial(ctx context.Context) (inventory.Snapshot, bool) {
	s.ctx = ctx
	return s.initial, s.needsRefresh
}
func TestProgressiveResultsRemainUsableAndStable(t *testing.T) {
	svc := &progressiveFake{initial: inventory.Snapshot{Connections: []inventory.Connection{{ID: "local", Name: "Local"}}}, needsRefresh: true}
	svc.snapshot = inventory.Snapshot{Connections: []inventory.Connection{{ID: "remote", Name: "Remote"}, {ID: "local", Name: "Local"}}}
	m := New(context.Background(), svc)
	_, background := m.Update(m.Init()())
	if background == nil || !m.loading || m.selectedID() != "local" {
		t.Fatal("missing initial results and background refresh")
	}
	if !strings.Contains(m.View().Content, "Local") {
		t.Fatal("loading hides local connections")
	}
	m.Update(key("/"))
	m.Update(key("l"))
	m.Update(background())
	if m.selectedID() != "local" || m.input.Value() != "l" || !m.input.Focused() {
		t.Fatal("refresh discarded search or selection")
	}
}
func TestProgressiveConnectCancelsRefreshAndIgnoresLateResults(t *testing.T) {
	svc := &progressiveFake{initial: inventory.Snapshot{Connections: []inventory.Connection{{ID: "local", Name: "Local"}, {ID: "second", Name: "Second"}}}, needsRefresh: true}
	m := New(context.Background(), svc)
	_, background := m.Update(m.Init()())
	late := background()
	refreshCtx := svc.ctx
	m.Update(key("/"))
	m.Update(special(tea.KeyDown))
	if m.selectedID() != "second" {
		t.Fatal("focused arrow did not navigate")
	}
	_, prepare := m.Update(special(tea.KeyEnter))
	if prepare == nil || !m.preparing || m.input.Focused() {
		t.Fatal("filter enter should connect")
	}
	if refreshCtx.Err() != context.Canceled {
		t.Fatal("refresh not cancelled before preparation")
	}
	m.Update(late)
	if m.selectedID() != "second" || !m.preparing {
		t.Fatal("late refresh changed pending connection")
	}
}
func TestHelpEscapeAndUppercaseQuit(t *testing.T) {
	m, _ := setup(t)
	m.Update(key("?"))
	m.Update(special(tea.KeyEscape))
	if m.help {
		t.Fatal("escape failed to close help")
	}
	_, quit := m.Update(key("Q"))
	if quit == nil {
		t.Fatal("Q did not quit")
	}
	m.status = "Important failure"
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 4})
	if !strings.Contains(m.View().Content, "Important failure") {
		t.Fatal("small view hides status")
	}
}

type interactiveFake struct {
	fakeService
	consent   interaction.Consent
	approved  chan bool
	completed chan struct{}
}

func (s *interactiveFake) Connect(ctx context.Context, c inventory.Connection, confirm interaction.Confirm, progress interaction.Progress) (*exec.Cmd, error) {
	defer close(s.completed)
	consent := s.consent
	consent.Target = c
	yes, err := confirm(ctx, consent)
	if err != nil {
		return nil, err
	}
	s.approved <- yes
	if !yes {
		return nil, errors.New("installation refused")
	}
	progress("Submitted key installation")
	return exec.Command("true"), nil
}
func interactiveSetup(t *testing.T) (*Model, *interactiveFake, tea.Cmd) {
	t.Helper()
	s := &interactiveFake{consent: interaction.Consent{PublicKeyPath: "/keys/id.pub", Fingerprint: "SHA256:test", Message: "Install key?"}, approved: make(chan bool, 1), completed: make(chan struct{})}
	s.snapshot = inventory.Snapshot{Connections: []inventory.Connection{{ID: "a", Name: "Target", Organization: "Acme", ServerName: "Web", Host: "192.0.2.1", User: "deploy"}}}
	m := New(context.Background(), s)
	m.Update(m.Init()())
	_, cmd := m.Update(special(tea.KeyEnter))
	return m, s, cmd
}
func TestInteractiveConsentApprovalProgressAndReturn(t *testing.T) {
	m, s, cmd := interactiveSetup(t)
	m.Update(cmd())
	for _, want := range []string{"Acme", "Web", "192.0.2.1", "deploy", "/keys/id.pub", "SHA256:test"} {
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("consent missing %s", want)
		}
	}
	_, none := m.Update(special(tea.KeyEnter))
	if none != nil || m.consent == nil {
		t.Fatal("enter silently approved")
	}
	_, next := m.Update(key("y"))
	m.Update(next())
	if !m.submitted || !strings.Contains(m.stage, "Submitted") {
		t.Fatal("progress absent")
	}
	if !<-s.approved {
		t.Fatal("approval not delivered")
	}
	// A progress update schedules the next channel receive.
	_, next = m.Update(progressMsg{m.generation, "Waiting for installation"})
	msg := next()
	_, run := m.Update(msg)
	if run == nil {
		t.Fatal("SSH launch absent")
	}
	finishLaunch(t, m)
	m.Update(returned{})
	if m.preparing || m.status != "Connection closed" {
		t.Fatal("return did not restore picker")
	}
	<-s.completed
}
func TestInteractiveConsentRefusalAndCancel(t *testing.T) {
	for _, refuse := range []string{"n", "esc", "cancel"} {
		t.Run(refuse, func(t *testing.T) {
			m, s, cmd := interactiveSetup(t)
			m.Update(cmd())
			if refuse == "cancel" {
				m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				<-s.completed
				if m.consent != nil || m.preparing || m.status != "Cancelled" {
					t.Fatal("cancel did not clear consent")
				}
				return
			}
			press := key("n")
			if refuse == "esc" {
				press = special(tea.KeyEscape)
			}
			_, next := m.Update(press)
			m.Update(next())
			<-s.completed
			if <-s.approved || m.preparing || !strings.Contains(m.status, "refused") {
				t.Fatal("refusal lost")
			}
		})
	}
}
func TestCancelSubmittedProgressIgnoresStaleMessages(t *testing.T) {
	m, s, cmd := interactiveSetup(t)
	m.Update(cmd())
	_, next := m.Update(key("y"))
	msg := next()
	_, wait := m.Update(msg)
	generation := m.generation
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	wait()
	<-s.completed
	m.Update(progressMsg{generation, "late"})
	m.Update(prepared{generation, nil, errors.New("canceled")})
	if !strings.Contains(m.status, "installation may still finish") || m.stage == "late" {
		t.Fatal("lost cancellation warning or accepted stale message")
	}
}
