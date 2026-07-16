// Package tui is shepherd's interactive Bubble Tea board. It depends on todo
// (domain) and store (persistence); nothing depends on it but main.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

const (
	appSubtitle = "your todos herded"
	padX        = 2
	padY        = 1
	noteHeight  = 5 // visible rows in the note textarea editor
)

type config struct {
	view       viewMode
	density    density
	categories []string
	statuses   []string // ordered, normalized so "done" is present and last
	autosave   int      // seconds of idle before autosaving; 0 disables
}

// loadConfig reads a tiny key=value config (leniently TOML-ish):
//
//	view = table
//	categories = ["work", "home", "personal"]   # or: work, home, personal
func loadConfig(path string) config {
	c := config{autosave: 60, statuses: []string{"open", "done"}}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
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
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "view":
			switch strings.ToLower(strings.Trim(v, `"`)) {
			case "priority":
				c.view = viewPriority
			case "table":
				c.view = viewTable
			default:
				c.view = viewCategory
			}
		case "density":
			if strings.ToLower(strings.Trim(v, `"`)) == "comfort" {
				c.density = comfort
			} else {
				c.density = compact
			}
		case "categories":
			for _, part := range strings.Split(strings.Trim(v, "[]"), ",") {
				if p := strings.Trim(strings.TrimSpace(part), `"`); p != "" {
					c.categories = append(c.categories, p)
				}
			}
		case "statuses":
			var ss []string
			for _, part := range strings.Split(strings.Trim(v, "[]"), ",") {
				if p := strings.ToLower(strings.Trim(strings.TrimSpace(part), `"`)); p != "" {
					ss = append(ss, p)
				}
			}
			if len(ss) > 0 {
				c.statuses = ss
			}
		case "autosave":
			if n, err := strconv.Atoi(strings.Trim(v, `"`)); err == nil {
				c.autosave = n
			}
		}
	}
	c.statuses = normalizeStatuses(c.statuses)
	return c
}

// saveConfig writes the known config keys back to config.toml. It rewrites the
// file from the known keys, so any user comments in it are dropped.
func saveConfig(path string, c config) error {
	den := "compact"
	if c.density == comfort {
		den = "comfort"
	}
	list := func(xs []string) string {
		q := make([]string, len(xs))
		for i, x := range xs {
			q[i] = fmt.Sprintf("%q", x)
		}
		return "[" + strings.Join(q, ", ") + "]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "view = %q\n", viewName[c.view])
	fmt.Fprintf(&b, "density = %q\n", den)
	fmt.Fprintf(&b, "autosave = %d\n", c.autosave)
	fmt.Fprintf(&b, "categories = %s\n", list(c.categories))
	fmt.Fprintf(&b, "statuses = %s\n", list(c.statuses))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// normalizeStatuses dedups the configured statuses and guarantees a non-terminal
// first status (the implicit default) plus "done" present and last — the two ends
// the cycle and archiving depend on. Without a non-terminal entry, tab-cycle
// could never reopen a done item.
func normalizeStatuses(ss []string) []string {
	out := make([]string, 0, len(ss)+1)
	seen := map[string]bool{}
	for _, s := range ss {
		if s == "done" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		out = append(out, "open")
	}
	return append(out, "done")
}

type mode int

const (
	modeList mode = iota
	modeAdd
	modeAddSub
	modeEdit
	modeNote
	modeCategory
	modeDue
	modeDefer
	modeLink
	modeFilter
	modeDetail
	modeHelp
	modeArchive
	modeProjects
	modeSettings
	modeSettingEdit
	modeProjectNew
	modeProjectRename
	modeConfirmDelete
)

// density controls spacing: compact (tight, default) or comfort (roomier).
type density int

const (
	compact density = iota
	comfort
)

func (d density) padX() int {
	if d == comfort {
		return padX + 2
	}
	return padX
}

func (d density) padY() int {
	if d == comfort {
		return padY + 1
	}
	return padY
}

// viewMode selects how the list is grouped/rendered.
type viewMode int

const (
	viewCategory viewMode = iota // grouped under category headers
	viewPriority                 // grouped under priority headers
	viewTable                    // flat bubbles/table
	viewProject                  // grouped by source board (global view only)
)

var viewName = map[viewMode]string{viewCategory: "category", viewPriority: "priority", viewTable: "table", viewProject: "project"}

type model struct {
	path          string
	items         []todo.Item
	archived      []todo.Item // loaded from archive.md, searched when filtering
	arcRows       []todo.Item // archive browse set (modeArchive); all boards' when global
	arcCur        int         // cursor into arcRows
	cursor        int         // index into the VISIBLE subset, not items
	filter        string
	mode          mode
	view          viewMode
	input         textinput.Model
	note          textarea.Model // multi-line editor for the note field (modeNote)
	noteEditing   bool           // a change was captured this note session (one undo snapshot)
	w             int
	height        int
	past          [][]todo.Item // undo stack (snapshots before each mutation)
	future        [][]todo.Item // redo stack (snapshots undone)
	dirty         bool          // cached: board differs from disk; recomputed per event
	saved         string        // fingerprint of the board as last saved to disk
	lastEdit      time.Time     // when the last mutation happened (debounce autosave)
	autosaveEvery time.Duration // idle gap before autosaving; 0 disables
	lastMod       time.Time     // todo file mtime we last saw
	helpScroll    int           // scroll offset in the help page
	categories    []string      // configured categories (tab-cycle in category mode)
	catIdx        int           // cursor into categories while cycling
	statuses      []string      // configured statuses, ordered, "done" last (Tab-cycle in list)
	density       density       // spacing mode
	global        bool          // read-only aggregate across all boards
	project       string        // the board to return to when leaving global
	projRows      []store.Board // board list for the picker (modeProjects)
	projCur       int           // cursor into projRows
	settingsCur   int           // cursor into the settings rows (modeSettings)
}

// currentConfig snapshots the live, editable settings for the settings screen
// and for writing back to config.toml.
func (m model) currentConfig() config {
	return config{
		view:       m.view,
		density:    m.density,
		categories: m.categories,
		statuses:   m.statuses,
		autosave:   int(m.autosaveEvery / time.Second),
	}
}

// resort orders items for the active view.
func (m *model) resort() {
	if m.view == viewProject {
		todo.SortBySource(m.items)
		return
	}
	todo.Sort(m.items, m.view == viewPriority)
}

// fingerprint is an order-independent snapshot of board content, so the saved
// indicator reflects real differences from disk, not row order (switching view
// reorders items but changes nothing on disk). Naive per-item serialize+sort;
// fine at histCap-scale boards.
func fingerprint(items []todo.Item) string {
	blocks := make([]string, len(items))
	for i, it := range items {
		blocks[i] = store.Serialize([]todo.Item{it})
	}
	sort.Strings(blocks)
	return strings.Join(blocks, "")
}

// markSaved records the current board as the on-disk baseline, clearing dirty.
func (m *model) markSaved() { m.saved = fingerprint(m.items); m.dirty = false }

// refreshDirty recomputes the cached dirty flag from the saved baseline.
func (m *model) refreshDirty() { m.dirty = fingerprint(m.items) != m.saved }

// histCap bounds the undo/redo depth so history can't grow unbounded.
// 100 is plenty for a todo list; raise if anyone ever hits it.
const histCap = 100

// newModel builds the initial board for the given project ("" = default).
func newModel(project string) model {
	ti := textinput.New()
	ti.Prompt = "› "
	na := textarea.New()
	na.ShowLineNumbers = false
	na.Prompt = ""
	na.CharLimit = 0 // notes can be long
	p := store.TodoPathFor(project)
	cfg := loadConfig(store.ConfigPath())
	m := model{
		path:          p,
		project:       project,
		items:         store.Load(p),
		archived:      store.Load(store.ArchivePath(p)),
		input:         ti,
		note:          na,
		lastMod:       store.FileModTime(p),
		view:          cfg.view,
		density:       cfg.density,
		categories:    cfg.categories,
		statuses:      cfg.statuses,
		autosaveEvery: time.Duration(cfg.autosave) * time.Second,
	}
	m.resort()
	m.markSaved()
	return m
}

// loadGlobal replaces the model's items with the read-only aggregate across
// every board, grouped by project. Shared by --all launch and the A toggle.
func (m *model) loadGlobal() {
	m.global = true
	m.items = store.LoadAll()
	m.archived = nil
	m.view = viewProject
	m.lastMod = store.BoardsLatestMod()
	m.past, m.future = nil, nil
	m.resort()
	m.markSaved()
	m.cursor = 0
	m.clamp()
}

// toggleGlobal flips between the focused board and the global aggregate. On the
// way in it flushes any unsaved edits to the current board; on the way out it
// reloads that board fresh from disk.
func (m *model) toggleGlobal() {
	if m.global {
		nm := newModel(m.project)
		nm.filter, nm.w, nm.height, nm.density = m.filter, m.w, m.height, m.density
		nm.clamp()
		*m = nm
		return
	}
	if m.dirty {
		_ = store.Save(m.path, m.items)
	}
	m.loadGlobal()
}

// Run builds the board for a project (or the global view), applies an initial
// filter, and runs it to exit.
func Run(filter, project string, global bool) error {
	m := newModel(project)
	if global {
		m.loadGlobal()
	}
	m.filter = filter
	m.clamp()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
