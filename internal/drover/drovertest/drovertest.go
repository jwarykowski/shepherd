// Package drovertest puts a fake drover on the PATH for tests: it records each
// call and answers runs, go, run and init the way drover does.
package drovertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const script = `#!/bin/sh
printf '%s\n' "$*" >> "$DROVER_FAKE_LOG"
case "$1" in
runs) cat "$DROVER_FAKE_RUNS" ;;
go) printf '[{"id":"held1","status":"go","subject":"%s"}]\n' "$3" ;;
run) printf '[{"id":"new1","status":"go","subject":"%s"}]\n' "$3" ;;
init)
	if [ -n "$DROVER_FAKE_EXISTS" ]; then
		echo "drover: /x/drover.toml already exists; edit it" >&2
		exit 1
	fi
	echo "wrote /x/drover.toml" ;;
*) echo "drover $*" ;;
esac
`

// Fake installs the fake drover as the only thing on the PATH, with runs as
// what `drover runs --json` prints. Calls returns the argument lines it got.
func Fake(t *testing.T, runs string) (calls func() []string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "drover"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runsFile := filepath.Join(dir, "runs.json")
	if err := os.WriteFile(runsFile, []byte(runs), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
	t.Setenv("DROVER_FAKE_LOG", log)
	t.Setenv("DROVER_FAKE_RUNS", runsFile)
	return func() []string {
		b, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}
