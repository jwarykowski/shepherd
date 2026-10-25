// Package testenv sandboxes a test binary's environment so no test can reach
// the user's real boards or config: HOME points at a fresh temp dir, and every
// variable that redirects shepherd's files (XDG_CONFIG_HOME included) is cleared.
// Imported only from TestMain; nothing in the shipped binary depends on it.
package testenv

import "os"

// redirects are the variables that point shepherd at a board or config file.
var redirects = []string{
	"XDG_CONFIG_HOME", "SHEPHERD_CONFIG", "SHEPHERD_TODO_FILE", "SHEPHERD_BOARD", "SHEPHERD_FILTER",
	"HERDR_TODO_FILE", "HERDR_PLUGIN_STATE_DIR",
}

// Run sandboxes the environment, runs the tests and returns their exit code.
func Run(run func() int) int {
	dir, err := os.MkdirTemp("", "shepherd-test-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, k := range redirects {
		_ = os.Unsetenv(k)
	}
	_ = os.Setenv("HOME", dir)
	// keep a real drover (and its agents) out of reach; drovertest.Fake puts a fake first
	_ = os.Setenv("PATH", "/bin:/usr/bin")
	return run()
}
