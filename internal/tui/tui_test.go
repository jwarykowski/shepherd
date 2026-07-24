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
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
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

// TestBoardsPickerJump checks b opens the board picker and enter jumps to the
// selected board, rebuilding the model against that board's file.
func TestBoardsPickerJump(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")
	t.Setenv("SHEPHERD_CONFIG", "") // else config/boards writes hit the real config dir
	base := filepath.Join(home, ".config", "shepherd")
	if err := os.MkdirAll(filepath.Join(base, "boards"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(base, "todo.md"), []byte("- [ ] alpha\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "boards", "web.md"), []byte("- [ ] beta\n"), 0o644)

	m := model{path: filepath.Join(base, "todo.md"), board: "", input: textinput.New(),
		items: store.Load(filepath.Join(base, "todo.md"))}

	m = drive(m, "b") // open picker
	if m.mode != modeBoards {
		t.Fatalf("b did not open picker, mode=%d", m.mode)
	}
	if len(m.projRows) != 2 || m.projCur != 0 {
		t.Fatalf("picker rows/cursor wrong: rows=%d cur=%d", len(m.projRows), m.projCur)
	}
	m = drive(m, "j", "enter") // select web, jump
	if m.board != "web" || m.mode != modeList {
		t.Fatalf("jump failed: board=%q mode=%d", m.board, m.mode)
	}
	if len(m.items) != 1 || m.items[0].Text != "beta" {
		t.Fatalf("did not load web board: %+v", m.items)
	}
}

// TestSettingsEditor drives the settings screen: cycle the enum rows, edit the
// text rows, and confirm changes apply to the model and persist to config.toml.
func TestSettingsEditor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_CONFIG", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")

	m := model{input: textinput.New(), statuses: []string{"open", "done"}, autosaveEvery: 60 * time.Second}

	m = drive(m, ",")
	if m.mode != modeSettings {
		t.Fatalf(", did not open settings, mode=%d", m.mode)
	}
	if v := m.View(); !strings.Contains(v, "autosave") || !strings.Contains(v, "settings") {
		t.Fatalf("settings screen did not render fields:\n%s", v)
	}
	m = drive(m, "enter") // row 0 view: category -> priority
	if m.view != viewPriority {
		t.Fatalf("view not cycled: %v", m.view)
	}
	m = drive(m, "j", "enter") // row 1 density -> comfort
	if m.density != comfort {
		t.Fatal("density not toggled")
	}
	m = drive(m, "j", "enter") // row 2 autosave: edit
	if m.mode != modeSettingEdit {
		t.Fatal("enter did not open the editor")
	}
	m.input.SetValue("30")
	m = drive(m, "enter")
	if m.autosaveEvery != 30*time.Second {
		t.Fatalf("autosave not applied: %v", m.autosaveEvery)
	}
	m = drive(m, "j", "enter") // row 3 categories
	m.input.SetValue("work, home")
	m = drive(m, "enter")
	m = drive(m, "j", "enter") // row 4 statuses
	m.input.SetValue("open, wip")
	m = drive(m, "enter")
	if len(m.statuses) != 3 || m.statuses[2] != "done" || m.statuses[1] != "wip" {
		t.Fatalf("statuses not normalized: %+v", m.statuses)
	}

	// everything persisted to config.toml
	got := loadConfig(store.ConfigPath())
	if got.view != viewPriority || got.density != comfort || got.autosave != 30 {
		t.Fatalf("config not persisted: %+v", got)
	}
	if strings.Join(got.categories, ",") != "work,home" {
		t.Fatalf("categories not persisted: %+v", got.categories)
	}
	if strings.Join(got.statuses, ",") != "open,wip,done" {
		t.Fatalf("statuses not persisted: %+v", got.statuses)
	}

	// clearing statuses must keep a non-terminal default so tab-cycle can reopen.
	if s := normalizeStatuses(nil); len(s) != 2 || s[0] != "open" || s[1] != "done" {
		t.Fatalf("empty statuses not guarded: %+v", s)
	}
	if s := normalizeStatuses([]string{"done"}); len(s) != 2 || s[0] != "open" {
		t.Fatalf("done-only statuses not guarded: %+v", s)
	}
}

// TestBoardPickerActions checks archive and delete from the picker act on the
// selected board and refresh the list.
func TestBoardPickerActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")
	t.Setenv("SHEPHERD_CONFIG", "") // else config/boards writes hit the real config dir
	base := filepath.Join(home, ".config", "shepherd")
	if err := os.MkdirAll(filepath.Join(base, "boards"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(base, "todo.md"), []byte("- [ ] a\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "boards", "web.md"), []byte("- [ ] b\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "boards", "api.md"), []byte("- [ ] c\n"), 0o644)

	// current board = default; picker rows: default, api, web
	m := model{path: filepath.Join(base, "todo.md"), board: "", input: textinput.New(),
		items: store.Load(filepath.Join(base, "todo.md"))}

	// archive: move cursor to api (row 1) and press A
	m = drive(m, "b", "j", "A")
	if _, err := os.Stat(store.TodoPathFor("api")); err == nil {
		t.Fatal("A did not archive the selected board")
	}
	if _, err := os.Stat(filepath.Join(base, "boards", "archived", "api.md")); err != nil {
		t.Fatal("archived file not in boards/archived/")
	}
	if len(m.projRows) != 2 { // default, web
		t.Fatalf("picker not refreshed after archive: %+v", m.projRows)
	}

	// delete: cursor now on web (row 1 after refresh), confirm with y
	m = drive(m, "j", "x", "y")
	if _, err := os.Stat(store.TodoPathFor("web")); err == nil {
		t.Fatal("x+y did not delete the selected board")
	}
	if len(m.projRows) != 1 {
		t.Fatalf("expected only default left, got %+v", m.projRows)
	}

	// create: press a, type a name, enter → board made, then prompts for a dir
	m = drive(m, "a")
	m.input.SetValue("fresh")
	m = drive(m, "enter")
	if _, err := os.Stat(store.TodoPathFor("fresh")); err != nil {
		t.Fatal("a+enter did not create the board")
	}
	// dir step: type a working dir, enter → saved and picker lands on the board
	m.input.SetValue("/tmp/fresh-src")
	m = drive(m, "enter")
	if got := store.BoardDir("fresh"); got != "/tmp/fresh-src" {
		t.Fatalf("board dir not saved: %q", got)
	}
	if len(m.projRows) != 2 || m.projRows[m.projCur].Name != "fresh" {
		t.Fatalf("picker not refreshed onto new board: cur=%d rows=%+v", m.projCur, m.projRows)
	}

	// invalid name (spaces): rejected with a visible notice, no file created.
	m = drive(m, "a")
	m.input.SetValue("bad name")
	m = drive(m, "enter")
	if m.projNotice == "" {
		t.Fatal("invalid board name gave no feedback")
	}
	if _, err := os.Stat(store.TodoPathFor("bad name")); err == nil {
		t.Fatal("invalid board name was created anyway")
	}
}

// TestBoardUnarchive archives a board, toggles the picker to the archived view
// with e, and restores it with u.
func TestBoardUnarchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")
	t.Setenv("SHEPHERD_CONFIG", "") // else config/boards writes hit the real config dir
	base := filepath.Join(home, ".config", "shepherd")
	if err := os.MkdirAll(filepath.Join(base, "boards"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(base, "todo.md"), []byte("- [ ] a\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "boards", "web.md"), []byte("- [ ] b\n"), 0o644)

	m := model{path: filepath.Join(base, "todo.md"), board: "", input: textinput.New()}

	// archive web (row 1), then toggle to the archived view with e
	m = drive(m, "b", "j", "A", "e")
	if !m.projArchived || len(m.projRows) != 1 || m.projRows[0].Name != "web" {
		t.Fatalf("archived view not showing web: archived=%v rows=%+v", m.projArchived, m.projRows)
	}
	// unarchive it: back to live boards, file restored
	m = drive(m, "u")
	if m.projArchived {
		t.Fatal("still in archived view after unarchive")
	}
	if _, err := os.Stat(store.TodoPathFor("web")); err != nil {
		t.Fatal("u did not restore the board file")
	}
	if _, err := os.Stat(filepath.Join(base, "boards", "archived", "web.md")); err == nil {
		t.Fatal("archived copy still present after unarchive")
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
	m = drive(m, "n")
	if m.mode != modeNote {
		t.Fatal("n did not open the note editor")
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
	m = drive(m, "esc")
	if m.mode != modeList {
		t.Fatal("esc did not return to list from detail")
	}
}

// TestQuitKeys checks q quits (a tea.Quit cmd) from the non-text modes and that
// ctrl+c quits from every mode, including text input, while q stays literal
// text inside an input field. esc backs out of an overlay instead of quitting.
func TestQuitKeys(t *testing.T) {
	isQuit := func(m model, md mode, s string) bool {
		m.mode = md
		_, cmd := m.Update(key(s))
		return cmd != nil && cmd() == tea.Quit()
	}
	for _, mode := range []mode{modeList, modeDetail, modeHelp, modeArchive, modeBoards, modeSettings, modeConfirmDelete} {
		if !isQuit(model{input: textinput.New()}, mode, "q") {
			t.Errorf("q did not quit from mode %d", mode)
		}
		if !isQuit(model{input: textinput.New()}, mode, "ctrl+c") {
			t.Errorf("ctrl+c did not quit from mode %d", mode)
		}
	}
	// ctrl+c aborts from a text-entry mode; q is literal there.
	if !isQuit(model{input: textinput.New()}, modeAdd, "ctrl+c") {
		t.Error("ctrl+c did not quit from text input")
	}
	if isQuit(model{input: textinput.New()}, modeAdd, "q") {
		t.Error("q should be literal text in an input, not quit")
	}
	// esc backs out of help rather than quitting.
	m := model{input: textinput.New(), mode: modeHelp}
	nm, _ := m.Update(key("esc"))
	if nm.(model).mode != modeList {
		t.Error("esc did not close help")
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
	if !strings.Contains(v, appName) {
		t.Fatal("brand missing")
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
	m := model{input: textinput.New(), w: 80, height: 10, filter: "bank",
		items: []todo.Item{{Text: "call bank"}}}
	head := m.header()
	first := strings.SplitN(head, "\n", 2)[0]
	if !strings.Contains(first, appName) || !strings.Contains(first, "/bank") || !strings.Contains(first, "0/1") || !strings.HasSuffix(first, "● saved") {
		t.Fatalf("header layout wrong: %q", first)
	}
	if lipgloss.Width(first) != 76 { // 80 - 2*padX
		t.Fatalf("header line width = %d, want 76", lipgloss.Width(first))
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

// C archives just the item under the cursor (any status), leaving the rest.
func TestArchiveSelected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] first\n- [ ] second\n"), 0o644)
	m := model{path: p, input: textinput.New(), items: store.Load(p)}
	m = drive(m, "j", "C") // cursor onto "second", archive it
	if len(m.items) != 1 || m.items[0].Text != "first" {
		t.Fatalf("C left wrong items: %+v", m.items)
	}
	arch, err := os.ReadFile(filepath.Join(dir, "archive.md"))
	if err != nil || !strings.Contains(string(arch), "second") {
		t.Fatalf("archive.md missing selected item: %q err=%v", arch, err)
	}
}

// C is a no-op on a subtask row: the archive holds whole items only.
func TestArchiveSelectedSkipsSubtask(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	m := model{path: p, input: textinput.New(), items: []todo.Item{
		{Text: "parent", Subs: []todo.Item{{Text: "child"}}},
	}}
	m = drive(m, "j", "C") // cursor on the subtask row
	if len(m.items) != 1 || len(m.items[0].Subs) != 1 {
		t.Fatalf("C on a subtask must not archive: %+v", m.items)
	}
	if _, err := os.Stat(filepath.Join(dir, "archive.md")); err == nil {
		t.Fatal("C on a subtask wrote to the archive")
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
	nm, _ := m.updateHelp(tea.KeyMsg{Type: tea.KeyEsc})
	if nm.(model).mode != modeList {
		t.Error("esc did not close help")
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
		if !strings.Contains(m.View(), appName) {
			t.Errorf("view %d missing brand", v)
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

func TestStatusCycleAndDoneToggle(t *testing.T) {
	newModel := func() model {
		return model{input: textinput.New(), note: textarea.New(), w: 50, height: 24,
			statuses: []string{"open", "in-progress", "done"}, cursor: 0,
			items: []todo.Item{{Text: "deploy"}}}
	}
	// space toggles done on any item
	nm, _ := newModel().updateList(key(" "))
	if got := nm.(model).items[0]; !got.Done {
		t.Fatalf("space did not toggle done: %+v", got)
	}
	// tab advances the configured status order: open → in-progress (fresh model,
	// since updateList mutates the shared items backing array in place)
	next, _ := newModel().updateList(key("tab"))
	if got := next.(model).items[0].Status; got != "in-progress" {
		t.Fatalf("tab did not advance to in-progress: %q", got)
	}
}

func TestGlobalReadOnly(t *testing.T) {
	m := model{
		input:  textinput.New(),
		global: true,
		view:   viewBoard,
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

	// v cycles through all 4 modes back to board
	if got := drive(m, "v", "v", "v", "v"); got.view != viewBoard {
		t.Fatalf("v cycle did not return to board after 4 steps: %v", got.view)
	}

	// items group by source; header id/label is the board name
	if id, label := m.groupOf(m.items[0]); id != "sdefault" || label != "default" {
		t.Fatalf("board group wrong: %q %q", id, label)
	}
	if d, tot := m.groupCount(m.items[1]); d != 0 || tot != 1 {
		t.Fatalf("board group count wrong: %d/%d", d, tot)
	}
}

// TestSaveConfigRoundTrip checks saveConfig writes the managed keys so
// loadConfig reads them back unchanged.
func TestSaveConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	want := config{view: viewTable, density: comfort, autosave: 30, categories: []string{"work"}, statuses: []string{"open", "done"}, hideFooter: true}
	if err := saveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	got := loadConfig(path)
	if got.view != want.view || got.density != want.density || got.autosave != want.autosave || got.hideFooter != want.hideFooter {
		t.Fatalf("scalar keys round-trip wrong: %+v", got)
	}
	if strings.Join(got.categories, ",") != "work" || strings.Join(got.statuses, ",") != "open,done" {
		t.Fatalf("list keys round-trip wrong: %+v", got)
	}
}

// TestFooterToggle checks F drops the help grid but keeps the repo/version line.
func TestFooterToggle(t *testing.T) {
	m := model{input: textinput.New(), items: []todo.Item{{Text: "a"}}}
	if m.hideFooter || !strings.Contains(m.listFooter(), "toggle") {
		t.Fatal("help grid should be shown by default")
	}
	m = drive(m, "F")
	if !m.hideFooter {
		t.Fatal("F should set hideFooter")
	}
	foot := m.listFooter()
	if strings.Contains(foot, "toggle") {
		t.Fatalf("help grid should be hidden; footer=%q", foot)
	}
	if !strings.Contains(foot, repoName) {
		t.Fatalf("repo/version line should stay visible; footer=%q", foot)
	}
	m = drive(m, "F")
	if m.hideFooter || !strings.Contains(m.listFooter(), "toggle") {
		t.Fatal("F should toggle the help grid back on")
	}
}
