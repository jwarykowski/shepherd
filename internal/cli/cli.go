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
  shepherd                      open the interactive board
  shepherd list [--json] [--all] list items (--all aggregates every board)
  shepherd stats [--json] [--all] show board metrics as charts (--all aggregates)
  shepherd add "<text>"         add an item (@category !h/!m/!l due: defer: link:)
  shepherd done <n>             mark item n done
  shepherd undone <n>           mark item n not done
  shepherd rm <n>               remove item n

Flags go after the verb. --project <name> (or $SHEPHERD_PROJECT) selects a
project board (else the default): e.g. shepherd list --project web.

Board flags (bare shepherd, the interactive board):
  --project <name>  open a project's board
  --filter <text>   start pre-filtered (text/note/category/due)
  --all             read-only global view across all boards
  --stats           print board stats and exit
  --version         print the version and exit

Indexes are 1-based and match the order shown by 'list'.`

// Usage returns the full command + flag reference, shared by `shepherd help`
// and the flag package's -h/--help handler so the two never diverge.
func Usage() string { return cliUsage }

// Run handles one command-API invocation and returns a process exit code.
//
// ponytail: last-writer-wins on the file (load, mutate, save; no lock). Fine
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
	case "stats":
		return cmdStats(rest, project, os.Stdout)
	case "add":
		return cmdAdd(rest, project, os.Stdout)
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
	Index     int    `json:"index"`
	Done      bool   `json:"done"`
	Priority  string `json:"priority,omitempty"` // "H"/"M"/"L"
	Text      string `json:"text"`
	Category  string `json:"category,omitempty"`
	Created   string `json:"created,omitempty"`
	Completed string `json:"completed,omitempty"`
	Defer     string `json:"defer,omitempty"` // ISO YYYY-MM-DD
	Due       string `json:"due,omitempty"`   // ISO YYYY-MM-DD
	Link      string `json:"link,omitempty"`
	Note      string `json:"note,omitempty"`
	Project   string `json:"project,omitempty"` // board name, only in --all
}

func toJSON(it todo.Item, idx int) itemJSON {
	j := itemJSON{Index: idx, Done: it.Done, Text: it.Text, Category: it.Category, Created: it.Created, Completed: it.Completed, Defer: it.Defer, Due: it.Due, Link: it.Link, Note: it.Note, Project: it.Source}
	if it.Prio != 0 {
		j.Priority = string(it.Prio)
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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var items []todo.Item
	if *all {
		items = store.LoadAll()
	} else {
		items = store.Load(store.TodoPathFor(project))
	}

	if *asJSON {
		out := make([]itemJSON, len(items))
		for i, it := range items {
			out[i] = toJSON(it, i+1)
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
	for i, it := range items {
		emit(w, formatLine(i+1, it))
	}
	return 0
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

func cmdToggle(args []string, project string, done bool, w io.Writer) int {
	path := store.TodoPathFor(project)
	items := store.Load(path)
	idx, ok := parseIndex(args, len(items))
	if !ok {
		return 1
	}
	todo.SetDone(&items[idx-1], done)
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(idx, items[idx-1]))
	return 0
}

func cmdRemove(args []string, project string, w io.Writer) int {
	path := store.TodoPathFor(project)
	items := store.Load(path)
	idx, ok := parseIndex(args, len(items))
	if !ok {
		return 1
	}
	removed := items[idx-1]
	items = append(items[:idx-1], items[idx:]...)
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, fmt.Sprintf("removed %q", removed.Text))
	return 0
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
	if it.Source != "" {
		fmt.Fprintf(&b, "  [%s]", it.Source)
	}
	if it.Category != "" {
		fmt.Fprintf(&b, "  @%s", it.Category)
	}
	if it.Due != "" {
		fmt.Fprintf(&b, "  due %s", it.Due)
	}
	return b.String()
}
