package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"shepherd/internal/store"
	"shepherd/internal/todo"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"
)

// cmdStats summarises a board (or all boards with --all) as charts, or the raw
// numbers with --json. Done-based counts include the archive.
func cmdStats(args []string, project string, w io.Writer) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output (no charts)")
	all := fs.Bool("all", false, "aggregate across every board")
	legend := fs.Bool("legend", false, "explain each chart and the backlog-health numbers")
	noColor := fs.Bool("no-color", false, "disable ANSI color in the charts")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if colorDisabled(*noColor) {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	if *legend {
		emit(w, statsLegend())
		return 0
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
	orderByConfig(&s, store.ConfigStatusOrder())

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

// colorDisabled decides whether to strip ANSI color from the charts, honoring
// (clig): an explicit --no-color flag, $NO_COLOR (any value), $TERM=dumb, or a
// non-tty stdout. lipgloss/termenv already auto-detect the last three, but we
// gate them explicitly so shepherd owns the behavior rather than inheriting it.
func colorDisabled(flagSet bool) bool {
	if flagSet {
		return true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return true
	}
	return !term.IsTerminal(os.Stdout.Fd())
}

// orderByConfig re-sorts s.ByStatus to follow the user's configured status
// order (from config.toml). Configured statuses come first in declared order;
// any remaining status keeps its count-descending fallback after them. No
// config → leave Compute's count order untouched.
func orderByConfig(s *todo.Stats, order []string) {
	if len(order) == 0 {
		return
	}
	rank := make(map[string]int, len(order))
	for i, name := range order {
		rank[name] = i
	}
	sort.SliceStable(s.ByStatus, func(i, j int) bool {
		ri, iok := rank[s.ByStatus[i].Name]
		rj, jok := rank[s.ByStatus[j].Name]
		if iok && jok {
			return ri < rj
		}
		if iok != jok {
			return iok // configured statuses sort ahead of unconfigured ones
		}
		return s.ByStatus[i].Count > s.ByStatus[j].Count
	})
}
