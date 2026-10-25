package tui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// copied records what the TUI put on the clipboard during a test.
var copied []string

// TestMain swaps the clipboard command for a recorder, so tests can assert on
// what was copied.
func TestMain(m *testing.M) {
	copyText = func(s string) tea.Cmd {
		copied = append(copied, s)
		return nil
	}
	os.Exit(m.Run())
}
