// Package tui is shepherd's interactive Bubble Tea board. It depends on todo
// (domain) and store (persistence); nothing depends on it but main.
package tui

import (
	"os"
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
}

// loadConfig reads a tiny key=value config (leniently TOML-ish):
//
//	view = table
//	categories = ["work", "home", "personal"]   # or: work, home, personal
func loadConfig(path string) config {
	c := config{}
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
)

var viewName = map[viewMode]string{viewCategory: "category", viewPriority: "priority", viewTable: "table"}

type model struct {
	path       string
	items      []todo.Item
	archived   []todo.Item // loaded from archive.md, searched when filtering
	cursor     int         // index into the VISIBLE subset, not items
	filter     string
	mode       mode
	view       viewMode
	input      textinput.Model
	w          int
	height     int
	past       [][]todo.Item // undo stack (snapshots before each mutation)
	future     [][]todo.Item // redo stack (snapshots undone)
	dirty      bool          // in-memory changes not yet saved
	lastMod    time.Time     // todo file mtime we last saw
	helpScroll int           // scroll offset in the help page
	categories []string      // configured categories (tab-cycle in category mode)
	catIdx     int           // cursor into categories while cycling
	density    density       // spacing mode
}

// resort orders items for the active view.
func (m *model) resort() {
	todo.Sort(m.items, m.view == viewPriority)
}

// histCap bounds the undo/redo depth so history can't grow unbounded.
// 100 is plenty for a todo list; raise if anyone ever hits it.
const histCap = 100

// newModel builds the initial board from the configured files.
func newModel() model {
	ti := textinput.New()
	ti.Prompt = "› "
	p := store.TodoPath()
	cfg := loadConfig(store.ConfigPath())
	m := model{
		path:       p,
		items:      store.Load(p),
		archived:   store.Load(store.ArchivePath(p)),
		input:      ti,
		lastMod:    store.FileModTime(p),
		view:       cfg.view,
		density:    cfg.density,
		categories: cfg.categories,
	}
	m.resort()
	return m
}

// Run builds the board, applies an initial filter, and runs it to exit.
func Run(filter string) error {
	m := newModel()
	m.filter = filter
	m.clamp()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
