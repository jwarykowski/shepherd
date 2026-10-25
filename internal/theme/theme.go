// Package theme is shepherd's colour palette: semantic tokens, the built-in
// themes, and config overrides. Pure (no I/O); tui and cli build their styles
// from it, so the board and the stats charts share one set of colours.
package theme

import (
	"image/color"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette is one set of colours. A nil token means "no colour": the text keeps
// the terminal's default and leans on bold/faint instead.
type Palette struct {
	Accent   color.Color // group headers
	OK       color.Color // checkboxes, done counts
	Info     color.Color // progress, filter matches, chart series
	Warn     color.Color // due soon, warnings
	Rule     color.Color // dividers, panel borders, empty bars
	Muted    color.Color // secondary chart bars
	CursorBG color.Color // cursor row background; nil falls back to reverse video
	PrioHigh color.Color
	PrioMed  color.Color
	PrioLow  color.Color // nil: rendered faint
}

// Theme is a palette for dark and for light terminal backgrounds.
type Theme struct {
	Dark, Light Palette
}

// Default is the theme used when none is configured.
const Default = "ansi"

func c(s string) color.Color { return lipgloss.Color(s) }

// hex builds a palette from hex strings in token order, "" for none.
func hex(accent, ok, info, warn, rule, muted, cursor, high, med, low string) Palette {
	pick := func(s string) color.Color {
		if s == "" {
			return nil
		}
		return c(s)
	}
	return Palette{
		Accent: pick(accent), OK: pick(ok), Info: pick(info), Warn: pick(warn), Rule: pick(rule),
		Muted: pick(muted), CursorBG: pick(cursor), PrioHigh: pick(high), PrioMed: pick(med), PrioLow: pick(low),
	}
}

// builtins are the named themes. ansi uses palette indices, so it follows the
// user's terminal colour scheme; the rest are fixed hex palettes, downsampled
// by the renderer on 256- or 16-colour terminals.
var builtins = map[string]Theme{
	"ansi": {
		Dark:  hex("6", "2", "4", "3", "240", "8", "236", "1", "3", ""),
		Light: hex("6", "2", "4", "3", "240", "8", "254", "1", "3", ""),
	},
	"catppuccin": {
		Dark:  hex("#89dceb", "#a6e3a1", "#89b4fa", "#f9e2af", "#585b70", "#7f849c", "#313244", "#f38ba8", "#fab387", "#7f849c"),
		Light: hex("#04a5e5", "#40a02b", "#1e66f5", "#df8e1d", "#acb0be", "#8c8fa1", "#ccd0da", "#d20f39", "#fe640b", "#8c8fa1"),
	},
	"dracula": {
		Dark:  hex("#8be9fd", "#50fa7b", "#bd93f9", "#f1fa8c", "#6272a4", "#6272a4", "#44475a", "#ff5555", "#ffb86c", "#6272a4"),
		Light: hex("#036a96", "#14710a", "#644ac9", "#846e15", "#6c664b", "#6c664b", "#dedccf", "#cb3a2a", "#a34d14", "#6c664b"),
	},
	"gruvbox": {
		Dark:  hex("#8ec07c", "#b8bb26", "#83a598", "#fabd2f", "#665c54", "#928374", "#3c3836", "#fb4934", "#fe8019", "#928374"),
		Light: hex("#427b58", "#79740e", "#076678", "#b57614", "#bdae93", "#928374", "#ebdbb2", "#9d0006", "#af3a03", "#928374"),
	},
	"nord": {
		Dark:  hex("#88c0d0", "#a3be8c", "#81a1c1", "#ebcb8b", "#4c566a", "#616e88", "#3b4252", "#bf616a", "#d08770", "#616e88"),
		Light: hex("#5e81ac", "#6f8f55", "#5e81ac", "#b48a3c", "#d8dee9", "#4c566a", "#e5e9f0", "#bf616a", "#c06a4a", "#4c566a"),
	},
	"mono": {},
}

// Names lists the built-in themes, Default first, the rest alphabetical.
func Names() []string {
	out := []string{Default}
	for n := range builtins {
		if n != Default {
			out = append(out, n)
		}
	}
	sort.Strings(out[1:])
	return out
}

// Next is the built-in theme after name, wrapping; for cycling in settings.
func Next(name string) string {
	ns := Names()
	for i, n := range ns {
		if n == name {
			return ns[(i+1)%len(ns)]
		}
	}
	return Default
}

// Valid reports whether name is a built-in theme.
func Valid(name string) bool {
	_, ok := builtins[name]
	return ok
}

// tokens maps override keys (theme.<key> in config) to palette fields.
var tokens = map[string]func(*Palette) *color.Color{
	"accent":    func(p *Palette) *color.Color { return &p.Accent },
	"ok":        func(p *Palette) *color.Color { return &p.OK },
	"info":      func(p *Palette) *color.Color { return &p.Info },
	"warn":      func(p *Palette) *color.Color { return &p.Warn },
	"rule":      func(p *Palette) *color.Color { return &p.Rule },
	"muted":     func(p *Palette) *color.Color { return &p.Muted },
	"cursor":    func(p *Palette) *color.Color { return &p.CursorBG },
	"prio_high": func(p *Palette) *color.Color { return &p.PrioHigh },
	"prio_med":  func(p *Palette) *color.Color { return &p.PrioMed },
	"prio_low":  func(p *Palette) *color.Color { return &p.PrioLow },
}

// Tokens lists the override keys, sorted.
func Tokens() []string {
	out := make([]string, 0, len(tokens))
	for k := range tokens {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var (
	hexRE     = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	ansiNames = map[string]string{
		"black": "0", "red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
		"bright-black": "8", "grey": "8", "gray": "8", "bright-red": "9", "bright-green": "10", "bright-yellow": "11",
		"bright-blue": "12", "bright-magenta": "13", "bright-cyan": "14", "bright-white": "15",
	}
)

// ParseColor reads a config colour: a palette index (0-255), #rgb/#rrggbb, an
// ANSI name (red, bright-blue, grey, …), or none. ok is false for anything else.
func ParseColor(s string) (color.Color, bool) {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), `"`))
	if s == "none" {
		return nil, true
	}
	if idx, named := ansiNames[s]; named {
		s = idx
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > 255 {
			return nil, false
		}
		return c(s), true
	}
	if hexRE.MatchString(s) {
		return c(s), true
	}
	return nil, false
}

// Resolve is the palette for the configured theme (unknown names fall back to
// Default) on a light or dark background, with theme.<token> overrides from the
// config's key/value pairs applied on top. Bad override values are ignored.
func Resolve(name string, light bool, kv map[string]string) Palette {
	t, ok := builtins[strings.ToLower(name)]
	if !ok {
		t = builtins[Default]
	}
	p := t.Dark
	if light {
		p = t.Light
	}
	for k, v := range kv {
		tok, isTheme := strings.CutPrefix(k, "theme.")
		field, known := tokens[tok]
		if !isTheme || !known {
			continue
		}
		if col, ok := ParseColor(v); ok {
			*field(&p) = col
		}
	}
	return p
}
