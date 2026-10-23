package store

import (
	"os"
	"path/filepath"
	"shepherd/internal/todo"
	"strings"
	"testing"
)

// TestBoardOpsRejectTraversal: a board name can't reach outside boards/, and
// "../todo" can't get past the default-board guard.
func TestBoardOpsRejectTraversal(t *testing.T) {
	seedBoards(t)
	def := filepath.Join(BaseDir(), "todo.md")
	if err := DeleteBoard("../todo"); err == nil {
		t.Fatal("DeleteBoard(../todo) succeeded")
	}
	if err := ArchiveBoard("../todo"); err == nil {
		t.Fatal("ArchiveBoard(../todo) succeeded")
	}
	if err := RenameBoard("../todo", "stolen"); err == nil {
		t.Fatal("RenameBoard(../todo) succeeded")
	}
	if !fileExists(def) {
		t.Fatal("default board was touched")
	}
}

func TestReservedBoardNames(t *testing.T) {
	seedBoards(t)
	for _, n := range []string{"default", "todo", "archive", "web-archive"} {
		if err := CreateBoard(n); err == nil {
			t.Errorf("CreateBoard(%q) succeeded", n)
		}
		if err := RenameBoard("web", n); err == nil {
			t.Errorf("RenameBoard(web, %q) succeeded", n)
		}
	}
}

// TestOverrideDisablesBoards: with $SHEPHERD_TODO_FILE set every board resolves
// to that one file, so board operations must refuse rather than act on it.
func TestOverrideDisablesBoards(t *testing.T) {
	seedBoards(t)
	f := filepath.Join(t.TempDir(), "main.md")
	_ = os.WriteFile(f, []byte("- [ ] keep\n"), 0o644)
	t.Setenv("SHEPHERD_TODO_FILE", f)
	if err := DeleteBoard("web"); err == nil {
		t.Fatal("DeleteBoard ran under the override")
	}
	if _, err := ResolveBoard("web"); err == nil {
		t.Fatal("ResolveBoard(web) accepted a board under the override")
	}
	if _, err := ResolveBoard(""); err != nil {
		t.Fatalf("ResolveBoard(\"\") under the override: %v", err)
	}
	if len(Load(f)) != 1 {
		t.Fatal("override file was touched")
	}
}

func TestSaveKeepsSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	_ = os.WriteFile(target, []byte("- [ ] a\n"), 0o664)
	_ = os.Chmod(target, 0o664)
	link := filepath.Join(dir, "todo.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Save(link, []todo.Item{{Text: "b"}}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Save replaced the symlink with a file")
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o664 {
		t.Fatalf("mode = %v, want 0664", fi.Mode().Perm())
	}
	if got := Load(target); len(got) != 1 || got[0].Text != "b" {
		t.Fatalf("target not written: %+v", got)
	}
}

func TestLoadBOMAndCRLF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(p, []byte("\uFEFF- [ ] first\r\n  due: 2020-01-01\r\n  id: abc\r\n- [ ] second\r\n"), 0o644)
	got := Load(p)
	if len(got) != 2 || got[0].Text != "first" || got[0].Due != "2020-01-01" || got[0].ID != "abc" {
		t.Fatalf("got %+v", got)
	}
}

// TestPrioLikeTextRoundTrip: text that looks like a priority marker stays text.
func TestPrioLikeTextRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	in := []todo.Item{{Text: "(H) literal"}, {Prio: 'M', Text: "(L) also"}, {Text: `\(H) slash`}}
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	got := Load(p)
	for i := range in {
		if got[i].Text != in[i].Text || got[i].Prio != in[i].Prio {
			t.Errorf("item %d: got prio %q text %q", i, got[i].Prio, got[i].Text)
		}
	}
}

func TestBoardDirRoundTripsQuotes(t *testing.T) {
	seedBoards(t)
	t.Setenv("SHEPHERD_CONFIG", "")
	dir := `/tmp/a"b\c`
	if err := SetBoardDir("web", dir); err != nil {
		t.Fatal(err)
	}
	if got := BoardDir("web"); got != dir {
		t.Fatalf("BoardDir = %q, want %q", got, dir)
	}
}

func TestAppendArchiveAfterUnterminatedLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.md")
	_ = os.WriteFile(ArchivePath(p), []byte("- [x] old"), 0o644)
	if err := AppendArchive(p, []todo.Item{{Done: true, Text: "new"}}); err != nil {
		t.Fatal(err)
	}
	got := LoadArchive(p)
	if len(got) != 2 || got[1].Text != "new" || strings.Contains(got[0].Text, "new") {
		t.Fatalf("got %+v", got)
	}
}
