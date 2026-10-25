package tui

import (
	"image/color"
	"shepherd/internal/theme"

	"charm.land/lipgloss/v2"
)

// styles are the coloured styles, built from the active theme palette.
type styles struct {
	box, prog, match, cat, warn, rule, cursor lipgloss.Style
	prio                                      map[byte]lipgloss.Style
}

// fg sets s's foreground, or leaves the terminal default for a nil colour.
func fg(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Foreground(c)
}

func newStyles(p theme.Palette) *styles {
	cursor := lipgloss.NewStyle().Reverse(true) // a colourless theme still shows the cursor
	if p.CursorBG != nil {
		cursor = lipgloss.NewStyle().Background(p.CursorBG)
	}
	return &styles{
		box:    fg(lipgloss.NewStyle(), p.OK),
		prog:   fg(lipgloss.NewStyle(), p.Info),
		match:  fg(lipgloss.NewStyle(), p.Info),
		cat:    fg(lipgloss.NewStyle().Bold(true), p.Accent),
		warn:   fg(lipgloss.NewStyle(), p.Warn),
		rule:   fg(lipgloss.NewStyle().Faint(true), p.Rule),
		cursor: cursor,
		prio: map[byte]lipgloss.Style{
			'H': fg(lipgloss.NewStyle(), p.PrioHigh),
			'M': fg(lipgloss.NewStyle(), p.PrioMed),
			'L': fg(lipgloss.NewStyle().Faint(true), p.PrioLow),
		},
	}
}

// defaultStyles serve models built without newModel (tests): the default theme
// on a dark background.
var defaultStyles = newStyles(theme.Resolve(theme.Default, false, nil))

// st is the model's active styles.
func (m model) st() *styles {
	if m.sty != nil {
		return m.sty
	}
	return defaultStyles
}

// applyTheme rebuilds the styles after the theme, its overrides or the
// terminal background change.
func (m *model) applyTheme() {
	m.sty = newStyles(theme.Resolve(m.theme, m.light, m.themeKV))
}

// adoptTerminal carries what the terminal reported (size, background, key
// protocol) from old onto a model rebuilt by newModel.
func (m *model) adoptTerminal(old model) {
	m.w, m.height, m.light, m.kbdEnhanced = old.w, old.height, old.light, old.kbdEnhanced
	m.applyTheme()
}
