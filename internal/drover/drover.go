// Package drover is shepherd's bridge to drover, the engine that hands board
// items to coding agents. shepherd never reads drover's files: it calls the
// drover command, so drover owns its config, its runs and its licence, and
// shepherd stays a board.
//
// Depends on todo and store; used by the cli and tui frontends.
package drover

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

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

// Run is one drover run, as `drover runs --json` reports it.
type Run struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Status  string `json:"status"` // hold, go, running, failed, blocked
	Note    string `json:"note"`   // the agent's summary, or why it stopped
	Done    bool   `json:"done"`
	Subject string `json:"subject"` // the board item's id
}

// Waiting is a run held for a person to release it.
func (r Run) Waiting() bool { return !r.Done && r.Status == "hold" }

// Failed is a run the agent didn't finish: its verdict was failed or blocked.
func (r Run) Failed() bool { return !r.Done && (r.Status == "failed" || r.Status == "blocked") }

// Active is a run released or under way.
func (r Run) Active() bool { return !r.Done && (r.Status == "go" || r.Status == "running") }

// Runs lists drover's live runs.
func Runs() ([]Run, error) {
	var runs []Run
	err := output(&runs, "runs", "--json")
	return runs, err
}

// BySubject indexes live runs by board item id, keeping the most pressing:
// a running or released run over a held one.
func BySubject(runs []Run) map[string]Run {
	out := map[string]Run{}
	for _, r := range runs {
		if r.Done || r.Subject == "" {
			continue
		}
		if cur, ok := out[r.Subject]; ok && cur.Active() {
			continue
		}
		out[r.Subject] = r
	}
	return out
}

// Task is a board item to send to an agent, on its board.
type Task struct {
	Item  todo.Item
	Board string // "" is the default board
}

// Send hands t to drover: a run drover is holding for the item is released,
// otherwise a run starts now, as if the item had just been added (which also
// retries a failed one). It returns
// the runs released or started.
func Send(t Task) ([]Run, error) {
	runs, err := Runs()
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		if r.Subject == t.Item.ID && r.Waiting() {
			var out []Run
			err := output(&out, "go", "--subject", t.Item.ID, "--json")
			return out, err
		}
	}
	args := []string{"run", "--subject", t.Item.ID, "--on", "shepherd.added", "--json"}
	for _, kv := range t.data() {
		args = append(args, "--data", kv)
	}
	var out []Run
	err = output(&out, args...)
	return out, err
}

// data is the event data drover's shepherd source sends for an item, so an
// action's prompt and target render the same either way.
func (t Task) data() []string {
	board := t.Board
	if board == "" {
		board = "default"
	}
	kv := []string{"title=" + t.Item.Text, "item_id=" + t.Item.ID, "board=" + board}
	if t.Item.Note != "" {
		kv = append(kv, "note="+t.Item.Note)
	}
	if t.Item.Link != "" {
		kv = append(kv, "url="+t.Item.Link)
	}
	if dir := store.BoardDir(board); dir != "" {
		kv = append(kv, "dir="+dir)
	}
	return kv
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

// output runs drover and decodes its JSON stdout into v. A failure carries
// drover's own message.
func output(v any, args ...string) error {
	if !Installed() {
		return ErrMissing
	}
	var stdout, stderr bytes.Buffer
	c := Command(args...)
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimPrefix(strings.TrimSpace(stderr.String()), "drover: ")
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	if err := json.Unmarshal(stdout.Bytes(), v); err != nil {
		return fmt.Errorf("drover %s: unexpected output: %w", args[0], err)
	}
	return nil
}
