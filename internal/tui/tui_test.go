package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// drive feeds keys through Update and returns the resulting model.
func drive(m model, keys ...string) model {
	for _, k := range keys {
		nm, _ := m.Update(key(k))
		m = nm.(model)
	}
	return m
}

func pinToday(t *testing.T, iso string) {
	t.Helper()
	todo.Today = func() string { return iso }
	t.Cleanup(func() { todo.Today = func() string { return time.Now().Format("2006-01-02") } })
}

func TestModelActions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n- [ ] beta\n"), 0o644)
	m := model{path: p, items: store.Load(p), input: textinput.New()}

	m = drive(m, " ")
	if !m.items[0].Done {
		t.Fatal("space did not toggle done")
	}
	m = drive(m, "j", "h")
	if m.items[0].Prio != 'H' || m.items[0].Text != "beta" || m.cursor != 0 {
		t.Fatalf("prio+sort+cursor wrong: cursor=%d items=%+v", m.cursor, m.items)
	}
	m = drive(m, "a")
	m.input.SetValue("gamma")
	m = drive(m, "enter")
	last := m.items[len(m.items)-1]
	if last.Text != "gamma" || last.Created == "" {
		t.Fatalf("add failed: %+v", last)
	}
	before := len(m.items)
	m = drive(m, "x")
	if len(m.items) != before-1 {
		t.Fatal("delete (x) failed")
	}
	m = drive(m, "c")
	for _, it := range m.items {
		if it.Done {
			t.Fatal("clear left a done item")
		}
	}
}

func TestPriorityToggleOff(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{{Text: "a"}}}
	m = drive(m, "h")
	if m.items[0].Prio != 'H' {
		t.Fatalf("h did not set high: %+v", m.items[0])
	}
	m = drive(m, "h")
	if m.items[0].Prio != 0 {
		t.Fatalf("repeat h did not clear priority: %+v", m.items[0])
	}
	m = drive(m, "h", "m")
	if m.items[0].Prio != 'M' {
		t.Fatalf("different key should switch, not clear: %+v", m.items[0])
	}
}

func TestFilter(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{
		{Text: "buy milk"}, {Text: "buy eggs"}, {Text: "call bank", Note: "urgent"},
	}}
	m = drive(m, "/")
	for _, r := range "buy" {
		m = drive(m, string(r))
	}
	if got := len(m.visible()); got != 2 {
		t.Fatalf("filter 'buy' want 2 visible, got %d", got)
	}
	m.input.SetValue("urgent")
	m.filter = "urgent"
	if got := m.visible(); len(got) != 1 || m.items[got[0]].Text != "call bank" {
		t.Fatalf("note filter failed: %v", got)
	}
	m = model{input: textinput.New(), items: []todo.Item{{Text: "a"}, {Text: "b"}, {Text: "c"}}, filter: "c"}
	m = drive(m, " ")
	if !m.items[2].Done || m.items[0].Done {
		t.Fatalf("filtered toggle hit wrong item: %+v", m.items)
	}
}

func TestSetCategory(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{
		{Text: "a", Category: "work"}, {Text: "b"},
	}}
	todo.Sort(m.items, false)
	m = drive(m, "g")
	m.input.SetValue("home")
	m = drive(m, "enter")
	if m.items[0].Category != "home" || m.items[0].Text != "a" {
		t.Fatalf("category set/sort wrong: %+v", m.items)
	}
	if m.sel() < 0 || m.items[m.sel()].Text != "a" {
		t.Fatalf("cursor did not follow item after re-sort: sel=%d", m.sel())
	}
}

func TestAddInheritsFilterCategory(t *testing.T) {
	// filter names a category in use -> new item inherits it
	m := model{input: textinput.New(), items: []todo.Item{{Text: "a", Category: "work"}}, filter: "work"}
	m = drive(m, "a")
	m.input.SetValue("ship it")
	m = drive(m, "enter")
	if got := lastByText(m, "ship it"); got.Category != "work" {
		t.Fatalf("add under category filter should inherit: %+v", got)
	}

	// inline @category overrides the filter
	m = drive(m, "a")
	m.input.SetValue("errand @home")
	m = drive(m, "enter")
	if got := lastByText(m, "errand"); got.Category != "home" {
		t.Fatalf("inline category should override filter: %+v", got)
	}

	// non-category filter (no match in config or items) -> no inheritance
	m = model{input: textinput.New(), items: []todo.Item{{Text: "a", Category: "work"}}, filter: "buy"}
	m = drive(m, "a")
	m.input.SetValue("buy milk")
	m = drive(m, "enter")
	if got := lastByText(m, "buy milk"); got.Category != "" {
		t.Fatalf("non-category filter should not tag: %+v", got)
	}

	// configured-but-unused category also counts
	m = model{input: textinput.New(), categories: []string{"personal"}, filter: "personal"}
	m = drive(m, "a")
	m.input.SetValue("call mum")
	m = drive(m, "enter")
	if got := lastByText(m, "call mum"); got.Category != "personal" {
		t.Fatalf("configured category filter should inherit: %+v", got)
	}
}

func lastByText(m model, text string) todo.Item {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].Text == text {
			return m.items[i]
		}
	}
	return todo.Item{}
}

func TestDetailNote(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{{Text: "task"}}}
	m = drive(m, "d")
	if m.mode != modeDetail {
		t.Fatal("d did not open detail")
	}
	m = drive(m, "e")
	m.input.SetValue("some note")
	m = drive(m, "enter")
	if m.items[0].Note != "some note" || m.mode != modeDetail {
		t.Fatalf("note save failed: %+v mode=%d", m.items[0], m.mode)
	}
	m = drive(m, "q")
	if m.mode != modeList {
		t.Fatal("q did not return to list")
	}
}

func TestView(t *testing.T) {
	m := model{input: textinput.New(), w: 50, height: 20, items: []todo.Item{
		{Text: "ship release", Prio: 'H', Category: "work"},
		{Text: "buy milk"},
	}}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = nm.(model)
	v := m.View()
	if !strings.Contains(v, appSubtitle) {
		t.Fatal("subtitle missing")
	}
	if !strings.Contains(v, strings.Repeat("─", 46)) { // 50 - 2*padX
		t.Fatal("divider not inner width")
	}
	for _, want := range []string{"work", "uncategorized", "0/2", "move   j/k", "cat    g", "arch   c", "editor ^e"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	if got := strings.Count(v, "\n") + 1; got != 20 {
		t.Fatalf("frame not pinned to height: %d rows", got)
	}
}

func TestFilterFarRight(t *testing.T) {
	m := model{input: textinput.New(), w: 50, height: 10, filter: "bank",
		items: []todo.Item{{Text: "call bank"}}}
	head := m.header()
	first := strings.SplitN(head, "\n", 2)[0]
	if !strings.Contains(first, appSubtitle) || !strings.Contains(first, "/bank") || !strings.HasSuffix(first, "0/1") {
		t.Fatalf("header layout wrong: %q", first)
	}
	if lipgloss.Width(first) != 46 {
		t.Fatalf("header line width = %d, want 46", lipgloss.Width(first))
	}
}

func TestUndoRedo(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{{Text: "a"}, {Text: "b"}}}
	m = drive(m, "x")
	m = drive(m, "x")
	if len(m.items) != 0 {
		t.Fatalf("deletes failed: %+v", m.items)
	}
	m = drive(m, "U")
	if len(m.items) != 1 || m.items[0].Text != "b" {
		t.Fatalf("undo 1 wrong: %+v", m.items)
	}
	m = drive(m, "U")
	if len(m.items) != 2 || m.items[0].Text != "a" {
		t.Fatalf("undo 2 wrong: %+v", m.items)
	}
	m = drive(m, "ctrl+r")
	if len(m.items) != 1 || m.items[0].Text != "b" {
		t.Fatalf("redo wrong: %+v", m.items)
	}
	m = drive(m, "a")
	m.input.SetValue("c")
	m = drive(m, "enter")
	if len(m.future) != 0 {
		t.Fatalf("new edit did not clear redo stack: %d", len(m.future))
	}
	m = drive(m, "ctrl+r")
	if m.items[len(m.items)-1].Text != "c" {
		t.Fatalf("redo after edit should be no-op: %+v", m.items)
	}
}

func TestViewToggle(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{
		{Text: "a", Category: "z", Prio: 'L'},
		{Text: "b", Category: "a", Prio: 'H'},
	}}
	m.resort()
	if m.items[0].Text != "b" {
		t.Fatalf("category sort wrong: %+v", m.items)
	}
	m = drive(m, "v")
	if m.view != viewPriority || m.items[0].Text != "b" {
		t.Fatalf("priority view sort wrong: view=%d %+v", m.view, m.items)
	}
	m = drive(m, "v", "v")
	if m.view != viewCategory {
		t.Fatalf("view did not cycle back: %d", m.view)
	}
}

func TestArchive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	_ = os.WriteFile(p, []byte("- [x] done one\n- [ ] keep me\n"), 0o644)
	m := model{path: p, input: textinput.New(), items: store.Load(p)}
	m = drive(m, "c")
	if len(m.items) != 1 || m.items[0].Text != "keep me" {
		t.Fatalf("archive left wrong items: %+v", m.items)
	}
	arch, err := os.ReadFile(filepath.Join(dir, "archive.md"))
	if err != nil || !strings.Contains(string(arch), "done one") {
		t.Fatalf("archive.md missing done item: %q err=%v", arch, err)
	}
}

func TestArchiveSearch(t *testing.T) {
	m := model{input: textinput.New(),
		items:    []todo.Item{{Text: "active task"}},
		archived: []todo.Item{{Text: "old bank thing", Done: true}, {Text: "unrelated"}},
		filter:   "bank"}
	got := m.archivedMatches()
	if len(got) != 1 || got[0].Text != "old bank thing" {
		t.Fatalf("archive search wrong: %+v", got)
	}
	m.filter = ""
	if len(m.archivedMatches()) != 0 {
		t.Fatal("archive should be empty with no filter")
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(p, []byte("# shepherd\nview = priority\ndensity = comfort\ncategories = [\"work\", \"home\", personal]\n"), 0o644)
	c := loadConfig(p)
	if c.view != viewPriority {
		t.Fatalf("view not parsed: %d", c.view)
	}
	if c.density != comfort {
		t.Fatalf("density not parsed: %d", c.density)
	}
	if len(c.categories) != 3 || c.categories[0] != "work" || c.categories[2] != "personal" {
		t.Fatalf("categories not parsed: %+v", c.categories)
	}
	d := loadConfig(filepath.Join(dir, "nope.toml"))
	if d.view != viewCategory || d.density != compact || d.categories != nil {
		t.Fatalf("defaults wrong: %+v", d)
	}
}

func TestNewModelLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] (H) seeded\n  category: work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_TODO_FILE", path)
	m := newModel("")
	if len(m.items) != 1 || m.items[0].Text != "seeded" || m.items[0].Prio != 'H' || m.items[0].Category != "work" {
		t.Fatalf("newModel did not load items: %+v", m.items)
	}
	if store.FileModTime(path).IsZero() {
		t.Error("FileModTime zero for existing file")
	}
}

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
	send("j")
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

func TestRenderAllViews(t *testing.T) {
	base := model{input: textinput.New(), w: 50, height: 20, items: []todo.Item{
		{Text: "ship release", Prio: 'H', Category: "work", Due: "2026-07-01"},
		{Text: "buy milk"},
	}}
	for _, v := range []viewMode{viewCategory, viewPriority, viewTable} {
		m := base
		m.view = v
		if !strings.Contains(m.View(), appSubtitle) {
			t.Errorf("view %d missing subtitle", v)
		}
	}
	dm := base
	dm.mode = modeDetail
	if !strings.Contains(dm.View(), "task") {
		t.Error("detail view missing task field")
	}
	hm := base
	hm.mode = modeHelp
	if !strings.Contains(hm.View(), "adding") {
		t.Error("help view missing sections")
	}
}

func TestUpdateReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := model{path: path, input: textinput.New()}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(model)
	if m.w != 80 || m.height != 24 {
		t.Fatalf("resize not applied: w=%d h=%d", m.w, m.height)
	}

	nm, _ = m.Update(tickMsg{})
	m = nm.(model)
	if len(m.items) != 1 || m.items[0].Text != "fresh" {
		t.Fatalf("tick did not reload external edit: %+v", m.items)
	}

	dirtyM := model{path: path, input: textinput.New(), dirty: true,
		items: []todo.Item{{Text: "unsaved"}}}
	nm, _ = dirtyM.Update(tickMsg{})
	if got := nm.(model).items[0].Text; got != "unsaved" {
		t.Fatalf("dirty model reloaded over unsaved work: %q", got)
	}

	edM := model{path: path, input: textinput.New(), items: []todo.Item{{Text: "stale"}}}
	nm, _ = edM.Update(editorDoneMsg{})
	if got := nm.(model).items[0].Text; got != "fresh" {
		t.Fatalf("editorDone did not reload: %q", got)
	}
}

func TestGroups(t *testing.T) {
	m := model{view: viewPriority, items: []todo.Item{
		{Text: "a", Prio: 'H'},
		{Text: "b", Prio: 'H'},
		{Text: "c"}, // no priority
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

func TestOverdueGroupLabel(t *testing.T) {
	pinToday(t, "2026-07-10")
	m := model{view: viewCategory, items: []todo.Item{
		{Text: "late", Category: "home", Due: "2026-07-01"},
	}}
	if _, label := m.groupOf(m.items[0]); label != "⚠ overdue" {
		t.Fatalf("overdue group label wrong: %q", label)
	}
}

func TestGlobalReadOnly(t *testing.T) {
	m := model{
		input:  textinput.New(),
		global: true,
		view:   viewProject,
		items: []todo.Item{
			{Text: "a", Source: "default"},
			{Text: "b", Source: "web"},
		},
	}
	m.resort()

	// space must not toggle done in the read-only global view
	if got := drive(m, " "); got.items[0].Done || got.items[1].Done {
		t.Fatal("space toggled done in read-only global view")
	}

	// v cycles through all 4 modes back to project
	if got := drive(m, "v", "v", "v", "v"); got.view != viewProject {
		t.Fatalf("v cycle did not return to project after 4 steps: %v", got.view)
	}

	// items group by source; header id/label is the board name
	if id, label := m.groupOf(m.items[0]); id != "sdefault" || label != "default" {
		t.Fatalf("project group wrong: %q %q", id, label)
	}
	if d, tot := m.groupCount(m.items[1]); d != 0 || tot != 1 {
		t.Fatalf("project group count wrong: %d/%d", d, tot)
	}
}
