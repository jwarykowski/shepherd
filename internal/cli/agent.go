package cli

// agent.go: hand an item to a coding agent (shepherd agent) and complete the
// item once the agent's branch merges (shepherd sync). The herdr / gh / agent
// orchestration lives in internal/agent, shared with the TUI.

import (
	"flag"
	"fmt"
	"io"
	"time"

	"shepherd/internal/agent"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// splitRef pulls the item ref out of args (it may come before or after the
// flags) and returns it with the remaining flag args.
func splitRef(args []string) (string, []string) {
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		return args[0], args[1:]
	}
	return "", args
}

// cmdAgent hands item or subtask <ref> to a coding agent: shepherd agent <ref>
// [--kind <agent>] [--to <pane>] [--here] [--json].
//
//   - inside herdr (default): create a git worktree on branch <prefix><slug>
//     through herdr, start the agent there with the item as its prompt, and
//     record the branch on the item (branch: token) for shepherd sync. A
//     subtask's prompt adds its parent as context, and its slug starts with
//     a short parent slug.
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
		return usageErr(w, *asJSON, "agent needs an item, e.g. shepherd agent 3 or 3.2")
	}
	items := store.Load(store.TodoPathFor(board))
	p, s, ok := resolveRef(ref, items)
	if !ok {
		return refErr(w, *asJSON, ref, len(items))
	}
	task := agent.Task{Item: items[p-1]}
	if s > 0 {
		task = agent.Task{Item: items[p-1].Subs[s-1], Parent: &items[p-1]}
	}
	it := task.Item
	if it.Done {
		return usageErr(w, *asJSON, fmt.Sprintf("item %s is already done", ref))
	}
	kind, prefix := agent.Config()
	if *kindFlag != "" {
		kind = *kindFlag
	}

	herdr := ""
	if !*here {
		herdr = agent.Herdr()
	}
	branch := ""
	switch {
	case *to != "":
		if herdr == "" {
			return usageErr(w, *asJSON, "--to needs herdr")
		}
		if err := agent.SendTo(herdr, *to, task); err != nil {
			return saveErr(w, *asJSON, err)
		}
	case herdr != "":
		repo, err := agent.RepoDir(board)
		if err != nil {
			return saveErr(w, *asJSON, err)
		}
		if branch, err = agent.StartWorktree(herdr, repo, kind, prefix, task); err != nil {
			return saveErr(w, *asJSON, err)
		}
	default:
		repo, err := agent.RepoDir(board)
		if err != nil {
			return saveErr(w, *asJSON, err)
		}
		// mark it before handing over the terminal: the agent runs until exit
		if code := setAgentState(board, it.ID, "", w, false); code != 0 {
			return code
		}
		if err := agent.Interactive(repo, kind, task.Prompt()); err != nil {
			return saveErr(w, *asJSON, err)
		}
		return 0
	}
	return setAgentState(board, it.ID, branch, w, *asJSON)
}

// setAgentState marks item or subtask id in-progress (and records branch, if
// any) under the board lock, then reports it like edit does.
func setAgentState(board, id, branch string, w io.Writer, asJSON bool) int {
	return locked(board, w, asJSON, func(path string, items []todo.Item) (int, error) {
		p, s, ok := resolveID(items, id)
		if !ok {
			return refErr(w, asJSON, id, len(items)), nil
		}
		it := &items[p-1]
		if s > 0 {
			it = &items[p-1].Subs[s-1]
		}
		todo.SetStatus(it, "in-progress")
		if branch != "" {
			it.Branch = branch
		}
		if err := store.Save(path, items); err != nil {
			return 0, err
		}
		switch {
		case asJSON:
			return emitJSON(w, toJSON(items[p-1], p)), nil
		case s == 0:
			say(w, formatLine(p, items[p-1]))
		default:
			say(w, formatSub(p, s, items[p-1].Subs[s-1]))
		}
		return 0, nil
	})
}

// sleep is time.Sleep, a var so tests can stop a --every loop.
var sleep = time.Sleep

// cmdSync completes items whose agent branch has merged: shepherd sync
// [--all] [--dry-run] [--json] [--every <dur>]. For every open item or subtask
// carrying a branch: token it asks gh for that branch's pull request, in the
// board's repository; a MERGED one completes it, exactly as done would (an
// item's subtasks cascade; a subtask completes its parent once it's the last).
// --every repeats the pass on that interval until killed (the herdr plugin
// runs it in the background).
func cmdSync(args []string, board string, w io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "echo the completed items as JSON")
	all := fs.Bool("all", false, "sync every board")
	dry := fs.Bool("dry-run", false, "report what would complete without writing")
	fs.BoolVar(dry, "n", false, "shorthand for --dry-run")
	every := fs.Duration("every", 0, "repeat on this interval until killed, e.g. 1m")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	for {
		if code := syncOnce(board, *all, *dry, *asJSON, w); code != 0 || *every <= 0 {
			return code
		}
		sleep(*every)
	}
}

// syncOnce is one sync pass over board (or every board).
func syncOnce(board string, all, dry, asJSON bool, w io.Writer) int {
	boards := []string{board}
	if all {
		boards = nil
		for _, b := range store.Boards() {
			boards = append(boards, b.Name)
		}
	}
	completed := []itemJSON{}
	for _, b := range boards {
		merged := agent.MergedIDs(b)
		if len(merged) == 0 {
			continue
		}
		code := locked(b, w, asJSON, func(path string, items []todo.Item) (int, error) {
			report := func(i, s int, it todo.Item) {
				j := toJSON(items[i], i+1)
				j.Board = b
				completed = append(completed, j)
				if asJSON {
					return
				}
				line := formatLine(i+1, it)
				if s > 0 {
					line = formatSub(i+1, s, it)
				}
				say(w, fmt.Sprintf("%s (%s merged)", line, it.Branch))
			}
			for i := range items {
				if !items[i].Done && merged[items[i].ID] {
					if !dry {
						todo.SetParentDone(&items[i], true)
					}
					report(i, 0, items[i])
					continue
				}
				for s := range items[i].Subs {
					if items[i].Subs[s].Done || !merged[items[i].Subs[s].ID] {
						continue
					}
					if !dry {
						todo.SetSubDone(&items[i], s, true)
					}
					report(i, s+1, items[i].Subs[s])
				}
			}
			if dry {
				return 0, nil
			}
			return 0, store.Save(path, items)
		})
		if code != 0 {
			return code
		}
	}
	if asJSON {
		return emitJSON(w, completed)
	}
	return 0
}
