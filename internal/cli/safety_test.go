package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// TestEditStatusDoneCascades: edit status:done completes the subtasks, the same
// as done does (AGENTS.md: completing a parent completes all its subtasks).
func TestEditStatusDoneCascades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	_ = store.Save(path, []todo.Item{{Text: "p", Subs: []todo.Item{{Text: "a"}, {Text: "b"}}}})
	if code := cmdEdit([]string{"1", "status:done"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit exit %d", code)
	}
	it := store.Load(path)[0]
	if !it.Done || !it.Subs[0].Done || !it.Subs[1].Done {
		t.Fatalf("cascade missing: %+v", it)
	}
	if code := cmdEdit([]string{"1", "status:open"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit exit %d", code)
	}
	it = store.Load(path)[0]
	if it.Done || it.Subs[0].Done {
		t.Fatalf("reopen didn't cascade: %+v", it)
	}
}

func TestBoardDeleteRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	def := filepath.Join(home, ".config", "shepherd", "todo.md")
	_ = os.MkdirAll(filepath.Dir(def), 0o755)
	_ = os.WriteFile(def, []byte("- [ ] keep\n"), 0o644)
	if code := cmdBoard([]string{"delete", "../todo", "--force"}, &bytes.Buffer{}); code == 0 {
		t.Fatal("board delete ../todo succeeded")
	}
	if len(store.Load(def)) != 1 {
		t.Fatal("default board was deleted")
	}
}

// TestJSONBoardError: a bad --board under --json reports on stdout as JSON.
func TestJSONBoardError(t *testing.T) {
	t.Setenv("SHEPHERD_TODO_FILE", "")
	var code int
	out := captureStdout(t, func() { code = Run("list", []string{"--json", "--board", "../x"}) })
	var obj map[string]string
	if code != 2 || json.Unmarshal([]byte(out), &obj) != nil || obj["error"] != "usage" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestListRejectsPositional(t *testing.T) {
	t.Setenv("SHEPHERD_TODO_FILE", filepath.Join(t.TempDir(), "todo.md"))
	var buf bytes.Buffer
	if code := cmdList([]string{"foo", "--json"}, "", &buf); code != 2 {
		t.Fatalf("list foo --json exit %d, want 2", code)
	}
}

// TestListStripsEscapes: item text can't drive the reader's terminal.
func TestListStripsEscapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	_ = store.Save(path, []todo.Item{{Text: "evil \x1b]0;PWNED\x07 \x1b[31mred"}})
	var buf bytes.Buffer
	if code := cmdList(nil, "", &buf); code != 0 {
		t.Fatalf("list exit %d", code)
	}
	if strings.ContainsAny(buf.String(), "\x1b\x07") {
		t.Fatalf("control characters in output: %q", buf.String())
	}
}

func TestResolveEmptyID(t *testing.T) {
	if _, _, ok := resolveID([]todo.Item{{Text: "legacy"}}, ""); ok {
		t.Fatal("an empty id matched an id-less item")
	}
}
