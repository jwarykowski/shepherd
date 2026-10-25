package cli

// agents.go: the agent commands. Everything here talks about agents; drover,
// which runs them, stays behind internal/drover.
//
//	shepherd agents setup [--runner claude|codex]   write drover's config, start it
//	shepherd agents start|stop|status [--json]      the background service
//	shepherd agents add github|webhook …            more places tasks come from (Pro)
//	shepherd agents sources [--json] | remove <name>
//	shepherd agents secret <name> [--scheme github]  take only signed deliveries
//	shepherd agents actions [--json]                what each kind of event does
//	shepherd agents action <name> on|off|edit       switch one, or change its prompt
//	shepherd go <ref>                               send an item to your agent
//	shepherd pro activate <key> | status [--json]   drover Pro

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"shepherd/internal/drover"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"
)

func cmdAgents(args []string, w io.Writer) int {
	if len(args) == 0 {
		return usageErr(w, false, "agents needs a subcommand: setup|start|stop|status|add|sources|remove|secret|actions|action")
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
	case "add":
		return agentsAdd(rest, w)
	case "sources":
		return agentsSources(rest, w)
	case "remove":
		return agentsRemove(rest, w)
	case "secret":
		return agentsSecret(rest, os.Stdin, w)
	case "actions":
		return agentsActions(rest, w)
	case "action":
		return agentsAction(rest, os.Stdin, w)
	default:
		return usageErr(w, false, fmt.Sprintf("unknown agents subcommand %q (setup|start|stop|status|add|sources|remove|secret|actions|action)", sub))
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
	say(w, "Ready. Add a task, then press r on it in the board (or run shepherd go <ref>).")
	return 0
}

// agentsAdd adds a GitHub repository or a webhook as a place tasks come from:
//
//	shepherd agents add github <owner/repo> [--dir path] [--label agent] [--name n]
//	shepherd agents add webhook <service> [--name n] [--dir path]
//
// The agent works in --dir, the current directory by default. Tasks from
// either wait in the agents view for a go.
func agentsAdd(args []string, w io.Writer) int {
	const want = "agents add needs github <owner/repo> or webhook <service>, e.g. shepherd agents add webhook sentry"
	if len(args) < 2 || strings.HasPrefix(args[1], "-") {
		return usageErr(w, false, want)
	}
	kind, arg := args[0], args[1]
	fs := flag.NewFlagSet("agents add", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory the agent works in")
	label := fs.String("label", "", "issues with this label are sent (github; default agent)")
	name := fs.String("name", "", "the source's name")
	if err := fs.Parse(args[2:]); err != nil {
		return 2
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return agentErr(err)
	}

	var src drover.Source
	switch kind {
	case "github":
		src, err = drover.AddGitHub(arg, abs, *label, *name)
	case "webhook":
		if !slices.Contains(webhookServices, arg) {
			return usageErr(w, false, fmt.Sprintf("unknown service %q: want %s (webhook takes any service)", arg, strings.Join(webhookServices, ", ")))
		}
		src, err = drover.AddWebhook(cmp.Or(*name, arg), arg, abs)
	default:
		return usageErr(w, false, want)
	}
	if code := agentErr(err); code != 0 {
		return code
	}

	switch src.Kind {
	case "github":
		say(w, fmt.Sprintf("Added %s: open issues in %s labelled %s become tasks for your agent, in %s.", src.Name, src.Detail, cmp.Or(*label, "agent"), abs))
	case "webhook":
		say(w, fmt.Sprintf("Added %s. Paste this URL into %s:", src.Name, serviceName(src.Detail)))
		emit(w, "")
		emit(w, "  "+src.URL)
		emit(w, "")
		if src.Help != "" {
			say(w, src.Help)
		}
		if src.Scheme != "" {
			say(w, fmt.Sprintf("Then copy %s's signing secret and run shepherd agents secret %s, so only %s can send tasks to that URL.",
				serviceName(src.Detail), src.Name, serviceName(src.Detail)))
		}
	}
	say(w, "They wait in the agents view (R) for your go, flagged external.")
	restartAgents(w)
	return 0
}

// webhookServices are the services agents add webhook knows; webhook is any
// other.
var webhookServices = []string{"webhook", "sentry", "linear", "datadog", "alertmanager"}

func serviceName(preset string) string {
	switch preset {
	case "webhook", "":
		return "the service"
	case "alertmanager":
		return "Grafana or Alertmanager"
	case "datadog":
		return "Datadog"
	default:
		return strings.ToUpper(preset[:1]) + preset[1:]
	}
}

// restartAgents restarts the background agents so a source change takes
// effect, or says how.
func restartAgents(w io.Writer) {
	ok, err := drover.Restart()
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "shepherd:", err)
	case ok:
		say(w, "Agents restarted to pick it up.")
	default:
		say(w, "Restart your agents to pick it up.")
	}
}

// agentsSources lists where tasks come from.
func agentsSources(args []string, w io.Writer) int {
	asJSON, _ := extractFlag(args, "--json")
	srcs, err := drover.Sources()
	if code := agentErr(err); code != 0 {
		return code
	}
	if asJSON {
		return emitJSON(w, srcs)
	}
	for _, s := range srcs {
		switch s.Kind {
		case "shepherd":
			emit(w, fmt.Sprintf("%-14s your boards", s.Name))
		case "github":
			emit(w, fmt.Sprintf("%-14s GitHub %s", s.Name, s.Detail))
		case "webhook":
			signed := ""
			if s.Signed != "" {
				signed = "  signed"
			}
			emit(w, fmt.Sprintf("%-14s webhook (%s)  %s%s", s.Name, s.Detail, s.URL, signed))
		default:
			emit(w, fmt.Sprintf("%-14s %s", s.Name, s.Kind))
		}
	}
	return 0
}

// agentsSecret makes a webhook source take only deliveries its service signed:
// shepherd agents secret <name> [--scheme github]. The secret is read without
// echo at a terminal, else from stdin; an empty one stops checking.
func agentsSecret(args []string, stdin *os.File, w io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageErr(w, false, "agents secret needs a webhook source's name: see shepherd agents sources")
	}
	name := args[0]
	fs := flag.NewFlagSet("agents secret", flag.ContinueOnError)
	scheme := fs.String("scheme", "", "how the service signs, for a generic webhook: github")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	var secret string
	if term.IsTerminal(stdin.Fd()) {
		fmt.Fprintf(os.Stderr, "Paste %s's signing secret (empty to stop checking): ", name)
		b, err := term.ReadPassword(stdin.Fd())
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return agentErr(err)
		}
		secret = string(b)
	} else {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return agentErr(err)
		}
		secret = line
	}
	secret = strings.TrimSpace(secret)
	if code := agentErr(drover.SetSecret(name, *scheme, secret)); code != 0 {
		return code
	}
	if secret == "" {
		say(w, name+" takes deliveries without a signature again.")
	} else {
		say(w, name+" now takes only signed deliveries.")
	}
	return 0
}

// agentsActions lists what each kind of event does: its source, its label,
// whether it waits, runs by itself or is off, and the start of its prompt.
func agentsActions(args []string, w io.Writer) int {
	asJSON, _ := extractFlag(args, "--json")
	acts, err := drover.Actions()
	if code := agentErr(err); code != 0 {
		return code
	}
	if asJSON {
		if acts == nil {
			acts = []drover.Action{}
		}
		return emitJSON(w, acts)
	}
	for _, a := range acts {
		state := "waits for go"
		switch {
		case a.Off:
			state = "off"
		case a.Auto:
			state = "runs itself"
		}
		source := cmp.Or(a.Source, "boards")
		if source == "shepherd" {
			source = "boards"
		}
		label := strings.TrimPrefix(a.Name, a.Source+": ")
		emit(w, fmt.Sprintf("%-10s %-22s %-13s %s", source, label, state, firstLine(a.Do, 60)))
	}
	return 0
}

// agentsAction switches an action on or off, or edits its prompt:
// shepherd agents action <name> on|off|edit. name is its label, "source label"
// or its id. edit opens $EDITOR at a terminal, else reads the prompt on stdin.
func agentsAction(args []string, stdin *os.File, w io.Writer) int {
	if len(args) < 2 {
		return usageErr(w, false, "agents action needs a name and on, off or edit, e.g. shepherd agents action \"sentry fix\" off")
	}
	ref, verb := strings.Join(args[:len(args)-1], " "), args[len(args)-1]
	acts, err := drover.Actions()
	if code := agentErr(err); code != 0 {
		return code
	}
	a, ok := findAction(acts, ref)
	if !ok {
		return usageErr(w, false, fmt.Sprintf("no action %q: see shepherd agents actions", ref))
	}
	switch verb {
	case "on", "off":
		if code := agentErr(drover.SetActionOff(a.ID, verb == "off")); code != 0 {
			return code
		}
		say(w, fmt.Sprintf("%s is %s.", a.Name, verb))
		return 0
	case "edit":
		prompt, err := editPrompt(a.Do, stdin)
		if code := agentErr(err); code != 0 {
			return code
		}
		if strings.TrimSpace(prompt) == "" || prompt == a.Do {
			say(w, "Prompt unchanged.")
			return 0
		}
		if code := agentErr(drover.SetActionPrompt(a.ID, prompt)); code != 0 {
			return code
		}
		say(w, fmt.Sprintf("Updated what %s tells the agent.", a.Name))
		return 0
	}
	return usageErr(w, false, fmt.Sprintf("agents action: want on, off or edit, got %q", verb))
}

// findAction matches an action by id, by its name, by its label without the
// source, or by "source label".
func findAction(acts []drover.Action, ref string) (drover.Action, bool) {
	norm := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(s, ":", " ")), " "))
	}
	want := norm(ref)
	var found []drover.Action
	for _, a := range acts {
		label := strings.TrimPrefix(a.Name, a.Source+": ")
		if a.ID == ref || norm(a.Name) == want || norm(label) == want || norm(a.Source+" "+label) == want {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		return drover.Action{}, false
	}
	return found[0], true
}

// editPrompt opens the prompt in $VISUAL or $EDITOR at a terminal; otherwise
// it reads the new prompt from stdin.
func editPrompt(current string, stdin *os.File) (string, error) {
	if !term.IsTerminal(stdin.Fd()) {
		b, err := io.ReadAll(stdin)
		return strings.TrimSpace(string(b)), err
	}
	f, err := os.CreateTemp("", "shepherd-prompt-*.md")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(current + "\n"); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], f.Name())...)
	c.Stdin, c.Stdout, c.Stderr = stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("editor: %w", err)
	}
	b, err := os.ReadFile(f.Name())
	return strings.TrimSpace(string(b)), err
}

// firstLine is s's first line, cut to n characters.
func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len([]rune(s)) > n {
		s = string([]rune(s)[:n-1]) + "…"
	}
	return s
}

// agentsRemove stops tasks coming from a source; a webhook's URL stops working.
func agentsRemove(args []string, w io.Writer) int {
	if len(args) != 1 {
		return usageErr(w, false, "agents remove needs a source name: see shepherd agents sources")
	}
	if code := agentErr(drover.RemoveSource(args[0])); code != 0 {
		return code
	}
	say(w, fmt.Sprintf("Removed %s.", args[0]))
	restartAgents(w)
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
			emit(w, "No Shepherd Pro licence: sending tasks to your coding agent needs one.")
			emit(w, "Get Pro at shepherdcli.dev/pro, then run shepherd pro activate <key>.")
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
