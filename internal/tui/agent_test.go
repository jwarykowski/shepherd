package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"shepherd/internal/agent"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// stubAgent replaces the external commands @ drives: herdr ("" = not
// available), git (always /repo), and herdr worktree/agent calls answered by
// fail (nil = succeed).
func stubAgent(t *testing.T, herdr string, fail error) {
	t.Helper()
	prevRun, prevHerdr := agent.Run, agent.Herdr
	t.Cleanup(func() { agent.Run, agent.Herdr = prevRun, prevHerdr })
	agent.Herdr = func() string { return herdr }
	agent.Run = func(dir, name string, args ...string) ([]byte, error) {
		switch {
		case name == "git":
			return []byte("/repo\n"), nil
		case fail != nil:
			return nil, fail
		case len(args) > 0 && args[0] == "worktree":
			return []byte(`{"result":{"root_pane":{"pane_id":"w9:p1"}}}`), nil
		}
		return nil, nil
	}
}

func agentModel(t *testing.T) model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "todo.md")
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	items := []todo.Item{{Text: "wire the webhook", Subs: []todo.Item{{Text: "sub"}}}}
	if err := store.Save(path, items); err != nil {
		t.Fatal(err)
	}
	return model{input: textinput.New(), w: 80, height: 24, path: path, items: store.Load(path)}
}

func TestAgentKeyInHerdr(t *testing.T) {
	stubAgent(t, "herdr", nil)
	m := agentModel(t)

	nm, cmd := m.Update(key("@"))
	m = nm.(model)
	if cmd == nil || !strings.Contains(m.notice, "starting claude") {
		t.Fatalf("@ should start the hand-off in the background: notice=%q cmd=%v", m.notice, cmd != nil)
	}
	if m.items[0].Status != "" {
		t.Fatal("item must not be marked before the agent has it")
	}

	nm, _ = m.Update(cmd())
	m = nm.(model)
	if m.items[0].Status != "in-progress" || m.items[0].Branch != "todo/wire-the-webhook" {
		t.Fatalf("item not marked: status=%q branch=%q", m.items[0].Status, m.items[0].Branch)
	}
	if !strings.Contains(m.notice, "todo/wire-the-webhook") {
		t.Fatalf("notice should name the branch: %q", m.notice)
	}
	if got := store.Load(m.path)[0].Branch; got != "todo/wire-the-webhook" {
		t.Fatalf("branch not saved for shepherd sync: %q", got)
	}
	if !strings.Contains(m.listFooter(), "todo/wire-the-webhook") {
		t.Fatal("footer should show the notice")
	}
	m = drive(m, "j")
	if m.notice != "" {
		t.Fatal("notice should clear on the next key")
	}
}

func TestAgentKeyHerdrFailure(t *testing.T) {
	stubAgent(t, "herdr", errors.New("worktree already exists"))
	m := agentModel(t)

	nm, cmd := m.Update(key("@"))
	nm, _ = nm.(model).Update(cmd())
	m = nm.(model)
	if !strings.Contains(m.notice, "worktree already exists") {
		t.Fatalf("notice should carry the error: %q", m.notice)
	}
	if m.items[0].Status != "" || m.items[0].Branch != "" {
		t.Fatalf("a failed hand-off must leave the item alone: %+v", m.items[0])
	}
}

func TestAgentKeyGlobalView(t *testing.T) {
	stubAgent(t, "herdr", nil)
	g := agentModel(t)
	g.global = true
	if nm, cmd := g.Update(key("@")); cmd != nil || !strings.Contains(nm.(model).notice, "open a board") {
		t.Fatal("@ in the global view should explain, not run")
	}
}

func TestAgentKeyOnSubtask(t *testing.T) {
	var started []string
	stubAgent(t, "herdr", nil)
	run := agent.Run
	agent.Run = func(dir, name string, args ...string) ([]byte, error) {
		started = append(started, strings.Join(args, " "))
		return run(dir, name, args...)
	}
	m := agentModel(t)
	m = drive(m, "j") // onto the subtask row
	if m.selRef().sub < 0 {
		t.Fatal("test setup: cursor should be on the subtask")
	}

	nm, cmd := m.Update(key("@"))
	if cmd == nil {
		t.Fatalf("@ on a subtask should start the hand-off: %q", nm.(model).notice)
	}
	nm, _ = nm.(model).Update(cmd())
	m = nm.(model)
	const branch = "todo/wire-the-webhook-sub"
	if sub := m.items[0].Subs[0]; sub.Status != "in-progress" || sub.Branch != branch {
		t.Fatalf("subtask not marked: %+v", sub)
	}
	if m.items[0].Status != "" || m.items[0].Branch != "" {
		t.Fatalf("parent must be left alone: %+v", m.items[0])
	}
	if got := store.Load(m.path)[0].Subs[0].Branch; got != branch {
		t.Fatalf("subtask branch not saved: %q", got)
	}
	if !strings.Contains(strings.Join(started, "\n"), "This is one part of: wire the webhook") {
		t.Fatalf("prompt should carry the parent: %q", started)
	}
}

func TestAgentKeyWithoutHerdr(t *testing.T) {
	stubAgent(t, "", nil)
	m := agentModel(t)

	nm, cmd := m.Update(key("@"))
	m = nm.(model)
	if cmd == nil {
		t.Fatal("without herdr @ should hand the terminal to the agent")
	}
	if m.items[0].Status != "in-progress" || m.items[0].Branch != "" {
		t.Fatalf("item should be in-progress without a branch: %+v", m.items[0])
	}
}
