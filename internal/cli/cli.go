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
  list [--json] [--all] [--filter <t>]  list items (--all aggregates every board)
  projects [--json] [--archived]        list boards, done/total (--archived: archived)
  stats [--json] [--all] [--legend]     board metrics (charts, or --json numbers)

Items (n = item index from 'list'; n.m = its subtask m):
  add "<text>"           add an item
  sub <n> "<text>"       add a subtask
  edit <n[.m]> "<toks>"  merge tokens onto an item (bare key clears)
  done|undone <n[.m]>    mark (not) done
  rm <n[.m]>             remove

  tokens: @category  !h|!m|!l  due:<date>  defer:<date>  link:<url>
          status:<name>  note:<text> (takes the rest of the line)

Boards (the default board can't be renamed/deleted/archived):
  project rename <old> <new>
  project archive <name>        stash under projects/archived/ (reversible)
  project unarchive <name>      restore an archived board
  project delete <name> --force

Board flags (bare shepherd):
  --project <name>  open a project's board (or set $SHEPHERD_PROJECT)
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

// Run handles one command-API invocation and returns a process exit code.
//
// last-writer-wins on the file (load, mutate, save; no lock). Fine
// for a single-user local todo; add locking only if concurrent writers appear.
func Run(verb string, args []string) int {
	if verb == "help" {
		fmt.Println(cliUsage)
		return 0
	}
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
		fmt.Fprintf(os.Stderr, "shepherd: unknown command %q\n\n%s\n", verb, cliUsage)
		return 2
	}
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
	j := itemJSON{Index: idx, Done: it.Done, Status: it.Status, Text: it.Text, Category: it.Category, Created: it.Created, Completed: it.Completed, Defer: it.Defer, Due: it.Due, Link: it.Link, Note: it.Note, Project: it.Source}
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

func cmdList(args []string, project string, w io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	all := fs.Bool("all", false, "aggregate items across every board (read-only)")
	filter := fs.String("filter", "", "only items matching this text (text/note/category/due/defer/link)")
	if err := fs.Parse(args); err != nil {
		return 2
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
		return 2
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
		emit(w, fmt.Sprintf("renamed board %q -> %q", rest[0], rest[1]))
		return 0
	case "delete":
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
		if !force {
			fmt.Fprintf(os.Stderr, "shepherd: refusing to delete %q without --force\n", name)
			return 2
		}
		if err := store.DeleteBoard(name); err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		emit(w, fmt.Sprintf("deleted board %q", name))
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
		emit(w, fmt.Sprintf("archived board %q", rest[0]))
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
		emit(w, fmt.Sprintf("unarchived board %q", rest[0]))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "shepherd: unknown project subcommand %q (rename|delete|archive|unarchive)\n", sub)
		return 2
	}
}

func cmdAdd(args []string, project string, w io.Writer) int {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		fmt.Fprintln(os.Stderr, `shepherd: add needs text, e.g. shepherd add "buy milk @home !h"`)
		return 2
	}
	it := todo.ParseQuickAdd(text)
	if it.Text == "" {
		fmt.Fprintln(os.Stderr, "shepherd: nothing to add after parsing tokens")
		return 2
	}
	path := store.TodoPathFor(project)
	items := append(store.Load(path), it)
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(len(items), it))
	return 0
}

// cmdSub adds a subtask to item n: shepherd sub <n> "<text>". The text takes
// the same quick-add tokens as `add`. Adding an open subtask reopens the parent
// (it's no longer all-done).
func cmdSub(args []string, project string, w io.Writer) int {
	path := store.TodoPathFor(project)
	items := store.Load(path)
	idx, ok := parseIndex(args, len(items))
	if !ok {
		return 1
	}
	text := strings.TrimSpace(strings.Join(args[1:], " "))
	if text == "" {
		fmt.Fprintln(os.Stderr, `shepherd: sub needs text, e.g. shepherd sub 1 "parse tokens"`)
		return 2
	}
	sub := todo.ParseQuickAdd(text)
	if sub.Text == "" {
		fmt.Fprintln(os.Stderr, "shepherd: nothing to add after parsing tokens")
		return 2
	}
	parent := &items[idx-1]
	parent.Subs = append(parent.Subs, sub)
	todo.SetDone(parent, todo.AllSubsDone(parent))
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatSub(idx, len(parent.Subs), sub))
	return 0
}

// cmdEdit merges quick-add tokens onto an existing item n (or subtask m):
// shepherd edit <n[.m]> "<tokens>". Only the fields present in the tokens
// change; a bare key token clears its field, note: takes the rest of the line,
// and text is replaced only when plain words are given (see todo.ApplyEdit).
func cmdEdit(args []string, project string, w io.Writer) int {
	path := store.TodoPathFor(project)
	items := store.Load(path)
	p, s, ok := parseRef(args, items)
	if !ok {
		return 1
	}
	text := strings.TrimSpace(strings.Join(args[1:], " "))
	if text == "" {
		fmt.Fprintln(os.Stderr, `shepherd: edit needs tokens, e.g. shepherd edit 2 "@home !h due:tomorrow"`)
		return 2
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
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	if s == 0 {
		emit(w, formatLine(p, items[p-1]))
	} else {
		emit(w, formatSub(p, s, items[p-1].Subs[s-1]))
	}
	return 0
}

func cmdToggle(args []string, project string, done bool, w io.Writer) int {
	path := store.TodoPathFor(project)
	items := store.Load(path)
	p, s, ok := parseRef(args, items)
	if !ok {
		return 1
	}
	if s == 0 {
		todo.SetParentDone(&items[p-1], done)
	} else {
		todo.SetSubDone(&items[p-1], s-1, done)
	}
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(p, items[p-1]))
	if s > 0 {
		emit(w, formatSub(p, s, items[p-1].Subs[s-1]))
	}
	return 0
}

func cmdRemove(args []string, project string, w io.Writer) int {
	path := store.TodoPathFor(project)
	items := store.Load(path)
	p, s, ok := parseRef(args, items)
	if !ok {
		return 1
	}
	var removed string
	if s == 0 {
		removed = items[p-1].Text
		items = append(items[:p-1], items[p:]...)
	} else {
		parent := &items[p-1]
		removed = parent.Subs[s-1].Text
		parent.Subs = append(parent.Subs[:s-1], parent.Subs[s:]...)
		// dropping a sub can complete the parent (all remaining subs done).
		if len(parent.Subs) > 0 {
			todo.SetDone(parent, todo.AllSubsDone(parent))
		}
	}
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, fmt.Sprintf("removed %q", removed))
	return 0
}

// parseRef reads a 1-based item ref from args: "n" for a parent, or "n.m" for
// subtask m of item n. Returns (parent, sub, ok) with sub==0 meaning the parent
// itself. Prints the reason to stderr and returns ok=false on any problem.
func parseRef(args []string, items []todo.Item) (int, int, bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "shepherd: need an item number")
		return 0, 0, false
	}
	tok := args[0]
	pStr, sStr, dotted := strings.Cut(tok, ".")
	p, err := strconv.Atoi(pStr)
	if err != nil || p < 1 || p > len(items) {
		fmt.Fprintf(os.Stderr, "shepherd: invalid item number %q (have %d items)\n", tok, len(items))
		return 0, 0, false
	}
	if !dotted {
		return p, 0, true
	}
	subs := len(items[p-1].Subs)
	s, err := strconv.Atoi(sStr)
	if err != nil || s < 1 || s > subs {
		fmt.Fprintf(os.Stderr, "shepherd: invalid subtask number %q (item %d has %d subtasks)\n", tok, p, subs)
		return 0, 0, false
	}
	return p, s, true
}

// parseIndex reads a 1-based item number from args and bounds-checks it against
// n items. Prints the reason to stderr and returns ok=false on any problem.
func parseIndex(args []string, n int) (int, bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "shepherd: need an item number")
		return 0, false
	}
	idx, err := strconv.Atoi(args[0])
	if err != nil || idx < 1 || idx > n {
		fmt.Fprintf(os.Stderr, "shepherd: invalid item number %q (have %d items)\n", args[0], n)
		return 0, false
	}
	return idx, true
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
