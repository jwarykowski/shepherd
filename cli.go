package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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

// runCLI handles the non-interactive command API used by scripts and agentic
// tools. It reuses the same load/save/parseQuickAdd the TUI uses, so the file
// format has a single owner. Returns a process exit code.
//
// ponytail: last-writer-wins on the file (load, mutate, save; no lock). Fine
// for a single-user local todo; add locking only if concurrent writers appear.
func runCLI(verb string, args []string) int {
	switch verb {
	case "list", "ls":
		return cmdList(args, os.Stdout)
	case "add":
		return cmdAdd(args, os.Stdout)
	case "done":
		return cmdToggle(args, true, os.Stdout)
	case "undone":
		return cmdToggle(args, false, os.Stdout)
	case "rm", "remove":
		return cmdRemove(args, os.Stdout)
	case "help":
		fmt.Println(cliUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "shepherd: unknown command %q\n\n%s\n", verb, cliUsage)
		return 2
	}
}

// itemJSON is the machine-readable view agents read via `list --json`
// (item's own fields are unexported).
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

// emit writes a best-effort line to w. Output to stdout/a buffer has no
// actionable failure mode, so the write error is deliberately discarded.
func emit(w io.Writer, s string) { _, _ = io.WriteString(w, s+"\n") }

func toJSON(it item, idx int) itemJSON {
	j := itemJSON{Index: idx, Done: it.done, Text: it.text, Category: it.category, Created: it.created, Due: it.due, Note: it.note}
	if it.prio != 0 {
		j.Priority = string(it.prio)
	}
	return j
}

func cmdList(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	items := load(todoPath())

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
	it := parseQuickAdd(text)
	if it.text == "" {
		fmt.Fprintln(os.Stderr, "shepherd: nothing to add after parsing tokens")
		return 2
	}
	path := todoPath()
	items := append(load(path), it)
	if err := save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(len(items), it))
	return 0
}

func cmdToggle(args []string, done bool, w io.Writer) int {
	path := todoPath()
	items := load(path)
	idx, ok := parseIndex(args, len(items))
	if !ok {
		return 1
	}
	items[idx-1].done = done
	if err := save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, formatLine(idx, items[idx-1]))
	return 0
}

func cmdRemove(args []string, w io.Writer) int {
	path := todoPath()
	items := load(path)
	idx, ok := parseIndex(args, len(items))
	if !ok {
		return 1
	}
	removed := items[idx-1]
	items = append(items[:idx-1], items[idx:]...)
	if err := save(path, items); err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	emit(w, fmt.Sprintf("removed %q", removed.text))
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

// formatLine renders one item for the plain-text `list`/`add`/`done` output.
func formatLine(idx int, it item) string {
	box := " "
	if it.done {
		box = "x"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d\t[%s]", idx, box)
	if it.prio != 0 {
		fmt.Fprintf(&b, " (%c)", it.prio)
	}
	fmt.Fprintf(&b, " %s", it.text)
	if it.category != "" {
		fmt.Fprintf(&b, "  @%s", it.category)
	}
	if it.due != "" {
		fmt.Fprintf(&b, "  due %s", it.due)
	}
	return b.String()
}
