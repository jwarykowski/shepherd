package cli

// handoff.go: pass an item to the user's handoff command (shepherd handoff).
// What the command does — start a coding agent, call drover, run a script — is
// up to config.toml; see internal/handoff.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"shepherd/internal/handoff"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// runHandoff runs the hand-off command attached to this terminal, a var so
// tests don't start real processes.
var runHandoff = func(c *exec.Cmd) error {
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// cmdHandoff passes item or subtask <ref> to the handoff command in
// config.toml: shepherd handoff <ref>. A subtask goes with its parent as
// context. The item is marked in-progress once the command is known to exist,
// before it runs (an agent session can last a long time); an unset or missing
// command leaves the item untouched. The command's exit status is returned.
func cmdHandoff(args []string, board string, w io.Writer) int {
	if len(args) != 1 {
		return usageErr(w, false, "handoff needs one item, e.g. shepherd handoff 3 or 3.2")
	}
	ref := args[0]
	items := store.Load(store.TodoPathFor(board))
	p, s, ok := resolveRef(ref, items)
	if !ok {
		return refErr(w, false, ref, len(items))
	}
	task := handoff.Task{Item: items[p-1], Board: board}
	if s > 0 {
		task = handoff.Task{Item: items[p-1].Subs[s-1], Parent: &items[p-1], Board: board}
	}
	if task.Item.Done {
		return usageErr(w, false, fmt.Sprintf("item %s is already done", ref))
	}
	cmd, err := handoff.Cmd(task)
	if err != nil {
		return usageErr(w, false, err.Error())
	}
	if code := markInProgress(board, task.Item.ID, w); code != 0 {
		return code
	}
	if err := runHandoff(cmd); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	return 0
}

// markInProgress sets item or subtask id to in-progress under the board lock
// and prints it, like edit does.
func markInProgress(board, id string, w io.Writer) int {
	return locked(board, w, false, func(path string, items []todo.Item) (int, error) {
		p, s, ok := resolveID(items, id)
		if !ok {
			return refErr(w, false, id, len(items)), nil
		}
		it := &items[p-1]
		if s > 0 {
			it = &items[p-1].Subs[s-1]
		}
		todo.SetStatus(it, "in-progress")
		if err := store.Save(path, items); err != nil {
			return 0, err
		}
		if s == 0 {
			say(w, formatLine(p, items[p-1]))
		} else {
			say(w, formatSub(p, s, items[p-1].Subs[s-1]))
		}
		return 0, nil
	})
}
