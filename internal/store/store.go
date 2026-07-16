// Package store handles shepherd's persistence: resolving the todo/config
// paths and reading/writing the markdown files. It depends only on todo.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"shepherd/internal/todo"
)

var (
	lineRE = regexp.MustCompile(`^- \[([ xX])\] (?:\(([HMLhml])\) )?(.*)$`)
	metaRE = regexp.MustCompile(`^  (created|completed|defer|note|category|due|link|status): (.*)$`)
	// subtask lines are the same checklist syntax indented two spaces, with
	// their own meta indented four. They never collide with metaRE (which needs
	// a bare `word:` at two spaces, never `- [`).
	subLineRE = regexp.MustCompile(`^  - \[([ xX])\] (?:\(([HMLhml])\) )?(.*)$`)
	subMetaRE = regexp.MustCompile(`^    (created|completed|defer|note|category|due|link|status): (.*)$`)
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
// a future "global view" would glob BaseDir()/projects/*.md
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

// Board is one todo board: a display name and its file path.
type Board struct {
	Name string
	Path string
}

// Boards lists the default board (if its file exists) then each
// projects/<name>.md, skipping archive siblings. filepath.Glob returns sorted
// paths, so the projects come out alphabetical.
func Boards() []Board {
	var bs []Board
	def := filepath.Join(BaseDir(), "todo.md")
	if _, err := os.Stat(def); err == nil {
		bs = append(bs, Board{Name: "default", Path: def})
	}
	matches, _ := filepath.Glob(filepath.Join(BaseDir(), "projects", "*.md"))
	for _, p := range matches {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if strings.HasSuffix(name, "-archive") {
			continue
		}
		bs = append(bs, Board{Name: name, Path: p})
	}
	return bs
}

// ValidProject returns an error if name is not a safe project slug. Exported so
// the TUI and CLI can validate a rename/unarchive target before touching files.
func ValidProject(name string) error {
	if name == "" || !projectRE.MatchString(name) {
		return fmt.Errorf("invalid project name %q (use letters, digits, . _ -)", name)
	}
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// archivedDir holds whole boards stashed by ArchiveBoard. Boards() never lists
// it because its glob (projects/*.md) is non-recursive.
func archivedDir() string { return filepath.Join(BaseDir(), "projects", "archived") }

// RenameBoard renames a named project board and its archive sibling. It refuses
// the default board, an invalid target, a missing source, or an existing target.
func RenameBoard(oldName, newName string) error {
	if oldName == "" || oldName == "default" {
		return fmt.Errorf("cannot rename the default board")
	}
	if err := ValidProject(newName); err != nil {
		return err
	}
	src, dst := TodoPathFor(oldName), TodoPathFor(newName)
	if !fileExists(src) {
		return fmt.Errorf("no board %q", oldName)
	}
	if fileExists(dst) {
		return fmt.Errorf("board %q already exists", newName)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	if a := ArchivePath(src); fileExists(a) {
		_ = os.Rename(a, ArchivePath(dst))
	}
	return nil
}

// DeleteBoard removes a named project board and its archive sibling. It refuses
// the default board.
func DeleteBoard(name string) error {
	if name == "" || name == "default" {
		return fmt.Errorf("cannot delete the default board")
	}
	p := TodoPathFor(name)
	if !fileExists(p) {
		return fmt.Errorf("no board %q", name)
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	if a := ArchivePath(p); fileExists(a) {
		_ = os.Remove(a)
	}
	return nil
}

// ArchiveBoard moves a whole board (and its archive sibling) into
// projects/archived/, hiding it from Boards(). Reversible via UnarchiveBoard.
func ArchiveBoard(name string) error {
	if name == "" || name == "default" {
		return fmt.Errorf("cannot archive the default board")
	}
	src := TodoPathFor(name)
	if !fileExists(src) {
		return fmt.Errorf("no board %q", name)
	}
	if err := os.MkdirAll(archivedDir(), 0o755); err != nil {
		return err
	}
	dst := filepath.Join(archivedDir(), name+".md")
	if fileExists(dst) {
		return fmt.Errorf("archived board %q already exists", name)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	if a := ArchivePath(src); fileExists(a) {
		_ = os.Rename(a, filepath.Join(archivedDir(), name+"-archive.md"))
	}
	return nil
}

// UnarchiveBoard moves an archived board (and its archive sibling) back into
// projects/, making it live again.
func UnarchiveBoard(name string) error {
	if err := ValidProject(name); err != nil {
		return err
	}
	src := filepath.Join(archivedDir(), name+".md")
	if !fileExists(src) {
		return fmt.Errorf("no archived board %q", name)
	}
	dst := TodoPathFor(name)
	if fileExists(dst) {
		return fmt.Errorf("board %q already exists", name)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	if a := filepath.Join(archivedDir(), name+"-archive.md"); fileExists(a) {
		_ = os.Rename(a, ArchivePath(dst))
	}
	return nil
}

// ArchivedBoards lists boards stashed under projects/archived/ (archive siblings
// skipped), for the `project unarchive` listing.
func ArchivedBoards() []Board {
	var bs []Board
	matches, _ := filepath.Glob(filepath.Join(archivedDir(), "*.md"))
	for _, p := range matches {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if strings.HasSuffix(name, "-archive") {
			continue
		}
		bs = append(bs, Board{Name: name, Path: p})
	}
	return bs
}

// BoardCounts returns the open and total top-level item counts for a board file
// (subtasks not counted), for the picker and `projects` listing.
func BoardCounts(path string) (open, total int) {
	items := Load(path)
	for _, it := range items {
		if !it.Done {
			open++
		}
	}
	return open, len(items)
}

// LoadAll returns every board's items with Source set to the board name — the
// read-only aggregate behind the global view. Never write these back: items
// from many files must not be flattened into one.
func LoadAll() []todo.Item {
	var all []todo.Item
	for _, b := range Boards() {
		for _, it := range Load(b.Path) {
			it.Source = b.Name
			all = append(all, it)
		}
	}
	return all
}

// LoadArchive parses the archive sibling of a todo file (done items moved out
// of the live board). Empty if there's no archive.
func LoadArchive(todoFile string) []todo.Item {
	return Load(ArchivePath(todoFile))
}

// LoadAllArchives returns every board's archived items with Source set to the
// board name — the done-item history behind the aggregate stats view.
func LoadAllArchives() []todo.Item {
	var all []todo.Item
	for _, b := range Boards() {
		for _, it := range LoadArchive(b.Path) {
			it.Source = b.Name
			all = append(all, it)
		}
	}
	return all
}

// BoardsLatestMod is the newest mtime across all boards; it drives the global
// view's reload check (a brand-new board file bumps the max).
func BoardsLatestMod() time.Time {
	var latest time.Time
	for _, b := range Boards() {
		if mt := FileModTime(b.Path); mt.After(latest) {
			latest = mt
		}
	}
	return latest
}

// parseCheck builds an Item from a checklist-line regexp match (done, prio, text).
func parseCheck(m []string) todo.Item {
	it := todo.Item{Done: m[1] != " ", Text: m[3]}
	if m[2] != "" {
		it.Prio = strings.ToUpper(m[2])[0]
	}
	return it
}

// applyMeta sets one metadata field on an item from a meta-line key/value.
func applyMeta(it *todo.Item, key, val string) {
	switch key {
	case "created":
		it.Created = val
	case "completed":
		it.Completed = val
	case "defer":
		it.Defer = val
	case "category":
		it.Category = strings.ToLower(val)
	case "due":
		it.Due = val
	case "link":
		it.Link = val
	case "note":
		// each physical line is its own note: line, appended on load;
		// a leading blank line is the one lost edge, not worth it.
		if it.Note == "" {
			it.Note = val
		} else {
			it.Note += "\n" + val
		}
	case "status":
		it.Status = strings.ToLower(val)
	}
}

// Load parses the markdown checklist at path into items (nil if unreadable).
// A column-0 checklist line starts a parent; a two-space checklist line is a
// subtask of that parent; two/four-space meta lines attach to the parent/sub.
func Load(path string) []todo.Item {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []todo.Item
	curSub := -1 // index into the last parent's Subs, or -1 for the parent itself
	for _, ln := range strings.Split(string(data), "\n") {
		if m := lineRE.FindStringSubmatch(ln); m != nil {
			items = append(items, parseCheck(m))
			curSub = -1
			continue
		}
		if m := subLineRE.FindStringSubmatch(ln); m != nil && len(items) > 0 {
			parent := &items[len(items)-1]
			parent.Subs = append(parent.Subs, parseCheck(m))
			curSub = len(parent.Subs) - 1
			continue
		}
		if m := subMetaRE.FindStringSubmatch(ln); m != nil && len(items) > 0 {
			parent := &items[len(items)-1]
			if curSub >= 0 && curSub < len(parent.Subs) {
				applyMeta(&parent.Subs[curSub], m[1], m[2])
			}
			continue
		}
		if m := metaRE.FindStringSubmatch(ln); m != nil && len(items) > 0 {
			applyMeta(&items[len(items)-1], m[1], m[2])
		}
	}
	return items
}

// writeItem renders one item's checklist line and meta at the given indent
// (parent: "", meta at "  "; subtask: "  ", meta at "    ").
func writeItem(b *strings.Builder, it todo.Item, indent string) {
	box := " "
	if it.Done {
		box = "x"
	}
	tag := ""
	if it.Prio != 0 {
		tag = fmt.Sprintf("(%c) ", it.Prio)
	}
	fmt.Fprintf(b, "%s- [%s] %s%s\n", indent, box, tag, it.Text)
	meta := indent + "  "
	if it.Created != "" {
		fmt.Fprintf(b, "%screated: %s\n", meta, it.Created)
	}
	if it.Completed != "" {
		fmt.Fprintf(b, "%scompleted: %s\n", meta, it.Completed)
	}
	if it.Defer != "" {
		fmt.Fprintf(b, "%sdefer: %s\n", meta, it.Defer)
	}
	if it.Due != "" {
		fmt.Fprintf(b, "%sdue: %s\n", meta, it.Due)
	}
	if it.Category != "" {
		fmt.Fprintf(b, "%scategory: %s\n", meta, it.Category)
	}
	if !it.Done && it.Status != "" {
		fmt.Fprintf(b, "%sstatus: %s\n", meta, it.Status)
	}
	if it.Link != "" {
		fmt.Fprintf(b, "%slink: %s\n", meta, strings.ReplaceAll(it.Link, "\n", " "))
	}
	if it.Note != "" {
		for _, ln := range strings.Split(it.Note, "\n") {
			fmt.Fprintf(b, "%snote: %s\n", meta, ln)
		}
	}
}

// Serialize renders items as the on-disk markdown format. Each parent's meta is
// written before its subtasks; subtask Subs are never recursed into (one level).
func Serialize(items []todo.Item) string {
	var b strings.Builder
	for _, it := range items {
		writeItem(&b, it, "")
		for _, sub := range it.Subs {
			writeItem(&b, sub, "  ")
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
