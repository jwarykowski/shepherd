package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// This file adds an optional working directory per board — the cwd a downstream
// tool (drover) runs an agent in. It's a shepherd concern because shepherd owns
// board creation, but it lives in a sidecar rather than the board .md: Save
// rewrites the .md from items and would drop any header, and config.toml is the
// shared TUI config with its own managed keys.

// boardDirsPath is the sidecar mapping board name → working directory, beside
// config.toml under BaseDir.
func boardDirsPath() string {
	return filepath.Join(filepath.Dir(ConfigPath()), "boards.toml")
}

// loadBoardDirs reads the sidecar into a name→dir map. Leniently parsed:
// `name = "dir"` lines, blanks and #-comments skipped. Missing file → empty map.
func loadBoardDirs() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(boardDirsPath())
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		if name := strings.TrimSpace(k); name != "" {
			out[name] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return out
}

// BoardDir returns the configured working directory for a board, or "" if none.
func BoardDir(name string) string { return loadBoardDirs()[name] }

// SetBoardDir records (or clears, when dir is empty) a board's working directory
// in the sidecar. ponytail: full rewrite from the map — boards.toml is
// tool-managed and small, so no comment-preserving merge; add one if users ever
// hand-edit it.
func SetBoardDir(name, dir string) error {
	if err := ValidBoard(name); err != nil {
		return err
	}
	dirs := loadBoardDirs()
	if dir = strings.TrimSpace(dir); dir == "" {
		delete(dirs, name)
	} else {
		dirs[name] = dir
	}
	names := make([]string, 0, len(dirs))
	for n := range dirs {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s = %q\n", n, dirs[n])
	}
	p := boardDirsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}
