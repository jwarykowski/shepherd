package tui

import (
	"os"
	"shepherd/internal/testenv"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// copied records what the TUI put on the clipboard during a test.
var copied []string

// TestMain sandboxes the environment (see testenv) and swaps the clipboard
// command for a recorder, so tests can assert on what was copied.
func TestMain(m *testing.M) {
	copyText = func(s string) tea.Cmd {
		copied = append(copied, s)
		return nil
	}
	os.Exit(testenv.Run(m.Run))
}
