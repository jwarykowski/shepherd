// Package store handles shepherd's persistence: resolving the todo/config
// paths and reading/writing the markdown files. It depends only on todo.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"shepherd/internal/todo"
)

var (
	lineRE = regexp.MustCompile(`^- \[([ xX])\] (?:\(([HMLhml])\) )?(.*)$`)
	metaRE = regexp.MustCompile(`^  (created|note|category|due): (.*)$`)
)

// TodoPath resolves the todo file: $HERDR_TODO_FILE, else
// $HERDR_PLUGIN_STATE_DIR/todo.md, else ~/.config/shepherd/todo.md.
func TodoPath() string {
	if p := os.Getenv("HERDR_TODO_FILE"); p != "" {
		return p
	}
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "shepherd")
	}
	return filepath.Join(dir, "todo.md")
}

// ConfigPath resolves the config file: $SHEPHERD_CONFIG, else a sibling
// config.toml next to the todo file.
func ConfigPath() string {
	if p := os.Getenv("SHEPHERD_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(TodoPath()), "config.toml")
}

// ArchivePath is a sibling archive.md next to the todo file.
func ArchivePath(todoFile string) string {
	return filepath.Join(filepath.Dir(todoFile), "archive.md")
}

// Load parses the markdown checklist at path into items (nil if unreadable).
func Load(path string) []todo.Item {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []todo.Item
	for _, ln := range strings.Split(string(data), "\n") {
		if m := lineRE.FindStringSubmatch(ln); m != nil {
			it := todo.Item{Done: m[1] != " ", Text: m[3]}
			if m[2] != "" {
				it.Prio = strings.ToUpper(m[2])[0]
			}
			items = append(items, it)
			continue
		}
		if m := metaRE.FindStringSubmatch(ln); m != nil && len(items) > 0 {
			last := &items[len(items)-1]
			switch m[1] {
			case "created":
				last.Created = m[2]
			case "category":
				last.Category = strings.ToLower(m[2])
			case "due":
				last.Due = m[2]
			case "note":
				last.Note = m[2]
			}
		}
	}
	return items
}

// Serialize renders items as the on-disk markdown format.
func Serialize(items []todo.Item) string {
	var b strings.Builder
	for _, it := range items {
		box := " "
		if it.Done {
			box = "x"
		}
		tag := ""
		if it.Prio != 0 {
			tag = fmt.Sprintf("(%c) ", it.Prio)
		}
		fmt.Fprintf(&b, "- [%s] %s%s\n", box, tag, it.Text)
		if it.Created != "" {
			fmt.Fprintf(&b, "  created: %s\n", it.Created)
		}
		if it.Due != "" {
			fmt.Fprintf(&b, "  due: %s\n", it.Due)
		}
		if it.Category != "" {
			fmt.Fprintf(&b, "  category: %s\n", it.Category)
		}
		if it.Note != "" {
			fmt.Fprintf(&b, "  note: %s\n", strings.ReplaceAll(it.Note, "\n", " "))
		}
	}
	return b.String()
}

// Save writes items to path, creating the directory if needed.
func Save(path string, items []todo.Item) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Serialize(items)), 0o644)
}
