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

func TestStatusRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	src := "- [ ] wip\n  status: in-progress\n- [x] fin\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	items := Load(p)
	if items[0].Status != "in-progress" {
		t.Fatalf("status not parsed: %+v", items[0])
	}
	if err := Save(p, items); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, p)) != src {
		t.Fatalf("status round-trip mismatch:\n%s", mustRead(t, p))
	}

	// A done item never serializes a status line even if the field is set.
	done := []todo.Item{{Done: true, Status: "in-progress", Text: "x"}}
	if strings.Contains(Serialize(done), "status:") {
		t.Fatalf("done item leaked status line:\n%s", Serialize(done))
	}
}

func TestNoteMultilineRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	// a multi-line note serializes as one note: line per physical line.
	src := "- [ ] task\n  note: first line\n  note: second line\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	items := Load(p)
	if items[0].Note != "first line\nsecond line" {
		t.Fatalf("multi-line note not joined: %q", items[0].Note)
	}
	if err := Save(p, items); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, p)); got != src {
		t.Fatalf("note round-trip mismatch:\n%s", got)
	}
}

// TestRoundTripMetadata covers the added fields (completed, defer, link) parse
// back and re-serialize in the fixed field order.
func TestRoundTripMetadata(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	src := "- [x] (H) ship it\n  created: 2026-07-10 13:40\n  completed: 2026-07-12 09:00\n  defer: 2026-07-11\n  due: 2026-07-15\n  category: work\n  link: https://ex.com/pr/1\n  note: block first\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	items := Load(p)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	it := items[0]
	if it.Completed != "2026-07-12 09:00" || it.Defer != "2026-07-11" || it.Link != "https://ex.com/pr/1" {
		t.Fatalf("new metadata not parsed: %+v", it)
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
	// SHEPHERD_TODO_FILE is the whole-file override.
	t.Setenv("SHEPHERD_TODO_FILE", "/x/y.md")
	if got := TodoPath(); got != "/x/y.md" {
		t.Errorf("SHEPHERD_TODO_FILE not honoured: %q", got)
	}
	_ = os.Unsetenv("SHEPHERD_TODO_FILE")

	// The old HERDR_ vars no longer affect paths.
	t.Setenv("HERDR_TODO_FILE", "/x/y.md")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/state")
	if got := TodoPath(); !strings.HasSuffix(got, "/.config/shepherd/todo.md") {
		t.Errorf("HERDR_ vars should be ignored, got %q", got)
	}
	_ = os.Unsetenv("HERDR_TODO_FILE")
	_ = os.Unsetenv("HERDR_PLUGIN_STATE_DIR")
	if got := TodoPath(); !strings.HasSuffix(got, "/.config/shepherd/todo.md") {
		t.Errorf("default path = %q", got)
	}

	// A named project -> projects/<name>.md under the base dir.
	if got := TodoPathFor("web"); !strings.HasSuffix(got, "/.config/shepherd/projects/web.md") {
		t.Errorf("project path = %q", got)
	}
}

func TestConfigPath(t *testing.T) {
	t.Setenv("SHEPHERD_CONFIG", "/explicit/config.toml")
	if got := ConfigPath(); got != "/explicit/config.toml" {
		t.Errorf("explicit config = %q", got)
	}
	_ = os.Unsetenv("SHEPHERD_CONFIG")

	// With a whole-file override, config is its sibling.
	t.Setenv("SHEPHERD_TODO_FILE", "/data/todo.md")
	if got := ConfigPath(); got != "/data/config.toml" {
		t.Errorf("sibling config = %q, want /data/config.toml", got)
	}
	_ = os.Unsetenv("SHEPHERD_TODO_FILE")

	// Otherwise shared at the base dir (also for project boards).
	if got := ConfigPath(); !strings.HasSuffix(got, "/.config/shepherd/config.toml") {
		t.Errorf("base config = %q", got)
	}
}

func TestResolveProject(t *testing.T) {
	t.Setenv("SHEPHERD_PROJECT", "envproj")
	if got, err := ResolveProject("flagproj"); err != nil || got != "flagproj" {
		t.Fatalf("flag should win: %q %v", got, err)
	}
	if got, err := ResolveProject(""); err != nil || got != "envproj" {
		t.Fatalf("env fallback: %q %v", got, err)
	}
	_ = os.Unsetenv("SHEPHERD_PROJECT")
	if got, err := ResolveProject(""); err != nil || got != "" {
		t.Fatalf("empty -> default: %q %v", got, err)
	}
	if _, err := ResolveProject("../evil"); err == nil {
		t.Fatal("traversal via flag not rejected")
	}
	t.Setenv("SHEPHERD_PROJECT", "../evil")
	if _, err := ResolveProject(""); err == nil {
		t.Fatal("traversal via env not rejected")
	}
}

func TestArchivePath(t *testing.T) {
	if got := ArchivePath("/c/shepherd/todo.md"); got != "/c/shepherd/archive.md" {
		t.Errorf("default archive = %q", got)
	}
	if got := ArchivePath("/c/shepherd/projects/web.md"); got != "/c/shepherd/projects/web-archive.md" {
		t.Errorf("project archive = %q", got)
	}
}

// seedBoards points BaseDir at a temp HOME and writes a default board plus two
// project boards (and an archive sibling that must be ignored).
func seedBoards(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHEPHERD_TODO_FILE", "")
	base := filepath.Join(home, ".config", "shepherd", "projects")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".config", "shepherd", "todo.md"), "- [ ] a\n")
	write(filepath.Join(base, "web.md"), "- [ ] b\n")
	write(filepath.Join(base, "api.md"), "- [ ] c\n")
	write(filepath.Join(base, "web-archive.md"), "- [x] old\n") // must be skipped
	return home
}

func TestBoards(t *testing.T) {
	seedBoards(t)
	bs := Boards()
	if len(bs) != 3 {
		t.Fatalf("want 3 boards, got %d: %+v", len(bs), bs)
	}
	// default first, then projects alphabetical; archive excluded.
	want := []string{"default", "api", "web"}
	for i, b := range bs {
		if b.Name != want[i] {
			t.Errorf("board %d = %q, want %q", i, b.Name, want[i])
		}
	}
}

func TestLoadAll(t *testing.T) {
	seedBoards(t)
	items := LoadAll()
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	bySource := map[string]string{}
	for _, it := range items {
		bySource[it.Source] = it.Text
	}
	if bySource["default"] != "a" || bySource["web"] != "b" || bySource["api"] != "c" {
		t.Fatalf("source tagging wrong: %+v", bySource)
	}
}
