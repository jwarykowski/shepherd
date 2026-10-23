package tui

import (
	"os"
	"path/filepath"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// tempHome isolates BaseDir and config under a temp HOME and returns BaseDir.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")
	t.Setenv("SHEPHERD_CONFIG", "")
	base := filepath.Join(home, ".config", "shepherd")
	if err := os.MkdirAll(filepath.Join(base, "boards"), 0o755); err != nil {
		t.Fatal(err)
	}
	return base
}

func texts(items []todo.Item) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[it.Text] = true
	}
	return out
}

// TestAutosaveKeepsOutsideWrite: an agent's CLI write made while the board has
// unsaved edits survives the next autosave instead of being overwritten.
func TestAutosaveKeepsOutsideWrite(t *testing.T) {
	base := tempHome(t)
	p := filepath.Join(base, "todo.md")
	_ = os.WriteFile(p, []byte("- [ ] alpha\n- [ ] beta\n"), 0o644)
	m := newModel("")
	m.autosaveEvery = time.Minute
	m = drive(m, "x") // delete the row under the cursor: dirty
	gone := "alpha"
	if texts(m.items)["alpha"] {
		gone = "beta"
	}

	time.Sleep(10 * time.Millisecond) // a distinct mtime on coarse filesystems
	_ = store.WithLock(p, func() error {
		return store.Save(p, append(store.Load(p), todo.Item{Text: "from-agent"}))
	})

	m.lastEdit = m.lastEdit.Add(-2 * time.Minute)
	nm, _ := m.Update(tickMsg{})
	m = nm.(model)
	got := texts(store.Load(p))
	if !got["from-agent"] || got[gone] || len(got) != 2 || !texts(m.items)["from-agent"] {
		t.Fatalf("after autosave: disk %v, board %v (deleted %q)", got, texts(m.items), gone)
	}
}

// TestNoteEditorKeepsItsItem: editing away the text the filter matched hides
// the item, but the keystrokes still go to its note, not the next row's.
func TestNoteEditorKeepsItsItem(t *testing.T) {
	m := model{input: textinput.New(), note: textarea.New(), filter: "zz", items: []todo.Item{
		{Text: "a", Note: "zz"}, {Text: "b zz"},
	}}
	m = drive(m, "d", "n")
	for range 3 {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = nm.(model)
	}
	m = drive(m, "Q")
	if m.items[0].Note != "Q" || m.items[1].Note != "" {
		t.Fatalf("notes: a=%q b=%q", m.items[0].Note, m.items[1].Note)
	}
}

// TestRenameSavesOpenBoard: renaming the open board keeps its unsaved edits.
func TestRenameSavesOpenBoard(t *testing.T) {
	base := tempHome(t)
	_ = os.WriteFile(filepath.Join(base, "boards", "web.md"), []byte("- [ ] one\n- [ ] two\n"), 0o644)
	m := newModel("web")
	m = drive(m, "x", "b", "r")
	m.input.SetValue("web2")
	m = drive(m, "enter")
	if got := store.Load(filepath.Join(base, "boards", "web2.md")); len(got) != 1 {
		t.Fatalf("web2 has %d items, want the unsaved delete kept", len(got))
	}
}

// TestSweepCannotBeUndone: the sweep writes archive.md at once, so undo would
// leave the items in both files; the board is saved and history dropped.
func TestSweepCannotBeUndone(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "todo.md")
	_ = os.WriteFile(p, []byte("- [x] done one\n- [ ] keep me\n"), 0o644)
	m := model{path: p, input: textinput.New(), items: store.Load(p)}
	m = drive(m, "c", "U")
	if texts(m.items)["done one"] || texts(store.Load(p))["done one"] {
		t.Fatal("undo put the swept item back on the board; it's also in archive.md")
	}
}

// TestLaneDetailSpaceStaysOnCard: space on the detail page in lane view
// follows the card, so a second space toggles the same card back.
func TestLaneDetailSpaceStaysOnCard(t *testing.T) {
	m := model{
		input: textinput.New(), view: viewLane, statuses: []string{"open", "done"},
		items: []todo.Item{{Text: "one"}, {Text: "two"}},
	}
	m = drive(m, "d", " ")
	if !lastByText(m, "one").Done || m.mode != modeDetail {
		t.Fatalf("first space: one done=%v mode=%d", lastByText(m, "one").Done, m.mode)
	}
	m = drive(m, " ")
	if lastByText(m, "one").Done || lastByText(m, "two").Done {
		t.Fatal("second space toggled a different card")
	}
}
