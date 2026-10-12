package cli

// agents.go: the agent commands. Everything here talks about agents; drover,
// which runs them, stays behind internal/drover.
//
//	shepherd agents setup [--runner claude|codex]   write drover's config, start it
//	shepherd agents start|stop|status [--json]      the background service
//	shepherd go <ref>                               send an item to your agent
//	shepherd pro activate <key> | status [--json]   drover Pro

import (
	"flag"
	"fmt"
	"io"
	"os"

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
	case "start":
		if code := agentErr(drover.Start()); code != 0 {
			return code
		}
		say(w, "Agents are running in the background.")
		return 0
	case "stop":
		if code := agentErr(drover.Stop()); code != 0 {
			return code
		}
		say(w, "Agents stopped. Nothing runs until shepherd agents start.")
		return 0
	case "status":
		return agentsStatus(rest, w)
	default:
		return usageErr(w, false, fmt.Sprintf("unknown agents subcommand %q (setup|start|stop|status)", sub))
	}
}

// agentsSetup writes the agents' starter config for every board (keeping one
// that exists) and starts them in the background.
func agentsSetup(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("agents setup", flag.ContinueOnError)
	runner := fs.String("runner", "", "coding agent: claude or codex; default the first one installed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	created, agent, err := drover.Setup(*runner)
	if code := agentErr(err); code != 0 {
		return code
	}
	switch {
	case !created:
		say(w, "Agents are already set up; keeping your settings.")
	case agent != "":
		say(w, fmt.Sprintf("Set up agents for every board, using %s.", agent))
	default:
		say(w, "Set up agents for every board.")
	}
	if code := agentErr(drover.StartBackground()); code != 0 {
		return code
	}
	say(w, "Agents are running in the background.")
	say(w, "Each board's tasks run in its working directory: shepherd board dir <board> <path>.")
	say(w, "Ready. Add a task, then press @ on it in the board (or run shepherd go <ref>).")
	return 0
}

// agentsStatus says whether agents are set up and running.
func agentsStatus(args []string, w io.Writer) int {
	asJSON, _ := extractFlag(args, "--json")
	installed, err := drover.Service()
	if code := agentErr(err); code != 0 {
		return code
	}
	running := drover.Watching()
	if asJSON {
		return emitJSON(w, map[string]bool{"set_up": installed || running, "running": running})
	}
	switch {
	case running:
		emit(w, "agents: running")
	case installed:
		emit(w, "agents: stopped (shepherd agents start)")
	default:
		emit(w, "agents: not set up (shepherd agents setup)")
	}
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
	it, parent := items[p-1], (*todo.Item)(nil)
	if s > 0 {
		it, parent = items[p-1].Subs[s-1], &items[p-1]
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
			it, parent = items[p-1].Subs[s-1], &items[p-1]
		}
	}
	runs, err := drover.Send(drover.Task{Item: it, Parent: parent, Board: board})
	if err != nil {
		if asJSON {
			return jsonErr(w, 1, "agents", err.Error())
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

// cmdPro activates and reports Shepherd Pro: shepherd pro activate <key> |
// status [--json].
func cmdPro(args []string, w io.Writer) int {
	if len(args) == 0 {
		return usageErr(w, false, "pro needs a subcommand: activate <key> | status")
	}
	switch args[0] {
	case "activate":
		if len(args) != 2 {
			return usageErr(w, false, "pro activate needs your licence key: shepherd pro activate <key>")
		}
		if code := agentErr(drover.Activate(args[1])); code != 0 {
			return code
		}
		l, err := drover.CurrentLicence()
		if code := agentErr(err); code != 0 {
			return code
		}
		say(w, fmt.Sprintf("Shepherd Pro is on for %s, until %s.", l.Email, l.Expires.Format("2 January 2006")))
		return 0
	case "status":
		asJSON, _ := extractFlag(args[1:], "--json")
		l, err := drover.CurrentLicence()
		if code := agentErr(err); code != 0 {
			return code
		}
		if asJSON {
			return emitJSON(w, l)
		}
		switch {
		case l.Pro() && l.InGrace:
			emit(w, fmt.Sprintf("Shepherd Pro, for %s: expired %s; renew within 14 days of that to keep it", l.Email, l.Expires.Format("2 January 2006")))
		case l.Pro():
			emit(w, fmt.Sprintf("Shepherd Pro, for %s, until %s", l.Email, l.Expires.Format("2 January 2006")))
		default:
			emit(w, "Shepherd Free: tasks come from your boards, and every run waits for your go.")
			emit(w, "Pro adds GitHub, Sentry and webhook tasks, and runs trusted jobs without asking: shepherdcli.dev/pro")
		}
		return 0
	default:
		return usageErr(w, false, fmt.Sprintf("unknown pro subcommand %q (activate|status)", args[0]))
	}
}

// agentErr reports an agents failure as a runtime error (exit 1).
func agentErr(err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, "shepherd:", err)
	return 1
}
