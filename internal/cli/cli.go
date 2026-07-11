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
  shepherd list [--json]        list items with their index
  shepherd add "<text>"         add an item (@category !h/!m/!l due:tomorrow)
  shepherd done <n>             mark item n done
  shepherd undone <n>           mark item n not done
  shepherd rm <n>               remove item n

Indexes are 1-based and match the order shown by 'list'.`

// Run handles one command-API invocation and returns a process exit code.
//
// ponytail: last-writer-wins on the file (load, mutate, save; no lock). Fine
// for a single-user local todo; add locking only if concurrent writers appear.
func Run(verb string, args []string) int {
	switch verb {
	case "list":
		return cmdList(args, os.Stdout)
	case "add":
		return cmdAdd(args, os.Stdout)
	case "done":
		return cmdToggle(args, true, os.Stdout)
	case "undone":
		return cmdToggle(args, false, os.Stdout)
	case "rm":
		return cmdRemove(args, os.Stdout)
	case "help":
		fmt.Println(cliUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "shepherd: unknown command %q\n\n%s\n", verb, cliUsage)
		return 2
	}
}

// itemJSON is the machine-readable view agents read via `list --json`.
type itemJSON struct {
	Index    int    `json:"index"`
	Done     bool   `json:"done"`
	Priority string `json:"priority,omitempty"` // "H"/"M"/"L"
	Text     string `json:"text"`
	Category string `json:"category,omitempty"`
	Created  string `json:"created,omitempty"`
	Due      string `json:"due,omitempty"` // ISO YYYY-MM-DD
	Note     string `json:"note,omitempty"`
}

func toJSON(it todo.Item, idx int) itemJSON {
	j := itemJSON{Index: idx, Done: it.Done, Text: it.Text, Category: it.Category, Created: it.Created, Due: it.Due, Note: it.Note}
	if it.Prio != 0 {
		j.Priority = string(it.Prio)
	}
	return j
}

// emit writes a best-effort line to w. Output to stdout/a buffer has no
// actionable failure mode, so the write error is deliberately discarded.
func emit(w io.Writer, s string) { _, _ = io.WriteString(w, s+"\n") }

func cmdList(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	items := store.Load(store.TodoPath())

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

func cmdAdd(args []string, w io.Writer) int {
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
	path := store.TodoPath()
	items := append(store.Load(path), it)
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(len(items), it))
	return 0
}

func cmdToggle(args []string, done bool, w io.Writer) int {
	path := store.TodoPath()
	items := store.Load(path)
	idx, ok := parseIndex(args, len(items))
	if !ok {
		return 1
	}
	items[idx-1].Done = done
	if err := store.Save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(idx, items[idx-1]))
	return 0
}

func cmdRemove(args []string, w io.Writer) int {
	path := store.TodoPath()
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
	if it.Category != "" {
		fmt.Fprintf(&b, "  @%s", it.Category)
	}
	if it.Due != "" {
		fmt.Fprintf(&b, "  due %s", it.Due)
	}
	return b.String()
}
