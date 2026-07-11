package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// TestRunCLIDispatch drives the command-API dispatcher: routing, exit codes,
// and the argument-error paths.
func TestRunCLIDispatch(t *testing.T) {
	t.Setenv("HERDR_TODO_FILE", filepath.Join(t.TempDir(), "todo.md"))
	cases := []struct {
		verb string
		args []string
		want int
	}{
		{"help", nil, 0},
		{"bogus", nil, 2}, // unknown verb
		{"add", nil, 2},   // add with no text
		{"add", []string{"x"}, 0},
		{"list", []string{"--json"}, 0},
		{"done", []string{"1"}, 0},
		{"done", []string{"99"}, 1}, // out of range
		{"done", []string{"nope"}, 1},
		{"rm", []string{"1"}, 0},
	}
	for _, c := range cases {
		if got := runCLI(c.verb, c.args); got != c.want {
			t.Errorf("runCLI(%q, %v) = %d, want %d", c.verb, c.args, got, c.want)
		}
	}
}

func TestDisplayDate(t *testing.T) {
	if got := displayDate("2026-07-15"); got != "15-07-2026" {
		t.Errorf("displayDate ISO = %q, want 15-07-2026", got)
	}
	if got := displayDate("not-a-date"); got != "not-a-date" {
		t.Errorf("displayDate passthrough = %q", got)
	}
}

func TestConfigPath(t *testing.T) {
	t.Setenv("SHEPHERD_CONFIG", "/explicit/config.toml")
	if got := configPath(); got != "/explicit/config.toml" {
		t.Errorf("explicit config = %q", got)
	}
	_ = os.Unsetenv("SHEPHERD_CONFIG")
	t.Setenv("HERDR_TODO_FILE", "/data/todo.md")
	if got := configPath(); got != "/data/config.toml" {
		t.Errorf("derived config = %q, want /data/config.toml", got)
	}
}

func TestParseDueRelativeAndExplicit(t *testing.T) {
	today = func() string { return "2026-07-10" }
	defer func() { today = func() string { return time.Now().Format(dateFormat) } }()
	cases := map[string]string{
		"2w":         "2026-07-24", // relative, no leading +
		"+3d":        "2026-07-13",
		"1m":         "2026-08-10",
		"1y":         "2027-07-10",
		"15-07-2026": "2026-07-15", // DMY normalised to ISO
		"2026-12-01": "2026-12-01", // ISO passthrough
		"gibberish":  "",           // unrecognised clears
	}
	for in, want := range cases {
		if got := parseDue(in); got != want {
			t.Errorf("parseDue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewModelLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] (H) seeded\n  category: work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_TODO_FILE", path)
	m := newModel()
	if len(m.items) != 1 || m.items[0].text != "seeded" || m.items[0].prio != 'H' || m.items[0].category != "work" {
		t.Fatalf("newModel did not load items: %+v", m.items)
	}
	if fileModTime(path).IsZero() {
		t.Error("fileModTime zero for existing file")
	}
}

// TestHelpScroll exercises the scroll subsystem kept for short panes.
func TestHelpScroll(t *testing.T) {
	m := model{input: textinput.New(), w: 50, height: 12, mode: modeHelp}
	max := m.helpMaxScroll()
	if max <= 0 {
		t.Fatalf("expected scrollable help at height 12, maxScroll=%d", max)
	}

	send := func(s string) {
		nm, _ := m.updateHelp(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = nm.(model)
	}
	send("G")
	if m.helpScroll != max {
		t.Errorf("after G scroll=%d, want max %d", m.helpScroll, max)
	}
	send("j") // clamps at max
	if m.helpScroll != max {
		t.Errorf("j past max scroll=%d, want %d", m.helpScroll, max)
	}
	send("k")
	if m.helpScroll != max-1 {
		t.Errorf("after k scroll=%d, want %d", m.helpScroll, max-1)
	}
	send("g")
	if m.helpScroll != 0 {
		t.Errorf("after g scroll=%d, want 0", m.helpScroll)
	}
	nm, _ := m.updateHelp(tea.KeyMsg{Type: tea.KeyEnter})
	if nm.(model).mode != modeList {
		t.Error("enter did not close help")
	}
}

// TestRenderAllViews renders every view path and asserts it produces output
// without panicking.
func TestRenderAllViews(t *testing.T) {
	base := model{input: textinput.New(), w: 50, height: 20, items: []item{
		{text: "ship release", prio: 'H', category: "work", due: "2026-07-01"},
		{text: "buy milk"},
	}}
	for _, v := range []viewMode{viewCategory, viewPriority, viewTable} {
		m := base
		m.view = v
		out := m.View()
		if !strings.Contains(out, appSubtitle) {
			t.Errorf("view %d missing subtitle", v)
		}
	}
	// detail view
	dm := base
	dm.mode = modeDetail
	if !strings.Contains(dm.View(), "task") {
		t.Error("detail view missing task field")
	}
	// help view
	hm := base
	hm.mode = modeHelp
	if !strings.Contains(hm.View(), "adding") {
		t.Error("help view missing sections")
	}
}

func TestTodoPathResolution(t *testing.T) {
	t.Setenv("HERDR_TODO_FILE", "/x/y.md")
	if got := todoPath(); got != "/x/y.md" {
		t.Errorf("HERDR_TODO_FILE not honoured: %q", got)
	}
	_ = os.Unsetenv("HERDR_TODO_FILE")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/state")
	if got := todoPath(); got != "/state/todo.md" {
		t.Errorf("state dir path = %q, want /state/todo.md", got)
	}
	_ = os.Unsetenv("HERDR_PLUGIN_STATE_DIR")
	if got := todoPath(); !strings.HasSuffix(got, "/.config/shepherd/todo.md") {
		t.Errorf("home fallback path = %q", got)
	}
}

// TestUpdateReload covers the message paths in Update: resize, external-change
// reload via tickMsg (and its dirty-guard), and editorDoneMsg reload.
func TestUpdateReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// resize
	m := model{path: path, input: textinput.New()}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(model)
	if m.w != 80 || m.height != 24 {
		t.Fatalf("resize not applied: w=%d h=%d", m.w, m.height)
	}

	// tickMsg reloads when the file is newer and there are no unsaved edits
	nm, _ = m.Update(tickMsg{})
	m = nm.(model)
	if len(m.items) != 1 || m.items[0].text != "fresh" {
		t.Fatalf("tick did not reload external edit: %+v", m.items)
	}

	// dirty guard: a pending edit blocks the reload from clobbering it
	dirtyM := model{path: path, input: textinput.New(), dirty: true,
		items: []item{{text: "unsaved"}}}
	nm, _ = dirtyM.Update(tickMsg{})
	if got := nm.(model).items[0].text; got != "unsaved" {
		t.Fatalf("dirty model reloaded over unsaved work: %q", got)
	}

	// editorDoneMsg always reloads from disk
	edM := model{path: path, input: textinput.New(), items: []item{{text: "stale"}}}
	nm, _ = edM.Update(editorDoneMsg{})
	if got := nm.(model).items[0].text; got != "fresh" {
		t.Fatalf("editorDone did not reload: %q", got)
	}
}

func TestGroupsPriorityView(t *testing.T) {
	m := model{view: viewPriority, items: []item{
		{text: "a", prio: 'H'},
		{text: "b", prio: 'H'},
		{text: "c"}, // no priority
	}}
	if _, label := m.groupOf(m.items[0]); label != "high priority" {
		t.Errorf("high group label = %q", label)
	}
	if _, label := m.groupOf(m.items[2]); label != "no priority" {
		t.Errorf("no-prio group label = %q", label)
	}
	if _, total := m.groupCount(m.items[0]); total != 2 {
		t.Errorf("high group total = %d, want 2", total)
	}
}
