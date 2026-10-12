// Package drover is shepherd's bridge to drover, the engine that hands board
// items to coding agents. shepherd never reads drover's files: it calls the
// drover command, so drover owns its config, its runs and its licence, and
// shepherd stays a board.
//
// Depends on todo and store; used by the cli and tui frontends.
package drover

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// ErrMissing is returned when drover isn't on the PATH.
var ErrMissing = errors.New("drover isn't installed: brew install jwarykowski/tap/drover, then shepherd agents setup")

// Swappable in tests: where drover is found and how it's run.
var (
	LookPath = exec.LookPath
	Command  = func(args ...string) *exec.Cmd { return exec.Command("drover", args...) }
)

// Installed reports whether drover is on the PATH.
func Installed() bool {
	_, err := LookPath("drover")
	return err == nil
}

// Recent is how far back finished runs are fetched: long enough for a run that
// opened a pull request to keep its task marked for review.
const Recent = 14 * 24 * time.Hour

// Run is one drover run, as `drover runs --json` reports it.
type Run struct {
	ID        string            `json:"id"`
	Text      string            `json:"text"`
	Status    string            `json:"status"` // hold, go, queued, running, failed, blocked
	Done      bool              `json:"done"`
	Subject   string            `json:"subject"` // a board item's id, for runs from Shepherd
	Action    string            `json:"action"`
	Note      string            `json:"note"` // the agent's summary, or why it stopped
	Link      string            `json:"link"`
	Source    string            `json:"source"`
	Created   string            `json:"created"`
	Started   string            `json:"started"`
	Completed string            `json:"completed"`
	Data      map[string]string `json:"data"`
}

// Waiting is a run held for a person to say go.
func (r Run) Waiting() bool { return !r.Done && r.Status == "hold" }

// Queued is a run released but waiting for a free agent.
func (r Run) Queued() bool { return !r.Done && (r.Status == "go" || r.Status == "queued") }

// Running is a run an agent is working on.
func (r Run) Running() bool { return !r.Done && r.Status == "running" }

// Failed is a run the agent didn't finish: its verdict was failed or blocked.
func (r Run) Failed() bool { return !r.Done && (r.Status == "failed" || r.Status == "blocked") }

// Active is a run released or under way.
func (r Run) Active() bool { return r.Queued() || r.Running() }

// External is a run whose text came from a source outsiders can write.
func (r Run) External() bool { return r.Data["external"] == "true" }

// URL is where the run's subject lives: the link it carries, else the url its
// event sent (an issue, an error).
func (r Run) URL() string { return cmp.Or(r.Link, r.Data["url"]) }

// When is the run's most telling time: finished, else started, else created.
func (r Run) When() time.Time {
	for _, s := range []string{r.Completed, r.Started, r.Created} {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// State names the run the way the board does: waiting, queued, running,
// failed or done.
func (r Run) State() string {
	switch {
	case r.Done:
		return "done"
	case r.Waiting():
		return "waiting"
	case r.Queued():
		return "queued"
	case r.Running():
		return "running"
	case r.Failed():
		return "failed"
	}
	return r.Status
}

// Runs lists drover's live runs and those finished within Recent, newest first.
func Runs() ([]Run, error) {
	return list("runs", "--json", "--all", "--since", fmt.Sprintf("%dh", int(Recent.Hours())))
}

// AllRuns lists every run drover keeps, newest first, for the archive.
func AllRuns() ([]Run, error) { return list("runs", "--json", "--all") }

func list(args ...string) ([]Run, error) {
	var runs []Run
	if err := output(&runs, nil, args...); err != nil {
		return nil, err
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].When().After(runs[j].When()) })
	return runs, nil
}

// BySubject indexes runs by board item id, keeping the one that says most about
// the item now: one under way, else waiting, else failed, else the latest to
// finish. runs must be newest first.
func BySubject(runs []Run) map[string]Run {
	rank := func(r Run) int {
		switch {
		case r.Active():
			return 4
		case r.Waiting():
			return 3
		case r.Failed():
			return 2
		}
		return 1
	}
	out := map[string]Run{}
	for _, r := range runs {
		if r.Subject == "" {
			continue
		}
		if cur, ok := out[r.Subject]; ok && rank(cur) >= rank(r) {
			continue
		}
		out[r.Subject] = r
	}
	return out
}

// Watching reports whether drover is running: the background service, or a
// drover watch in a terminal.
func Watching() bool {
	var st struct {
		Watching bool `json:"watching"`
	}
	return output(&st, nil, "status", "--json") == nil && st.Watching
}

// LogRow is one line of a run's log.
type LogRow struct {
	Time string `json:"time"`
	Kind string `json:"kind"` // agent, tool, result, system, output
	Text string `json:"text"`
}

// Log reads a run's log.
func Log(id string) ([]LogRow, error) {
	var rows []LogRow
	err := output(&rows, nil, "log", id, "--json")
	return rows, err
}

// Archive takes runs off the live list without running them.
func Archive(ids ...string) error {
	return run(nil, append([]string{"archive"}, ids...)...)
}

// Task is a board item to send to an agent, on its board, with its parent when
// it's a subtask.
type Task struct {
	Item   todo.Item
	Parent *todo.Item
	Board  string // "" is the default board
}

// Send hands t to drover: a run drover is holding for the item is released,
// otherwise a run starts now, as if the item had just been added (which also
// retries a failed one).
func Send(t Task) ([]Run, error) {
	runs, err := Runs()
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		if r.Subject == t.Item.ID && r.Waiting() {
			var out []Run
			err := output(&out, nil, "go", "--subject", t.Item.ID, "--json")
			return out, err
		}
	}
	data, err := json.Marshal(t.data())
	if err != nil {
		return nil, err
	}
	var out []Run
	err = output(&out, bytes.NewReader(data), "run", "--subject", t.Item.ID, "--on", "shepherd.added", "--data-json", "--json")
	return out, err
}

// Retry sends a run that isn't a board item again (Send covers those): a held
// one is released, a failed one re-run with the same action.
func Retry(r Run) error {
	if r.Waiting() {
		return run(nil, "go", r.ID)
	}
	return run(nil, "run", "--subject", r.Subject, "--action", r.Action)
}

// data is the event data drover's shepherd source sends for an item, plus the
// parent a subtask belongs to, so the agent knows what it's one part of. It
// goes to drover on stdin, so a long or private note never shows in ps.
func (t Task) data() map[string]string {
	board := t.Board
	if board == "" {
		board = "default"
	}
	d := map[string]string{"title": t.Item.Text, "item_id": t.Item.ID, "board": board}
	put := func(k, v string) {
		if v != "" {
			d[k] = v
		}
	}
	put("note", t.Item.Note)
	put("url", t.Item.Link)
	put("dir", store.BoardDir(board))
	if t.Parent != nil {
		put("parent", t.Parent.Text)
		put("parent_note", t.Parent.Note)
	}
	return d
}

// Passthrough runs drover with its output on w (and errors on errw), for the
// commands whose output is for a person: init, service, license.
func Passthrough(w, errw io.Writer, args ...string) error {
	if !Installed() {
		return ErrMissing
	}
	c := Command(args...)
	c.Stdout, c.Stderr = w, errw
	return c.Run()
}

// run runs drover for its effect, discarding stdout.
func run(stdin io.Reader, args ...string) error { return output(nil, stdin, args...) }

// output runs drover and decodes its JSON stdout into v (nil discards it). A
// failure carries drover's own message.
func output(v any, stdin io.Reader, args ...string) error {
	if !Installed() {
		return ErrMissing
	}
	var stdout, stderr bytes.Buffer
	c := Command(args...)
	c.Stdin, c.Stdout, c.Stderr = stdin, &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimPrefix(strings.TrimSpace(stderr.String()), "drover: ")
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(stdout.Bytes(), v); err != nil {
		return fmt.Errorf("drover %s: unexpected output: %w", args[0], err)
	}
	return nil
}
