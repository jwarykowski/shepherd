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

// idSuffixLen is how much of the item's id ends a task name. It's the id's
// tail: an id starts with its creation timestamp (todo.NewID), so items made
// around the same time share a prefix, while the tail is random.
const idSuffixLen = 6

// Name is the task's worktree / agent name (and, after the prefix, its
// branch): the item's slug, or for a subtask a short parent slug then the
// subtask's, ending in the tail of the item's id. The id keeps the branch
// unique to this item: two items with the same text, or a new item reusing an
// old one's text, would otherwise share a branch, and shepherd sync would
// complete the new item from the old item's merged pull request.
func (t Task) Name() string {
	name := Slug(t.Item.Text)
	if t.Parent != nil {
		name = slugN(t.Parent.Text, 20) + "-" + name
	}
	if id := t.Item.ID; id != "" {
		name += "-" + id[max(0, len(id)-idSuffixLen):]
	}
	return name
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

// RepoDir is the git repository the board's agents work in and its pull
// requests are checked in: the repository root of the board's working
// directory (shepherd board dir). board "" is the default board. It errors when
// the board has no working directory or it isn't in a git repository; shepherd
// sync relies on that to skip such boards rather than guess from wherever it
// happens to run.
func RepoDir(board string) (string, error) {
	board = boardName(board)
	dir := store.BoardDir(board)
	if dir == "" {
		return "", fmt.Errorf("board %s has no working directory (set one with: shepherd board dir %s <path>)", board, board)
	}
	return repoRoot(dir, board)
}

// AdoptRepoDir is RepoDir for a hand-off: when the board has no working
// directory yet it uses the git repository containing the current directory
// and records it as the board's (adopted reports that), so shepherd sync later
// checks the same repository the agent worked in.
func AdoptRepoDir(board string) (repo string, adopted bool, err error) {
	board = boardName(board)
	if store.BoardDir(board) != "" {
		repo, err = RepoDir(board)
		return repo, false, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	if repo, err = repoRoot(wd, board); err != nil {
		return "", false, err
	}
	if err := store.SetBoardDir(board, repo); err != nil {
		return "", false, err
	}
	return repo, true, nil
}

func boardName(board string) string {
	if board == "" {
		return "default"
	}
	return board
}

// repoRoot is the root of the git repository containing dir.
func repoRoot(dir, board string) (string, error) {
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
// has a merged pull request (asked of gh in the board's repository, see
// RepoDir). A board without a working directory, and items without a PR, are
// skipped.
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
