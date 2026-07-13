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
	ver := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *ver {
		fmt.Println("shepherd", version())
		return
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
