// Package tui is shepherd's interactive Bubble Tea board. It depends on todo
// (domain) and store (persistence); nothing depends on it but main.
package tui

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

const (
	appSubtitle = "your todos herded"
	padX        = 2
	padY        = 1
)

type config struct {
	view       viewMode
	density    density
	categories []string
	autosave   int // seconds of idle before autosaving; 0 disables
}

// loadConfig reads a tiny key=value config (leniently TOML-ish):
//
//	view = table
//	categories = ["work", "home", "personal"]   # or: work, home, personal
func loadConfig(path string) config {
	c := config{autosave: 60}
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
		case "autosave":
			if n, err := strconv.Atoi(strings.Trim(v, `"`)); err == nil {
				c.autosave = n
			}
		}
	}
	return c
}

type mode int

const (
	modeList mode = iota
	modeAdd
	modeEdit
	modeNote
	modeCategory
	modeDue
	modeDefer
	modeLink
	modeFilter
	modeDetail
	modeHelp
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
	cursor        int         // index into the VISIBLE subset, not items
	filter        string
	mode          mode
	view          viewMode
	input         textinput.Model
	w             int
	height        int
	past          [][]todo.Item // undo stack (snapshots before each mutation)
	future        [][]todo.Item // redo stack (snapshots undone)
	dirty         bool          // in-memory changes not yet saved
	lastEdit      time.Time     // when the last mutation happened (debounce autosave)
	autosaveEvery time.Duration // idle gap before autosaving; 0 disables
	lastMod       time.Time     // todo file mtime we last saw
	helpScroll    int           // scroll offset in the help page
	categories    []string      // configured categories (tab-cycle in category mode)
	catIdx        int           // cursor into categories while cycling
	density       density       // spacing mode
	global        bool          // read-only aggregate across all boards
	project       string        // the board to return to when leaving global
}

// resort orders items for the active view.
func (m *model) resort() {
	if m.view == viewProject {
		todo.SortBySource(m.items)
		return
	}
	todo.Sort(m.items, m.view == viewPriority)
}

// histCap bounds the undo/redo depth so history can't grow unbounded.
// 100 is plenty for a todo list; raise if anyone ever hits it.
const histCap = 100

// newModel builds the initial board for the given project ("" = default).
func newModel(project string) model {
	ti := textinput.New()
	ti.Prompt = "› "
	p := store.TodoPathFor(project)
	cfg := loadConfig(store.ConfigPath())
	m := model{
		path:          p,
		project:       project,
		items:         store.Load(p),
		archived:      store.Load(store.ArchivePath(p)),
		input:         ti,
		lastMod:       store.FileModTime(p),
		view:          cfg.view,
		density:       cfg.density,
		categories:    cfg.categories,
		autosaveEvery: time.Duration(cfg.autosave) * time.Second,
	}
	m.resort()
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
	m.past, m.future, m.dirty = nil, nil, false
	m.resort()
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
		m.dirty = false
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
