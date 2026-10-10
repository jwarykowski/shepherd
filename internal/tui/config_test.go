package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveConfigKeepsUnmanagedKeys pins that a settings save rewrites only the
// keys the board owns: placement/direction (open.sh), agent_* (shepherd agent)
// and comments survive, and managed keys aren't duplicated.
func TestSaveConfigKeepsUnmanagedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := "# my shepherd\nview = \"priority\"\nplacement = \"overlay\"\ndirection = \"down\"\nagent_kind = \"codex\"\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	c := loadConfig(path)
	c.autosave = 30
	if err := saveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"# my shepherd", `placement = "overlay"`, `direction = "down"`, `agent_kind = "codex"`, "autosave = 30", `view = "priority"`} {
		if !strings.Contains(got, want) {
			t.Errorf("saved config lost %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "view ="); n != 1 {
		t.Errorf("view written %d times:\n%s", n, got)
	}
	// a second save is stable: nothing accumulates
	if err := saveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != got {
		t.Errorf("save not idempotent:\n%s\nvs\n%s", got, again)
	}
}
