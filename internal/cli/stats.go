package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"shepherd/internal/store"
	"shepherd/internal/todo"

	"github.com/charmbracelet/x/term"
)

// cmdStats summarises a board (or all boards with --all) as charts, or the raw
// numbers with --json. Done-based counts include the archive.
func cmdStats(args []string, project string, w io.Writer) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output (no charts)")
	all := fs.Bool("all", false, "aggregate across every board")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var items []todo.Item
	title := "shepherd stats"
	if *all {
		items = append(store.LoadAll(), store.LoadAllArchives()...)
		title += " · all boards"
	} else {
		path := store.TodoPathFor(project)
		items = append(store.Load(path), store.LoadArchive(path)...)
		if project != "" {
			title += " · " + project
		}
	}

	s := todo.Compute(items)

	if *asJSON {
		b, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "shepherd:", err)
			return 1
		}
		emit(w, string(b))
		return 0
	}

	emit(w, renderStats(s, title, termWidth()))
	return 0
}

// termWidth reports the terminal width, or 80 when stdout isn't a tty.
func termWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 80
}
