package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
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
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
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

// TestProjectsPickerJump checks p opens the board picker and enter jumps to the
// selected board, rebuilding the model against that board's file.
func TestProjectsPickerJump(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_PROJECT", "")
	base := filepath.Join(home, ".config", "shepherd")
	if err := os.MkdirAll(filepath.Join(base, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(base, "todo.md"), []byte("- [ ] alpha\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "projects", "web.md"), []byte("- [ ] beta\n"), 0o644)

	m := model{path: filepath.Join(base, "todo.md"), project: "", input: textinput.New(),
		items: store.Load(filepath.Join(base, "todo.md"))}

	m = drive(m, "p") // open picker
	if m.mode != modeProjects {
		t.Fatalf("p did not open picker, mode=%d", m.mode)
	}
	if len(m.projRows) != 2 || m.projCur != 0 {
		t.Fatalf("picker rows/cursor wrong: rows=%d cur=%d", len(m.projRows), m.projCur)
	}
	m = drive(m, "j", "enter") // select web, jump
	if m.project != "web" || m.mode != modeList {
		t.Fatalf("jump failed: project=%q mode=%d", m.project, m.mode)
	}
	if len(m.items) != 1 || m.items[0].Text != "beta" {
		t.Fatalf("did not load web board: %+v", m.items)
	}
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

func TestSubtasks(t *testing.T) {
	m := model{input: textinput.New(), statuses: []string{"open", "in-progress", "done"},
		items: []todo.Item{{Text: "parent"}}}

	// S adds a subtask to the selected parent
	m = drive(m, "S")
	m.input.SetValue("step one")
	m = drive(m, "enter")
	m = drive(m, "S")
	m.input.SetValue("step two")
	m = drive(m, "enter")
	if len(m.items[0].Subs) != 2 {
		t.Fatalf("want 2 subs, got %d", len(m.items[0].Subs))
	}
	if got := len(m.rows()); got != 3 { // parent + 2 subs
		t.Fatalf("want 3 rows, got %d", got)
	}

	// navigate onto the first sub, toggle it: parent stays open
	m = drive(m, "j", " ")
	if !m.items[0].Subs[0].Done {
		t.Fatal("space did not toggle the subtask")
	}
	if m.items[0].Done {
		t.Fatal("parent went done with one sub still open")
	}
	// toggle the last sub: parent auto-completes
	m = drive(m, "j", " ")
	if !m.items[0].Done {
		t.Fatalf("all subs done should complete parent: %+v", m.items[0])
	}

	// undo restores the prior sub state (deep-copy snapshot, not shared slice)
	m = drive(m, "U")
	if m.items[0].Done || m.items[0].Subs[1].Done {
		t.Fatalf("undo did not restore sub state: %+v", m.items[0])
	}

	// cursor sits on the still-open second subtask; tab cycles its status
	ref := m.selRef()
	if ref.sub < 0 || m.items[0].Subs[ref.sub].Done {
		t.Fatalf("expected cursor on an open subtask row: %+v", ref)
	}
	m = drive(m, "tab")
	if m.items[0].Subs[ref.sub].Status != "in-progress" {
		t.Fatalf("tab did not set subtask status: %+v", m.items[0].Subs[ref.sub])
	}

	// category (g) stays parent-only: a no-op on a sub row
	if got := drive(m, "g"); got.mode != modeList {
		t.Fatalf("g on a subtask row should be a no-op, entered mode %d", got.mode)
	}
	// due/defer/link DO work on a sub: set a due date via t
	m = drive(m, "t")
	m.input.SetValue("tomorrow")
	m = drive(m, "enter")
	if m.items[0].Subs[ref.sub].Due == "" {
		t.Fatalf("t on a subtask row did not set its due date: %+v", m.items[0].Subs[ref.sub])
	}

	// x on a sub row removes just that subtask
	before := len(m.items[0].Subs)
	m = drive(m, "x")
	if len(m.items[0].Subs) != before-1 {
		t.Fatalf("x did not delete the subtask: %d subs", len(m.items[0].Subs))
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
	m := model{input: textinput.New(), note: textarea.New(), items: []todo.Item{{Text: "task"}}}
	m = drive(m, "d")
	if m.mode != modeDetail {
		t.Fatal("d did not open detail")
	}
	m = drive(m, "e")
	if m.mode != modeNote {
		t.Fatal("e did not open the note editor")
	}
	// typing saves live; enter inserts a newline (multi-line note)
	m = drive(m, "a", "b", "enter", "c")
	if m.items[0].Note != "ab\nc" {
		t.Fatalf("note did not save live: %q", m.items[0].Note)
	}
	m = drive(m, "esc")
	if m.mode != modeDetail || m.items[0].Note != "ab\nc" {
		t.Fatalf("esc should close keeping the saved note: %q mode=%d", m.items[0].Note, m.mode)
	}
	m = drive(m, "q")
	if m.mode != modeList {
		t.Fatal("q did not return to list")
	}
}

func TestSubtaskDetail(t *testing.T) {
	m := model{input: textinput.New(), note: textarea.New(), w: 50, height: 24,
		mode: modeDetail, cursor: 1,
		items: []todo.Item{{Text: "parent task", Subs: []todo.Item{{Text: "child step"}}}}}
	out := m.detailView()
	if !strings.Contains(out, "child step") {
		t.Fatal("detail did not show the subtask text")
	}
	if !strings.Contains(out, "parent task") {
		t.Fatal("subtask detail did not show the parent name")
	}
}

func TestDetailNoteWraps(t *testing.T) {
	long := "this is a long note that should wrap onto several lines in detail"
	m := model{input: textinput.New(), w: 30, height: 24, mode: modeDetail,
		items: []todo.Item{{Text: "task", Note: long}}}
	wrapped := lipgloss.NewStyle().Width(m.width()).Render(long)
	if !strings.Contains(wrapped, "\n") {
		t.Fatalf("test setup: note should wrap at width %d", m.width())
	}
	if !strings.Contains(m.detailView(), wrapped) {
		t.Fatal("detail view did not wrap the note")
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
	if !strings.Contains(v, strings.Repeat("┈", 46)) { // 50 - 2*padX
		t.Fatal("divider not inner width")
	}
	for _, want := range []string{"work", "uncategorized", "0/2", "move", "edit", "fields", "board", "space toggle", "^e editor"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	if got := strings.Count(v, "\n") + 1; got != 20 {
		t.Fatalf("frame not pinned to height: %d rows", got)
	}
}

func TestListScroll(t *testing.T) {
	items := make([]todo.Item, 40)
	for i := range items {
		items[i] = todo.Item{Text: fmt.Sprintf("item-%02d", i), Category: "work"}
	}
	m := model{input: textinput.New(), w: 50, height: 20, items: items, cursor: 30}
	v := m.View()
	if !strings.Contains(v, "item-30") {
		t.Fatal("cursor row scrolled out of view")
	}
	if strings.Contains(v, "item-00") {
		t.Fatal("distant top row should be windowed out")
	}
	if got := strings.Count(v, "\n") + 1; got != 20 {
		t.Fatalf("windowed view should still fill height 20, got %d", got)
	}
}

func TestFilterFarRight(t *testing.T) {
	m := model{input: textinput.New(), w: 60, height: 10, filter: "bank",
		items: []todo.Item{{Text: "call bank"}}}
	head := m.header()
	first := strings.SplitN(head, "\n", 2)[0]
	if !strings.Contains(first, appSubtitle) || !strings.Contains(first, "/bank") || !strings.Contains(first, "0/1") || !strings.HasSuffix(first, "● saved") {
		t.Fatalf("header layout wrong: %q", first)
	}
	if lipgloss.Width(first) != 56 { // 60 - 2*padX
		t.Fatalf("header line width = %d, want 56", lipgloss.Width(first))
	}
}

func TestAutosaveAndManualSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n- [ ] beta\n"), 0o644)

	// Debounced autosave: a mutation marks dirty; a tick before the idle gap
	// elapses does NOT save; a tick after it does.
	m := model{path: p, items: store.Load(p), input: textinput.New(), autosaveEvery: time.Minute}
	m.markSaved()
	m = drive(m, "x") // delete "alpha" → dirty
	if !m.dirty {
		t.Fatal("mutation did not mark dirty")
	}
	nm, _ := m.Update(tickMsg{})
	m = nm.(model)
	if !m.dirty {
		t.Fatal("autosave fired before debounce elapsed")
	}
	m.lastEdit = m.lastEdit.Add(-2 * time.Minute) // pretend the idle gap passed
	nm, _ = m.Update(tickMsg{})
	m = nm.(model)
	if m.dirty {
		t.Fatal("autosave did not fire after debounce elapsed")
	}
	if got := len(store.Load(p)); got != 1 {
		t.Fatalf("autosave wrote wrong contents: %d items", got)
	}

	// autosave = 0 disables the tick save; only manual w / quit persist.
	m2 := model{path: p, items: store.Load(p), input: textinput.New(), autosaveEvery: 0}
	m2.markSaved()
	m2 = drive(m2, "x") // delete the last item → dirty, 0 items in memory
	m2.lastEdit = m2.lastEdit.Add(-2 * time.Minute)
	nm, _ = m2.Update(tickMsg{})
	m2 = nm.(model)
	if !m2.dirty {
		t.Fatal("autosave=0 should never autosave")
	}
	m2 = drive(m2, "w") // manual save
	if m2.dirty {
		t.Fatal("w did not clear dirty")
	}
	if got := len(store.Load(p)); got != 0 {
		t.Fatalf("manual save wrote wrong contents: %d items", got)
	}
}

// TestDirtyUndoRedo covers the regression: undo/redo back to the on-disk
// content must clear the saved indicator, and a view switch must never dirty it.
func TestDirtyUndoRedo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n- [ ] beta\n"), 0o644)
	m := model{path: p, items: store.Load(p), input: textinput.New()}
	m.resort()
	m.markSaved()
	if m.dirty {
		t.Fatal("freshly loaded board should be clean")
	}
	m = drive(m, " ") // toggle alpha done
	if !m.dirty {
		t.Fatal("edit should mark dirty")
	}
	m = drive(m, "U") // undo back to the saved content
	if m.dirty {
		t.Fatal("undo to saved state should be clean")
	}
	m = drive(m, "ctrl+r") // redo away from saved content
	if !m.dirty {
		t.Fatal("redo away from saved state should be dirty")
	}
	m = drive(m, "U") // undo again → clean
	m = drive(m, "v") // view switch reorders items but changes nothing
	if m.dirty {
		t.Fatal("view switch should not mark dirty")
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

// TestArchiveBrowse checks e opens the read-only archive browser over the
// board's archived items, j/k move the cursor, and esc returns to the board.
func TestArchiveBrowse(t *testing.T) {
	m := model{input: textinput.New(),
		items:    []todo.Item{{Text: "active task"}},
		archived: []todo.Item{{Text: "done one", Done: true}, {Text: "done two", Done: true}},
		w:        80, height: 24}

	m = drive(m, "e")
	if m.mode != modeArchive || len(m.arcRows) != 2 {
		t.Fatalf("e should open archive with 2 rows: mode=%d rows=%d", m.mode, len(m.arcRows))
	}
	if !strings.Contains(m.archiveView(), "done one") {
		t.Fatalf("archive view missing item: %q", m.archiveView())
	}
	m = drive(m, "j")
	if m.arcCur != 1 {
		t.Fatalf("j should move cursor to 1, got %d", m.arcCur)
	}
	m = drive(m, "esc")
	if m.mode != modeList {
		t.Fatalf("esc should return to list, mode=%d", m.mode)
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

func TestTabCyclesStatus(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n"), 0o644)
	m := model{path: p, items: store.Load(p), input: textinput.New(),
		statuses: []string{"open", "in-progress", "done"}}

	m = drive(m, "tab")
	if m.items[0].Done || m.items[0].Status != "in-progress" {
		t.Fatalf("tab did not set in-progress: %+v", m.items[0])
	}
	m = drive(m, "tab")
	if !m.items[0].Done || m.items[0].Status != "" {
		t.Fatalf("tab did not advance to done: %+v", m.items[0])
	}
}

func TestLoadConfigStatuses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	// "done" given out of order + duplicated: normalize dedups and forces it last.
	_ = os.WriteFile(p, []byte("statuses = [\"open\", \"done\", \"in-progress\", \"open\"]\n"), 0o644)
	c := loadConfig(p)
	got := strings.Join(c.statuses, ",")
	if got != "open,in-progress,done" {
		t.Fatalf("statuses not normalized: %q", got)
	}

	// Unset: default two-state open/done.
	d := loadConfig(filepath.Join(dir, "nope.toml"))
	if strings.Join(d.statuses, ",") != "open,done" {
		t.Fatalf("default statuses wrong: %+v", d.statuses)
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
