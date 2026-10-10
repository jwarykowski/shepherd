// Package agent hands a todo item to a coding agent: a git worktree plus an
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

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns item text into a branch-safe name: lowercase alphanumeric runs
// joined by "-", at most 40 bytes, never ending in "-".
func Slug(text string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "item"
	}
	return s
}

// Prompt is what the agent is asked to do: the item text, then its note.
func Prompt(it todo.Item) string {
	if it.Note == "" {
		return it.Text
	}
	return it.Text + "\n\n" + it.Note
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

// StartWorktree creates a herdr worktree on branch <prefix><slug> off repo and
// starts a kind agent in it with the item as its prompt, returning the branch.
func StartWorktree(herdr, repo, kind, prefix string, it todo.Item) (string, error) {
	name := Slug(it.Text)
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
	if _, err := Run(repo, herdr, "agent", "start", name, "--kind", kind, "--pane", pane, "--", Prompt(it)); err != nil {
		return "", err
	}
	return branch, nil
}

// SendTo submits the item as a prompt to the agent in herdr pane.
func SendTo(herdr, pane string, it todo.Item) error {
	_, err := Run("", herdr, "agent", "prompt", pane, Prompt(it))
	return err
}

// MergedIDs returns the ids of board's open items whose branch: has a merged
// pull request (asked of gh in the board's repository). Items without a
// reachable repo or PR are skipped.
func MergedIDs(board string) map[string]bool {
	var candidates []todo.Item
	for _, it := range store.Load(store.TodoPathFor(board)) {
		if !it.Done && it.Branch != "" {
			candidates = append(candidates, it)
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
