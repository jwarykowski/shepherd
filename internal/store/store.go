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

// projectRE is the allowed project-name slug. Anchored and free of path
// separators or dots-only names, so a project can never escape BaseDir.
var projectRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// BaseDir is where every board lives: ~/.config/shepherd. Fixed on purpose —
// shepherd does not follow $HERDR_PLUGIN_STATE_DIR, so the default and all
// project boards stay in one dotfiles-syncable directory.
func BaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "shepherd")
}

// todoFileOverride is the explicit whole-file override, $SHEPHERD_TODO_FILE
// (else ""). Both TodoPathFor and ConfigPath key off it.
func todoFileOverride() string {
	return os.Getenv("SHEPHERD_TODO_FILE")
}

// ResolveProject picks the effective project name: the flag if non-empty, else
// $SHEPHERD_PROJECT, else "". A non-empty name must be a safe slug — this is
// the one validation point, so the env path can't smuggle path traversal.
func ResolveProject(flag string) (string, error) {
	name := flag
	if name == "" {
		name = os.Getenv("SHEPHERD_PROJECT")
	}
	if name != "" && !projectRE.MatchString(name) {
		return "", fmt.Errorf("invalid project name %q (use letters, digits, . _ -)", name)
	}
	return name, nil
}

// TodoPathFor resolves the todo file for a (validated) project. The override
// wins; else an empty project is the default todo.md and a named project is
// projects/<name>.md — both under BaseDir.
//
// ponytail: a future "global view" would glob BaseDir()/projects/*.md
// (skipping *-archive.md).
func TodoPathFor(project string) string {
	if p := todoFileOverride(); p != "" {
		return p
	}
	if project != "" {
		return filepath.Join(BaseDir(), "projects", project+".md")
	}
	return filepath.Join(BaseDir(), "todo.md")
}

// TodoPath resolves the default todo file (no project).
func TodoPath() string { return TodoPathFor("") }

// ConfigPath resolves the shared config file: $SHEPHERD_CONFIG, else a sibling
// of the whole-file override if one is set, else BaseDir/config.toml. It stays
// at BaseDir for project boards so every board shares one config.
func ConfigPath() string {
	if p := os.Getenv("SHEPHERD_CONFIG"); p != "" {
		return p
	}
	if p := todoFileOverride(); p != "" {
		return filepath.Join(filepath.Dir(p), "config.toml")
	}
	return filepath.Join(BaseDir(), "config.toml")
}

// ArchivePath is the archive sibling of the todo file: todo.md -> archive.md,
// projects/web.md -> projects/web-archive.md.
func ArchivePath(todoFile string) string {
	dir := filepath.Dir(todoFile)
	base := strings.TrimSuffix(filepath.Base(todoFile), ".md")
	if base == "todo" {
		return filepath.Join(dir, "archive.md")
	}
	return filepath.Join(dir, base+"-archive.md")
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
