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
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// People only ever meet Shepherd: every message from this package talks about
// "agents", never about drover, which runs them behind the scenes.

// ErrMissing is returned when drover isn't on the PATH.
var ErrMissing = errors.New("agents aren't installed: brew install jwarykowski/tap/shepherd, then shepherd agents setup")

// ErrPro is returned when something needs Shepherd Pro.
var ErrPro = errors.New("GitHub and webhook sources need Shepherd Pro: shepherdcli.dev/pro, then shepherd pro activate <key>")

// ErrNoAction is returned when nothing is set up to run on a board's tasks.
var ErrNoAction = errors.New("agents aren't set up for your boards yet: run shepherd agents setup")

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
	Archived  bool              `json:"archived"` // off the live list
	Thread    string            `json:"thread"`   // the runs of one action about one thing share one
	Runner    string            `json:"runner"`   // the coding agent that ran it
	Session   string            `json:"session"`  // its conversation, to take over or carry on
	Worktree  string            `json:"worktree"` // where it worked
	Branch    string            `json:"branch"`
	Result    string            `json:"result"` // the pull request it opened or updated
	Events    int               `json:"events"` // later events folded into it while it waited
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

// URL is the run's most useful link: the pull request it made, else where its
// subject lives (an issue, an error).
func (r Run) URL() string { return cmp.Or(r.Result, r.Link, r.Data["url"]) }

// TakeOver is the command that carries on the run's conversation by hand, in
// its worktree, or nil when there's none to carry on.
func (r Run) TakeOver() *exec.Cmd {
	if r.Session == "" || r.Worktree == "" {
		return nil
	}
	var c *exec.Cmd
	switch r.Runner {
	case "claude":
		c = exec.Command("claude", "--resume", r.Session)
	case "codex":
		c = exec.Command("codex", "resume", r.Session)
	default:
		return nil
	}
	c.Dir = r.Worktree
	return c
}

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

// Setup writes the agents' starter config for every board, using runner
// (claude or codex; "" picks the first installed). created is false when a
// config already exists, which is kept. agent names the coding agent chosen.
func Setup(runner string) (created bool, agent string, err error) {
	args := []string{"init"}
	if runner != "" {
		args = append(args, "--runner", runner)
	}
	out, err := capture(nil, args...)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return false, "", nil
		}
		return false, "", err
	}
	if _, after, ok := strings.Cut(out, "→ "); ok {
		agent = strings.Fields(after)[0]
	}
	return true, agent, nil
}

// StartBackground installs the background service (or refreshes it) and starts
// it; Start and Stop switch an installed one on and off.
func StartBackground() error { return run(nil, "service", "install") }
func Start() error           { return run(nil, "service", "start") }
func Stop() error            { return run(nil, "service", "stop") }

// Service reports whether the background service is installed.
func Service() (installed bool, err error) {
	var st struct {
		Installed bool `json:"installed"`
	}
	err = output(&st, nil, "service", "status", "--json")
	return st.Installed, err
}

// Source is a place tasks come from besides the boards: a GitHub repository
// or a webhook, as the agents list it.
type Source struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`             // shepherd (the boards), github, webhook or custom
	Detail   string `json:"detail,omitempty"` // the repository, or the service a webhook is for
	URL      string `json:"url,omitempty"`    // a webhook's URL, to paste into the service
	Help     string `json:"help,omitempty"`   // where the URL goes, just after adding
	Scheme   string `json:"scheme,omitempty"` // how its service signs deliveries; "" if unknown
	Signed   string `json:"signed,omitempty"` // set once only signed deliveries are taken
	External bool   `json:"external,omitempty"`
}

// Sources lists where tasks come from.
func Sources() ([]Source, error) {
	var s []Source
	err := output(&s, nil, "source", "list", "--json")
	return s, err
}

// AddGitHub sends repo's open issues carrying label to the agent working in
// dir. name is the source's name; "" names it after the repository.
func AddGitHub(repo, dir, label, name string) (Source, error) {
	args := []string{"source", "add", "github", repo, "--dir", dir, "--json"}
	if label != "" {
		args = append(args, "--label", label)
	}
	if name != "" {
		args = append(args, "--name", name)
	}
	var s Source
	err := output(&s, nil, args...)
	return s, err
}

// AddWebhook makes a webhook source called name for service (a preset:
// webhook, sentry, linear, datadog or alertmanager) and returns it with the URL
// to paste into the service.
func AddWebhook(name, service, dir string) (Source, error) {
	var s Source
	err := output(&s, nil, "source", "add", "webhook", name, "--preset", service, "--dir", dir, "--json")
	return s, err
}

// SetSecret makes a webhook source take only deliveries signed with secret,
// the way its service signs (scheme "" uses the service's own; github for any
// service that signs like GitHub). An empty secret stops checking. The secret
// goes on stdin, so it never shows in ps.
func SetSecret(name, scheme, secret string) error {
	args := []string{"source", "secret", name}
	if scheme != "" {
		args = append(args, "--scheme", scheme)
	}
	return run(strings.NewReader(secret+"\n"), args...)
}

// RemoveSource stops tasks coming from the source called name.
func RemoveSource(name string) error { return run(nil, "source", "rm", name) }

// Restart restarts the background service, so a source added or removed takes
// effect. ok is false when there's no service to restart (agents running in a
// terminal, or not set up).
func Restart() (ok bool, err error) {
	installed, err := Service()
	if err != nil || !installed {
		return false, err
	}
	if err := Stop(); err != nil {
		return false, err
	}
	return true, Start()
}

// Action is what the agents do with one kind of event from one source.
type Action struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	On      string `json:"on"`     // the event type
	Source  string `json:"source"` // the source it's scoped to; "" for every source
	Auto    bool   `json:"auto"`
	Off     bool   `json:"off"`
	Timeout string `json:"timeout"`
	Do      string `json:"do"` // what the agent is told
}

// Actions lists every action.
func Actions() ([]Action, error) {
	var a []Action
	err := output(&a, nil, "action", "list", "--json")
	return a, err
}

// SetActionOff switches an action off (it matches nothing) or back on.
func SetActionOff(id string, off bool) error {
	return run(nil, "action", "edit", id, "--off", strconv.FormatBool(off))
}

// SetActionPrompt replaces what an action tells the agent.
func SetActionPrompt(id, prompt string) error {
	f, err := os.CreateTemp("", "shepherd-prompt-*.md")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(prompt); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return run(nil, "action", "edit", id, "--do-file", f.Name())
}

// Licence is the edition agents run as.
type Licence struct {
	Tier    string    `json:"tier"` // free | pro
	Email   string    `json:"email,omitempty"`
	Expires time.Time `json:"expires,omitzero"`
	InGrace bool      `json:"in_grace,omitempty"`
	Reason  string    `json:"reason,omitempty"`
}

// Pro reports whether this is Shepherd Pro.
func (l Licence) Pro() bool { return l.Tier == "pro" }

// CurrentLicence reads the edition.
func CurrentLicence() (Licence, error) {
	var l Licence
	err := output(&l, nil, "license", "status", "--json")
	return l, err
}

// Activate checks a Pro key and saves it.
func Activate(key string) error { return run(strings.NewReader(key+"\n"), "license", "activate") }

// run runs drover for its effect, discarding stdout.
func run(stdin io.Reader, args ...string) error {
	_, err := capture(stdin, args...)
	return err
}

// output runs drover and decodes its JSON stdout into v.
func output(v any, stdin io.Reader, args ...string) error {
	out, err := capture(stdin, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return fmt.Errorf("agents: unexpected reply to %s: %w", args[0], err)
	}
	return nil
}

// capture runs drover and returns its stdout. A failure is put in Shepherd's
// words.
func capture(stdin io.Reader, args ...string) (string, error) {
	if !Installed() {
		return "", ErrMissing
	}
	var stdout, stderr bytes.Buffer
	c := Command(args...)
	c.Stdin, c.Stdout, c.Stderr = stdin, &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimPrefix(strings.TrimSpace(stderr.String()), "drover: ")
		if msg == "" {
			msg = err.Error()
		}
		return "", translate(msg)
	}
	return stdout.String(), nil
}

// translate turns drover's error messages into Shepherd's: the ones people can
// act on get their own wording, and the rest lose drover's name.
func translate(msg string) error {
	switch {
	case strings.Contains(msg, "Pro licence"):
		return ErrPro
	case strings.Contains(msg, "no coding agent is set up"):
		return errors.New("agents aren't set up yet: run shepherd agents setup")
	case strings.Contains(msg, "no action"):
		return ErrNoAction
	case strings.Contains(msg, "no coding agent found"):
		return errors.New("no coding agent found: install Claude Code or Codex, or pass --runner")
	case strings.Contains(msg, "already watching"):
		return errors.New("agents are already running")
	}
	msg = strings.NewReplacer("drover's", "the agents'", "drover", "agents").Replace(msg)
	return errors.New(msg)
}
