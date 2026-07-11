package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestCLIRoundTrip exercises every write path: add parses the quick-add tokens,
// done flips a flag, list --json reports the right shape, a bad index errors
// instead of panicking, and rm drops the item.
func TestCLIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("HERDR_TODO_FILE", path)

	if code := cmdAdd([]string{"buy milk @home !h due:2026-07-15"}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	items := load(path)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].prio != 'H' || items[0].category != "home" || items[0].due != "2026-07-15" {
		t.Fatalf("add parsed wrong: %+v", items[0])
	}

	if code := cmdToggle([]string{"1"}, true, &bytes.Buffer{}); code != 0 {
		t.Fatalf("done exit %d", code)
	}
	if !load(path)[0].done {
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
	if len(load(path)) != 0 {
		t.Fatal("item not removed")
	}
}
