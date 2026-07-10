package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	src := "- [ ] alpha\n- [x] (H) beta\n  created: 2026-07-10 13:40\n  category: work\n  note: from the store\n- [ ] (L) gamma\n"
	_ = os.WriteFile(p, []byte(src), 0o644)
	items := load(p)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if !items[1].done || items[1].prio != 'H' || items[1].text != "beta" {
		t.Fatalf("bad parse of beta: %+v", items[1])
	}
	if items[1].created != "2026-07-10 13:40" || items[1].note != "from the store" || items[1].category != "work" {
		t.Fatalf("metadata not parsed: %+v", items[1])
	}
	if err := save(p, items); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, p)) != src {
		t.Fatalf("round-trip mismatch:\n%s", mustRead(t, p))
	}
}

func TestSortStable(t *testing.T) {
	items := []item{{text: "a"}, {prio: 'L', text: "b"}, {prio: 'H', text: "c"}, {prio: 'H', text: "d"}}
	sortItems(items)
	got := ""
	for _, it := range items {
		got += it.text
	}
	if got != "cdba" { // H,H (stable), L, none
		t.Fatalf("want cdba, got %s", got)
	}
}

func TestSortByCategoryThenPrio(t *testing.T) {
	items := []item{
		{text: "a1", category: "alpha", prio: 'L'},
		{text: "b1", category: "beta", prio: 'H'},
		{text: "a2", category: "alpha", prio: 'H'},
		{text: "u1"}, // uncategorized -> last
		{text: "b2", category: "beta", prio: 'M'},
	}
	sortItems(items)
	got := ""
	for _, it := range items {
		got += it.text + " "
	}
	// alpha(H,L), beta(H,M), uncategorized
	if got != "a2 a1 b1 b2 u1 " {
		t.Fatalf("category+prio order wrong: %q", got)
	}
}

func TestModelActions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n- [ ] beta\n"), 0o644)
	m := model{path: p, items: load(p), input: textinput.New()}

	// toggle first item done
	m = drive(m, " ")
	if !m.items[0].done {
		t.Fatal("space did not toggle done")
	}
	// move down, set high priority -> beta jumps to top, cursor follows
	m = drive(m, "j", "h")
	if m.items[0].prio != 'H' || m.items[0].text != "beta" || m.cursor != 0 {
		t.Fatalf("prio+sort+cursor wrong: cursor=%d items=%+v", m.cursor, m.items)
	}
	// add an item — gets a created timestamp
	m = drive(m, "a")
	m.input.SetValue("gamma")
	m = drive(m, "enter")
	last := m.items[len(m.items)-1]
	if last.text != "gamma" || last.created == "" {
		t.Fatalf("add failed: %+v", last)
	}
	// delete current is now x (d opens detail)
	before := len(m.items)
	m = drive(m, "x")
	if len(m.items) != before-1 {
		t.Fatal("delete (x) failed")
	}
	// clear done removes toggled beta
	m = drive(m, "c")
	for _, it := range m.items {
		if it.done {
			t.Fatal("clear left a done item")
		}
	}
}

func TestPriorityToggleOff(t *testing.T) {
	m := model{input: textinput.New(), items: []item{{text: "a"}}}
	m = drive(m, "h") // set high
	if m.items[0].prio != 'H' {
		t.Fatalf("h did not set high: %+v", m.items[0])
	}
	m = drive(m, "h") // same again clears
	if m.items[0].prio != 0 {
		t.Fatalf("repeat h did not clear priority: %+v", m.items[0])
	}
	m = drive(m, "h", "m") // high then medium -> medium (not cleared)
	if m.items[0].prio != 'M' {
		t.Fatalf("different key should switch, not clear: %+v", m.items[0])
	}
}

func TestFilter(t *testing.T) {
	m := model{input: textinput.New(), items: []item{
		{text: "buy milk"}, {text: "buy eggs"}, {text: "call bank", note: "urgent"},
	}}
	// enter filter mode, type "buy"
	m = drive(m, "/")
	for _, r := range "buy" {
		m = drive(m, string(r))
	}
	if got := len(m.visible()); got != 2 {
		t.Fatalf("filter 'buy' want 2 visible, got %d", got)
	}
	// note is searched too
	m.input.SetValue("urgent")
	m.filter = "urgent"
	if got := m.visible(); len(got) != 1 || m.items[got[0]].text != "call bank" {
		t.Fatalf("note filter failed: %v", got)
	}
	// toggle acts on the visible item, not items[cursor]
	m = model{input: textinput.New(), items: []item{{text: "a"}, {text: "b"}, {text: "c"}}, filter: "c"}
	m = drive(m, " ")
	if !m.items[2].done || m.items[0].done {
		t.Fatalf("filtered toggle hit wrong item: %+v", m.items)
	}
}

func TestSetCategory(t *testing.T) {
	m := model{input: textinput.New(), items: []item{
		{text: "a", category: "work"}, {text: "b"},
	}}
	sortItems(m.items) // work first, b (uncategorized) last; cursor 0 -> "a"
	m = drive(m, "g")  // category mode on "a"
	m.input.SetValue("home")
	m = drive(m, "enter")
	// "a" now category home; re-sorted: home("a") before work? home<work -> a first
	if m.items[0].category != "home" || m.items[0].text != "a" {
		t.Fatalf("category set/sort wrong: %+v", m.items)
	}
	if m.sel() < 0 || m.items[m.sel()].text != "a" {
		t.Fatalf("cursor did not follow item after re-sort: sel=%d", m.sel())
	}
}

func TestDetailNote(t *testing.T) {
	m := model{input: textinput.New(), items: []item{{text: "task"}}}
	m = drive(m, "d") // open detail
	if m.mode != modeDetail {
		t.Fatal("d did not open detail")
	}
	m = drive(m, "e") // edit note
	m.input.SetValue("some note")
	m = drive(m, "enter")
	if m.items[0].note != "some note" || m.mode != modeDetail {
		t.Fatalf("note save failed: %+v mode=%d", m.items[0], m.mode)
	}
	m = drive(m, "q") // back to list
	if m.mode != modeList {
		t.Fatal("q did not return to list")
	}
}

func TestView(t *testing.T) {
	m := model{input: textinput.New(), w: 50, height: 20, items: []item{
		{text: "ship release", prio: 'H', category: "work"},
		{text: "buy milk"},
	}}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}) // no-op, ensure View path
	m = nm.(model)
	v := m.View()
	if !strings.Contains(v, appTitle) {
		t.Fatal("title/emoji missing")
	}
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
	// footer pinned to bottom: inner rows + vertical padding == pane height
	if got := strings.Count(v, "\n") + 1; got != 20 {
		t.Fatalf("frame not pinned to height: %d rows", got)
	}
}

func TestFilterFarRight(t *testing.T) {
	m := model{input: textinput.New(), w: 50, height: 10, filter: "bank",
		items: []item{{text: "call bank"}}}
	head := m.header()
	first := strings.SplitN(head, "\n", 2)[0]
	// title left; filter then done/total flush to the right edge (inner width 46)
	if !strings.Contains(first, appTitle) || !strings.Contains(first, "/bank") || !strings.HasSuffix(first, "0/1") {
		t.Fatalf("header layout wrong: %q", first)
	}
	if lipgloss.Width(first) != 46 {
		t.Fatalf("header line width = %d, want 46", lipgloss.Width(first))
	}
}

func TestUndoRedo(t *testing.T) {
	m := model{input: textinput.New(), items: []item{{text: "a"}, {text: "b"}}}
	m = drive(m, "x") // delete "a" -> [b]
	m = drive(m, "x") // delete "b" -> []
	if len(m.items) != 0 {
		t.Fatalf("deletes failed: %+v", m.items)
	}
	m = drive(m, "U") // undo -> [b]
	if len(m.items) != 1 || m.items[0].text != "b" {
		t.Fatalf("undo 1 wrong: %+v", m.items)
	}
	m = drive(m, "U") // undo -> [a b]
	if len(m.items) != 2 || m.items[0].text != "a" {
		t.Fatalf("undo 2 wrong: %+v", m.items)
	}
	m = drive(m, "ctrl+r") // redo -> [b]
	if len(m.items) != 1 || m.items[0].text != "b" {
		t.Fatalf("redo wrong: %+v", m.items)
	}
	m = drive(m, "a") // new edit invalidates redo
	m.input.SetValue("c")
	m = drive(m, "enter")
	if len(m.future) != 0 {
		t.Fatalf("new edit did not clear redo stack: %d", len(m.future))
	}
	m = drive(m, "ctrl+r") // no-op now
	if m.items[len(m.items)-1].text != "c" {
		t.Fatalf("redo after edit should be no-op: %+v", m.items)
	}
}

func TestDueLabel(t *testing.T) {
	today = func() string { return "2026-07-10" }
	defer func() { today = func() string { return time.Now().Format(dateFormat) } }()
	cases := []struct {
		due     string
		want    string
		overdue bool
	}{
		{"2026-07-08", "overdue 2d", true},
		{"2026-07-10", "due today", true},
		{"2026-07-11", "due 1d", false},
		{"2026-07-13", "due 3d", false},
		{"garbage", "garbage", false},
	}
	for _, c := range cases {
		got, over := dueLabel(c.due)
		if got != c.want || over != c.overdue {
			t.Fatalf("dueLabel(%q) = (%q,%v), want (%q,%v)", c.due, got, over, c.want, c.overdue)
		}
	}
}

func TestParseDue(t *testing.T) {
	today = func() string { return "2026-07-10" }
	defer func() { today = func() string { return time.Now().Format(dateFormat) } }()
	cases := map[string]string{
		"":           "",
		"today":      "2026-07-10",
		"tomorrow":   "2026-07-11",
		"week":       "2026-07-17",
		"next month": "2026-08-10",
		"+3d":        "2026-07-13",
		"1y":         "2027-07-10",
		"25-12-2026": "2026-12-25", // DMY input -> ISO storage
		"2026-12-25": "2026-12-25",
		"whatever":   "", // unrecognized -> blank
	}
	for in, want := range cases {
		if got := parseDue(in); got != want {
			t.Fatalf("parseDue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDueSort(t *testing.T) {
	items := []item{
		{text: "late", due: "2026-07-20"},
		{text: "none"},
		{text: "soon", due: "2026-07-11"},
	}
	sortItems(items) // same cat+prio: soonest due first, no-due last
	got := items[0].text + items[1].text + items[2].text
	if got != "soonlatenone" {
		t.Fatalf("due sort wrong: %q", got)
	}
}

func TestArchive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	_ = os.WriteFile(p, []byte("- [x] done one\n- [ ] keep me\n"), 0o644)
	m := model{path: p, input: textinput.New(), items: load(p)}
	m = drive(m, "c") // archive done items
	if len(m.items) != 1 || m.items[0].text != "keep me" {
		t.Fatalf("archive left wrong items: %+v", m.items)
	}
	arch, err := os.ReadFile(filepath.Join(dir, "archive.md"))
	if err != nil || !strings.Contains(string(arch), "done one") {
		t.Fatalf("archive.md missing done item: %q err=%v", arch, err)
	}
}

func TestQuickAdd(t *testing.T) {
	today = func() string { return "2026-07-10" }
	defer func() { today = func() string { return time.Now().Format(dateFormat) } }()
	it := parseQuickAdd("ship the thing @work !h due:tomorrow")
	if it.text != "ship the thing" || it.category != "work" || it.prio != 'H' || it.due != "2026-07-11" {
		t.Fatalf("quick-add parse wrong: %+v", it)
	}
	// unrecognized tokens stay in text
	it = parseQuickAdd("buy !x milk")
	if it.text != "buy !x milk" {
		t.Fatalf("bad token should stay: %q", it.text)
	}
}

func TestOverduePin(t *testing.T) {
	today = func() string { return "2026-07-10" }
	defer func() { today = func() string { return time.Now().Format(dateFormat) } }()
	items := []item{
		{text: "future", category: "work", due: "2026-08-01"},
		{text: "late", category: "home", due: "2026-07-01"}, // overdue
		{text: "plain", category: "work"},
	}
	sortItems(items)
	if items[0].text != "late" {
		t.Fatalf("overdue not pinned first: %+v", items)
	}
	m := model{items: items}
	if _, label := m.groupOf(items[0]); label != "⚠ overdue" {
		t.Fatalf("overdue group label wrong: %q", label)
	}
}

func TestViewToggle(t *testing.T) {
	m := model{input: textinput.New(), items: []item{
		{text: "a", category: "z", prio: 'L'},
		{text: "b", category: "a", prio: 'H'},
	}}
	m.resort() // category view: a(cat) before z(cat)
	if m.items[0].text != "b" {
		t.Fatalf("category sort wrong: %+v", m.items)
	}
	m = drive(m, "v") // -> priority view: H before L
	if m.view != viewPriority || m.items[0].text != "b" {
		t.Fatalf("priority view sort wrong: view=%d %+v", m.view, m.items)
	}
	m = drive(m, "v", "v") // table -> back to category
	if m.view != viewCategory {
		t.Fatalf("view did not cycle back: %d", m.view)
	}
}

func TestArchiveSearch(t *testing.T) {
	m := model{input: textinput.New(),
		items:    []item{{text: "active task"}},
		archived: []item{{text: "old bank thing", done: true}, {text: "unrelated"}},
		filter:   "bank"}
	got := m.archivedMatches()
	if len(got) != 1 || got[0].text != "old bank thing" {
		t.Fatalf("archive search wrong: %+v", got)
	}
	// no filter -> no archive results
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
	// missing file -> zero-value defaults (category view, compact)
	d := loadConfig(filepath.Join(dir, "nope.toml"))
	if d.view != viewCategory || d.density != compact || d.categories != nil {
		t.Fatalf("defaults wrong: %+v", d)
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
