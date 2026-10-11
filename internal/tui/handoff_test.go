package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/store"
)

func handoffModel(t *testing.T, config string) (model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] ship it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_TODO_FILE", path)
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(dir, "config.toml"))
	return newModel(""), path
}

// @ with no handoff command says how to set one and leaves the item alone.
func TestHandoffKeyUnset(t *testing.T) {
	m, path := handoffModel(t, "")
	nm, cmd := m.updateList(key("@"))
	got := nm.(model)
	if cmd != nil || !strings.Contains(got.notice, "no handoff command") {
		t.Fatalf("notice = %q, cmd = %v", got.notice, cmd != nil)
	}
	if it := store.Load(path)[0]; it.Status != "" {
		t.Fatalf("status changed to %q", it.Status)
	}
	// the next key clears the notice
	nm, _ = got.updateList(key("j"))
	if n := nm.(model).notice; n != "" {
		t.Fatalf("notice not cleared: %q", n)
	}
}

// @ with a command marks the item in-progress, saves, and hands the terminal
// to the command.
func TestHandoffKeyRuns(t *testing.T) {
	m, path := handoffModel(t, `handoff = ["true", "{prompt}"]`+"\n")
	nm, cmd := m.updateList(key("@"))
	if cmd == nil || nm.(model).notice != "" {
		t.Fatalf("no command started (notice %q)", nm.(model).notice)
	}
	if it := store.Load(path)[0]; it.Status != "in-progress" || it.ID == "" {
		t.Fatalf("item not marked and saved: %+v", it)
	}
}
