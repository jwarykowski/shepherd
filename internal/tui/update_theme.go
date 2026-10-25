package tui

import (
	"fmt"
	"image/color"
	"shepherd/internal/theme"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// enterTheme opens the quick theme picker on the active theme.
func (m *model) enterTheme() {
	m.themeWas = m.theme
	m.mode = modeTheme
}

// updateTheme previews each theme as the cursor moves; enter keeps and saves
// it, esc restores the one the picker opened on.
func (m model) updateTheme(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	names := theme.Names()
	i := max(slices.Index(names, m.theme), 0)
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc":
		m.theme = m.themeWas
		m.applyTheme()
		m.mode = modeList
	case "enter":
		m.saveSettings()
		m.mode = modeList
	case "j", "down":
		m.theme = names[(i+1)%len(names)]
		m.applyTheme()
	case "k", "up":
		m.theme = names[(i+len(names)-1)%len(names)]
		m.applyTheme()
	}
	return m, nil
}

// themeOverlay draws the picker box centred over the rendered board.
func (m model) themeOverlay(board string) string {
	board, _ = cutRowMarks(board)
	board, _, _ = cutCursorMark(board)
	var rows []string
	for _, n := range theme.Names() {
		p := theme.Resolve(n, m.light, nil)
		var sw strings.Builder
		for _, c := range []color.Color{p.Accent, p.OK, p.Info, p.Warn, p.PrioHigh} {
			if c != nil {
				sw.WriteString(fg(lipgloss.NewStyle(), c).Render("●"))
			}
		}
		mark := "  "
		if n == m.theme {
			mark = "> "
		}
		rows = append(rows, fmt.Sprintf("%s%-12s%s", mark, n, sw.String()))
	}
	rows = append(rows, "", dimStyle.Render("enter keep · esc back"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.st().rule.GetForeground()).
		Padding(0, 1).Render(strings.Join(rows, "\n"))
	w, h := lipgloss.Width(board), lipgloss.Height(board)
	x, y := max((w-lipgloss.Width(box))/2, 0), max((h-lipgloss.Height(box))/2, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(board), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}
