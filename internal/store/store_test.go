package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/todo"
)

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	src := "- [ ] alpha\n- [x] (H) beta\n  created: 2026-07-10 13:40\n  category: work\n  note: from the store\n- [ ] (L) gamma\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	items := Load(p)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if !items[1].Done || items[1].Prio != 'H' || items[1].Text != "beta" {
		t.Fatalf("bad parse of beta: %+v", items[1])
	}
	if items[1].Created != "2026-07-10 13:40" || items[1].Note != "from the store" || items[1].Category != "work" {
		t.Fatalf("metadata not parsed: %+v", items[1])
	}
	if err := Save(p, items); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, p)) != src {
		t.Fatalf("round-trip mismatch:\n%s", mustRead(t, p))
	}
}

func TestAppendArchive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	done := []todo.Item{{Text: "done one", Done: true}}
	if err := AppendArchive(p, done); err != nil {
		t.Fatal(err)
	}
	if err := AppendArchive(p, []todo.Item{{Text: "done two", Done: true}}); err != nil {
		t.Fatal(err)
	}
	arch := string(mustRead(t, ArchivePath(p)))
	if !strings.Contains(arch, "done one") || !strings.Contains(arch, "done two") {
		t.Fatalf("archive missing appended items: %q", arch)
	}
}

func TestTodoPathResolution(t *testing.T) {
	t.Setenv("HERDR_TODO_FILE", "/x/y.md")
	if got := TodoPath(); got != "/x/y.md" {
		t.Errorf("HERDR_TODO_FILE not honoured: %q", got)
	}
	_ = os.Unsetenv("HERDR_TODO_FILE")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/state")
	if got := TodoPath(); got != "/state/todo.md" {
		t.Errorf("state dir path = %q, want /state/todo.md", got)
	}
	_ = os.Unsetenv("HERDR_PLUGIN_STATE_DIR")
	if got := TodoPath(); !strings.HasSuffix(got, "/.config/shepherd/todo.md") {
		t.Errorf("home fallback path = %q", got)
	}
}

func TestConfigPath(t *testing.T) {
	t.Setenv("SHEPHERD_CONFIG", "/explicit/config.toml")
	if got := ConfigPath(); got != "/explicit/config.toml" {
		t.Errorf("explicit config = %q", got)
	}
	_ = os.Unsetenv("SHEPHERD_CONFIG")
	t.Setenv("HERDR_TODO_FILE", "/data/todo.md")
	if got := ConfigPath(); got != "/data/config.toml" {
		t.Errorf("derived config = %q, want /data/config.toml", got)
	}
}
