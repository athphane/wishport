package tui

import (
	"fmt"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/athphane/wishport/internal/interaction"
)

func (m *Model) tableView(width, height int) (string, string) {
	p := colors(m.dark)
	// Leave room for the same gutter used by list text. Collapse secondary
	// columns before falling back to the list in a small terminal.
	width--
	columnCount, fixedWidth := 3, 14
	if width >= 80 {
		columnCount++
		fixedWidth += 20
	}
	if width >= 110 {
		columnCount++
		fixedWidth += 16
	}
	remaining := width - fixedWidth - 2*columnCount
	nameWidth := remaining / 2
	cols := []table.Column{{Title: "Name", Width: nameWidth}, {Title: "Source", Width: 14}}
	if columnCount == 5 {
		cols = append(cols, table.Column{Title: "Organization", Width: 16})
	}
	if columnCount >= 4 {
		cols = append(cols, table.Column{Title: "Server", Width: 20})
	}
	cols = append(cols, table.Column{Title: "SSH target", Width: remaining - nameWidth})
	rows := make([]table.Row, 0, len(m.visible))
	for _, c := range m.visible {
		name := c.Name
		if c.Unavailable != "" {
			name = "! " + name
		}
		row := table.Row{clean(name), sourceLabel(c)}
		if columnCount == 5 {
			org := c.OrganizationName
			if org == "" {
				org = c.Organization
			}
			row = append(row, clean(org))
		}
		if columnCount >= 4 {
			row = append(row, clean(c.ServerName))
		}
		row = append(row, clean(endpoint(c)))
		rows = append(rows, row)
	}
	activeInk := lipgloss.LightDark(m.dark)(lipgloss.Color("#FFFFFF"), lipgloss.Color("#21172F"))
	styles := table.Styles{
		Header:   ink(p.muted).Bold(true).Padding(0, 1).BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(p.dim),
		Cell:     lipgloss.NewStyle().Padding(0, 1),
		Selected: ink(activeInk).Background(p.accent).Bold(true),
	}
	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithStyles(styles), table.WithWidth(width), table.WithHeight(height), table.WithFocused(true))
	// MoveDown also positions the viewport so the selected row stays visible.
	t.MoveDown(m.selected)
	return ink(p.text).PaddingLeft(1).Render(t.View()), ink(p.dim).Render(fmt.Sprintf("%d / %d ", m.selected+1, len(rows)))
}

func (m *Model) preparationProgress(width int) string {
	stage := m.stage
	if m.consent != nil {
		stage = interaction.StageApproval
	}
	if stage == stageSSHPrepare || stage == stageSSHLaunch {
		if width < 20 {
			return ""
		}
		p := colors(m.dark)
		bar := progress.New(progress.WithColors(p.accentSoft, p.accent), progress.WithoutPercentage(), progress.WithWidth(width))
		bar.EmptyColor = p.dim
		fill := 0.1
		if stage == stageSSHLaunch {
			fill = float64(m.launchFrame+1) / float64(launchFrames)
		}
		return bar.ViewAs(fill)
	}
	step, total := interaction.PreparationStep(stage)
	if step == 0 || width < 20 {
		return ""
	}
	p := colors(m.dark)
	label := fmt.Sprintf("Step %d/%d", step, total)
	bar := progress.New(progress.WithColors(p.accentSoft, p.accent), progress.WithoutPercentage(), progress.WithWidth(max(1, width-len(label)-2)))
	bar.EmptyColor = p.dim
	// Only completed stages fill the bar. Polling and retries cannot advance
	// it with time, and the current stage is not presented as complete.
	return bar.ViewAs(float64(step-1)/float64(total)) + "  " + ink(p.muted).Render(label)
}
