package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"strings"
	"testing"
)

func agentBoard(t *testing.T, items ...todo.Item) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	if err := store.Save(path, items); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLiteralText: --text keeps #123 and due: as text while the positional
// string still supplies tokens; a stray word beside --text is a usage error.
func TestLiteralText(t *testing.T) {
	path := agentBoard(t)
	lit := "fix #123 crash due: soon"
	if code := cmdAdd([]string{"--text", lit, "@work !h"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	it := store.Load(path)[0]
	if it.Text != lit || len(it.Tags) != 0 || it.Category != "work" || it.Prio != 'H' {
		t.Fatalf("add --text wrong: %+v", it)
	}
	if code := cmdAdd([]string{"--text=a\nb"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("add --text= exit %d", code)
	}
	if got := store.Load(path); len(got) != 2 || got[1].Text != "a b" {
		t.Fatalf("newline not collapsed: %+v", got)
	}
	if code := cmdEdit([]string{"1", "--text", "#456 now", "!l"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit exit %d", code)
	}
	if it := store.Load(path)[0]; it.Text != "#456 now" || it.Prio != 'L' || it.Category != "work" {
		t.Fatalf("edit --text wrong: %+v", it)
	}
	if code := cmdSub([]string{"1", "--text", "step #1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("sub exit %d", code)
	}
	if s := store.Load(path)[0].Subs[0]; s.Text != "step #1" || len(s.Tags) != 0 {
		t.Fatalf("sub --text wrong: %+v", s)
	}
	for _, args := range [][]string{
		{"--text", "x", "stray word"},
		{"--text", "  "},
		{"--text"},
	} {
		if code := cmdAdd(args, "", &bytes.Buffer{}); code != 2 {
			t.Errorf("add %q exit %d, want 2", args, code)
		}
	}
}

// TestIfRev: a matching rev writes, a stale one exits 1 and leaves the file
// untouched, and --if-rev with several refs is a usage error.
func TestIfRev(t *testing.T) {
	path := agentBoard(t, todo.Item{ID: "a", Text: "one"}, todo.Item{ID: "b", Text: "two"})
	var buf bytes.Buffer
	cmdList([]string{"--json"}, "", &buf)
	var got []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil || got[0].Rev == "" {
		t.Fatalf("list --json rev missing: %v %+v", err, got)
	}
	rev := got[0].Rev
	if code := cmdEdit([]string{"a", "!h", "--if-rev", rev}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit with current rev exit %d", code)
	}
	before := store.Serialize(store.Load(path))
	stale := []struct {
		name string
		run  func() int
	}{
		{"edit", func() int { return cmdEdit([]string{"a", "!l", "--if-rev", rev}, "", &bytes.Buffer{}) }},
		{"done", func() int { return cmdToggle([]string{"a", "--if-rev", rev}, "", true, &bytes.Buffer{}) }},
		{"rm", func() int { return cmdRemove([]string{"a", "--if-rev", rev}, "", &bytes.Buffer{}) }},
		{"archive", func() int { return cmdArchive([]string{"a", "--if-rev", rev}, "", &bytes.Buffer{}) }},
		{"sub", func() int { return cmdSub([]string{"a", "x", "--if-rev", rev}, "", &bytes.Buffer{}) }},
		{"claim", func() int { return cmdClaim([]string{"a", "--by", "bot", "--if-rev", rev}, "", &bytes.Buffer{}) }},
	}
	for _, tc := range stale {
		if code := tc.run(); code != 1 {
			t.Errorf("%s with stale rev exit %d, want 1", tc.name, code)
		}
	}
	if after := store.Serialize(store.Load(path)); after != before {
		t.Fatalf("stale rev wrote the board:\n%s", after)
	}
	var out bytes.Buffer
	if code := cmdToggle([]string{"a", "--json", "--if-rev", rev}, "", true, &out); code != 1 || !strings.Contains(out.String(), `"conflict"`) {
		t.Fatalf("json conflict: exit %d %s", code, out.String())
	}
	if code := cmdToggle([]string{"a", "b", "--if-rev", rev}, "", true, &bytes.Buffer{}); code != 2 {
		t.Fatalf("multi-ref --if-rev exit %d, want 2", code)
	}
}

// TestClaimCLI: claim sets the claimer and the working status, refuses another
// claimer or a done item (exit 1), and --release hands it back.
func TestClaimCLI(t *testing.T) {
	path := agentBoard(t, todo.Item{ID: "a", Text: "one"}, todo.Item{ID: "b", Text: "two", Done: true})
	var out bytes.Buffer
	if code := cmdClaim([]string{"a", "--by", "bot-1", "--json"}, "", &out); code != 0 {
		t.Fatalf("claim exit %d", code)
	}
	var j itemJSON
	if err := json.Unmarshal(out.Bytes(), &j); err != nil || j.Claimed != "bot-1" || j.Status != "in-progress" {
		t.Fatalf("claim echo wrong: %v %s", err, out.String())
	}
	if code := cmdClaim([]string{"a", "--by", "bot-1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("re-claim own exit %d", code)
	}
	for _, args := range [][]string{
		{"a", "--by", "bot-2"},
		{"b", "--by", "bot-1"},
		{"a", "--release", "--by", "bot-2"},
	} {
		if code := cmdClaim(args, "", &bytes.Buffer{}); code != 1 {
			t.Errorf("claim %q exit %d, want 1", args, code)
		}
	}
	for _, args := range [][]string{{"a"}, {"--by", "x"}, {"a", "b", "--by", "x"}, {"a", "--by", "x\ny"}} {
		if code := cmdClaim(args, "", &bytes.Buffer{}); code != 2 {
			t.Errorf("claim %q exit %d, want 2", args, code)
		}
	}
	if it := store.Load(path)[0]; it.Claimed != "bot-1" || it.Status != "in-progress" {
		t.Fatalf("failed claims changed the item: %+v", it)
	}
	if code := cmdClaim([]string{"a", "--release", "--by", "bot-1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("release exit %d", code)
	}
	if it := store.Load(path)[0]; it.Claimed != "" || it.Status != "" {
		t.Fatalf("release wrong: %+v", it)
	}
}
