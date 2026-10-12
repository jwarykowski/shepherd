package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/drover/drovertest"
	"shepherd/internal/store"
)

// board writes a one-item board (no id yet, as if hand-written) and points the
// command API at it.
func board(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] fix login\n- [x] old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_TODO_FILE", path)
	return path
}

func TestGoSendsAnItemToDrover(t *testing.T) {
	path := board(t)
	calls := drovertest.Fake(t, `[]`)
	var out bytes.Buffer
	if code := cmdGo([]string{"1"}, "", &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	id := store.Load(path)[0].ID
	if id == "" {
		t.Fatal("the item wasn't given an id before it was sent")
	}
	got := calls()
	if last := got[len(got)-1]; !strings.HasPrefix(last, "run --subject "+id+" --on shepherd.added") {
		t.Fatalf("calls = %q", got)
	}
	if !strings.Contains(out.String(), "sent to your agent: fix login") {
		t.Fatalf("output %q", out.String())
	}
}

func TestGoJSONAndRefusals(t *testing.T) {
	board(t)
	drovertest.Fake(t, `[]`)
	var out bytes.Buffer
	if code := cmdGo([]string{"1", "--json"}, "", &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var runs []map[string]any
	if err := json.Unmarshal(out.Bytes(), &runs); err != nil || len(runs) != 1 {
		t.Fatalf("json %q, err %v", out.String(), err)
	}
	if code := cmdGo([]string{"2"}, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("a done item should be a usage error, got %d", code)
	}
	if code := cmdGo([]string{"9"}, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("an unknown ref should be a usage error, got %d", code)
	}
	if code := cmdGo(nil, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("no ref should be a usage error, got %d", code)
	}
}

func TestGoWithoutDrover(t *testing.T) {
	board(t)
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	if code := cmdGo([]string{"1", "--json"}, "", &out); code != 1 || !strings.Contains(out.String(), "isn't installed") {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
}

func TestAgentsSetupInitsThenStartsTheService(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	var out bytes.Buffer
	if code := cmdAgents([]string{"setup", "--runner", "codex"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := strings.Join(calls(), "|"); got != "init --runner codex|service install" {
		t.Fatalf("calls = %q", got)
	}
	if !strings.Contains(out.String(), "press @") {
		t.Fatalf("output %q", out.String())
	}
}

func TestAgentsSetupKeepsAnExistingConfig(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	t.Setenv("DROVER_FAKE_EXISTS", "1")
	var out bytes.Buffer
	if code := cmdAgents([]string{"setup"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := strings.Join(calls(), "|"); got != "init|service install" {
		t.Fatalf("calls = %q", got)
	}
	if !strings.Contains(out.String(), "already set up") {
		t.Fatalf("output %q", out.String())
	}
}

func TestAgentsAndProPassThrough(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	for _, args := range [][]string{{"status", "--json"}, {"stop"}, {"start"}} {
		if code := cmdAgents(args, &bytes.Buffer{}); code != 0 {
			t.Fatalf("agents %v: exit %d", args, code)
		}
	}
	if code := cmdPro([]string{"activate", "KEY"}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("pro activate: exit %d", code)
	}
	want := "service status --json|service stop|service start|license activate KEY"
	if got := strings.Join(calls(), "|"); got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if code := cmdAgents([]string{"bogus"}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("unknown subcommand: exit %d", code)
	}
}
