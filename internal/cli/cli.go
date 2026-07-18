// Package cli is shepherd's non-interactive command API, used by scripts and
// agentic tools. It reuses store + todo, so the file format has a single owner.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

const cliUsage = `shepherd — todo board

Usage:
  shepherd [board flags]     open the interactive board
  shepherd <command> [args]  run a one-shot command

Read:
  list [--json] [--all] [--filter <t>]     list items (--all aggregates every board)
  projects [--json] [--archived]           list boards, done/total (--archived: archived)
  stats [--json] [--all] [--legend] [--no-color]  board metrics (charts, or --json numbers)
  help                                     print this help

Items (ref = an item's stable id from 'list --json', or its 1-based index n;
       n.m or <id> = a subtask). Agents should address items by id: the index
       shifts as the board reorders, the id never does.
  add "<text>" [--json]        add an item
  sub <ref> "<text>" [--json]  add a subtask
  edit <ref> "<toks>" [--json] merge tokens onto an item (bare key clears)
  done|undone <ref>... [--json] mark one or more (not) done
  rm <ref>... [--dry-run] [--json]  remove one or more (--dry-run/-n previews)

  --json  echo the resulting item(s) as JSON (like 'list --json'), and report
          failures as {"error":…} on stdout instead of text on stderr.
  Mutating verbs are safe to repeat: re-marking a done item keeps its stamp.

  syntax: @category  !h|!m|!l  due:<date>  defer:<date>  link:<url>
          status:<name>  note:<text> (takes the rest of the line)

Boards (the default board can't be renamed/deleted/archived):
  project rename <old> <new>
  project archive <name>            stash under projects/archived/ (reversible)
  project unarchive <name>          restore an archived board
  project delete <name> --force [--dry-run]

Global flags (any command):
  --project <name>  act on a project's board (or set $SHEPHERD_PROJECT)
  -q, --quiet       suppress state-change confirmation lines
  --no-input        never prompt (accepted for script-compat; this API never prompts)
  -h, --help        print a command's flags

Board flags (bare shepherd):
  --filter <text>   start pre-filtered
  --all             read-only view across all boards
  --stats           print stats and exit
  --legend          explain the stats charts and exit
  --version         print the version and exit

Flags follow the verb. Indexes are 1-based, matching 'list' order.
Completing a parent completes its subtasks; the last subtask completes the parent.`

// Usage returns the full command + flag reference, shared by `shepherd help`
// and the flag package's -h/--help handler so the two never diverge.
func Usage() string { return cliUsage }

// knownVerbs is the dispatch table, used both to route and to suggest a
// correction for a mistyped verb. Version reporting is the `--version` board
// flag (the clig standard), handled in main — not a subcommand here.
var knownVerbs = []string{"help", "list", "projects", "project", "stats", "add", "sub", "edit", "done", "undone", "rm"}

// Run handles one command-API invocation and returns a process exit code.
//
// Each mutating verb runs its load→mutate→save under store.WithLock, so
// parallel shepherd processes (e.g. multiple agents) serialise and can't lose
// one another's edits. Reads (list/stats/projects) take no lock: Save's atomic
// rename means a reader always sees a whole file.
func Run(verb string, args []string) int {
	if verb == "help" {
		fmt.Println(cliUsage)
		return 0
	}
	args = extractGlobals(args)
	flagVal, rest, err := extractProject(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 2
	}
	project, err := store.ResolveProject(flagVal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 2
	}
	switch verb {
	case "list":
		return cmdList(rest, project, os.Stdout)
	case "projects":
		return cmdProjects(rest, project, os.Stdout)
	case "project":
		return cmdProject(rest, os.Stdout)
	case "stats":
		return cmdStats(rest, project, os.Stdout)
	case "add":
		return cmdAdd(rest, project, os.Stdout)
	case "sub":
		return cmdSub(rest, project, os.Stdout)
	case "edit":
		return cmdEdit(rest, project, os.Stdout)
	case "done":
		return cmdToggle(rest, project, true, os.Stdout)
	case "undone":
		return cmdToggle(rest, project, false, os.Stdout)
	case "rm":
		return cmdRemove(rest, project, os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "shepherd: unknown command %q\n", verb)
		if s := suggest(verb); s != "" {
			fmt.Fprintf(os.Stderr, "did you mean %q?\n", s)
		}
		fmt.Fprintf(os.Stderr, "\n%s\n", cliUsage)
		return 2
	}
}

// quiet suppresses state-change confirmation lines (clig -q/--quiet). It gates
// only confirmations, never requested data (list/projects/stats output).
//
// a process-lifetime global for a one-shot CLI — safe because each
// invocation is its own process; extractGlobals resets it so repeated in-process
// Run calls (tests) don't leak state.
var quiet bool

// extractGlobals strips the flags that apply to every command and may appear
// before or after the verb: -q/--quiet and --no-input. They must be removed
// here because the per-command FlagSets (ContinueOnError) would reject them.
// --no-input is accepted for script-compat (clig) but is a no-op: the command
// API reads only argv and never prompts.
func extractGlobals(args []string) []string {
	quiet = false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "-q", "--quiet":
			quiet = true
		case "--no-input":
			// no-op: the command API never prompts.
		default:
			rest = append(rest, a)
		}
	}
	return rest
}

// extractDryRun pulls -n/--dry-run out of args (for the destructive verbs that
// support a preview: rm and project delete).
func extractDryRun(args []string) (bool, []string) {
	dry := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "-n" || a == "--dry-run" {
			dry = true
			continue
		}
		rest = append(rest, a)
	}
	return dry, rest
}

// extractJSON pulls --json out of args (the mutating verbs parse it by hand
// rather than through a FlagSet, like extractDryRun). In JSON mode a verb echoes
// the resulting item(s) so an agent needn't re-list to confirm, and reports
// failures as a structured object instead of free text on stderr.
func extractJSON(args []string) (bool, []string) {
	asJSON := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		rest = append(rest, a)
	}
	return asJSON, rest
}

// emitJSON marshals v and writes it to w. Returns exit 0, or 1 if v somehow
// can't be marshalled (never expected for these types).
func emitJSON(w io.Writer, v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, string(b))
	return 0
}

// jsonErr writes a structured error object to stdout — {"error":kind,"detail":…}
// — so agents branch on the outcome without scraping stderr, and returns code.
func jsonErr(w io.Writer, code int, kind, detail string) int {
	_ = emitJSON(w, map[string]string{"error": kind, "detail": detail})
	return code
}

// usageErr and saveErr render an input error (exit 2) / an IO failure (exit 1)
// as a stderr line in text mode or a structured object in JSON mode.
func usageErr(w io.Writer, asJSON bool, msg string) int {
	if asJSON {
		return jsonErr(w, 2, "usage", msg)
	}
	fmt.Fprintln(os.Stderr, "shepherd:", msg)
	return 2
}

func saveErr(w io.Writer, asJSON bool, err error) int {
	if asJSON {
		return jsonErr(w, 1, "io", err.Error())
	}
	fmt.Fprintln(os.Stderr, "shepherd:", err)
	return 1
}

// refErr reports an unresolvable item ref: a usage error when the ref was
// missing entirely (bad==""), else not_found naming the offending token.
func refErr(w io.Writer, asJSON bool, bad string, n int) int {
	if bad == "" {
		return usageErr(w, asJSON, "need an item id or number")
	}
	if asJSON {
		return jsonErr(w, 2, "not_found", bad)
	}
	fmt.Fprintf(os.Stderr, "shepherd: no item %q (have %d items)\n", bad, n)
	return 2
}

// suggest returns the closest known verb to a mistyped one (edit distance < 3),
// or "" when nothing is close enough to be worth suggesting.
func suggest(verb string) string {
	best, bestD := "", 3
	for _, v := range knownVerbs {
		if d := levenshtein(verb, v); d < bestD {
			best, bestD = v, d
		}
	}
	return best
}

// levenshtein is the classic edit distance over two short verb strings.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(b)]
}

// extractProject pulls a --project <name> / --project=<name> flag out of args
// (flags follow the verb), returning its value and the remaining args. This
// runs before the per-command FlagSets, which would reject it as unknown, and
// before `add` joins its args into text. Last occurrence wins.
func extractProject(args []string) (string, []string, error) {
	project := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--project":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--project needs a name")
			}
			project = args[i+1]
			i++
		case strings.HasPrefix(a, "--project="):
			project = strings.TrimPrefix(a, "--project=")
		default:
			rest = append(rest, a)
		}
	}
	return project, rest, nil
}

// itemJSON is the machine-readable view agents read via `list --json`.
type itemJSON struct {
	ID        string     `json:"id,omitempty"` // stable opaque id; use it (not index) to address the item
	Index     int        `json:"index"`
	Done      bool       `json:"done"`
	Status    string     `json:"status,omitempty"`   // named non-terminal status; empty when open or done
	Priority  string     `json:"priority,omitempty"` // "H"/"M"/"L"
	Text      string     `json:"text"`
	Category  string     `json:"category,omitempty"`
	Created   string     `json:"created,omitempty"`
	Completed string     `json:"completed,omitempty"`
	Defer     string     `json:"defer,omitempty"` // ISO YYYY-MM-DD
	Due       string     `json:"due,omitempty"`   // ISO YYYY-MM-DD
	Link      string     `json:"link,omitempty"`
	Note      string     `json:"note,omitempty"`
	Project   string     `json:"project,omitempty"`  // board name, only in --all
	Subtasks  []itemJSON `json:"subtasks,omitempty"` // Index is the 1-based position under the parent (n.m)
}

func toJSON(it todo.Item, idx int) itemJSON {
	j := itemJSON{ID: it.ID, Index: idx, Done: it.Done, Status: it.Status, Text: it.Text, Category: it.Category, Created: it.Created, Completed: it.Completed, Defer: it.Defer, Due: it.Due, Link: it.Link, Note: it.Note, Project: it.Source}
	if it.Prio != 0 {
		j.Priority = string(it.Prio)
	}
	for si, sub := range it.Subs {
		j.Subtasks = append(j.Subtasks, toJSON(sub, si+1))
	}
	return j
}

// emit writes a best-effort line to w. Output to stdout/a buffer has no
// actionable failure mode, so the write error is deliberately discarded.
func emit(w io.Writer, s string) { _, _ = io.WriteString(w, s+"\n") }

// parseExit maps a FlagSet.Parse error to an exit code: 0 when the user asked
// for help (-h/--help printed the flags), 2 for any real parse error.
func parseExit(err error) int {
	if err == flag.ErrHelp {
		return 0
	}
	return 2
}

// say writes a state-change confirmation, suppressed by -q/--quiet. Requested
// data (list/projects/stats output) uses emit and is never suppressed.
func say(w io.Writer, s string) {
	if quiet {
		return
	}
	emit(w, s)
}

func cmdList(args []string, project string, w io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	all := fs.Bool("all", false, "aggregate items across every board (read-only)")
	filter := fs.String("filter", "", "only items matching this text (text/note/category/due/defer/link)")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	var items []todo.Item
	if *all {
		items = store.LoadAll()
	} else {
		items = store.Load(store.TodoPathFor(project))
	}
	// Match after loading so the printed index stays the item's real position
	// on the board — the one done/rm expect. q is lowercased for todo.Match.
	q := strings.ToLower(strings.TrimSpace(*filter))

	if *asJSON {
		out := make([]itemJSON, 0, len(items))
		for i, it := range items {
			if !todo.Match(it, q) {
				continue
			}
			out = append(out, toJSON(it, i+1))
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		emit(w, string(b))
		return 0
	}

	if len(items) == 0 {
		emit(w, "(no items)")
		return 0
	}
	shown := 0
	for i, it := range items {
		if !todo.Match(it, q) {
			continue
		}
		shown++
		emit(w, formatLine(i+1, it))
		for si, sub := range it.Subs {
			emit(w, formatSub(i+1, si+1, sub))
		}
	}
	if shown == 0 {
		emit(w, "(no matches)")
	}
	return 0
}

// cmdProjects lists every board with its open/total counts, marking the
// effective project (--project / $SHEPHERD_PROJECT, else default).
func cmdProjects(args []string, project string, w io.Writer) int {
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	archived := fs.Bool("archived", false, "list archived boards instead")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	cur := project
	if cur == "" {
		cur = "default"
	}
	boards := store.Boards()
	if *archived {
		boards = store.ArchivedBoards()
		cur = "" // nothing is "current" among archived boards
	}
	if *asJSON {
		type row struct {
			Name    string `json:"name"`
			Open    int    `json:"open"`
			Total   int    `json:"total"`
			Current bool   `json:"current"`
		}
		out := make([]row, 0, len(boards))
		for _, b := range boards {
			open, total := store.BoardCounts(b.Path)
			out = append(out, row{b.Name, open, total, b.Name == cur})
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		emit(w, string(b))
		return 0
	}
	if len(boards) == 0 {
		if *archived {
			emit(w, "(no archived boards)")
		} else {
			emit(w, "(no boards)")
		}
		return 0
	}
	for _, b := range boards {
		open, total := store.BoardCounts(b.Path)
		mark := " "
		if b.Name == cur {
			mark = "*"
		}
		emit(w, fmt.Sprintf("%s %s\t%d/%d", mark, b.Name, total-open, total))
	}
	return 0
}

// cmdProject groups whole-board actions: rename, delete, archive, unarchive.
// Board names are explicit positional args (not the --project flag); the default
// board cannot be renamed, deleted, or archived.
func cmdProject(args []string, w io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "shepherd: project needs a subcommand: rename|delete|archive|unarchive")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "rename":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, `shepherd: project rename <old> <new>`)
			return 2
		}
		if err := store.RenameBoard(rest[0], rest[1]); err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		say(w, fmt.Sprintf("renamed board %q -> %q", rest[0], rest[1]))
		return 0
	case "delete":
		dry, rest := extractDryRun(rest)
		force := false
		name := ""
		for _, a := range rest {
			if a == "--force" {
				force = true
			} else if name == "" {
				name = a
			}
		}
		if name == "" {
			fmt.Fprintln(os.Stderr, `shepherd: project delete <name> --force`)
			return 2
		}
		if dry {
			emit(w, fmt.Sprintf("would delete board %q", name))
			return 0
		}
		if !force {
			fmt.Fprintf(os.Stderr, "shepherd: refusing to delete %q without --force\n", name)
			return 2
		}
		if err := store.DeleteBoard(name); err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		say(w, fmt.Sprintf("deleted board %q", name))
		return 0
	case "archive":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, `shepherd: project archive <name>`)
			return 2
		}
		if err := store.ArchiveBoard(rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		say(w, fmt.Sprintf("archived board %q", rest[0]))
		return 0
	case "unarchive":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, `shepherd: project unarchive <name>`)
			return 2
		}
		if err := store.UnarchiveBoard(rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		say(w, fmt.Sprintf("unarchived board %q", rest[0]))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "shepherd: unknown project subcommand %q (rename|delete|archive|unarchive)\n", sub)
		return 2
	}
}

// The mutating verbs run their whole load→mutate→save under store.WithLock, so
// concurrent shepherd processes (parallel agents) serialise and never lose one
// another's edits. Each captures its exit code from inside the locked closure;
// a returned error is an IO/lock failure (exit 1 via saveErr).

func cmdAdd(args []string, project string, w io.Writer) int {
	asJSON, args := extractJSON(args)
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return usageErr(w, asJSON, `add needs text, e.g. shepherd add "buy milk @home !h"`)
	}
	it := todo.ParseQuickAdd(text)
	if it.Text == "" {
		return usageErr(w, asJSON, "nothing to add after parsing tokens")
	}
	path := store.TodoPathFor(project)
	exit := 0
	err := store.WithLock(path, func() error {
		items := append(store.Load(path), it)
		if err := store.Save(path, items); err != nil { // Save backfills the new item's id
			return err
		}
		idx := len(items)
		if asJSON {
			exit = emitJSON(w, toJSON(items[idx-1], idx))
		} else {
			say(w, formatLine(idx, items[idx-1]))
		}
		return nil
	})
	if err != nil {
		return saveErr(w, asJSON, err)
	}
	return exit
}

// cmdSub adds a subtask to item <ref>: shepherd sub <ref> "<text>". The text
// takes the same quick-add tokens as `add`. Adding an open subtask reopens the
// parent (it's no longer all-done). <ref> must resolve to a top-level item.
func cmdSub(args []string, project string, w io.Writer) int {
	asJSON, args := extractJSON(args)
	path := store.TodoPathFor(project)
	exit := 0
	err := store.WithLock(path, func() error {
		items := store.Load(path)
		if len(args) == 0 {
			exit = usageErr(w, asJSON, `sub needs an item and text, e.g. shepherd sub 1 "parse tokens"`)
			return nil
		}
		p, s, ok := resolveRef(args[0], items)
		if !ok || s != 0 {
			exit = refErr(w, asJSON, args[0], len(items))
			return nil
		}
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			exit = usageErr(w, asJSON, `sub needs text, e.g. shepherd sub 1 "parse tokens"`)
			return nil
		}
		sub := todo.ParseQuickAdd(text)
		if sub.Text == "" {
			exit = usageErr(w, asJSON, "nothing to add after parsing tokens")
			return nil
		}
		parent := &items[p-1]
		parent.Subs = append(parent.Subs, sub)
		todo.SetDone(parent, todo.AllSubsDone(parent))
		if err := store.Save(path, items); err != nil {
			return err
		}
		if asJSON {
			exit = emitJSON(w, toJSON(items[p-1], p))
		} else {
			say(w, formatSub(p, len(parent.Subs), parent.Subs[len(parent.Subs)-1]))
		}
		return nil
	})
	if err != nil {
		return saveErr(w, asJSON, err)
	}
	return exit
}

// cmdEdit merges quick-add tokens onto an existing item <ref> (or subtask):
// shepherd edit <ref> "<tokens>". Only the fields present in the tokens change;
// a bare key token clears its field, note: takes the rest of the line, and text
// is replaced only when plain words are given (see todo.ApplyEdit).
func cmdEdit(args []string, project string, w io.Writer) int {
	asJSON, args := extractJSON(args)
	path := store.TodoPathFor(project)
	exit := 0
	err := store.WithLock(path, func() error {
		items := store.Load(path)
		if len(args) == 0 {
			exit = usageErr(w, asJSON, `edit needs an item and tokens, e.g. shepherd edit 2 "@home !h due:tomorrow"`)
			return nil
		}
		p, s, ok := resolveRef(args[0], items)
		if !ok {
			exit = refErr(w, asJSON, args[0], len(items))
			return nil
		}
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			exit = usageErr(w, asJSON, `edit needs tokens, e.g. shepherd edit 2 "@home !h due:tomorrow"`)
			return nil
		}
		if s == 0 {
			todo.ApplyEdit(&items[p-1], text)
		} else {
			todo.ApplyEdit(&items[p-1].Subs[s-1], text)
			// editing a sub's status/done can complete or reopen the parent, same
			// as done/undone — ApplyEdit works on one Item, so reconcile here.
			todo.SetDone(&items[p-1], todo.AllSubsDone(&items[p-1]))
		}
		if err := store.Save(path, items); err != nil {
			return err
		}
		if asJSON {
			exit = emitJSON(w, toJSON(items[p-1], p))
		} else if s == 0 {
			say(w, formatLine(p, items[p-1]))
		} else {
			say(w, formatSub(p, s, items[p-1].Subs[s-1]))
		}
		return nil
	})
	if err != nil {
		return saveErr(w, asJSON, err)
	}
	return exit
}

// cmdToggle marks one or more items (not) done: shepherd done|undone <ref>...
// Multiple refs are a single atomic write — agents mark a burst of subtasks done
// in one call. Marking done never reorders the in-memory slice, so every ref
// stays valid through the loop.
func cmdToggle(args []string, project string, done bool, w io.Writer) int {
	asJSON, args := extractJSON(args)
	path := store.TodoPathFor(project)
	exit := 0
	err := store.WithLock(path, func() error {
		items := store.Load(path)
		refs, bad, ok := parseRefs(args, items)
		if !ok {
			exit = refErr(w, asJSON, bad, len(items))
			return nil
		}
		for _, r := range refs {
			if r[1] == 0 {
				todo.SetParentDone(&items[r[0]-1], done)
			} else {
				todo.SetSubDone(&items[r[0]-1], r[1]-1, done)
			}
		}
		if err := store.Save(path, items); err != nil {
			return err
		}
		if asJSON {
			exit = emitJSON(w, affectedJSON(items, refs))
			return nil
		}
		for _, r := range refs {
			say(w, formatLine(r[0], items[r[0]-1]))
			if r[1] > 0 {
				say(w, formatSub(r[0], r[1], items[r[0]-1].Subs[r[1]-1]))
			}
		}
		return nil
	})
	if err != nil {
		return saveErr(w, asJSON, err)
	}
	return exit
}

// cmdRemove deletes one or more items/subtasks: shepherd rm <ref>... [--dry-run].
// Refs are resolved to an identity set before any deletion, so a multi-remove is
// order- and index-shift-safe (removing item 2 never renumbers item 3 mid-op).
func cmdRemove(args []string, project string, w io.Writer) int {
	asJSON, args := extractJSON(args)
	dry, args := extractDryRun(args)
	path := store.TodoPathFor(project)
	exit := 0
	err := store.WithLock(path, func() error {
		items := store.Load(path)
		refs, bad, ok := parseRefs(args, items)
		if !ok {
			exit = refErr(w, asJSON, bad, len(items))
			return nil
		}
		rmParent := map[int]bool{}      // parent index -> whole item removed
		rmSub := map[int]map[int]bool{} // parent index -> sub indices removed
		var removed []string
		for _, r := range refs {
			p, s := r[0], r[1]
			if s == 0 {
				rmParent[p] = true
				removed = append(removed, items[p-1].Text)
			} else {
				if rmSub[p] == nil {
					rmSub[p] = map[int]bool{}
				}
				rmSub[p][s] = true
				removed = append(removed, items[p-1].Subs[s-1].Text)
			}
		}
		if dry {
			if asJSON {
				exit = emitJSON(w, map[string]any{"dry_run": true, "removed": removed})
			} else {
				for _, t := range removed {
					emit(w, fmt.Sprintf("would remove %q", t))
				}
			}
			return nil
		}
		kept := make([]todo.Item, 0, len(items))
		for i := range items {
			p := i + 1
			if rmParent[p] {
				continue
			}
			if subs := rmSub[p]; subs != nil {
				nsub := make([]todo.Item, 0, len(items[i].Subs))
				for j := range items[i].Subs {
					if !subs[j+1] {
						nsub = append(nsub, items[i].Subs[j])
					}
				}
				items[i].Subs = nsub
				// dropping a sub can complete the parent (all remaining subs done).
				if len(items[i].Subs) > 0 {
					todo.SetDone(&items[i], todo.AllSubsDone(&items[i]))
				}
			}
			kept = append(kept, items[i])
		}
		if err := store.Save(path, kept); err != nil {
			return err
		}
		if asJSON {
			exit = emitJSON(w, map[string]any{"removed": removed})
		} else {
			for _, t := range removed {
				say(w, fmt.Sprintf("removed %q", t))
			}
		}
		return nil
	})
	if err != nil {
		return saveErr(w, asJSON, err)
	}
	return exit
}

// resolveRef resolves one item ref against items — a stable id, a "n" index, or
// a "n.m" subtask — returning (parent, sub, ok) with sub==0 for a top-level
// item. Ids are tried first, but an index like "3"/"3.1" is never a 32-char hex
// id, so the two forms never collide. Pure: it prints nothing.
func resolveRef(tok string, items []todo.Item) (int, int, bool) {
	if p, s, ok := resolveID(items, tok); ok {
		return p, s, true
	}
	pStr, sStr, dotted := strings.Cut(tok, ".")
	p, err := strconv.Atoi(pStr)
	if err != nil || p < 1 || p > len(items) {
		return 0, 0, false
	}
	if !dotted {
		return p, 0, true
	}
	s, err := strconv.Atoi(sStr)
	if err != nil || s < 1 || s > len(items[p-1].Subs) {
		return 0, 0, false
	}
	return p, s, true
}

// parseRefs resolves one or more item refs against items. On the first
// unresolvable token it returns that token with ok=false (bad=="" means no ref
// was given at all); the caller renders the error via refErr.
func parseRefs(args []string, items []todo.Item) ([][2]int, string, bool) {
	if len(args) == 0 {
		return nil, "", false
	}
	refs := make([][2]int, 0, len(args))
	for _, tok := range args {
		p, s, ok := resolveRef(tok, items)
		if !ok {
			return nil, tok, false
		}
		refs = append(refs, [2]int{p, s})
	}
	return refs, "", true
}

// affectedJSON renders the distinct top-level items touched by refs (in first-
// seen order), each including its subtasks — the machine echo for a mutation.
func affectedJSON(items []todo.Item, refs [][2]int) []itemJSON {
	seen := map[int]bool{}
	out := make([]itemJSON, 0, len(refs))
	for _, r := range refs {
		if seen[r[0]] {
			continue
		}
		seen[r[0]] = true
		out = append(out, toJSON(items[r[0]-1], r[0]))
	}
	return out
}

// resolveID looks an item or subtask up by its stable id, returning (parent,
// sub, ok) with sub==0 for a top-level item — the same shape as resolveRef.
func resolveID(items []todo.Item, id string) (int, int, bool) {
	for i := range items {
		if items[i].ID == id {
			return i + 1, 0, true
		}
		for j := range items[i].Subs {
			if items[i].Subs[j].ID == id {
				return i + 1, j + 1, true
			}
		}
	}
	return 0, 0, false
}

// formatLine renders one item for the plain-text list/add/done output.
func formatLine(idx int, it todo.Item) string {
	box := " "
	if it.Done {
		box = "x"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d\t[%s]", idx, box)
	if it.Prio != 0 {
		fmt.Fprintf(&b, " (%c)", it.Prio)
	}
	fmt.Fprintf(&b, " %s", it.Text)
	if !it.Done && it.Status != "" {
		fmt.Fprintf(&b, "  ~%s", it.Status)
	}
	if it.Source != "" {
		fmt.Fprintf(&b, "  [%s]", it.Source)
	}
	if it.Category != "" {
		fmt.Fprintf(&b, "  @%s", it.Category)
	}
	if it.Due != "" {
		fmt.Fprintf(&b, "  due %s", it.Due)
	}
	if d, total := todo.SubCount(it); total > 0 {
		fmt.Fprintf(&b, "  %d/%d", d, total)
	}
	return b.String()
}

// formatSub renders a subtask as an indented, dotted-index line under its parent.
func formatSub(parent, sub int, it todo.Item) string {
	box := " "
	if it.Done {
		box = "x"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %d.%d\t[%s]", parent, sub, box)
	if it.Prio != 0 {
		fmt.Fprintf(&b, " (%c)", it.Prio)
	}
	fmt.Fprintf(&b, " %s", it.Text)
	if !it.Done && it.Status != "" {
		fmt.Fprintf(&b, "  ~%s", it.Status)
	}
	if it.Due != "" {
		fmt.Fprintf(&b, "  due %s", it.Due)
	}
	return b.String()
}
