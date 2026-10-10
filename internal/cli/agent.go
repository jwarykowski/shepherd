package cli

// agent.go: hand an item to a coding agent (shepherd agent) and complete the
// item once the agent's branch merges (shepherd sync). herdr, gh and the agent
// CLIs are driven as external commands through the run/interactive vars, so
// tests stub them without spawning processes.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// run executes name in dir and returns its stdout; a failure carries the
// command's stderr. A package var so tests can stub external commands.
var run = func(dir, name string, args ...string) ([]byte, error) {
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

// interactive runs name in dir attached to this terminal, for running an agent
// directly when herdr isn't available. A package var for tests.
var interactive = func(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// herdrBin finds a reachable herdr: $HERDR_BIN_PATH (set inside herdr panes)
// or herdr on PATH, answering `herdr api snapshot`. "" when herdr isn't usable.
var herdrBin = func() string {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		p, err := exec.LookPath("herdr")
		if err != nil {
			return ""
		}
		bin = p
	}
	if _, err := run("", bin, "api", "snapshot"); err != nil {
		return ""
	}
	return bin
}

// agentConfig reads the agent defaults from the shared config.toml:
// agent_kind (default "claude") and agent_branch_prefix (default "todo/").
func agentConfig() (kind, prefix string) {
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

// slug turns item text into a branch-safe name: lowercase alphanumeric runs
// joined by "-", at most 40 bytes, never ending in "-".
func slug(text string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "item"
	}
	return s
}

// agentPrompt is what the agent is asked to do: the item text, then its note.
func agentPrompt(it todo.Item) string {
	if it.Note == "" {
		return it.Text
	}
	return it.Text + "\n\n" + it.Note
}

// repoDir is where the agent works: the board's working directory (board dir)
// if set, else the git repository containing the current directory.
func repoDir(board string) (string, error) {
	dir := store.BoardDir(board)
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	out, err := run(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not in a git repository (set one with: shepherd board dir %s <path>)", dir, board)
	}
	return strings.TrimSpace(string(out)), nil
}

// splitRef pulls the item ref out of args (it may come before or after the
// flags) and returns it with the remaining flag args.
func splitRef(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// cmdAgent hands item <ref> to a coding agent: shepherd agent <ref>
// [--kind <agent>] [--to <pane>] [--here] [--json].
//
//   - inside herdr (default): create a git worktree on branch <prefix><slug>
//     through herdr, start the agent there with the item as its prompt, and
//     record the branch on the item (branch: token) for shepherd sync.
//   - --to <pane>: send the item to an agent already running in herdr pane
//     <pane> instead; no worktree.
//   - --here, or no herdr: run the agent CLI in the foreground in the repo.
//
// The item is marked in-progress once the agent has it.
func cmdAgent(args []string, board string, w io.Writer) int {
	ref, args := splitRef(args)
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "echo the updated item as JSON")
	kindFlag := fs.String("kind", "", "agent to run: claude, codex, opencode, … (default: config agent_kind, else claude)")
	to := fs.String("to", "", "send to the agent already running in this herdr pane id instead")
	here := fs.Bool("here", false, "run the agent in this terminal instead of a herdr worktree")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if ref == "" {
		ref = fs.Arg(0)
	}
	if ref == "" {
		return usageErr(w, *asJSON, "agent needs an item, e.g. shepherd agent 3")
	}
	items := store.Load(store.TodoPathFor(board))
	p, s, ok := resolveRef(ref, items)
	if !ok || s != 0 {
		return refErr(w, *asJSON, ref, len(items))
	}
	it := items[p-1]
	if it.Done {
		return usageErr(w, *asJSON, fmt.Sprintf("item %s is already done", ref))
	}
	kind, prefix := agentConfig()
	if *kindFlag != "" {
		kind = *kindFlag
	}
	prompt := agentPrompt(it)

	herdr := ""
	if !*here {
		herdr = herdrBin()
	}
	branch := ""
	switch {
	case *to != "":
		if herdr == "" {
			return usageErr(w, *asJSON, "--to needs herdr")
		}
		if _, err := run("", herdr, "agent", "prompt", *to, prompt); err != nil {
			return saveErr(w, *asJSON, err)
		}
	case herdr != "":
		repo, err := repoDir(board)
		if err != nil {
			return saveErr(w, *asJSON, err)
		}
		name := slug(it.Text)
		branch = prefix + name
		out, err := run(repo, herdr, "worktree", "create", "--cwd", repo, "--branch", branch, "--label", name, "--focus")
		if err != nil {
			return saveErr(w, *asJSON, err)
		}
		var created struct {
			Result struct {
				RootPane struct {
					PaneID string `json:"pane_id"`
				} `json:"root_pane"`
			} `json:"result"`
		}
		if err := json.Unmarshal(out, &created); err != nil || created.Result.RootPane.PaneID == "" {
			return saveErr(w, *asJSON, fmt.Errorf("herdr worktree create: unexpected output"))
		}
		pane := created.Result.RootPane.PaneID
		if _, err := run(repo, herdr, "agent", "start", name, "--kind", kind, "--pane", pane, "--", prompt); err != nil {
			return saveErr(w, *asJSON, err)
		}
	default:
		repo, err := repoDir(board)
		if err != nil {
			return saveErr(w, *asJSON, err)
		}
		// mark it before handing over the terminal: the agent runs until exit
		if code := setAgentState(board, it.ID, "", w, false); code != 0 {
			return code
		}
		if err := interactive(repo, kind, prompt); err != nil {
			return saveErr(w, *asJSON, err)
		}
		return 0
	}
	return setAgentState(board, it.ID, branch, w, *asJSON)
}

// setAgentState marks item id in-progress (and records branch, if any) under
// the board lock, then reports it like edit does.
func setAgentState(board, id, branch string, w io.Writer, asJSON bool) int {
	return locked(board, w, asJSON, func(path string, items []todo.Item) (int, error) {
		p, _, ok := resolveID(items, id)
		if !ok {
			return refErr(w, asJSON, id, len(items)), nil
		}
		todo.SetStatus(&items[p-1], "in-progress")
		if branch != "" {
			items[p-1].Branch = branch
		}
		if err := store.Save(path, items); err != nil {
			return 0, err
		}
		if asJSON {
			return emitJSON(w, toJSON(items[p-1], p)), nil
		}
		say(w, formatLine(p, items[p-1]))
		return 0, nil
	})
}

// cmdSync completes items whose agent branch has merged: shepherd sync
// [--all] [--dry-run] [--json]. For every open item carrying a branch: token
// it asks gh for that branch's pull request, in the board's repository; a
// MERGED one completes the item (subtasks cascade, as with done).
func cmdSync(args []string, board string, w io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "echo the completed items as JSON")
	all := fs.Bool("all", false, "sync every board")
	dry := fs.Bool("dry-run", false, "report what would complete without writing")
	fs.BoolVar(dry, "n", false, "shorthand for --dry-run")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	boards := []string{board}
	if *all {
		boards = nil
		for _, b := range store.Boards() {
			boards = append(boards, b.Name)
		}
	}
	completed := []itemJSON{}
	for _, b := range boards {
		merged := mergedIDs(b)
		if len(merged) == 0 {
			continue
		}
		code := locked(b, w, *asJSON, func(path string, items []todo.Item) (int, error) {
			for i := range items {
				if items[i].Done || !merged[items[i].ID] {
					continue
				}
				if !*dry {
					todo.SetParentDone(&items[i], true)
				}
				j := toJSON(items[i], i+1)
				j.Board = b
				completed = append(completed, j)
				if !*asJSON {
					say(w, fmt.Sprintf("%s (%s merged)", formatLine(i+1, items[i]), items[i].Branch))
				}
			}
			if *dry {
				return 0, nil
			}
			return 0, store.Save(path, items)
		})
		if code != 0 {
			return code
		}
	}
	if *asJSON {
		return emitJSON(w, completed)
	}
	return 0
}

// mergedIDs returns the ids of board's open items whose branch: has a merged
// pull request. Items without a reachable repo or PR are skipped.
func mergedIDs(board string) map[string]bool {
	var candidates []todo.Item
	for _, it := range store.Load(store.TodoPathFor(board)) {
		if !it.Done && it.Branch != "" {
			candidates = append(candidates, it)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	repo, err := repoDir(board)
	if err != nil {
		return nil
	}
	merged := map[string]bool{}
	for _, it := range candidates {
		out, err := run(repo, "gh", "pr", "view", it.Branch, "--json", "state", "-q", ".state")
		if err == nil && strings.TrimSpace(string(out)) == "MERGED" {
			merged[it.ID] = true
		}
	}
	return merged
}
