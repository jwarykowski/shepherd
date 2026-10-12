package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"shepherd/internal/drover"
	"shepherd/internal/drover/drovertest"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

const agentBoard = `- [ ] ship it
  id: i1
- [ ] (H) move checkout to Postgres
  id: p1
  - [ ] dual-write orders
    id: s2
  - [ ] backfill old orders
    id: s3
- [x] shipped
  id: i9
`

func agentModel(t *testing.T) (model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	if err := os.WriteFile(path, []byte(agentBoard), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_TODO_FILE", path)
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(dir, "config.toml"))
	m := newModel("")
	m.w, m.height = 100, 40
	return m, path
}

// @ saves the board, sends the item in the background and says how it went;
// the refresh after it doesn't start a second polling loop.
func TestAtSendsTheItemToDrover(t *testing.T) {
	m, path := agentModel(t)
	calls := drovertest.Fake(t, `[]`)
	for i, r := range m.rows() {
		if m.rowItem(r).ID == "i1" {
			m.cursor = i
		}
	}
	nm, cmd := m.updateList(key("@"))
	got := nm.(model)
	if cmd == nil || !strings.Contains(got.notice, "sending") {
		t.Fatalf("notice %q, cmd %v", got.notice, cmd != nil)
	}
	saved := false
	for _, it := range store.Load(path) {
		saved = saved || it.ID == "i1"
	}
	if !saved {
		t.Fatal("board not saved first")
	}
	msg := cmd().(sentMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if c := calls(); !strings.HasPrefix(c[len(c)-1], "run --subject i1") {
		t.Fatalf("calls %q", c)
	}
	nm, refresh := got.Update(msg)
	if n := nm.(model).notice; n != "sent to your agent: ship it" {
		t.Fatalf("notice %q", n)
	}
	rm := refresh().(runsMsg)
	if rm.poll {
		t.Fatal("the refresh after @ must not be a polling fetch")
	}
	if _, next := nm.(model).Update(rm); next != nil {
		t.Fatal("a refresh scheduled another poll: two polling loops")
	}
	if _, next := nm.(model).Update(runsMsg{poll: true, installed: true}); next == nil {
		t.Fatal("the polling fetch should schedule the next one")
	}
}

// A subtask goes with its parent.
func TestAtOnASubtaskSendsItsParent(t *testing.T) {
	m, _ := agentModel(t)
	drovertest.Fake(t, `[]`)
	m.cursor = 1 // dual-write orders, under the high-priority parent
	_, cmd := m.updateList(key("@"))
	if msg := cmd().(sentMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if in := drovertest.Stdin(); !strings.Contains(in, `"parent":"move checkout to Postgres"`) {
		t.Fatalf("stdin %s", in)
	}
}

func TestAtWithoutDrover(t *testing.T) {
	m, _ := agentModel(t)
	t.Setenv("PATH", t.TempDir())
	nm, cmd := m.updateList(key("@"))
	if cmd != nil || !strings.Contains(nm.(model).notice, "aren't installed") {
		t.Fatalf("notice %q", nm.(model).notice)
	}
}

func TestRunMarks(t *testing.T) {
	m, _ := agentModel(t)
	m.runs = drover.BySubject([]drover.Run{
		{ID: "r1", Status: "hold", Subject: "a"},
		{ID: "r2", Status: "queued", Subject: "b"},
		{ID: "r3", Status: "running", Subject: "c"},
		{ID: "r4", Status: "failed", Subject: "d"},
		{ID: "r5", Done: true, Subject: "e"},
		{ID: "r6", Done: true, Subject: "f"},
	})
	for id, want := range map[string]string{"a": "waiting", "b": "queued", "c": "running", "d": "failed", "e": "review"} {
		if l := ansi.Strip(m.runLabel(todo.Item{ID: id})); l != want {
			t.Errorf("%s: %q, want %q", id, l, want)
		}
	}
	if l := m.runLabel(todo.Item{ID: "f", Done: true}); l != "" {
		t.Errorf("a done item with a finished run needs no mark, got %q", l)
	}
	if l := m.runLabel(todo.Item{ID: "idle"}); l != "" {
		t.Errorf("idle = %q", l)
	}
}

func TestDroverDownWarning(t *testing.T) {
	m, _ := agentModel(t)
	m.droverOn = true
	m.allRuns = []drover.Run{{ID: "r1", Status: "queued", Subject: "i1"}}
	if m.droverDown() == "" {
		t.Fatal("queued runs with drover stopped should warn")
	}
	m.watching = true
	if m.droverDown() != "" {
		t.Fatal("no warning while drover is watching")
	}
	m.watching, m.allRuns = false, []drover.Run{{ID: "r1", Done: true}}
	if m.droverDown() != "" {
		t.Fatal("no warning when nothing is waiting to run")
	}
}

const agentRuns = `[
 {"id":"gh","text":"checkout 500s on Safari","status":"hold","subject":"https://x/212","source":"acme","data":{"via":"acme","external":"true","url":"https://x/212"}},
 {"id":"r2","text":"dual-write orders","status":"running","subject":"s2","source":"cli"},
 {"id":"r3","text":"backfill old orders","status":"failed","subject":"s3","source":"cli","note":"timeout on orders_2019"},
 {"id":"r1","text":"ship it","status":"hold","subject":"i1","source":"cli"},
 {"id":"gone","text":"deleted task","status":"hold","subject":"zz","source":"cli"},
 {"id":"fin","text":"move checkout to Postgres","done":true,"subject":"p1","source":"cli","note":"split it into subtasks"},
 {"id":"old","text":"ship it","done":true,"archived":true,"subject":"i1","source":"cli","completed":"2026-01-01T00:00:00Z"}
]`

func openedAgents(t *testing.T) model {
	t.Helper()
	m, _ := agentModel(t)
	drovertest.Fake(t, agentRuns)
	nm, cmd := m.updateList(key("R"))
	m = nm.(model)
	nm, _ = m.Update(cmd())
	return nm.(model)
}

// R lists every run by state, subtask runs under their parent, with the
// task's current title, external runs flagged and removed tasks said so.
func TestAgentsView(t *testing.T) {
	m := openedAgents(t)
	if m.mode != modeAgents {
		t.Fatalf("mode = %v", m.mode)
	}
	v := ansi.Strip(m.agentsView())
	for _, want := range []string{"waiting", "checkout 500s on Safari external", "ship it", "deleted task (task removed)",
		"failed", "move checkout to Postgres", "backfill old orders", "timeout on orders_2019", "running", "dual-write orders"} {
		if !strings.Contains(v, want) {
			t.Errorf("agents view lacks %q:\n%s", want, v)
		}
	}
	if strings.Count(v, "ship it") != 1 {
		t.Errorf("an archived run belongs in the archive view only:\n%s", v)
	}
	if !strings.Contains(v, "split it into subtasks") {
		t.Errorf("a finished run stays until it's archived:\n%s", v)
	}
	order := []string{"waiting", "failed", "running"}
	last := -1
	for _, s := range order {
		i := strings.Index(v, "\n"+s)
		if i < last {
			t.Errorf("%s out of order:\n%s", s, v)
		}
		last = i
	}
}

func TestAgentsKeys(t *testing.T) {
	m := openedAgents(t)
	rows := m.agentRows()
	at := func(id string) int {
		for i, r := range rows {
			if r.ID == id {
				return i
			}
		}
		t.Fatalf("no row %s", id)
		return 0
	}

	// C archives a run that hasn't run
	m.agCur = at("gone")
	_, cmd := m.updateAgents(key("C"))
	if cmd == nil || cmd().(archivedMsg).err != nil {
		t.Fatal("C should archive the run")
	}
	// C archives a finished one too, and c clears every finished run
	m.agCur = at("fin")
	if _, cmd = m.updateAgents(key("C")); cmd == nil {
		t.Fatal("C should archive a finished run")
	} else if am := cmd().(archivedMsg); am.err != nil || !strings.Contains(am.notice, "archived") {
		t.Fatalf("archive: %+v", am)
	}
	if _, cmd = m.updateAgents(key("c")); cmd == nil {
		t.Fatal("c should clear finished runs")
	} else if am := cmd().(archivedMsg); !strings.Contains(am.notice, "cleared 1 finished run") {
		t.Fatalf("clear: %+v", am)
	}
	// C refuses a running one
	m.agCur = at("r2")
	nm, cmd := m.updateAgents(key("C"))
	if cmd != nil || !strings.Contains(nm.(model).notice, "still running") {
		t.Fatalf("notice %q", nm.(model).notice)
	}

	// enter jumps to the task's detail; esc comes back to the agents view
	m.agCur = at("r3")
	nm, _ = m.updateAgents(key("enter"))
	dm := nm.(model)
	if dm.mode != modeDetail || dm.rowItem(dm.selRef()).ID != "s3" {
		t.Fatalf("mode %v, item %+v", dm.mode, dm.rowItem(dm.selRef()))
	}
	if d := ansi.Strip(dm.detailView()); !strings.Contains(d, "runs (1)") || !strings.Contains(d, "timeout on orders_2019") {
		t.Fatalf("detail lacks the runs:\n%s", d)
	}
	nm, _ = dm.updateDetail(key("esc"))
	if nm.(model).mode != modeAgents {
		t.Fatalf("esc from a jumped-to task should return to the agents view, got %v", nm.(model).mode)
	}

	// l opens the run's log
	m.agCur = at("r3")
	nm, cmd = m.updateAgents(key("l"))
	lm := nm.(model)
	nm, _ = lm.Update(cmd())
	if lv := ansi.Strip(nm.(model).logView()); !strings.Contains(lv, "reading the task") || !strings.Contains(lv, "opened PR #41") {
		t.Fatalf("log view:\n%s", lv)
	}
	nm, _ = nm.(model).updateLog(key("esc"))
	if nm.(model).mode != modeAgents {
		t.Fatal("esc from the log should go back")
	}

	// esc leaves the agents view
	nm, _ = m.updateAgents(key("esc"))
	if nm.(model).mode != modeList {
		t.Fatal("esc should return to the board")
	}
}

// e shows the archive: every finished run drover keeps.
func TestAgentsArchive(t *testing.T) {
	m := openedAgents(t)
	nm, cmd := m.updateAgents(key("e"))
	am := nm.(model)
	nm, _ = am.Update(cmd())
	am = nm.(model)
	if !am.agArchive || len(am.agArchiveRuns) != 1 || am.agArchiveRuns[0].ID != "old" {
		t.Fatalf("archive = %+v", am.agArchiveRuns)
	}
	if v := ansi.Strip(am.agentsView()); !strings.Contains(v, "agents · archive") {
		t.Fatalf("view:\n%s", v)
	}
}

var _ tea.Model = model{}
