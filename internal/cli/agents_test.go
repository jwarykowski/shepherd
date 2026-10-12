package cli

import (
	"bytes"
	"errors"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/drover"
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
	if code := cmdGo([]string{"1", "--json"}, "", &out); code != 1 || !strings.Contains(out.String(), "aren't installed") {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
}

// noDrover fails if s names drover: people only ever meet Shepherd.
func noDrover(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(strings.ToLower(s), "drover") {
		t.Errorf("output names drover: %q", s)
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
	if !strings.Contains(out.String(), "using codex") || !strings.Contains(out.String(), "press @") {
		t.Fatalf("output %q", out.String())
	}
	noDrover(t, out.String())
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
	noDrover(t, out.String())
}

func TestAgentsStatusStartStop(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	var out bytes.Buffer
	for _, args := range [][]string{{"status"}, {"stop"}, {"start"}} {
		if code := cmdAgents(args, &out); code != 0 {
			t.Fatalf("agents %v: exit %d", args, code)
		}
	}
	// the fake says it's watching
	if !strings.Contains(out.String(), "agents: running") || !strings.Contains(out.String(), "Agents stopped") {
		t.Fatalf("output %q", out.String())
	}
	noDrover(t, out.String())
	want := "service status --json|status --json|service stop|service start"
	if got := strings.Join(calls(), "|"); got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if code := cmdAgents([]string{"bogus"}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("unknown subcommand: exit %d", code)
	}
}

func TestPro(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	var out bytes.Buffer
	if code := cmdPro([]string{"activate", "KEY"}, &out); code != 0 {
		t.Fatalf("pro activate: exit %d", code)
	}
	if !strings.Contains(out.String(), "Shepherd Pro is on for jo@example.com") {
		t.Fatalf("output %q", out.String())
	}
	if got := calls(); got[0] != "license activate" {
		t.Fatalf("the key goes on stdin, not argv: %q", got)
	}
	out.Reset()
	if code := cmdPro([]string{"status"}, &out); code != 0 || !strings.Contains(out.String(), "Shepherd Pro, for jo@example.com") {
		t.Fatalf("status: %d %q", code, out.String())
	}
	noDrover(t, out.String())
	if code := cmdPro([]string{"activate", "BAD"}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("a bad key should fail, got %d", code)
	}
}

func TestAgentsAddWebhookPrintsTheURLAndRestarts(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	dir := t.TempDir()
	var out bytes.Buffer
	if code := cmdAgents([]string{"add", "webhook", "sentry", "--dir", dir}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := calls()
	if got[0] != "source add webhook sentry --preset sentry --dir "+dir+" --json" {
		t.Fatalf("calls = %q", got)
	}
	if strings.Join(got[1:], "|") != "service status --json|service stop|service start" {
		t.Fatalf("not restarted: %q", got)
	}
	for _, want := range []string{"Paste this URL into Sentry", "https://hooks.test/h/s1", "In Sentry: paste it.", "agents view (R)", "restarted"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks %q: %q", want, out.String())
		}
	}
	noDrover(t, out.String())
	if code := cmdAgents([]string{"add", "webhook", "pagerduty"}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("unknown service: exit %d", code)
	}
}

func TestAgentsAddGitHubWithoutAService(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	t.Setenv("DROVER_FAKE_INSTALLED", "false")
	var out bytes.Buffer
	if code := cmdAgents([]string{"add", "github", "acme/api", "--label", "bot"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	wd, _ := os.Getwd()
	if got := calls()[0]; got != "source add github acme/api --dir "+wd+" --json --label bot" {
		t.Fatalf("call = %q", got)
	}
	if !strings.Contains(out.String(), "labelled bot") || !strings.Contains(out.String(), "Restart your agents") {
		t.Fatalf("output %q", out.String())
	}
	noDrover(t, out.String())
}

func TestAgentsAddNeedsPro(t *testing.T) {
	drovertest.Fake(t, `[]`)
	t.Setenv("DROVER_FAKE_FREE", "1")
	if code := cmdAgents([]string{"add", "github", "acme/api"}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	_, err := drover.AddGitHub("acme/api", "/x", "", "")
	if !errors.Is(err, drover.ErrPro) {
		t.Fatalf("err %v", err)
	}
	noDrover(t, err.Error())
}

func TestAgentsSourcesAndRemove(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	var out bytes.Buffer
	if code := cmdAgents([]string{"sources"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "your boards") || !strings.Contains(out.String(), "webhook (sentry)  https://hooks.test/h/s1") {
		t.Fatalf("output %q", out.String())
	}
	out.Reset()
	if code := cmdAgents([]string{"remove", "sentry"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := calls(); got[1] != "source rm sentry" || !strings.Contains(out.String(), "Removed sentry") {
		t.Fatalf("calls %q, output %q", got, out.String())
	}
	noDrover(t, out.String())
}
