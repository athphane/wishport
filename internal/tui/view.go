package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	bkey "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) leftGutter() int {
	if m.width < 60 {
		return 1
	}
	return 3
}
func (m *Model) contentWidth() int {
	limit := 92
	if m.tableMode {
		limit = 124
	}
	return max(1, min(limit, m.width-2*m.leftGutter()))
}
func clipped(s string, width int) string { return ansi.Truncate(s, max(1, width), "…") }
func binding(k, desc string) bkey.Binding {
	return bkey.NewBinding(bkey.WithKeys(k), bkey.WithHelp(k, desc))
}

func (m *Model) helpView(width int) string {
	h := help.New()
	h.SetWidth(width)
	h.Styles = help.DefaultStyles(m.dark)
	p := colors(m.dark)
	h.Styles.ShortKey = ink(p.muted)
	h.Styles.ShortDesc = ink(p.dim)
	h.Styles.FullKey = ink(p.muted)
	h.Styles.FullDesc = ink(p.dim)
	if m.preparing {
		return h.ShortHelpView([]bkey.Binding{binding("ctrl+c", "cancel")})
	}
	if m.input.Focused() {
		return h.ShortHelpView([]bkey.Binding{binding("ctrl+r", "refresh Forge"), binding("tab", "tabs"), binding("↑/↓", "move"), binding("enter", "connect"), binding("esc", "clear search")})
	}
	if m.help {
		return h.FullHelpView([][]bkey.Binding{{binding("↑/↓ · j/k", "move"), binding("enter/o", "connect")}, {binding("/ · ctrl+k", "search"), binding("ctrl+r / R", "refresh inventory")}, {binding("tab / shift+tab", "next / previous tab"), binding("←/→", "tabs outside search")}, {binding("t", "table / list"), binding("y", "copy address")}, {binding("pgup/pgdown · b/f", "page"), binding("home/end · g/G", "first / last")}, {binding("server:name", "server names only"), binding("esc", "close help"), binding("q", "quit")}})
	}
	return h.ShortHelpView([]bkey.Binding{binding("ctrl+r", "refresh Forge"), binding("tab", "tabs"), binding("t", "table / list"), binding("↑/↓", "move"), binding("enter", "connect"), binding("/", "search"), binding("?", "help"), binding("q", "quit")})
}

func (m *Model) tabsView(width int) string {
	p := colors(m.dark)
	lightDark := lipgloss.LightDark(m.dark)
	background := lightDark(lipgloss.Color("#EAE5F0"), lipgloss.Color("#292432"))
	activeInk := lightDark(lipgloss.Color("#FFFFFF"), lipgloss.Color("#21172F"))
	var tabs []string
	for tab := tabAll; tab < tabCount; tab++ {
		if tab == tabDiscovered && !m.hasDiscovery() {
			continue
		}
		style := ink(p.muted).Background(background).Width(9).Align(lipgloss.Center).Padding(0, 1)
		if tab == tabDiscovered {
			style = style.Width(12)
		}
		if width < 42 {
			style = style.Width(len(tab.label()) + 2)
		}
		if tab == m.tab {
			style = style.Foreground(activeInk).Background(p.accent).Bold(true)
		}
		tabs = append(tabs, style.Render(tab.label()))
	}
	return clipped(strings.Join(tabs, " "), width)
}

type directoryItem struct {
	c     inventory.Connection
	title string
}

func (i directoryItem) Title() string       { return i.title }
func (i directoryItem) FilterValue() string { return inventory.SearchText(i.c) }
func (i directoryItem) Description() string {
	c := i.c
	parts := []string{sourceLabel(c)}
	if c.Source == inventory.SourceForge {
		org := c.OrganizationName
		if org == "" {
			org = c.Organization
		}
		if org != "" {
			parts = append(parts, org)
		}
		if c.Kind == inventory.KindForgeSite && c.ServerName != "" {
			parts = append(parts, c.ServerName)
		}
	} else if c.Description != "" {
		parts = append(parts, c.Description)
	}
	if endpoint(c) != "" {
		parts = append(parts, endpoint(c))
	}
	if c.Unavailable != "" {
		parts = append(parts, "unavailable")
	}
	return clean(strings.Join(parts, " · "))
}
func sourceLabel(c inventory.Connection) string {
	switch c.Source {
	case inventory.SourceSSHConfig:
		return "SSH config"
	case inventory.SourceWishlist:
		return "Wishlist"
	case inventory.SourceTailscale:
		return "Tailscale"
	case inventory.SourceMDNS:
		return "mDNS"
	case inventory.SourceSRV:
		return "DNS SRV"
	case inventory.SourceForge:
		if c.Kind == inventory.KindForgeSite {
			return "Forge site"
		}
		return "Forge server"
	default:
		return "YAML"
	}
}
func endpoint(c inventory.Connection) string {
	host := c.Host
	if c.SSHAlias != "" {
		host = c.SSHAlias
	}
	if host == "" {
		return ""
	}
	if c.User != "" {
		host = c.User + "@" + host
	}
	if c.Port != 0 && c.Port != 22 {
		host += ":" + strconv.Itoa(c.Port)
	}
	return host
}
func matchedTitle(name, query string, selected bool, p palette) string {
	name = clean(name)
	if query == "" {
		return name
	}
	wanted := []rune(strings.ToLower(query))
	var matches []int
	for i, r := range []rune(strings.ToLower(name)) {
		if len(wanted) > 0 && r == wanted[0] {
			matches = append(matches, i)
			wanted = wanted[1:]
		}
	}
	if len(wanted) > 0 {
		return name
	}
	base := ink(p.text)
	if selected {
		base = ink(p.accent).Bold(true)
	}
	return lipgloss.StyleRunes(name, matches, base.Underline(true), base)
}

func (m *Model) directoryView(width, height int) (string, string) {
	p := colors(m.dark)
	query, serversOnly := searchQuery(m.input.Value())
	if len(m.visible) == 0 {
		title, description := "No connections yet", "Add a host to ~/.ssh/config or connect your Forge account."
		if m.loading {
			title = "Loading connections…"
			description = "Your local hosts will appear first."
		} else if m.input.Value() != "" {
			title = "No matching connections"
			description = "Try a server name, domain, organization, or SSH user."
			if serversOnly {
				title = "No matching servers"
				description = "Try another server name, or remove server: to search everything."
			}
		}
		if !m.loading && m.input.Value() == "" {
			switch m.tab {
			case tabLocal:
				title = "No local connections"
				description = "Add a host to SSH config, Wishlist config, or wishport YAML."
			case tabForge:
				title = "No Forge connections"
				description = "Connect your Forge account with wishport login, or press R to refresh."
			case tabDiscovered:
				title = "No discovered connections"
				description = "Check enabled discovery sources and press Ctrl+R to refresh."
			}
		}
		body := ink(p.text).Render(title)
		if height >= 4 {
			body += "\n\n" + ink(p.muted).Render(ansi.Hardwrap(description, max(1, width-6), false))
		}
		return lipgloss.NewStyle().PaddingLeft(2).Width(width).Height(height).MaxHeight(height).Render(body), ""
	}
	if m.tableMode && width >= 58 && height >= 4 {
		return m.tableView(width, height)
	}
	items := make([]list.Item, len(m.visible))
	for i, c := range m.visible {
		items[i] = directoryItem{c, matchedTitle(c.Name, query, i == m.selected, p)}
	}
	d := list.NewDefaultDelegate()
	d.Styles = list.NewDefaultItemStyles(m.dark)
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(p.text)
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(p.muted)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(p.accent).BorderForeground(p.accent).Bold(true)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(p.accentSoft).BorderForeground(p.accent)
	d.SetSpacing(1)
	if m.height < 18 {
		d.ShowDescription = false
		d.SetHeight(1)
		d.SetSpacing(0)
	}
	l := list.New(items, d, width, height)
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Select(m.selected)
	l.Paginator.ActiveDot = ink(p.accent).Render("● ")
	l.Paginator.InactiveDot = ink(p.dim).Render("○ ")
	pagination := ""
	if l.Paginator.TotalPages > 1 {
		pagination = l.Paginator.View()
	}
	return l.View(), pagination
}

func (m *Model) View() tea.View {
	width := m.contentWidth()
	p := colors(m.dark)
	if m.consent != nil {
		content := m.consentView()
		if !m.consentFits() {
			content = ink(p.amber).Render(ansi.Hardwrap("Resize the terminal to review all key details before approving.\nn / esc refuse · ctrl+c cancel", max(1, m.width-2), true))
		}
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, content))
		v.AltScreen = true
		return v
	}
	// Very small windows retain the active status instead of clipping it beneath chrome.
	if m.height < 8 {
		message := m.status
		if message == "" {
			message = "No connections"
			if len(m.visible) > 0 {
				message = clean(m.visible[m.selected].Name)
			}
		}
		content := ink(p.accent).Bold(true).Render("wishport · "+m.tab.label()) + "\n" + clipped(message, width) + "\n" + m.helpView(width)
		v := tea.NewView(lipgloss.NewStyle().MaxHeight(max(1, m.height)).Render(content))
		v.AltScreen = true
		return v
	}
	badgeInk := lipgloss.Color("#21172F")
	if !m.dark {
		badgeInk = lipgloss.Color("#FFFFFF")
	}
	badge := lipgloss.NewStyle().Foreground(badgeInk).Background(p.accent).Bold(true).Padding(0, 1).Render("WISHPORT")
	title := badge
	if width > 52 {
		title += "  " + ink(p.muted).Render("Your servers, one place.")
	}
	if m.loading || m.preparing {
		title += "  " + m.spinner.View()
	}
	tabs := m.tabsView(max(1, width-2))
	header := lipgloss.NewStyle().PaddingLeft(2).Render(title + "\n\n" + tabs + "\n\n" + m.input.View())
	count := fmt.Sprintf("%d connections", len(m.visible))
	if m.input.Value() != "" {
		count = fmt.Sprintf("%d matches", len(m.visible))
	}
	if _, serversOnly := searchQuery(m.input.Value()); serversOnly {
		count = fmt.Sprintf("%d matches · server names only", len(m.visible))
	}
	if m.loading && len(m.visible) > 0 {
		count += " · refreshing"
	}
	header += "\n" + lipgloss.NewStyle().PaddingLeft(2).Foreground(p.dim).Render(count) + "\n"
	foot := []string{}
	if m.preparing {
		stage := m.stage
		if stage == "" {
			stage = "Preparing connection…"
		}
		foot = append(foot, m.spinner.View()+" "+ink(p.accent).Render(clipped(stage, width-3)))
		if m.height >= 18 {
			if bar := m.preparationProgress(width - 2); bar != "" {
				foot = append(foot, bar)
			}
		}
	} else if m.status != "" {
		statusInk := p.green
		if strings.HasPrefix(m.status, "Cannot") || strings.HasPrefix(m.status, "Connection ended") {
			statusInk = p.red
		}
		if strings.HasPrefix(m.status, "Cancelled") {
			statusInk = p.amber
		}
		foot = append(foot, ink(statusInk).Render(clipped(m.status, width)))
	}
	if len(m.snapshot.Warnings) > 0 {
		w := m.snapshot.Warnings[0]
		text := "Warning · " + clean(w.Source) + ": " + clean(w.Message)
		if len(m.snapshot.Warnings) > 1 {
			text += fmt.Sprintf(" (+%d more)", len(m.snapshot.Warnings)-1)
		}
		foot = append(foot, ink(p.amber).Render(clipped(text, width)))
	}
	if m.height >= 18 && len(m.visible) > 0 {
		c := m.visible[m.selected]
		detail := endpoint(c)
		if c.Directory != "" {
			detail += "  →  " + c.Directory
		}
		if c.Unavailable != "" {
			detail = c.Unavailable
		}
		if detail != "" {
			foot = append(foot, ink(p.muted).Render(clipped(clean(detail), width)))
		}
		if c.Link != nil && c.Link.URL != "" {
			foot = append(foot, ink(p.accentSoft).Render(clipped(clean(c.Link.Name+" "+c.Link.URL), width-2)))
		}
	}
	if m.height >= 18 && m.snapshot.Status != "" {
		foot = append(foot, ink(p.dim).Render(clipped(clean(m.snapshot.Status), width)))
	}
	foot = append(foot, m.helpView(width))
	footer := lipgloss.NewStyle().Width(width).PaddingLeft(2).Render(strings.Join(foot, "\n"))
	// Reserve one line for pagination and one blank line between list and footer.
	bodyHeight := max(1, m.height-2-lipgloss.Height(header)-lipgloss.Height(footer)-2)
	if m.height < 14 {
		header = lipgloss.NewStyle().PaddingLeft(2).Render(tabs + "\n" + m.input.View())
		bodyHeight = max(1, m.height-2-lipgloss.Height(header)-lipgloss.Height(footer)-1)
	}
	body, pagination := m.directoryView(width, bodyHeight)
	dividerWidth := max(1, width-2)
	divider := ink(p.dim).Render(strings.Repeat("─", dividerWidth))
	if pagination != "" {
		divider = pagination + ink(p.dim).Render(strings.Repeat("─", max(0, dividerWidth-lipgloss.Width(pagination))))
	}
	divider = lipgloss.NewStyle().PaddingLeft(2).Render(divider)
	content := header + "\n" + body + "\n" + divider + "\n" + footer
	content = lipgloss.NewStyle().Width(width).MaxHeight(max(1, m.height-2)).Render(content)
	content = lipgloss.NewStyle().PaddingLeft(m.leftGutter()).Render("\n" + content)
	v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, content))
	v.AltScreen = true
	return v
}

func (m *Model) consentView() string {
	p := colors(m.dark)
	c := m.consent.consent
	t := c.Target
	width := max(1, min(78, m.width-4))
	label := ink(p.muted).Width(14)
	field := func(name, value string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, label.Render(name), ink(p.text).Render(ansi.Hardwrap(clean(value), max(1, width-20), true)))
	}
	details := []string{
		ink(p.accent).Bold(true).Render("Authorize SSH key"),
		ink(p.muted).Render("Add your public key to this Forge connection."), m.preparationProgress(max(1, width-6)),
		field("Organization", t.Organization), field("Server", t.ServerName), field("Host", endpoint(t)), field("SSH user", t.User), "",
		field("Public key", c.PublicKeyPath), field("Fingerprint", c.Fingerprint), "",
		ink(p.muted).Render("Only this user and server will receive the key."), "",
		ink(p.accent).Bold(true).Render("y  approve") + "    " + ink(p.muted).Render("n / esc  refuse"),
	}
	panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.accentSoft).Padding(1, 2).Width(width).Render(strings.Join(details, "\n"))
	return panel + "\n" + ink(p.dim).Render("  Enter does not approve · ctrl+c cancels")
}
func (m *Model) consentFits() bool {
	return lipgloss.Height(m.consentView()) <= m.height-2 && lipgloss.Width(m.consentView()) <= m.width-2
}
