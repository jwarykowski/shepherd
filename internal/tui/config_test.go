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
// lines is read in full and rewritten on one line, leaving no orphans behind.
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
	if !strings.Contains(got, `categories = ["work", "home"]`) {
		t.Errorf("multi-line categories not kept:\n%s", got)
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

// TestSaveConfigThroughSymlink pins that a stow-managed (symlinked) config is
// written through to its target, not replaced by a plain file.
func TestSaveConfigThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles-config.toml")
	link := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("placement = \"tab\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(link, loadConfig(link)); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config symlink replaced: %v", err)
	}
	if data, _ := os.ReadFile(target); !strings.Contains(string(data), `placement = "tab"`) {
		t.Fatalf("target not updated:\n%s", data)
	}
}
