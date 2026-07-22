package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"shepherd/internal/todo"
)

// TestWithLockSerialisesWriters proves the advisory lock closes the lost-update
// race: N concurrent load→append→save transactions must all survive. Without
// WithLock each writer clobbers the file another just wrote and the final count
// falls short of N. Each WithLock opens its own fd on the sidecar, so flock
// (per open-file-description) serialises them even within one process.
func TestWithLockSerialisesWriters(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = WithLock(p, func() error {
				items := append(Load(p), todo.Item{Text: fmt.Sprintf("t%d", i)})
				return Save(p, items)
			})
		}(i)
	}
	wg.Wait()
	if got := len(Load(p)); got != n {
		t.Fatalf("lost updates: want %d items, got %d", n, got)
	}
}

// TestMain pins NewID to empty so the byte-equality round-trip tests below
// exercise the legacy (id-less) on-disk format unchanged. Tests that care about
// ids set todo.NewID themselves and restore it.
func TestMain(m *testing.M) {
	todo.NewID = func() string { return "" }
	os.Exit(m.Run())
}

// TestIDBackfillAndRoundTrip covers ids end to end: Save mints one for every
// id-less item (legacy board), it persists as the first meta line, a reload
// reads it back, and a second Save keeps it rather than regenerating.
func TestIDBackfillAndRoundTrip(t *testing.T) {
	n := 0
	orig := todo.NewID
	todo.NewID = func() string { n++; return fmt.Sprintf("id%02d", n) }
	defer func() { todo.NewID = orig }()

	p := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(p, []byte("- [ ] parent\n  - [ ] step\n- [ ] solo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, Load(p)); err != nil { // Save backfills ids
		t.Fatal(err)
	}
	got := Load(p)
	if got[0].ID == "" || got[0].Subs[0].ID == "" || got[1].ID == "" {
		t.Fatalf("ids not backfilled+persisted: %+v", got)
	}
	if !strings.Contains(string(mustRead(t, p)), "- [ ] parent\n  id: ") {
		t.Fatalf("id not serialised as first meta line:\n%s", mustRead(t, p))
	}
	before := got[0].ID
	if err := Save(p, got); err != nil {
		t.Fatal(err)
	}
	if reloaded := Load(p); reloaded[0].ID != before {
		t.Fatalf("id changed on re-save: %q -> %q", before, reloaded[0].ID)
	}
}

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

	// A done item never serialises a status line even if the field is set.
	done := []todo.Item{{Done: true, Status: "in-progress", Text: "x"}}
	if strings.Contains(Serialize(done), "status:") {
		t.Fatalf("done item leaked status line:\n%s", Serialize(done))
	}
}

func TestNoteMultilineRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	// a multi-line note serialises as one note: line per physical line.
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
// back and re-serialise in the fixed field order.
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

// TestSubtaskRoundTrip covers nested subtasks: a two-space checklist line is a
// child, its four-space meta attaches to the child, and the whole thing
// re-serialises byte-identically with parent meta before the subs.
func TestSubtaskRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	src := "- [ ] (H) parent\n  created: 2026-07-10 13:40\n  - [ ] first step\n  - [x] (M) second step\n    due: 2026-07-15\n- [ ] loner\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	items := Load(p)
	if len(items) != 2 {
		t.Fatalf("want 2 parents, got %d: %+v", len(items), items)
	}
	if len(items[0].Subs) != 2 {
		t.Fatalf("want 2 subs on parent, got %d", len(items[0].Subs))
	}
	if items[0].Subs[1].Text != "second step" || !items[0].Subs[1].Done || items[0].Subs[1].Prio != 'M' || items[0].Subs[1].Due != "2026-07-15" {
		t.Fatalf("second sub parsed wrong: %+v", items[0].Subs[1])
	}
	if len(items[1].Subs) != 0 {
		t.Fatalf("loner should have no subs")
	}
	if err := Save(p, items); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, p)); got != src {
		t.Fatalf("subtask round-trip mismatch:\nwant %q\ngot  %q", src, got)
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

	// A named board -> boards/<name>.md under the base dir.
	if got := TodoPathFor("web"); !strings.HasSuffix(got, "/.config/shepherd/boards/web.md") {
		t.Errorf("board path = %q", got)
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

	// Otherwise shared at the base dir (also for board boards).
	if got := ConfigPath(); !strings.HasSuffix(got, "/.config/shepherd/config.toml") {
		t.Errorf("base config = %q", got)
	}
}

func TestResolveBoard(t *testing.T) {
	t.Setenv("SHEPHERD_BOARD", "envproj")
	if got, err := ResolveBoard("flagproj"); err != nil || got != "flagproj" {
		t.Fatalf("flag should win: %q %v", got, err)
	}
	if got, err := ResolveBoard(""); err != nil || got != "envproj" {
		t.Fatalf("env fallback: %q %v", got, err)
	}
	_ = os.Unsetenv("SHEPHERD_BOARD")
	if got, err := ResolveBoard(""); err != nil || got != "" {
		t.Fatalf("empty -> default: %q %v", got, err)
	}
	if _, err := ResolveBoard("../evil"); err == nil {
		t.Fatal("traversal via flag not rejected")
	}
	t.Setenv("SHEPHERD_BOARD", "../evil")
	if _, err := ResolveBoard(""); err == nil {
		t.Fatal("traversal via env not rejected")
	}
}

func TestArchivePath(t *testing.T) {
	if got := ArchivePath("/c/shepherd/todo.md"); got != "/c/shepherd/archive.md" {
		t.Errorf("default archive = %q", got)
	}
	if got := ArchivePath("/c/shepherd/boards/web.md"); got != "/c/shepherd/boards/web-archive.md" {
		t.Errorf("board archive = %q", got)
	}
}

// seedBoards points BaseDir at a temp HOME and writes a default board plus two
// board boards (and an archive sibling that must be ignored).
func seedBoards(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	base := filepath.Join(home, ".config", "shepherd", "boards")
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
	// default first, then boards alphabetical; archive excluded.
	want := []string{"default", "api", "web"}
	for i, b := range bs {
		if b.Name != want[i] {
			t.Errorf("board %d = %q, want %q", i, b.Name, want[i])
		}
	}
}

func TestCreateBoard(t *testing.T) {
	seedBoards(t)
	if err := CreateBoard("newone"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !fileExists(TodoPathFor("newone")) {
		t.Fatal("board file not created")
	}
	if CreateBoard("web") == nil {
		t.Fatal("creating an existing board should error")
	}
	if CreateBoard("../escape") == nil {
		t.Fatal("invalid name should error")
	}
	if err := ArchiveBoard("newone"); err != nil {
		t.Fatal(err)
	}
	if CreateBoard("newone") == nil {
		t.Fatal("creating a name that is archived should error")
	}
}

func TestRenameBoard(t *testing.T) {
	seedBoards(t)
	if err := RenameBoard("web", "webapp"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if fileExists(TodoPathFor("web")) {
		t.Fatal("old board still exists")
	}
	if !fileExists(TodoPathFor("webapp")) {
		t.Fatal("new board missing")
	}
	if !fileExists(ArchivePath(TodoPathFor("webapp"))) {
		t.Fatal("archive sibling not moved")
	}
	// guards: default refused, existing target refused
	if RenameBoard("default", "x") == nil {
		t.Fatal("renaming default should error")
	}
	if RenameBoard("api", "webapp") == nil {
		t.Fatal("renaming onto an existing board should error")
	}
	if RenameBoard("api", "../escape") == nil {
		t.Fatal("invalid target name should error")
	}
}

func TestDeleteBoard(t *testing.T) {
	seedBoards(t)
	if err := DeleteBoard("web"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if fileExists(TodoPathFor("web")) || fileExists(ArchivePath(TodoPathFor("web"))) {
		t.Fatal("board or archive sibling not removed")
	}
	if DeleteBoard("default") == nil {
		t.Fatal("deleting default should error")
	}
	if DeleteBoard("nope") == nil {
		t.Fatal("deleting a missing board should error")
	}
}

func TestArchiveUnarchiveBoard(t *testing.T) {
	seedBoards(t)
	if err := ArchiveBoard("web"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if fileExists(TodoPathFor("web")) {
		t.Fatal("archived board still live")
	}
	// hidden from Boards(), visible in ArchivedBoards()
	for _, b := range Boards() {
		if b.Name == "web" {
			t.Fatal("archived board should not appear in Boards()")
		}
	}
	arc := ArchivedBoards()
	if len(arc) != 1 || arc[0].Name != "web" {
		t.Fatalf("ArchivedBoards wrong: %+v", arc)
	}
	if err := UnarchiveBoard("web"); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if !fileExists(TodoPathFor("web")) || !fileExists(ArchivePath(TodoPathFor("web"))) {
		t.Fatal("board or archive sibling not restored")
	}
	if len(ArchivedBoards()) != 0 {
		t.Fatal("archived dir should be empty after unarchive")
	}
}

// TestBoardNameCollisions covers a live and an archived board sharing a name:
// create/archive/unarchive/rename must all refuse rather than clobber.
func TestBoardNameCollisions(t *testing.T) {
	seedBoards(t)
	if err := os.MkdirAll(archivedDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// "dup" exists both live and archived at once (constructed directly).
	if err := os.WriteFile(TodoPathFor("dup"), []byte("- [ ] live\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archivedDir(), "dup.md"), []byte("- [ ] arc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if CreateBoard("dup") == nil {
		t.Fatal("create onto a name that is live+archived should error")
	}
	if ArchiveBoard("dup") == nil {
		t.Fatal("archive onto an existing archived name should error")
	}
	if UnarchiveBoard("dup") == nil {
		t.Fatal("unarchive onto an existing live name should error")
	}
	if RenameBoard("api", "dup") == nil {
		t.Fatal("rename onto an archived name should error")
	}
	// the live and archived copies are untouched after every refusal.
	if !fileExists(TodoPathFor("dup")) || !fileExists(filepath.Join(archivedDir(), "dup.md")) {
		t.Fatal("a refused op clobbered a file")
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

// TestBaseDirXDG checks BaseDir honors $XDG_CONFIG_HOME and falls back to
// ~/.config when it is unset (clig: follow the XDG spec).
func TestBaseDirXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	if got, want := BaseDir(), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "shepherd"); got != want {
		t.Fatalf("XDG_CONFIG_HOME ignored: got %s want %s", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/example")
	if got, want := BaseDir(), filepath.Join("/home/example", ".config", "shepherd"); got != want {
		t.Fatalf("HOME fallback wrong: got %s want %s", got, want)
	}
}
