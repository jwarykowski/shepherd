package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/drover"
	"shepherd/internal/drover/drovertest"
	"shepherd/internal/store"
)

func agentModel(t *testing.T) (model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] ship it\n- [x] shipped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_TODO_FILE", path)
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(dir, "config.toml"))
	return newModel(""), path
}

// @ saves the board (so the item has an id), then sends it to drover in the
// background and says how it went.
func TestAtSendsTheItemToDrover(t *testing.T) {
	m, path := agentModel(t)
	calls := drovertest.Fake(t, `[]`)
	nm, cmd := m.updateList(key("@"))
	got := nm.(model)
	if cmd == nil || !strings.Contains(got.notice, "sending") {
		t.Fatalf("notice %q, cmd %v", got.notice, cmd != nil)
	}
	id := store.Load(path)[0].ID
	if id == "" {
		t.Fatal("board not saved with an id first")
	}
	msg := cmd().(sentMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if c := calls(); !strings.HasPrefix(c[len(c)-1], "run --subject "+id) {
		t.Fatalf("calls %q", c)
	}
	nm, _ = got.Update(msg)
	if n := nm.(model).notice; n != "sent to your agent: ship it" {
		t.Fatalf("notice %q", n)
	}
	// the next key clears it
	nm, _ = nm.(model).updateList(key("j"))
	if n := nm.(model).notice; n != "" {
		t.Fatalf("notice not cleared: %q", n)
	}
}

func TestAtWithoutDrover(t *testing.T) {
	m, _ := agentModel(t)
	t.Setenv("PATH", t.TempDir())
	nm, cmd := m.updateList(key("@"))
	if cmd != nil || !strings.Contains(nm.(model).notice, "isn't installed") {
		t.Fatalf("notice %q", nm.(model).notice)
	}
}

// Rows drover is holding or working on carry a mark; others don't.
func TestRunMarks(t *testing.T) {
	m, _ := agentModel(t)
	m.runs = map[string]drover.Run{
		"held": {ID: "r1", Status: "hold", Subject: "held"},
		"busy": {ID: "r2", Status: "running", Subject: "busy"},
	}
	if l := m.runLabel("held"); !strings.Contains(l, "waiting") {
		t.Fatalf("held = %q", l)
	}
	if l := m.runLabel("busy"); !strings.Contains(l, "running") {
		t.Fatalf("busy = %q", l)
	}
	if l := m.runLabel("idle"); l != "" {
		t.Fatalf("idle = %q", l)
	}
	if l := m.runLabel(""); l != "" {
		t.Fatalf("no id = %q", l)
	}
}
