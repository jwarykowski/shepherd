// shepherd — an interactive todo board backed by a markdown file, plus a
// non-interactive command API (see internal/cli) for scripts and agents.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strings"

	"shepherd/internal/cli"
	"shepherd/internal/store"
	"shepherd/internal/tui"

	"github.com/charmbracelet/x/term"
)

//go:embed herdr-plugin.toml
var pluginManifest string

// version reads `version = "x.y.z"` out of the embedded manifest so the binary
// and the plugin manifest never drift.
func version() string {
	for _, ln := range strings.Split(pluginManifest, "\n") {
		if k, v, ok := strings.Cut(ln, "="); ok && strings.TrimSpace(k) == "version" {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return "unknown"
}

func main() {
	cli.Version = version()

	// A leading non-flag arg switches to the command API; bare `shepherd` and
	// `shepherd --filter …` stay the interactive board.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		os.Exit(cli.Run(os.Args[1], os.Args[2:]))
	}

	flag.Usage = func() { fmt.Fprintln(os.Stderr, cli.Usage()) }
	filter := flag.String("filter", os.Getenv("SHEPHERD_FILTER"), "start with this filter applied (matches text/note/category/due)")
	project := flag.String("project", "", "open this project's board (else $SHEPHERD_PROJECT, else the default)")
	all := flag.Bool("all", false, "open the read-only global view across all boards")
	stats := flag.Bool("stats", false, "print board stats and exit")
	legend := flag.Bool("legend", false, "explain each stats chart and exit")
	ver := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *ver {
		fmt.Println("shepherd", version())
		return
	}

	// --legend is static glossary text — print it regardless of --stats/--all.
	if *legend {
		os.Exit(cli.Run("stats", []string{"--legend"}))
	}

	if *stats {
		var a []string
		if *all {
			a = append(a, "--all")
		}
		if *project != "" {
			a = append(a, "--project", *project)
		}
		os.Exit(cli.Run("stats", a))
	}
	// The interactive board needs a real terminal on both ends. When either
	// stdin or stdout is redirected (piped, cron, CI), degrade gracefully to the
	// help text and point at the command API instead of letting Bubble Tea crash.
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, cli.Usage())
		fmt.Fprintln(os.Stderr, "\nshepherd: not a terminal — run a subcommand (e.g. `shepherd list`) for non-interactive use")
		os.Exit(1)
	}

	tui.Version = version()

	name, err := store.ResolveProject(*project)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		os.Exit(2)
	}

	if err := tui.Run(*filter, name, *all); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
