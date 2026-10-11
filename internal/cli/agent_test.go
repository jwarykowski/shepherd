package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shepherd/internal/agent"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// fakeCmds stubs the external commands agent/sync drive. Each call is recorded
// as "name arg arg…"; reply decides the output per call.
type fakeCmds struct {
	calls []string
	dirs  []string
	reply func(name string, args []string) (string, error)
}

func stubCommands(t *testing.T, herdr string, reply func(name string, args []string) (string, error)) *fakeCmds {
	t.Helper()
	f := &fakeCmds{reply: reply}
	prevRun, prevInteractive, prevHerdr := agent.Run, agent.Interactive, agent.Herdr
	t.Cleanup(func() { agent.Run, agent.Interactive, agent.Herdr = prevRun, prevInteractive, prevHerdr })
	agent.Run = func(dir, name string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
		f.dirs = append(f.dirs, dir)
		if name == "git" {
			return []byte("/repo\n"), nil
		}
		out, err := f.reply(name, args)
		return []byte(out), err
	}
	agent.Interactive = func(dir, kind, prompt string) error {
		f.calls = append(f.calls, "interactive "+kind+" "+prompt)
		f.dirs = append(f.dirs, dir)
		return nil
	}
	agent.Herdr = func() string { return herdr }
	return f
}

// agentBoard isolates a board and config with one open item (text + note).
func agentBoard(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_CONFIG", cfg)
	cmdAdd([]string{"Add retry to API client! note:use exponential backoff"}, "", &bytes.Buffer{})
	return path
}

// idTag is the id suffix agent.Task.Name puts on an item's branch.
func idTag(it todo.Item) string { return "-" + it.ID[len(it.ID)-6:] }

// syncBoard isolates a board and config for sync tests, with the default
// board's working directory set (sync skips boards without one).
func syncBoard(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(dir, "config.toml"))
	if err := store.SetBoardDir("default", "/repo"); err != nil {
		t.Fatal(err)
	}
	return path
}

func worktreeReply(name string, args []string) (string, error) {
	if name == "herdr" && len(args) > 1 && args[0] == "worktree" {
		return `{"result":{"root_pane":{"pane_id":"w9:p1"}}}`, nil
	}
	return "", nil
}

func TestAgentHerdrWorktree(t *testing.T) {
	path := agentBoard(t, "")
	f := stubCommands(t, "herdr", worktreeReply)

	var out bytes.Buffer
	if code := cmdAgent([]string{"1"}, "", &out); code != 0 {
		t.Fatalf("agent exit %d", code)
	}
	name := "add-retry-to-api-client" + idTag(store.Load(path)[0])
	want := []string{
		"git rev-parse --show-toplevel",
		"herdr worktree create --cwd /repo --branch todo/" + name + " --label " + name + " --focus",
		"herdr agent start " + name + " --kind claude --pane w9:p1 -- Add retry to API client!\n\nuse exponential backoff",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
	it := store.Load(path)[0]
	if it.Status != "in-progress" || it.Branch != "todo/"+name {
		t.Fatalf("item not marked: status=%q branch=%q", it.Status, it.Branch)
	}
	// the board had no working directory: it adopts the repo, and says so
	if got := store.BoardDir("default"); got != "/repo" {
		t.Fatalf("board dir should be adopted from the current repo, got %q", got)
	}
	if !strings.Contains(out.String(), "board default now works in /repo") {
		t.Fatalf("adoption should be reported: %q", out.String())
	}

	// a second hand-off of the same item is refused, not a second worktree
	f.calls = nil
	if code := cmdAgent([]string{"1"}, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("re-hand-off exit %d, want 2", code)
	}
	if len(f.calls) != 0 {
		t.Fatalf("a refused hand-off must not call out: %q", f.calls)
	}
}

func TestAgentBranchUniquePerItem(t *testing.T) {
	path := agentBoard(t, "")
	cmdAdd([]string{"Add retry to API client!"}, "", &bytes.Buffer{}) // same text, new item
	stubCommands(t, "herdr", worktreeReply)

	for _, ref := range []string{"1", "2"} {
		if code := cmdAgent([]string{ref}, "", &bytes.Buffer{}); code != 0 {
			t.Fatalf("agent %s exit %d", ref, code)
		}
	}
	items := store.Load(path)
	if items[0].Branch == items[1].Branch {
		t.Fatalf("items with the same text must get different branches: %q", items[0].Branch)
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Branch, "todo/add-retry-to-api-client-") || !strings.HasSuffix(it.Branch, idTag(it)) {
			t.Fatalf("branch should be the slug then the id: %q", it.Branch)
		}
	}
}

func TestAgentUsesBoardDir(t *testing.T) {
	agentBoard(t, "")
	if err := store.SetBoardDir("default", "/elsewhere"); err != nil {
		t.Fatal(err)
	}
	f := stubCommands(t, "herdr", worktreeReply)

	var out bytes.Buffer
	if code := cmdAgent([]string{"1"}, "", &out); code != 0 {
		t.Fatalf("agent exit %d", code)
	}
	if f.dirs[0] != "/elsewhere" {
		t.Fatalf("repo should come from the board dir, ran git in %q", f.dirs[0])
	}
	if store.BoardDir("default") != "/elsewhere" || strings.Contains(out.String(), "now works in") {
		t.Fatal("an existing board dir must be left alone")
	}
}

func TestAgentConfigAndKindFlag(t *testing.T) {
	path := agentBoard(t, "agent_kind = \"codex\"\nagent_branch_prefix = \"agent/\"\n")
	f := stubCommands(t, "herdr", worktreeReply)

	if code := cmdAgent([]string{"1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("agent exit %d", code)
	}
	if !strings.Contains(f.calls[1], "--branch agent/add-retry") || !strings.Contains(f.calls[2], "--kind codex") {
		t.Fatalf("config not applied: %q", f.calls)
	}
	if store.Load(path)[0].Branch != "agent/add-retry-to-api-client"+idTag(store.Load(path)[0]) {
		t.Fatal("branch not recorded with the configured prefix")
	}

	f.calls = nil
	cmdEdit([]string{"1", "branch:"}, "", &bytes.Buffer{}) // clear it, so it can be handed off again
	cmdAgent([]string{"1", "--kind", "opencode"}, "", &bytes.Buffer{})
	if !strings.Contains(f.calls[2], "--kind opencode") {
		t.Fatalf("--kind should override config: %q", f.calls[2])
	}
}

func TestAgentToRunningPane(t *testing.T) {
	path := agentBoard(t, "")
	f := stubCommands(t, "herdr", worktreeReply)

	var buf bytes.Buffer
	if code := cmdAgent([]string{"1", "--to", "w3:p2", "--json"}, "", &buf); code != 0 {
		t.Fatalf("agent --to exit %d", code)
	}
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "herdr agent prompt w3:p2 Add retry") {
		t.Fatalf("calls: %q", f.calls)
	}
	var echoed itemJSON
	if err := json.Unmarshal(buf.Bytes(), &echoed); err != nil || echoed.Status != "in-progress" {
		t.Fatalf("json echo %q: %v", buf.String(), err)
	}
	if it := store.Load(path)[0]; it.Branch != "" {
		t.Fatalf("--to must not record a branch, got %q", it.Branch)
	}
}

func TestAgentWithoutHerdr(t *testing.T) {
	path := agentBoard(t, "")
	f := stubCommands(t, "", worktreeReply)

	if code := cmdAgent([]string{"1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("agent exit %d", code)
	}
	last := f.calls[len(f.calls)-1]
	if !strings.HasPrefix(last, "interactive claude Add retry") || f.dirs[len(f.dirs)-1] != "/repo" {
		t.Fatalf("expected the agent run in the repo, got %q in %q", last, f.dirs[len(f.dirs)-1])
	}
	if it := store.Load(path)[0]; it.Status != "in-progress" || it.Branch != "" {
		t.Fatalf("item: status=%q branch=%q", it.Status, it.Branch)
	}
}

func TestAgentErrors(t *testing.T) {
	path := agentBoard(t, "")
	stubCommands(t, "herdr", func(name string, args []string) (string, error) {
		return "", errors.New("worktree already exists")
	})

	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 2},                   // no ref
		{[]string{"9"}, 2},         // unknown ref
		{[]string{"1"}, 1},         // herdr failure
		{[]string{"1", "--to"}, 2}, // flag missing its value
	} {
		if code := cmdAgent(tc.args, "", &bytes.Buffer{}); code != tc.code {
			t.Errorf("agent %q exit %d, want %d", tc.args, code, tc.code)
		}
	}
	if it := store.Load(path)[0]; it.Status != "" || it.Branch != "" {
		t.Fatalf("a failed hand-off must leave the item alone: %+v", it)
	}

	cmdToggle([]string{"1"}, "", true, &bytes.Buffer{})
	if code := cmdAgent([]string{"1"}, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("done item: exit %d, want 2", code)
	}
}

func TestSyncCompletesMergedBranches(t *testing.T) {
	path := syncBoard(t)
	cmdAdd([]string{"merged one branch:todo/merged"}, "", &bytes.Buffer{})
	cmdAdd([]string{"open one branch:todo/open"}, "", &bytes.Buffer{})
	cmdAdd([]string{"no branch"}, "", &bytes.Buffer{})
	f := stubCommands(t, "herdr", func(name string, args []string) (string, error) {
		if name == "gh" && args[2] == "todo/merged" {
			return "MERGED\n", nil
		}
		return "OPEN\n", nil
	})

	if code := cmdSync([]string{"--dry-run"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("dry-run exit %d", code)
	}
	if store.Load(path)[0].Done {
		t.Fatal("--dry-run wrote the board")
	}

	var buf bytes.Buffer
	if code := cmdSync([]string{"--json"}, "", &buf); code != 0 {
		t.Fatalf("sync exit %d", code)
	}
	items := store.Load(path)
	if !items[0].Done || items[1].Done || items[2].Done {
		t.Fatalf("only the merged branch should complete: %v %v %v", items[0].Done, items[1].Done, items[2].Done)
	}
	var echoed []itemJSON
	if err := json.Unmarshal(buf.Bytes(), &echoed); err != nil || len(echoed) != 1 || echoed[0].Branch != "todo/merged" {
		t.Fatalf("json %q: %v", buf.String(), err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "no branch") {
			t.Fatalf("items without a branch must not be looked up: %q", c)
		}
	}

	// already done: a second sync has nothing to ask about
	f.calls = nil
	cmdSync(nil, "", &bytes.Buffer{})
	for _, c := range f.calls {
		if strings.Contains(c, "todo/merged") {
			t.Fatalf("done items must be skipped: %q", c)
		}
	}
}

func TestBranchToken(t *testing.T) {
	it := todo.ParseQuickAdd("fix login branch:fix/login")
	if it.Text != "fix login" || it.Branch != "fix/login" {
		t.Fatalf("parse: %+v", it)
	}
	todo.ApplyEdit(&it, "branch:")
	if it.Branch != "" {
		t.Fatal("bare branch: should clear")
	}
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := store.Save(path, []todo.Item{todo.ParseQuickAdd("x branch:todo/x")}); err != nil {
		t.Fatal(err)
	}
	if got := store.Load(path)[0].Branch; got != "todo/x" {
		t.Fatalf("round trip: %q", got)
	}
	if !todo.Match(store.Load(path)[0], "todo/x") {
		t.Fatal("filter should match the branch")
	}
}

func TestSyncEvery(t *testing.T) {
	syncBoard(t)
	cmdAdd([]string{"x branch:todo/x"}, "", &bytes.Buffer{})
	f := stubCommands(t, "herdr", func(name string, args []string) (string, error) { return "OPEN\n", nil })

	type stop struct{}
	prev := sleep
	t.Cleanup(func() { sleep = prev })
	naps := 0
	sleep = func(d time.Duration) {
		if d != 2*time.Minute {
			t.Errorf("slept %v, want the --every interval", d)
		}
		if naps++; naps == 2 {
			panic(stop{})
		}
	}
	defer func() {
		if _, ok := recover().(stop); !ok {
			t.Fatal("--every should keep looping until stopped")
		}
		lookups := 0
		for _, c := range f.calls {
			if strings.HasPrefix(c, "gh pr view todo/x") {
				lookups++
			}
		}
		if lookups != 2 {
			t.Fatalf("want one gh lookup per pass (2), got %d", lookups)
		}
	}()
	cmdSync([]string{"--every", "2m"}, "", &bytes.Buffer{})
}

func TestAgentSubtask(t *testing.T) {
	path := agentBoard(t, "")
	cmdSub([]string{"1", "write the tests note:table-driven"}, "", &bytes.Buffer{})
	f := stubCommands(t, "herdr", worktreeReply)

	if code := cmdAgent([]string{"1.1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("agent exit %d", code)
	}
	branch := "todo/add-retry-to-api-cli-write-the-tests" + idTag(store.Load(path)[0].Subs[0])
	if !strings.Contains(f.calls[1], "--branch "+branch) {
		t.Fatalf("subtask branch: %q", f.calls[1])
	}
	wantPrompt := "write the tests\n\ntable-driven\n\nThis is one part of: Add retry to API client!\n\nuse exponential backoff"
	if !strings.HasSuffix(f.calls[2], "-- "+wantPrompt) {
		t.Fatalf("subtask prompt: %q", f.calls[2])
	}
	items := store.Load(path)
	if sub := items[0].Subs[0]; sub.Status != "in-progress" || sub.Branch != branch {
		t.Fatalf("subtask not marked: %+v", sub)
	}
	if items[0].Status != "" || items[0].Branch != "" {
		t.Fatalf("parent must be left alone: %+v", items[0])
	}
}

func TestSyncCompletesMergedSubtask(t *testing.T) {
	path := syncBoard(t)
	cmdAdd([]string{"parent"}, "", &bytes.Buffer{})
	cmdSub([]string{"1", "merged sub branch:todo/sub"}, "", &bytes.Buffer{})
	cmdSub([]string{"1", "other sub"}, "", &bytes.Buffer{})
	stubCommands(t, "herdr", func(name string, args []string) (string, error) {
		if name == "gh" && args[2] == "todo/sub" {
			return "MERGED\n", nil
		}
		return "OPEN\n", nil
	})

	var buf bytes.Buffer
	if code := cmdSync(nil, "", &buf); code != 0 {
		t.Fatalf("sync exit %d", code)
	}
	items := store.Load(path)
	if !items[0].Subs[0].Done || items[0].Subs[1].Done || items[0].Done {
		t.Fatalf("only the merged subtask should complete: %+v", items[0])
	}
	if !strings.Contains(buf.String(), "1.1") || !strings.Contains(buf.String(), "todo/sub merged") {
		t.Fatalf("output should name the subtask: %q", buf.String())
	}
}

func TestSyncSkipsBoardWithoutDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(dir, "config.toml"))
	cmdAdd([]string{"merged one branch:todo/merged"}, "", &bytes.Buffer{})
	f := stubCommands(t, "herdr", func(name string, args []string) (string, error) { return "MERGED\n", nil })

	if code := cmdSync(nil, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("sync exit %d", code)
	}
	if len(f.calls) != 0 {
		t.Fatalf("a board without a working directory must not be checked: %q", f.calls)
	}
	if store.Load(path)[0].Done {
		t.Fatal("nothing should complete without the board's repo")
	}
}
