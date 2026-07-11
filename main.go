// shepherd — an interactive todo board backed by a markdown file, plus a
// non-interactive command API (see internal/cli) for scripts and agents.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"shepherd/internal/cli"
	"shepherd/internal/tui"
)

func main() {
	// A leading non-flag arg switches to the command API; bare `shepherd` and
	// `shepherd --filter …` stay the interactive board.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		os.Exit(cli.Run(os.Args[1], os.Args[2:]))
	}

	filter := flag.String("filter", os.Getenv("SHEPHERD_FILTER"), "start with this filter applied (matches text/note/category/due)")
	flag.Parse()

	if err := tui.Run(*filter); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
