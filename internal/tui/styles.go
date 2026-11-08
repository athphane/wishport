package tui

import (
	"image/color"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

type palette struct{ text, muted, dim, accent, accentSoft, green, amber, red color.Color }

func colors(dark bool) palette {
	if dark {
		return palette{lipgloss.Color("#E4E4EB"), lipgloss.Color("#92919F"), lipgloss.Color("#565461"), lipgloss.Color("#BC93F9"), lipgloss.Color("#8767B5"), lipgloss.Color("#91C9A0"), lipgloss.Color("#E6BC7A"), lipgloss.Color("#EE9393")}
	}
	return palette{lipgloss.Color("#272333"), lipgloss.Color("#706979"), lipgloss.Color("#AAA4B4"), lipgloss.Color("#7542BB"), lipgloss.Color("#9A75C9"), lipgloss.Color("#31774A"), lipgloss.Color("#95600A"), lipgloss.Color("#B83E50")}
}
func ink(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
func (m *Model) setTheme(dark bool) {
	m.dark = dark
	p := colors(dark)
	s := textinput.DefaultStyles(dark)
	s.Focused.Prompt = ink(p.accent).Bold(true)
	s.Focused.Text = ink(p.text)
	s.Focused.Placeholder = ink(p.muted)
	s.Blurred.Prompt = ink(p.dim)
	s.Blurred.Text = ink(p.text)
	s.Blurred.Placeholder = ink(p.dim)
	m.input.SetStyles(s)
	m.spinner.Style = ink(p.accent)
}
