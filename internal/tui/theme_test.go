package tui

import (
	"os"
	"reflect"
	"shepherd/internal/store"
	"shepherd/internal/theme"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TestThemeConfig pins that config.toml picks the theme and its theme.<token>
// overrides, and an unknown name falls back to the default.
func TestThemeConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"
	_ = os.WriteFile(path, []byte("theme = \"nord\"\ntheme.accent = \"#ff0000\"\n"), 0o644)
	c := loadConfig(path)
	if c.theme != "nord" || c.themeKV["theme.accent"] != `"#ff0000"` {
		t.Fatalf("config = %q %v", c.theme, c.themeKV)
	}
	_ = os.WriteFile(path, []byte("theme = \"nope\"\n"), 0o644)
	if c := loadConfig(path); c.theme != theme.Default {
		t.Fatalf("unknown theme = %q", c.theme)
	}
}

// TestThemeSettingCycles pins that the settings row previews the next theme
// live, saves it, and keeps the hand-written overrides in the file.
func TestThemeSettingCycles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHEPHERD_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	path := store.ConfigPath()
	_ = os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o755)
	_ = os.WriteFile(path, []byte("theme = \"ansi\"\ntheme.warn = \"#123456\"\n"), 0o644)
	cfg := loadConfig(path)
	m := model{input: textinput.New(), mode: modeSettings, settingsCur: 7, theme: cfg.theme, themeKV: cfg.themeKV}
	m.applyTheme()
	before := m.st().cat.GetForeground()
	m = drive(m, "enter")
	if m.theme != theme.Next("ansi") {
		t.Fatalf("theme = %q", m.theme)
	}
	if reflect.DeepEqual(m.st().cat.GetForeground(), before) {
		t.Error("styles not rebuilt for the new theme")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `theme = "`+m.theme+`"`) || !strings.Contains(string(data), `theme.warn = "#123456"`) {
		t.Fatalf("config after cycle:\n%s", data)
	}
	if !reflect.DeepEqual(m.st().warn.GetForeground(), lipgloss.Color("#123456")) {
		t.Error("override lost after cycling")
	}
}

// TestThemeFollowsBackground pins that a light background switches palettes
// and a colourless theme still marks the cursor (reverse video).
func TestThemeFollowsBackground(t *testing.T) {
	m := model{input: textinput.New(), theme: "gruvbox"}
	m.applyTheme()
	dark := m.st().cursor.GetBackground()
	nm, _ := m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")})
	if reflect.DeepEqual(nm.(model).st().cursor.GetBackground(), dark) {
		t.Error("light background kept the dark palette")
	}
	m.theme = "mono"
	m.applyTheme()
	if !m.st().cursor.GetReverse() {
		t.Error("mono cursor has no highlight")
	}
}

// TestThemePicker pins that ^t previews themes live over the board, esc
// restores the original, and enter keeps and saves the pick.
func TestThemePicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHEPHERD_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	m := model{input: textinput.New(), theme: theme.Default}
	m.applyTheme()

	m = drive(m, "g", "t", "j")
	if m.mode != modeTheme || m.theme != theme.Next(theme.Default) {
		t.Fatalf("mode %v theme %q", m.mode, m.theme)
	}
	if v := m.render(); !strings.Contains(v, "> "+m.theme) || !strings.Contains(v, "enter keep") {
		t.Fatalf("picker not drawn:\n%s", v)
	}
	m = drive(m, "k", "k", "esc")
	if m.mode != modeList || m.theme != theme.Default {
		t.Fatalf("esc: mode %v theme %q", m.mode, m.theme)
	}

	m = drive(m, "g", "t", "j", "j", "enter")
	want := theme.Next(theme.Next(theme.Default))
	if m.mode != modeList || m.theme != want {
		t.Fatalf("enter: mode %v theme %q", m.mode, m.theme)
	}
	if data, _ := os.ReadFile(store.ConfigPath()); !strings.Contains(string(data), `theme = "`+want+`"`) {
		t.Fatalf("not saved:\n%s", data)
	}
}
