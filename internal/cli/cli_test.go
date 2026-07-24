package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/store"
	"shepherd/internal/todo"
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

// TestCLIByID checks that mutators resolve an item by its stable id (agents'
// primary handle) regardless of list position, and that a done stays put.
func TestCLIByID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	cmdAdd([]string{"first"}, "", &bytes.Buffer{})
	cmdAdd([]string{"second"}, "", &bytes.Buffer{})

	id := store.Load(path)[1].ID // "second"
	if id == "" {
		t.Fatal("save did not backfill an id")
	}
	if code := cmdToggle([]string{id}, "", true, &bytes.Buffer{}); code != 0 {
		t.Fatalf("done by id exit %d", code)
	}
	items := store.Load(path)
	if !items[1].Done || items[0].Done {
		t.Fatalf("id addressed the wrong item: %+v", items)
	}
}

// TestCLIMultiAndJSON covers multi-id verbs, the --json echo on a mutator, and
// the structured not_found error an agent branches on.
func TestCLIMultiAndJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	for _, txt := range []string{"a", "b", "c"} {
		cmdAdd([]string{txt}, "", &bytes.Buffer{})
	}

	// multi-id done in one call, echoing the affected items as JSON.
	var buf bytes.Buffer
	if code := cmdToggle([]string{"1", "3", "--json"}, "", true, &buf); code != 0 {
		t.Fatalf("multi done exit %d", code)
	}
	var echoed []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &echoed); err != nil {
		t.Fatalf("json echo: %v (%s)", err, buf.String())
	}
	if len(echoed) != 2 || !echoed[0].Done || echoed[0].ID == "" {
		t.Fatalf("echo wrong: %+v", echoed)
	}
	items := store.Load(path)
	if !items[0].Done || items[1].Done || !items[2].Done {
		t.Fatalf("multi done hit wrong items: %+v", items)
	}

	// multi-rm is identity-based, so removing 1 and 3 together is index-safe.
	if code := cmdRemove([]string{"1", "3"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("multi rm exit %d", code)
	}
	if left := store.Load(path); len(left) != 1 || left[0].Text != "b" {
		t.Fatalf("multi rm left wrong board: %+v", left)
	}

	// structured error on an unknown id: exit 2 and {"error":"not_found"}.
	var eb bytes.Buffer
	if code := cmdToggle([]string{"deadbeef", "--json"}, "", true, &eb); code != 2 {
		t.Fatalf("not_found exit = %d, want 2", code)
	}
	var perr map[string]string
	if err := json.Unmarshal(eb.Bytes(), &perr); err != nil || perr["error"] != "not_found" {
		t.Fatalf("want not_found json, got %s (%v)", eb.String(), err)
	}
}

// TestDiffBoard covers the watch change detection: added/updated/removed by
// stable id, and no event for an unchanged item.
func TestDiffBoard(t *testing.T) {
	prev := []todo.Item{
		{ID: "a", Text: "keep"},
		{ID: "b", Text: "change me"},
		{ID: "c", Text: "gone soon"},
	}
	cur := []todo.Item{
		{ID: "a", Text: "keep"},                // unchanged
		{ID: "b", Text: "changed", Done: true}, // updated
		{ID: "d", Text: "brand new"},           // added
		// "c" removed
	}
	evs := diffBoard(prev, cur)
	got := map[string]string{} // id -> type
	for _, e := range evs {
		got[e.Item.ID] = e.Type
	}
	if _, ok := got["a"]; ok {
		t.Fatalf("unchanged item emitted an event: %+v", evs)
	}
	if got["b"] != "updated" || got["d"] != "added" || got["c"] != "removed" {
		t.Fatalf("wrong events: %+v", got)
	}
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(evs), evs)
	}
}

// TestArchiveItem covers the per-item archive verb: the item leaves the live
// board and lands in the sibling archive.md, --json echoes it, subtasks are
// refused, and an unknown id is a structured not_found.
func TestArchiveItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	for _, txt := range []string{"keep", "archive me"} {
		cmdAdd([]string{txt}, "", &bytes.Buffer{})
	}
	id := store.Load(path)[1].ID

	var buf bytes.Buffer
	if code := cmdArchive([]string{id, "--json"}, "", &buf); code != 0 {
		t.Fatalf("archive exit %d (%s)", code, buf.String())
	}
	var echoed []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &echoed); err != nil {
		t.Fatalf("json echo: %v (%s)", err, buf.String())
	}
	if len(echoed) != 1 || echoed[0].Text != "archive me" {
		t.Fatalf("echo wrong: %+v", echoed)
	}
	if live := store.Load(path); len(live) != 1 || live[0].Text != "keep" {
		t.Fatalf("archived item still on board: %+v", live)
	}
	if arc := store.LoadArchive(path); len(arc) != 1 || arc[0].Text != "archive me" {
		t.Fatalf("archive.md missing item: %+v", arc)
	}

	// subtasks can't be archived on their own.
	cmdAdd([]string{"parent"}, "", &bytes.Buffer{})
	cmdSub([]string{"1", "child"}, "", &bytes.Buffer{})
	var sb bytes.Buffer
	if code := cmdArchive([]string{"1.1", "--json"}, "", &sb); code != 2 {
		t.Fatalf("archive subtask exit = %d, want 2", code)
	}
	var perr map[string]string
	if err := json.Unmarshal(sb.Bytes(), &perr); err != nil || perr["error"] != "usage" {
		t.Fatalf("want usage error, got %s (%v)", sb.String(), err)
	}

	// unknown id is a structured not_found.
	var eb bytes.Buffer
	if code := cmdArchive([]string{"deadbeef", "--json"}, "", &eb); code != 2 {
		t.Fatalf("not_found exit = %d, want 2", code)
	}
	if err := json.Unmarshal(eb.Bytes(), &perr); err != nil || perr["error"] != "not_found" {
		t.Fatalf("want not_found json, got %s (%v)", eb.String(), err)
	}
}

// TestReclassifyArchived checks that a "removed" watch event is retagged
// "archived" when its id is present in the sibling archive.md, while a plain
// removal (id absent from the archive) stays "removed".
func TestReclassifyArchived(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := store.AppendArchive(path, []todo.Item{{ID: "arc", Text: "archived one"}}); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
	evs := []watchEvent{
		{"removed", itemJSON{ID: "arc"}}, // in archive -> archived
		{"removed", itemJSON{ID: "del"}}, // not in archive -> stays removed
		{"added", itemJSON{ID: "new"}},   // untouched
	}
	reclassifyArchived(path, evs)
	if evs[0].Type != "archived" || evs[1].Type != "removed" || evs[2].Type != "added" {
		t.Fatalf("reclassify wrong: %+v", evs)
	}
}

// TestCLIEdit checks that edit merges tokens onto an item (text preserved on a
// token-only edit, replaced when plain words are given) and can target a subtask.
func TestCLIEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)

	cmdAdd([]string{"write report"}, "", &bytes.Buffer{})
	if code := cmdEdit([]string{"1", "@work !h due:2026-07-20"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit exit %d", code)
	}
	it := store.Load(path)[0]
	if it.Text != "write report" || it.Category != "work" || it.Prio != 'H' || it.Due != "2026-07-20" {
		t.Fatalf("token-only edit wrong: %+v", it)
	}
	if code := cmdEdit([]string{"1", "final report"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit text exit %d", code)
	}
	if it := store.Load(path)[0]; it.Text != "final report" || it.Prio != 'H' {
		t.Fatalf("text edit wrong: %+v", it)
	}

	cmdSub([]string{"1", "gather data"}, "", &bytes.Buffer{})
	if code := cmdEdit([]string{"1.1", "!m due:2026-07-18"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit subtask exit %d", code)
	}
	if sub := store.Load(path)[0].Subs[0]; sub.Text != "gather data" || sub.Prio != 'M' || sub.Due != "2026-07-18" {
		t.Fatalf("subtask edit wrong: %+v", sub)
	}

	if code := cmdEdit([]string{"1"}, "", &bytes.Buffer{}); code == 0 {
		t.Fatal("edit with no tokens should error")
	}

	// status:/note: tokens and clearing round-trip through the store.
	if code := cmdEdit([]string{"1", "status:in-progress note:ping the vendor first"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit status/note exit %d", code)
	}
	if it := store.Load(path)[0]; it.Status != "in-progress" || it.Note != "ping the vendor first" {
		t.Fatalf("status/note not persisted: %+v", it)
	}
	if code := cmdEdit([]string{"1", "! note:"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit clear exit %d", code)
	}
	if it := store.Load(path)[0]; it.Prio != 0 || it.Note != "" {
		t.Fatalf("bare tokens did not clear: %+v", it)
	}
}

// TestCLIListFilter checks --filter narrows the output while keeping each item's
// real board index (so done/rm by that index still work) and reports no matches.
func TestCLIListFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)

	cmdAdd([]string{"alpha task @work"}, "", &bytes.Buffer{})
	cmdAdd([]string{"beta task @home"}, "", &bytes.Buffer{})

	var buf bytes.Buffer
	if code := cmdList([]string{"--filter", "home", "--json"}, "", &buf); code != 0 {
		t.Fatalf("list --filter exit %d", code)
	}
	var got []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(got) != 1 || got[0].Index != 2 || got[0].Category != "home" {
		t.Fatalf("filter should return only item 2 with its real index: %+v", got)
	}

	buf.Reset()
	if code := cmdList([]string{"--filter", "zzz"}, "", &buf); code != 0 {
		t.Fatalf("list --filter miss exit %d", code)
	}
	if buf.String() != "(no matches)\n" {
		t.Fatalf("no-match output wrong: %q", buf.String())
	}
}

// TestCLIEditSubStatusCascade checks that editing a subtask's status to done
// completes the parent (last sub done), and an intermediate status reopens it —
// the same cascade done/undone give, since edit is the only status setter now.
func TestCLIEditSubStatusCascade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)

	cmdAdd([]string{"ship release"}, "", &bytes.Buffer{})
	cmdSub([]string{"1", "cut tag"}, "", &bytes.Buffer{})

	if code := cmdEdit([]string{"1.1", "status:done"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit sub status:done exit %d", code)
	}
	if it := store.Load(path)[0]; !it.Done || !it.Subs[0].Done {
		t.Fatalf("last sub done should complete parent: %+v", it)
	}

	if code := cmdEdit([]string{"1.1", "status:in-progress"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("edit sub status:in-progress exit %d", code)
	}
	if it := store.Load(path)[0]; it.Done || it.Subs[0].Status != "in-progress" {
		t.Fatalf("intermediate sub status should reopen parent: %+v", it)
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
		{"done", []string{"99"}, 2}, // out of range (input error)
		{"done", []string{"nope"}, 2},
		{"rm", []string{"1"}, 0},
	}
	for _, c := range cases {
		if got := Run(c.verb, c.args); got != c.want {
			t.Errorf("Run(%q, %v) = %d, want %d", c.verb, c.args, got, c.want)
		}
	}
}

// TestQuiet checks -q/--quiet suppresses the mutation confirmation but never
// the requested data (list output).
func TestQuiet(t *testing.T) {
	t.Setenv("SHEPHERD_TODO_FILE", filepath.Join(t.TempDir(), "todo.md"))

	var buf bytes.Buffer
	if code := cmdAddWith(t, "-q", "add", []string{"ship it"}, &buf); code != 0 {
		t.Fatalf("quiet add exit %d", code)
	}
	if buf.String() != "" {
		t.Fatalf("quiet add should print nothing, got %q", buf.String())
	}
	// data output is not suppressed by quiet
	if code := Run("list", []string{"-q"}); code != 0 {
		t.Fatalf("quiet list exit %d", code)
	}
	var data bytes.Buffer
	if code := cmdList(extractGlobals([]string{"-q"}), "", &data); code != 0 || data.Len() == 0 {
		t.Fatalf("quiet must not suppress list data: code=%d out=%q", code, data.String())
	}
}

// cmdAddWith runs add through the global-flag extraction so -q is honored, and
// resets the quiet global afterward.
func cmdAddWith(t *testing.T, global, _ string, args []string, w io.Writer) int {
	t.Helper()
	rest := extractGlobals(append([]string{global}, args...))
	defer extractGlobals(nil) // reset quiet
	return cmdAdd(rest, "", w)
}

// TestDryRun checks rm --dry-run previews the removal without writing the file.
func TestDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	cmdAdd([]string{"keep me"}, "", &bytes.Buffer{})

	var buf bytes.Buffer
	if code := cmdRemove([]string{"1", "--dry-run"}, "", &buf); code != 0 {
		t.Fatalf("dry-run exit %d", code)
	}
	if !strings.Contains(buf.String(), "would remove") {
		t.Fatalf("dry-run should preview, got %q", buf.String())
	}
	if len(store.Load(path)) != 1 {
		t.Fatal("dry-run must not delete the item")
	}
}

// TestHelpExitsZero checks -h/--help on a subcommand exits 0, not 2 (clig).
func TestHelpExitsZero(t *testing.T) {
	t.Setenv("SHEPHERD_TODO_FILE", filepath.Join(t.TempDir(), "todo.md"))
	if code := cmdList([]string{"--help"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("list --help want exit 0, got %d", code)
	}
	if code := cmdList([]string{"--bogus"}, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("list --bogus want exit 2, got %d", code)
	}
}

// TestSuggest checks a mistyped verb resolves to the closest real one.
func TestSuggest(t *testing.T) {
	cases := map[string]string{"lst": "list", "ad": "add", "delete": "", "boad": "board"}
	for in, want := range cases {
		if got := suggest(in); got != want {
			t.Errorf("suggest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractBoard(t *testing.T) {
	p, rest, err := extractBoard([]string{"a", "--board", "web", "b"})
	if err != nil || p != "web" || len(rest) != 2 || rest[0] != "a" || rest[1] != "b" {
		t.Fatalf("space form: %q %v %v", p, rest, err)
	}
	p, rest, err = extractBoard([]string{"--board=api", "x"})
	if err != nil || p != "api" || len(rest) != 1 || rest[0] != "x" {
		t.Fatalf("equals form: %q %v %v", p, rest, err)
	}
	if _, _, err := extractBoard([]string{"--board"}); err == nil {
		t.Fatal("want error for --board with no value")
	}
}

// TestListAll checks the aggregate read spans every board and tags each item
// with its source board.
func TestListAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")

	if code := Run("add", []string{"a @work"}); code != 0 { // default board
		t.Fatalf("add default exit %d", code)
	}
	if code := Run("add", []string{"b @dev", "--board", "web"}); code != 0 {
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
		proj[j.Board] = true
	}
	if !proj["default"] || !proj["web"] {
		t.Fatalf("expected default+web sources, got %+v", got)
	}
}

// TestBoards checks the boards listing reports each board with open/total
// counts and marks the effective board as current.
func TestBoards(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")

	if code := Run("add", []string{"a"}); code != 0 { // default board, 1 open
		t.Fatalf("add default exit %d", code)
	}
	if code := Run("add", []string{"b", "--board", "web"}); code != 0 {
		t.Fatalf("add web exit %d", code)
	}
	if code := Run("done", []string{"1", "--board", "web"}); code != 0 { // web: 0 open, 1 total
		t.Fatalf("done web exit %d", code)
	}

	var buf bytes.Buffer
	if code := cmdBoards([]string{"--json"}, "web", &buf); code != 0 {
		t.Fatalf("boards exit %d", code)
	}
	type row struct {
		Name    string `json:"name"`
		Open    int    `json:"open"`
		Total   int    `json:"total"`
		Current bool   `json:"current"`
	}
	var got []row
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	byName := map[string]row{}
	for _, r := range got {
		byName[r.Name] = r
	}
	if d := byName["default"]; d.Open != 1 || d.Total != 1 || d.Current {
		t.Fatalf("default board wrong: %+v", d)
	}
	if w := byName["web"]; w.Open != 0 || w.Total != 1 || !w.Current {
		t.Fatalf("web board wrong: %+v", w)
	}
}

// TestBoardActions exercises the whole-board verbs end to end via Run.
func TestBoardActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "")
	t.Setenv("SHEPHERD_BOARD", "")

	if code := Run("add", []string{"b", "--board", "web"}); code != 0 {
		t.Fatalf("seed exit %d", code)
	}
	exists := func(name string) bool { _, err := os.Stat(store.TodoPathFor(name)); return err == nil }

	if code := Run("board", []string{"rename", "web", "webapp"}); code != 0 {
		t.Fatalf("rename exit %d", code)
	}
	if exists("web") || !exists("webapp") {
		t.Fatal("rename did not move the board")
	}
	// delete requires --force
	if code := Run("board", []string{"delete", "webapp"}); code == 0 {
		t.Fatal("delete without --force should fail")
	}
	if !exists("webapp") {
		t.Fatal("board removed despite missing --force")
	}
	if code := Run("board", []string{"archive", "webapp"}); code != 0 {
		t.Fatalf("archive exit %d", code)
	}
	if exists("webapp") {
		t.Fatal("archived board still live")
	}
	// boards --archived lists it; the live listing does not
	var arc bytes.Buffer
	if code := cmdBoards([]string{"--archived"}, "", &arc); code != 0 {
		t.Fatalf("boards --archived exit %d", code)
	}
	if !strings.Contains(arc.String(), "webapp") {
		t.Fatalf("--archived did not list the archived board:\n%s", arc.String())
	}
	var live bytes.Buffer
	_ = cmdBoards(nil, "", &live)
	if strings.Contains(live.String(), "webapp") {
		t.Fatalf("live listing showed an archived board:\n%s", live.String())
	}
	if code := Run("board", []string{"unarchive", "webapp"}); code != 0 {
		t.Fatalf("unarchive exit %d", code)
	}
	if code := Run("board", []string{"delete", "webapp", "--force"}); code != 0 {
		t.Fatalf("delete --force exit %d", code)
	}
	if exists("webapp") {
		t.Fatal("board not deleted with --force")
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

// TestBoardRouting checks --board writes under BaseDir/boards and that a
// traversal name is rejected before any file is touched.
func TestBoardRouting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHEPHERD_TODO_FILE", "") // override must not win
	t.Setenv("SHEPHERD_BOARD", "")

	if code := Run("add", []string{"ship it", "--board", "web"}); code != 0 {
		t.Fatalf("add --board exit %d", code)
	}
	want := filepath.Join(home, ".config", "shepherd", "boards", "web.md")
	if items := store.Load(want); len(items) != 1 || items[0].Text != "ship it" {
		t.Fatalf("board file wrong at %s: %+v", want, items)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "shepherd", "todo.md")); err == nil {
		t.Fatal("default todo.md should not have been written")
	}
	if code := Run("list", []string{"--board", "../evil"}); code != 2 {
		t.Fatalf("bad board want exit 2, got %d", code)
	}
}
