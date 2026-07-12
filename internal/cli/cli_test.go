package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"shepherd/internal/store"
)

// TestCLIRoundTrip exercises every write path: add parses quick-add tokens,
// done flips a flag, list --json reports the right shape, a bad index errors
// instead of panicking, and rm drops the item.
func TestCLIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)

	if code := cmdAdd([]string{"buy milk @home !h due:2026-07-15"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	items := store.Load(path)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Prio != 'H' || items[0].Category != "home" || items[0].Due != "2026-07-15" {
		t.Fatalf("add parsed wrong: %+v", items[0])
	}

	if code := cmdToggle([]string{"1"}, "", true, &bytes.Buffer{}); code != 0 {
		t.Fatalf("done exit %d", code)
	}
	if !store.Load(path)[0].Done {
		t.Fatal("item not marked done")
	}

	var buf bytes.Buffer
	if code := cmdList([]string{"--json"}, "", &buf); code != 0 {
		t.Fatalf("list exit %d", code)
	}
	var got []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(got) != 1 || got[0].Index != 1 || got[0].Priority != "H" || !got[0].Done {
		t.Fatalf("json wrong: %+v", got)
	}

	if code := cmdToggle([]string{"9"}, "", true, &bytes.Buffer{}); code == 0 {
		t.Fatal("expected nonzero exit for out-of-range index")
	}

	if code := cmdRemove([]string{"1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("rm exit %d", code)
	}
	if len(store.Load(path)) != 0 {
		t.Fatal("item not removed")
	}
}

// TestRunDispatch drives the command-API dispatcher: routing, exit codes, and
// the argument-error paths.
func TestRunDispatch(t *testing.T) {
	t.Setenv("SHEPHERD_TODO_FILE", filepath.Join(t.TempDir(), "todo.md"))
	cases := []struct {
		verb string
		args []string
		want int
	}{
		{"help", nil, 0},
		{"bogus", nil, 2}, // unknown verb
		{"add", nil, 2},   // add with no text
		{"add", []string{"x"}, 0},
		{"list", []string{"--json"}, 0},
		{"done", []string{"1"}, 0},
		{"done", []string{"99"}, 1}, // out of range
		{"done", []string{"nope"}, 1},
		{"rm", []string{"1"}, 0},
	}
	for _, c := range cases {
		if got := Run(c.verb, c.args); got != c.want {
			t.Errorf("Run(%q, %v) = %d, want %d", c.verb, c.args, got, c.want)
		}
	}
}

func TestExtractProject(t *testing.T) {
	p, rest, err := extractProject([]string{"a", "--project", "web", "b"})
	if err != nil || p != "web" || len(rest) != 2 || rest[0] != "a" || rest[1] != "b" {
		t.Fatalf("space form: %q %v %v", p, rest, err)
	}
	p, rest, err = extractProject([]string{"--project=api", "x"})
	if err != nil || p != "api" || len(rest) != 1 || rest[0] != "x" {
		t.Fatalf("equals form: %q %v %v", p, rest, err)
	}
	if _, _, err := extractProject([]string{"--project"}); err == nil {
		t.Fatal("want error for --project with no value")
	}
}

// TestListAll checks the aggregate read spans every board and tags each item
// with its source project.
func TestListAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_PROJECT", "")

	if code := Run("add", []string{"a @work"}); code != 0 { // default board
		t.Fatalf("add default exit %d", code)
	}
	if code := Run("add", []string{"b @dev", "--project", "web"}); code != 0 {
		t.Fatalf("add web exit %d", code)
	}

	var buf bytes.Buffer
	if code := cmdList([]string{"--all", "--json"}, "", &buf); code != 0 {
		t.Fatalf("list --all exit %d", code)
	}
	var got []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 items across boards, got %d", len(got))
	}
	proj := map[string]bool{}
	for _, j := range got {
		proj[j.Project] = true
	}
	if !proj["default"] || !proj["web"] {
		t.Fatalf("expected default+web sources, got %+v", got)
	}
}

// TestDoneStampsCompleted checks marking done records a completion timestamp in
// the JSON, and reopening clears it.
func TestDoneStampsCompleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)

	if code := cmdAdd([]string{"ship it"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	if code := cmdToggle([]string{"1"}, "", true, &bytes.Buffer{}); code != 0 {
		t.Fatalf("done exit %d", code)
	}
	read := func() itemJSON {
		var buf bytes.Buffer
		if code := cmdList([]string{"--json"}, "", &buf); code != 0 {
			t.Fatalf("list exit %d", code)
		}
		var got []itemJSON
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("json: %v", err)
		}
		return got[0]
	}
	if j := read(); !j.Done || j.Completed == "" {
		t.Fatalf("done should stamp completed: %+v", j)
	}
	if code := cmdToggle([]string{"1"}, "", false, &bytes.Buffer{}); code != 0 {
		t.Fatalf("undone exit %d", code)
	}
	if j := read(); j.Done || j.Completed != "" {
		t.Fatalf("undone should clear completed: %+v", j)
	}
}

// TestProjectRouting checks --project writes under BaseDir/projects and that a
// traversal name is rejected before any file is touched.
func TestProjectRouting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHEPHERD_TODO_FILE", "") // override must not win
	t.Setenv("SHEPHERD_PROJECT", "")

	if code := Run("add", []string{"ship it", "--project", "web"}); code != 0 {
		t.Fatalf("add --project exit %d", code)
	}
	want := filepath.Join(home, ".config", "shepherd", "projects", "web.md")
	if items := store.Load(want); len(items) != 1 || items[0].Text != "ship it" {
		t.Fatalf("project file wrong at %s: %+v", want, items)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "shepherd", "todo.md")); err == nil {
		t.Fatal("default todo.md should not have been written")
	}
	if code := Run("list", []string{"--project", "../evil"}); code != 2 {
		t.Fatalf("bad project want exit 2, got %d", code)
	}
}
