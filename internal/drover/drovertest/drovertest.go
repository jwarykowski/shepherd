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
run)
	case "$*" in *--data-json*) cat > "$DROVER_FAKE_LOG.stdin" ;; esac
	printf '[{"id":"new1","status":"go","subject":"%s"}]\n' "$3" ;;
status) printf '{"watching":%s}\n' "${DROVER_FAKE_WATCHING:-true}" ;;
log) printf '[{"time":"14:02:01","kind":"agent","text":"reading the task"},{"time":"14:02:09","kind":"result","text":"success: opened PR #41"}]\n' ;;
archive) : ;;
init)
	if [ -n "$DROVER_FAKE_EXISTS" ]; then
		echo "drover: /x/drover.toml already exists; edit it" >&2
		exit 1
	fi
	echo "wrote /x/drover.toml: Shepherd boards → ${3:-claude}" ;;
service)
	case "$2" in
	status) printf '{"installed":%s,"running":false}\n' "${DROVER_FAKE_INSTALLED:-true}" ;;
	*) echo "drover $*" ;;
	esac ;;
license)
	case "$2" in
	status) printf '{"tier":"pro","email":"jo@example.com","expires":"2027-10-12T00:00:00Z"}\n' ;;
	activate)
		read -r key
		if [ "$key" = "BAD" ]; then echo "drover: not activated: licence key signature does not verify" >&2; exit 1; fi ;;
	esac ;;
*) echo "drover $*" ;;
esac
`

// Fake installs the fake drover as the only thing on the PATH, with runs as
// what `drover runs --json` prints. Calls returns the argument lines it got;
// Stdin returns what the last `drover run --data-json` read.
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

// Stdin is what the fake drover's last `run --data-json` read on stdin.
func Stdin() string {
	b, _ := os.ReadFile(os.Getenv("DROVER_FAKE_LOG") + ".stdin")
	return string(b)
}
