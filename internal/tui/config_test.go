package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveConfigKeepsUnmanagedKeys pins that a settings save rewrites only the
// keys the board owns: placement/direction (open.sh) and comments survive,
// managed keys aren't duplicated, and a managed array written across several
// lines leaves no orphaned lines behind.
func TestSaveConfigKeepsUnmanagedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := "# my shepherd\nview = \"priority\"\nplacement = \"overlay\"\ndirection = \"down\"\n" +
		"categories = [\n  \"work\",\n  \"home\",\n]\n"
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
	for _, want := range []string{"# my shepherd", `placement = "overlay"`, `direction = "down"`, "autosave = 30", `view = "priority"`} {
		if !strings.Contains(got, want) {
			t.Errorf("saved config lost %q:\n%s", want, got)
		}
	}
	for _, key := range []string{"view =", "categories ="} {
		if n := strings.Count(got, key); n != 1 {
			t.Errorf("%s written %d times:\n%s", key, n, got)
		}
	}
	if strings.Contains(got, "  \"work\",") {
		t.Errorf("multi-line array left orphaned lines:\n%s", got)
	}
	// a second save is stable: nothing accumulates
	if err := saveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != got {
		t.Errorf("save not idempotent:\n%s\nvs\n%s", got, again)
	}
}
