package tui

import (
	"os"
	"testing"
)

// copied records what the TUI put on the clipboard during a test.
var copied []string

// TestMain swaps the system clipboard for a recorder, so tests never run
// wl-copy/xclip (which can block forever on a live desktop) and can assert on
// what was copied.
func TestMain(m *testing.M) {
	copyText = func(s string) error {
		copied = append(copied, s)
		return nil
	}
	os.Exit(m.Run())
}
