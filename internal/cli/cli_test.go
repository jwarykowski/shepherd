package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"shepherd/internal/store"
)

// TestCLIRoundTrip exercises every write path: add parses quick-add tokens,
// done flips a flag, list --json reports the right shape, a bad index errors
// instead of panicking, and rm drops the item.
func TestCLIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("HERDR_TODO_FILE", path)

	if code := cmdAdd([]string{"buy milk @home !h due:2026-07-15"}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	items := store.Load(path)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Prio != 'H' || items[0].Category != "home" || items[0].Due != "2026-07-15" {
		t.Fatalf("add parsed wrong: %+v", items[0])
	}

	if code := cmdToggle([]string{"1"}, true, &bytes.Buffer{}); code != 0 {
		t.Fatalf("done exit %d", code)
	}
	if !store.Load(path)[0].Done {
		t.Fatal("item not marked done")
	}

	var buf bytes.Buffer
	if code := cmdList([]string{"--json"}, &buf); code != 0 {
		t.Fatalf("list exit %d", code)
	}
	var got []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(got) != 1 || got[0].Index != 1 || got[0].Priority != "H" || !got[0].Done {
		t.Fatalf("json wrong: %+v", got)
	}

	if code := cmdToggle([]string{"9"}, true, &bytes.Buffer{}); code == 0 {
		t.Fatal("expected nonzero exit for out-of-range index")
	}

	if code := cmdRemove([]string{"1"}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("rm exit %d", code)
	}
	if len(store.Load(path)) != 0 {
		t.Fatal("item not removed")
	}
}

// TestRunDispatch drives the command-API dispatcher: routing, exit codes, and
// the argument-error paths.
func TestRunDispatch(t *testing.T) {
	t.Setenv("HERDR_TODO_FILE", filepath.Join(t.TempDir(), "todo.md"))
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
