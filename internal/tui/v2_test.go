package tui

import (
	"os"
	"path/filepath"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func realCursorInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.SetVirtualCursor(false)
	return ti
}

// TestViewChrome pins the terminal-level view fields: title, tab progress,
// focus reporting, and no marks leaking into the drawn content.
func TestViewChrome(t *testing.T) {
	m := model{input: textinput.New(), w: 60, height: 20, board: "web", items: []todo.Item{{Text: "a", Done: true}, {Text: "b"}, {Text: "c"}, {Text: "d"}}}
	v := m.View()
	if v.WindowTitle != "shepherd · web · 1/4" {
		t.Errorf("title = %q", v.WindowTitle)
	}
	if v.ProgressBar == nil || v.ProgressBar.Value != 25 || v.ProgressBar.State != tea.ProgressBarDefault {
		t.Errorf("progress = %+v", v.ProgressBar)
	}
	if !v.ReportFocus || !v.AltScreen {
		t.Error("focus reporting or alt screen off")
	}
	if strings.Contains(v.Content, "1337;shepherd") {
		t.Error("marks leaked into the view")
	}
	if v.MouseMode != tea.MouseModeNone || v.OnMouse != nil {
		t.Error("mouse on without config")
	}
	m.dirty = true
	if got := m.View().WindowTitle; !strings.HasSuffix(got, " ●") {
		t.Errorf("dirty title = %q", got)
	}
}

// TestRealCursor pins that the terminal cursor sits after the typed text in
// the add prompt and inside the note editor.
func TestRealCursor(t *testing.T) {
	ta := textarea.New()
	ta.SetVirtualCursor(false)
	m := model{input: realCursorInput(), note: ta, w: 60, height: 20, items: []todo.Item{{Text: "a"}}}
	m = drive(m, "a", "h", "i")
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("no cursor in add prompt")
	}
	line := ansi.Strip(strings.Split(v.Content, "\n")[v.Cursor.Y])
	i := strings.Index(line, "› hi")
	if i < 0 || v.Cursor.X != ansi.StringWidth(line[:i+len("› hi")]) {
		t.Errorf("cursor at %d,%d on %q", v.Cursor.X, v.Cursor.Y, line)
	}

	m = drive(m, "esc", "d", "n", "x")
	v = m.View()
	if v.Cursor == nil || !strings.Contains(ansi.Strip(strings.Split(v.Content, "\n")[v.Cursor.Y]), "x") {
		t.Fatalf("note cursor %+v", v.Cursor)
	}
	nm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if nm.(model).mode != modeDetail {
		t.Error("shift+enter did not close the note")
	}
}

// TestFocusEvents pins that regaining focus checks the disk at once and losing
// it flushes unsaved edits (unless autosave is off).
func TestFocusEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n"), 0o644)
	m := model{path: p, input: textinput.New(), autosaveEvery: time.Minute}
	nm, cmd := m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("focus did not check the disk")
	}
	nm, _ = nm.(model).Update(cmd())
	m = nm.(model)
	if len(m.items) != 1 {
		t.Fatalf("focus did not load: %v", m.items)
	}

	m = drive(m, "a", "beta", "enter")
	nm, _ = m.Update(tea.BlurMsg{})
	if nm.(model).dirty || len(store.Load(p)) != 2 {
		t.Fatal("blur did not save")
	}

	m = drive(nm.(model), "x")
	m.autosaveEvery = 0
	nm, _ = m.Update(tea.BlurMsg{})
	if !nm.(model).dirty {
		t.Fatal("blur saved with autosave off")
	}
}

// TestMouse pins click-to-select, double-click to open, wheel scrolling, and
// lane clicks picking the column under the pointer.
func TestMouse(t *testing.T) {
	m := model{input: textinput.New(), w: 60, height: 40, mouse: true, items: []todo.Item{{Text: "one"}, {Text: "two"}, {Text: "three"}}}
	click := func(text string) {
		t.Helper()
		v := m.View()
		for y, ln := range strings.Split(ansi.Strip(v.Content), "\n") {
			if left, _, ok := strings.Cut(ln, text); ok {
				cmd := v.OnMouse(tea.MouseClickMsg{X: ansi.StringWidth(left), Y: y, Button: tea.MouseLeft})
				if cmd == nil {
					t.Fatalf("click on %q hit no row", text)
				}
				nm, _ := m.Update(cmd())
				m = nm.(model)
				return
			}
		}
		t.Fatalf("%q not on screen", text)
	}
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse mode off")
	}
	click("three")
	if m.rowText(m.selRef()) != "three" || m.mode != modeList {
		t.Fatalf("click selected %q", m.rowText(m.selRef()))
	}
	click("three")
	if m.mode != modeDetail {
		t.Fatal("double click did not open detail")
	}

	m.mode = modeList
	m.cursor = 0
	nm, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if nm.(model).cursor != 1 {
		t.Fatal("wheel did not move")
	}

	m = model{input: textinput.New(), w: 60, height: 40, mouse: true, view: viewTable, items: []todo.Item{{Text: "one"}, {Text: "two"}, {Text: "three"}}}
	click("two")
	if m.rowText(m.selRef()) != "two" {
		t.Fatalf("table click selected %q", m.rowText(m.selRef()))
	}

	m = model{
		input: textinput.New(), w: 60, height: 40, mouse: true, view: viewLane, statuses: []string{"open", "doing", "done"},
		items: []todo.Item{{Text: "todo-a"}, {Text: "busy-b", Status: "doing"}, {Text: "busy-c", Status: "doing"}},
	}
	m.resort()
	click("busy-c")
	if m.lane != 1 || m.rowText(m.selRef()) != "busy-c" {
		t.Fatalf("lane click: lane %d row %q", m.lane, m.rowText(m.selRef()))
	}
}

// TestMouseSetting pins that the settings row toggles mouse and persists it.
func TestMouseSetting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHEPHERD_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	m := model{input: textinput.New(), mode: modeSettings, settingsCur: 6}
	m = drive(m, "enter")
	if !m.mouse || !loadConfig(store.ConfigPath()).mouse {
		t.Fatal("mouse setting not toggled and saved")
	}
}
