// Package tui implements the interactive connection picker.
package tui

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/inventory"
)

type Service interface {
	Load(context.Context, bool) inventory.Snapshot
	Command(context.Context, inventory.Connection) (*exec.Cmd, error)
}

// ProgressiveService provides local/cached results before a background refresh.
// The bool reports whether Load(ctx, false) should follow the initial result.
type ProgressiveService interface {
	LoadInitial(context.Context) (inventory.Snapshot, bool)
}

type initialLoaded struct {
	generation int
	snapshot   inventory.Snapshot
	refresh    bool
}

type loaded struct {
	generation int
	snapshot   inventory.Snapshot
}
type prepared struct {
	generation int
	command    *exec.Cmd
	err        error
}
type returned struct{ err error }

type launchTick struct{ generation int }

const (
	stageSSHPrepare = "Preparing SSH connection"
	stageSSHLaunch  = "Opening OpenSSH"
	launchFrames    = 8
)

type inventoryTab int

const (
	tabAll inventoryTab = iota
	tabLocal
	tabForge
	tabDiscovered
	tabCount
)

func (tab inventoryTab) label() string {
	return [...]string{"All", "Local", "Forge", "Discovered"}[tab]
}

func (tab inventoryTab) includes(c inventory.Connection) bool {
	switch tab {
	case tabLocal:
		return c.Source == inventory.SourceSSHConfig || c.Source == inventory.SourceWishlist || c.Source == inventory.SourceYAML
	case tabForge:
		return c.Source == inventory.SourceForge
	case tabDiscovered:
		return c.Source == inventory.SourceTailscale || c.Source == inventory.SourceMDNS || c.Source == inventory.SourceSRV
	default:
		return true
	}
}

type Model struct {
	ctx                      context.Context
	service                  Service
	input                    textinput.Model
	snapshot                 inventory.Snapshot
	visible                  []inventory.Connection
	selected                 int
	tab                      inventoryTab
	tableMode                bool
	tabSelections            [tabCount]string
	width, height            int
	loading, preparing, help bool
	generation               int
	cancel                   context.CancelFunc
	status                   string
	operationCtx             context.Context
	events                   chan tea.Msg
	consent                  *consentMsg
	stage                    string
	pendingSSH               *exec.Cmd
	launchFrame              int
	submitted                bool
	spinner                  spinner.Model
	spinnerStarted           bool
	dark                     bool
	refreshInterval          time.Duration
}

type refreshTick struct{}
type addressCopied struct {
	address string
	err     error
}

func (m *Model) SetRefreshInterval(interval time.Duration) { m.refreshInterval = interval }
func (m *Model) refreshLater() tea.Cmd {
	return tea.Tick(m.refreshInterval, func(time.Time) tea.Msg { return refreshTick{} })
}

func New(ctx context.Context, svc Service) *Model {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "Search this tab… or server:name"
	input.CharLimit = 256
	input.SetVirtualCursor(true)
	m := &Model{ctx: ctx, service: svc, input: input, width: 80, height: 24, dark: true, spinner: spinner.New(spinner.WithSpinner(spinner.Dot))}
	m.setTheme(true)
	return m
}
func (m *Model) Init() tea.Cmd {
	cmd := m.initialLoad()
	if m.refreshInterval > 0 {
		return tea.Batch(cmd, m.refreshLater())
	}
	return cmd
}

func (m *Model) initialLoad() tea.Cmd {
	if svc, ok := m.service.(ProgressiveService); ok {
		m.stop()
		m.generation++
		generation := m.generation
		ctx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.loading = true
		return func() tea.Msg {
			snapshot, refresh := svc.LoadInitial(ctx)
			return initialLoaded{generation, snapshot, refresh}
		}
	}
	return m.load(false)
}
func (m *Model) load(refresh bool) tea.Cmd {
	m.stop()
	m.submitted = false
	m.stage = ""
	m.generation++
	generation := m.generation
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.loading = true
	m.status = ""
	return func() tea.Msg { return loaded{generation, m.service.Load(ctx, refresh)} }
}
func (m *Model) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.loading = false
	m.preparing = false
	m.consent = nil
	m.pendingSSH = nil
	m.launchFrame = 0
}
func (m *Model) selectedID() string {
	if m.selected >= 0 && m.selected < len(m.visible) {
		return m.visible[m.selected].ID
	}
	return ""
}
func (m *Model) filter(id string) {
	m.visible = nil
	query, serversOnly := searchQuery(m.input.Value())
	for _, c := range m.snapshot.Connections {
		if !m.tab.includes(c) {
			continue
		}
		match := false
		if serversOnly {
			// Sites deliberately stay out of server-only results, even when
			// their parent server matches. Match names, never other metadata.
			if c.Kind != inventory.KindForgeSite {
				for _, name := range []string{c.Name, c.ServerName, c.SSHAlias} {
					if name != "" && fuzzy(query, strings.ToLower(name)) {
						match = true
						break
					}
				}
			}
		} else {
			match = fuzzy(query, strings.ToLower(inventory.SearchText(c)))
		}
		if match {
			m.visible = append(m.visible, c)
		}
	}
	m.selected = 0
	for i, c := range m.visible {
		if c.ID == id {
			m.selected = i
			break
		}
	}
}

func (m *Model) switchTab(delta inventoryTab) {
	id := m.selectedID()
	m.tabSelections[m.tab] = id
	m.tab = (m.tab + delta + tabCount) % tabCount
	if m.tab == tabDiscovered && !m.hasDiscovery() {
		m.tab = (m.tab + delta + tabCount) % tabCount
	}
	if remembered := m.tabSelections[m.tab]; remembered != "" {
		id = remembered
	}
	m.filter(id)
}

func (m *Model) hasDiscovery() bool {
	if service, ok := m.service.(interface{ DiscoveryEnabled() bool }); ok && service.DiscoveryEnabled() {
		return true
	}
	for _, c := range m.snapshot.Connections {
		if tabDiscovered.includes(c) {
			return true
		}
	}
	return false
}

func (m *Model) movePage(direction int) {
	rows := max(1, (m.height-14)/3)
	if m.tableMode {
		rows = max(1, m.height-16)
	} else if m.height < 18 {
		rows = max(1, m.height-8)
	}
	m.selected = max(0, min(len(m.visible)-1, m.selected+direction*rows))
}

func (m *Model) copyAddress() tea.Cmd {
	if len(m.visible) == 0 {
		return nil
	}
	c := m.visible[m.selected]
	return func() tea.Msg {
		if service, ok := m.service.(interface {
			Address(context.Context, inventory.Connection) (string, error)
		}); ok {
			address, err := service.Address(m.ctx, c)
			return addressCopied{address, err}
		}
		address := c.Host
		if address == "" {
			address = c.SSHAlias
		}
		return addressCopied{address: address}
	}
}

func searchQuery(input string) (query string, serversOnly bool) {
	query = strings.ToLower(strings.TrimSpace(input))
	if strings.HasPrefix(query, "server:") {
		return strings.TrimSpace(strings.TrimPrefix(query, "server:")), true
	}
	return query, false
}

func fuzzy(query, text string) bool {
	remaining := []rune(query)
	for _, r := range text {
		if len(remaining) > 0 && r == remaining[0] {
			remaining = remaining[1:]
		}
	}
	return len(remaining) == 0
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshTick:
		if m.refreshInterval <= 0 {
			return m, nil
		}
		if m.loading || m.preparing {
			return m, m.refreshLater()
		}
		return m, tea.Batch(m.load(true), m.refreshLater())
	case addressCopied:
		if msg.err != nil || msg.address == "" {
			m.status = "Cannot copy address"
			return m, nil
		}
		m.status = "Address sent to terminal clipboard"
		return m, tea.SetClipboard(clean(msg.address))
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, m.contentWidth()-6))
		if !m.spinnerStarted {
			m.spinnerStarted = true
			return m, tea.Batch(m.spinner.Tick, tea.RequestBackgroundColor)
		}
	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case initialLoaded:
		if msg.generation != m.generation {
			return m, nil
		}
		id := m.selectedID()
		m.stop()
		m.snapshot = msg.snapshot
		m.filter(id)
		if msg.snapshot.NeedsRefresh {
			return m, m.load(true)
		}
		if msg.refresh {
			return m, m.load(false)
		}
	case loaded:
		if msg.generation != m.generation {
			return m, nil
		}
		id := m.selectedID()
		m.stop()
		m.snapshot = msg.snapshot
		m.filter(id)
		if msg.snapshot.NeedsRefresh {
			return m, m.load(true)
		}
	case progressMsg:
		if msg.generation != m.generation || !m.preparing {
			return m, nil
		}
		m.stage = clean(msg.text)
		if strings.Contains(strings.ToLower(m.stage), "submitted") {
			m.submitted = true
		}
		return m, m.listen()
	case consentMsg:
		if msg.generation != m.generation || !m.preparing {
			return m, nil
		}
		m.consent = &msg
		return m, nil
	case prepared:
		if msg.generation != m.generation || !m.preparing {
			return m, nil
		}
		if msg.err != nil {
			m.stop()
			m.status = "Cannot connect: " + clean(msg.err.Error())
			return m, nil
		}
		if msg.command == nil {
			m.stop()
			m.status = "Cannot connect: no command returned"
			return m, nil
		}
		m.pendingSSH = msg.command
		m.launchFrame = 0
		m.stage = stageSSHLaunch
		return m, m.nextLaunchFrame()
	case launchTick:
		if msg.generation != m.generation || !m.preparing || m.pendingSSH == nil {
			return m, nil
		}
		m.launchFrame++
		if m.launchFrame < launchFrames {
			return m, m.nextLaunchFrame()
		}
		command := m.pendingSSH
		m.pendingSSH = nil
		// Preserve the command's context through terminal handoff. This short
		// animation describes launching OpenSSH, not remote authentication.
		return m, tea.ExecProcess(command, func(err error) tea.Msg { return returned{err} })
	case returned:
		m.stop()
		m.status = "Connection closed"
		if msg.err != nil {
			m.status = "Connection ended: " + clean(msg.err.Error())
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			if m.loading || m.preparing {
				m.stop()
				m.generation++
				m.status = m.cancelStatus()
				return m, nil
			}
			return m, tea.Quit
		}
		if m.consent != nil {
			switch key {
			case "y":
				if !m.consentFits() {
					return m, nil
				}
				m.consent.reply <- true
				m.consent = nil
				return m, m.listen()
			case "n", "esc":
				m.consent.reply <- false
				m.consent = nil
				return m, m.listen()
			}
			return m, nil
		}
		if m.preparing {
			return m, nil
		}
		// Tab also works during search; left/right still move the input cursor.
		switch key {
		case "ctrl+r":
			return m, m.load(true)
		case "tab":
			m.switchTab(1)
			return m, nil
		case "shift+tab":
			m.switchTab(-1)
			return m, nil
		}
		if m.input.Focused() {
			switch key {
			case "esc":
				m.input.Blur()
				m.input.Reset()
				m.filter(m.selectedID())
				return m, nil
			case "enter":
				m.input.Blur()
				return m, m.connect()
			case "down":
				if m.selected < len(m.visible)-1 {
					m.selected++
				}
				return m, nil
			case "up":
				if m.selected > 0 {
					m.selected--
				}
				return m, nil
			case "pgdown":
				m.movePage(1)
				return m, nil
			case "pgup":
				m.movePage(-1)
				return m, nil
			}
			id := m.selectedID()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.filter(id)
			return m, cmd
		}
		switch key {
		case "right":
			m.switchTab(1)
		case "left":
			m.switchTab(-1)
		case "q", "Q":
			m.stop()
			return m, tea.Quit
		case "/", "ctrl+k":
			return m, m.input.Focus()
		case "esc":
			if m.help {
				m.help = false
				return m, nil
			}
			m.input.Reset()
			m.filter(m.selectedID())
		case "R":
			return m, m.load(true)
		case "?":
			m.help = !m.help
		case "t":
			m.tableMode = !m.tableMode
			m.input.SetWidth(max(1, m.contentWidth()-6))
		case "down", "j":
			if m.selected < len(m.visible)-1 {
				m.selected++
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "y":
			return m, m.copyAddress()
		case "pgdown", "f":
			m.movePage(1)
		case "pgup", "b":
			m.movePage(-1)
		case "home", "g":
			m.selected = 0
		case "end", "G":
			m.selected = max(0, len(m.visible)-1)
		case "enter", "o":
			return m, m.connect()
		}
	default:
		if m.input.Focused() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) connect() tea.Cmd {
	if len(m.visible) == 0 {
		return nil
	}
	connection := m.visible[m.selected]
	if connection.Unavailable != "" {
		m.status = clean(connection.Unavailable)
		return nil
	}
	// Cancel refresh before preparing so late results cannot move the target.
	m.stop()
	m.preparing = true
	m.status = ""
	m.generation++
	generation := m.generation
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.stage = stageSSHPrepare
	m.submitted = false
	if svc, ok := m.service.(InteractiveService); ok {
		m.operationCtx = ctx
		m.events = make(chan tea.Msg)
		return interactiveCommand(ctx, generation, svc, connection, m.events)
	}
	return func() tea.Msg {
		command, err := m.service.Command(ctx, connection)
		return prepared{generation, command, err}
	}
}

func (m *Model) nextLaunchFrame() tea.Cmd {
	generation := m.generation
	return tea.Tick(25*time.Millisecond, func(time.Time) tea.Msg { return launchTick{generation} })
}

// clean removes terminal control characters, including escape sequences' introducers.
// The remaining escape payload is harmless printable text.
func clean(s string) string { return inventory.DisplayText(s) }
