// Package agent hands a todo item (or one subtask) to a coding agent: a git worktree plus an
// agent started through herdr, a prompt to an agent already running in a
// herdr pane, or the agent CLI run in a terminal. It also finds items whose
// agent branch has merged. herdr, gh, git and the agent CLIs are external
// commands driven through the Run/Interactive/Herdr vars, so tests stub them.
//
// Depends on todo and store; used by the cli and tui frontends.
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// Run executes name in dir and returns its stdout; a failure carries the
// command's stderr.
var Run = func(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = fmt.Errorf("%s: %s", filepath.Base(name), s)
		}
	}
	return out, err
}

// Interactive runs the agent attached to this terminal (no-herdr mode).
var Interactive = func(dir, kind, prompt string) error {
	cmd := Command(dir, kind, prompt)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Command is the agent CLI invocation for running it directly: `<kind>
// "<prompt>"` in dir. Every agent herdr knows takes the prompt as its first
// argument.
func Command(dir, kind, prompt string) *exec.Cmd {
	cmd := exec.Command(kind, prompt)
	cmd.Dir = dir
	return cmd
}

// Herdr finds a reachable herdr: $HERDR_BIN_PATH (set inside herdr panes) or
// herdr on PATH, answering `herdr api snapshot`. "" when herdr isn't usable.
var Herdr = func() string {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		p, err := exec.LookPath("herdr")
		if err != nil {
			return ""
		}
		bin = p
	}
	if _, err := Run("", bin, "api", "snapshot"); err != nil {
		return ""
	}
	return bin
}

// Config reads the agent defaults from the shared config.toml: agent_kind
// (default "claude") and agent_branch_prefix (default "todo/").
func Config() (kind, prefix string) {
	kv := store.ScanKVFile(store.ConfigPath())
	kind = strings.Trim(kv["agent_kind"], `"`)
	if kind == "" {
		kind = "claude"
	}
	prefix, ok := kv["agent_branch_prefix"]
	if !ok {
		prefix = "todo/"
	}
	return kind, strings.Trim(prefix, `"`)
}

// Task is the work handed to an agent: an item, or a subtask together with
// its parent (which gives the agent context and names the branch).
type Task struct {
	Item   todo.Item
	Parent *todo.Item // nil for a top-level item
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns item text into a branch-safe name: lowercase alphanumeric runs
// joined by "-", at most 40 bytes, never ending in "-".
func Slug(text string) string { return slugN(text, 40) }

func slugN(text string, n int) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if len(s) > n {
		s = strings.TrimRight(s[:n], "-")
	}
	if s == "" {
		s = "item"
	}
	return s
}

// Name is the task's worktree / agent name: the item's slug, or for a subtask
// a short parent slug then the subtask's, so sibling subtasks never collide
// with each other or with the parent.
func (t Task) Name() string {
	if t.Parent == nil {
		return Slug(t.Item.Text)
	}
	return slugN(t.Parent.Text, 20) + "-" + Slug(t.Item.Text)
}

// Prompt is what the agent is asked to do: the item text, then its note; for
// a subtask, followed by the parent it belongs to (text and note) as context.
func (t Task) Prompt() string {
	parts := []string{t.Item.Text}
	if t.Item.Note != "" {
		parts = append(parts, t.Item.Note)
	}
	if t.Parent != nil {
		parts = append(parts, "This is one part of: "+t.Parent.Text)
		if t.Parent.Note != "" {
			parts = append(parts, t.Parent.Note)
		}
	}
	return strings.Join(parts, "\n\n")
}

// RepoDir is where the agent works: the board's working directory (board dir)
// if set, else the git repository containing the current directory. board ""
// is the default board.
func RepoDir(board string) (string, error) {
	if board == "" {
		board = "default"
	}
	dir := store.BoardDir(board)
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	out, err := Run(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not in a git repository (set one with: shepherd board dir %s <path>)", dir, board)
	}
	return strings.TrimSpace(string(out)), nil
}

// StartWorktree creates a herdr worktree on branch <prefix><name> off repo and
// starts a kind agent in it with the task as its prompt, returning the branch.
func StartWorktree(herdr, repo, kind, prefix string, t Task) (string, error) {
	name := t.Name()
	branch := prefix + name
	out, err := Run(repo, herdr, "worktree", "create", "--cwd", repo, "--branch", branch, "--label", name, "--focus")
	if err != nil {
		return "", err
	}
	var created struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &created); err != nil || created.Result.RootPane.PaneID == "" {
		return "", fmt.Errorf("herdr worktree create: unexpected output")
	}
	pane := created.Result.RootPane.PaneID
	if _, err := Run(repo, herdr, "agent", "start", name, "--kind", kind, "--pane", pane, "--", t.Prompt()); err != nil {
		return "", err
	}
	return branch, nil
}

// SendTo submits the task as a prompt to the agent in herdr pane.
func SendTo(herdr, pane string, t Task) error {
	_, err := Run("", herdr, "agent", "prompt", pane, t.Prompt())
	return err
}

// MergedIDs returns the ids of board's open items and subtasks whose branch:
// has a merged pull request (asked of gh in the board's repository). Items
// without a reachable repo or PR are skipped.
func MergedIDs(board string) map[string]bool {
	var candidates []todo.Item
	for _, it := range store.Load(store.TodoPathFor(board)) {
		for _, c := range append([]todo.Item{it}, it.Subs...) {
			if !c.Done && c.Branch != "" {
				candidates = append(candidates, c)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	repo, err := RepoDir(board)
	if err != nil {
		return nil
	}
	merged := map[string]bool{}
	for _, it := range candidates {
		out, err := Run(repo, "gh", "pr", "view", it.Branch, "--json", "state", "-q", ".state")
		if err == nil && strings.TrimSpace(string(out)) == "MERGED" {
			merged[it.ID] = true
		}
	}
	return merged
}
