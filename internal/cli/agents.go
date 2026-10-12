package cli

// agents.go: the agent commands. shepherd is the one front door; drover does
// the work behind it (see internal/drover).
//
//	shepherd agents setup [--runner claude|codex]   write drover's config, start it
//	shepherd agents start|stop|status [--json]      the background service
//	shepherd go <ref>                               send an item to your agent
//	shepherd pro activate <key> | status [--json]   drover Pro

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"shepherd/internal/drover"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

func cmdAgents(args []string, w io.Writer) int {
	if len(args) == 0 {
		return usageErr(w, false, "agents needs a subcommand: setup|start|stop|status")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "setup":
		return agentsSetup(rest, w)
	case "start", "stop":
		return droverCode(drover.Passthrough(w, os.Stderr, "service", sub))
	case "status":
		return droverCode(drover.Passthrough(w, os.Stderr, append([]string{"service", "status"}, rest...)...))
	default:
		return usageErr(w, false, fmt.Sprintf("unknown agents subcommand %q (setup|start|stop|status)", sub))
	}
}

// agentsSetup writes a starter drover config for every board (unless one
// exists) and starts drover in the background.
func agentsSetup(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("agents setup", flag.ContinueOnError)
	runner := fs.String("runner", "", "coding agent: claude or codex; default the first one installed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	initArgs := []string{"init"}
	if *runner != "" {
		initArgs = append(initArgs, "--runner", *runner)
	}
	var errBuf bytes.Buffer
	switch err := drover.Passthrough(w, &errBuf, initArgs...); {
	case err == drover.ErrMissing:
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	case err != nil && strings.Contains(errBuf.String(), "already exists"):
		say(w, "drover is already set up; keeping its config")
	case err != nil:
		os.Stderr.Write(errBuf.Bytes())
		return 1
	}
	if err := drover.Passthrough(w, os.Stderr, "service", "install"); err != nil {
		return droverCode(err)
	}
	say(w, "Ready. Add a task, then press @ on it in the board (or run shepherd go <ref>).")
	return 0
}

// cmdGo sends an item or subtask to the coding agent: shepherd go <ref>. A run
// drover holds for it is released; otherwise one starts now.
func cmdGo(args []string, board string, w io.Writer) int {
	asJSON, args := extractFlag(args, "--json")
	if len(args) != 1 {
		return usageErr(w, asJSON, "go needs one item, e.g. shepherd go 3")
	}
	items := store.Load(store.TodoPathFor(board))
	p, s, ok := resolveRef(args[0], items)
	if !ok {
		return refErr(w, asJSON, args[0], len(items))
	}
	it := items[p-1]
	if s > 0 {
		it = items[p-1].Subs[s-1]
	}
	if it.Done {
		return usageErr(w, asJSON, fmt.Sprintf("item %s is already done", args[0]))
	}
	if it.ID == "" { // a hand-written item without an id yet: give it one
		if code := ensureIDs(board, w, asJSON); code != 0 {
			return code
		}
		items = store.Load(store.TodoPathFor(board))
		if it = items[p-1]; s > 0 {
			it = items[p-1].Subs[s-1]
		}
	}
	runs, err := drover.Send(drover.Task{Item: it, Board: board})
	if err != nil && strings.Contains(err.Error(), "no action") {
		err = fmt.Errorf("%w; shepherd agents setup adds one", err)
	}
	if err != nil {
		if asJSON {
			return jsonErr(w, 1, "drover", err.Error())
		}
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(w, runs)
		return 0
	}
	for _, r := range runs {
		state := "sent to your agent"
		if r.Status == "running" {
			state = "already running"
		}
		say(w, fmt.Sprintf("%s: %s (run %s)", state, it.Text, r.ID))
	}
	return 0
}

// ensureIDs saves the board once under its lock, which stamps an id on every
// item that lacks one.
func ensureIDs(board string, w io.Writer, asJSON bool) int {
	return locked(board, w, asJSON, func(path string, items []todo.Item) (int, error) {
		return 0, store.Save(path, items)
	})
}

// cmdPro wraps drover's licence: shepherd pro activate <key> | status [--json].
func cmdPro(args []string, w io.Writer) int {
	if len(args) == 0 {
		return usageErr(w, false, "pro needs a subcommand: activate <key> | status")
	}
	switch args[0] {
	case "activate", "status":
		return droverCode(drover.Passthrough(w, os.Stderr, append([]string{"license"}, args...)...))
	default:
		return usageErr(w, false, fmt.Sprintf("unknown pro subcommand %q (activate|status)", args[0]))
	}
}

// droverCode maps a passed-through drover command's result to an exit code:
// drover has already printed its own error.
func droverCode(err error) int {
	if err == nil {
		return 0
	}
	if err == drover.ErrMissing {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
	}
	return 1
}
