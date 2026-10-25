package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"shepherd/internal/store"
	"shepherd/internal/theme"
	"shepherd/internal/todo"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// cmdStats summarises a board (or all boards with --all) as charts, or the raw
// numbers with --json. Done-based counts include the archive.
func cmdStats(args []string, board string, w io.Writer) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output (no charts)")
	all := fs.Bool("all", false, "aggregate across every board")
	legend := fs.Bool("legend", false, "explain each chart and the backlog-health numbers")
	noColor := fs.Bool("no-color", false, "disable ANSI color in the charts")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if fs.NArg() > 0 {
		return usageErr(w, *asJSON, fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	plain := colorDisabled(*noColor)
	kv := store.ScanKVFile(store.ConfigPath())
	// only ask the terminal for its background when colour will be drawn
	light := !plain && !lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	palette = theme.Resolve(strings.Trim(kv["theme"], `"`), light, kv)
	draw := func(s string) {
		if plain {
			s = ansi.Strip(s)
		}
		emit(w, s)
	}
	if *legend {
		draw(statsLegend())
		return 0
	}

	var items []todo.Item
	title := "shepherd stats"
	if *all {
		items = append(store.LoadAll(), store.LoadAllArchives()...)
		title += " · all boards"
	} else {
		path := store.TodoPathFor(board)
		items = append(store.Load(path), store.LoadArchive(path)...)
		if board != "" {
			title += " · " + board
		}
	}

	s := todo.Compute(items)
	orderByConfig(&s, store.ConfigStatusOrder())

	if *asJSON {
		return emitJSON(w, s)
	}

	draw(renderStats(s, title, termWidth()))
	return 0
}

// termWidth reports the terminal width, or 80 when stdout isn't a tty.
func termWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 80
}

// colorDisabled decides whether to strip ANSI colour from the charts, honouring
// (clig): an explicit --no-colour flag, $NO_COLOUR (any value), $TERM=dumb, or a
// non-tty stdout. lipgloss v2 renders full colour and leaves downsampling to the
// writer, so shepherd strips the ANSI itself when any of these hold.
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
