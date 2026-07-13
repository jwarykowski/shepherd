package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shepherd/internal/todo"
)

// TestStatsJSON checks the JSON path counts archived done items and emits pure
// numbers (no chart glyphs).
func TestStatsJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)

	if err := os.WriteFile(path, []byte(
		"- [ ] (H) open one\n  created: 12-07-2026 09:00\n"+
			"- [x] done live\n  created: 10-07-2026 09:00\n  completed: 12-07-2026 09:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// archived done item lives in the sibling archive.md
	if err := os.WriteFile(filepath.Join(dir, "archive.md"), []byte(
		"- [x] done archived\n  created: 01-07-2026 09:00\n  completed: 05-07-2026 09:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if code := cmdStats([]string{"--json"}, "", &buf); code != 0 {
		t.Fatalf("stats --json exit %d", code)
	}
	out := buf.String()
	if strings.ContainsAny(out, "█╭╰│⠿") {
		t.Errorf("--json leaked chart glyphs:\n%s", out)
	}

	var s todo.Stats
	if err := json.Unmarshal(buf.Bytes(), &s); err != nil {
		t.Fatalf("json: %v", err)
	}
	if s.Total != 3 || s.Open != 1 || s.Done != 2 {
		t.Errorf("counts = total %d open %d done %d, want 3/1/2 (archive counted)", s.Total, s.Open, s.Done)
	}
}

// TestStatsAll checks --all aggregates boards and populates ByProject.
func TestStatsAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHEPHERD_TODO_FILE", "") // don't let the override short-circuit boards
	base := filepath.Join(home, ".config", "shepherd")
	if err := os.MkdirAll(filepath.Join(base, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, body string) {
		if err := os.WriteFile(filepath.Join(base, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("todo.md", "- [ ] a\n  created: 12-07-2026 09:00\n")
	write("projects/web.md", "- [ ] b\n  created: 12-07-2026 09:00\n- [ ] c\n  created: 12-07-2026 09:00\n")

	var buf bytes.Buffer
	if code := cmdStats([]string{"--all", "--json"}, "", &buf); code != 0 {
		t.Fatalf("stats --all exit %d", code)
	}
	var s todo.Stats
	if err := json.Unmarshal(buf.Bytes(), &s); err != nil {
		t.Fatalf("json: %v", err)
	}
	if s.Total != 3 || s.ByProject["default"].Open != 1 || s.ByProject["web"].Open != 2 {
		t.Errorf("all aggregate wrong: total %d, byProject %+v", s.Total, s.ByProject)
	}
}
