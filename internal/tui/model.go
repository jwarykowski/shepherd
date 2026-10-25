// Package tui is shepherd's interactive Bubble Tea board. It depends on todo
// (domain) and store (persistence); nothing depends on it but main.
package tui

import (
	"fmt"
	"os"
	"shepherd/internal/store"
	"shepherd/internal/theme"
	"shepherd/internal/todo"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const (
	appName    = "shepherd"
	appEmoji   = "𓋾"
	padX       = 2
	padY       = 1
	noteHeight = 5 // visible rows in the note textarea editor
)

type config struct {
	view       viewMode
	density    density
	categories []string
	statuses   []string          // ordered, normalised so "done" is present and last
	autosave   int               // seconds of idle before autosaving; 0 disables
	hideFooter bool              // start with the list footer (help grid + version) hidden
	mouse      bool              // wheel scrolling and click-to-select (off: the terminal keeps text selection)
	theme      string            // built-in theme name (see theme.Names)
	themeKV    map[string]string // theme.<token> colour overrides, kept verbatim from the file
	keyKV      map[string]string // key.<action> binding overrides (see applyKeys)
}

// loadConfig reads a tiny key=value config (leniently TOML-ish):
//
//	view = table
//	categories = ["work", "home", "personal"]   # or: work, home, personal
func loadConfig(path string) config {
	c := config{autosave: 60, statuses: []string{"open", "done"}}
	kv := store.ScanKVFile(path)
	val := func(k string) string { return strings.ToLower(strings.Trim(kv[k], `"`)) }
	switch val("view") {
	case "priority":
		c.view = viewPriority
	case "tag":
		c.view = viewTag
	case "table":
		c.view = viewTable
	case "lane":
		c.view = viewLane
	default:
		c.view = viewCategory
	}
	if val("density") == "comfort" {
		c.density = comfort
	}
	c.categories = store.ParseList(kv["categories"], false)
	if ss := store.ParseList(kv["statuses"], true); len(ss) > 0 {
		c.statuses = ss
	}
	if n, err := strconv.Atoi(strings.Trim(kv["autosave"], `"`)); err == nil {
		c.autosave = n
	}
	c.hideFooter = val("hidefooter") == "true"
	c.mouse = val("mouse") == "true"
	c.theme = val("theme")
	if !theme.Valid(c.theme) {
		c.theme = theme.Default
	}
	for k, v := range kv {
		if strings.HasPrefix(k, "theme.") {
			if c.themeKV == nil {
				c.themeKV = map[string]string{}
			}
			c.themeKV[k] = v
		}
		if strings.HasPrefix(k, "key.") {
			if c.keyKV == nil {
				c.keyKV = map[string]string{}
			}
			c.keyKV[k] = v
		}
	}
	c.statuses = normalizeStatuses(c.statuses)
	return c
}

// managedConfigKeys are the config.toml keys the settings UI owns and rewrites.
var managedConfigKeys = map[string]bool{
	"view": true, "density": true, "autosave": true,
	"categories": true, "statuses": true, "hidefooter": true, "mouse": true, "theme": true,
}

// saveConfig writes the managed config keys to config.toml and keeps every
// other line: the same file carries keys the settings UI has no control for
// (open.sh's placement and direction) and the user's comments.
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
	fmt.Fprintf(&b, "hidefooter = %v\n", c.hideFooter)
	fmt.Fprintf(&b, "mouse = %v\n", c.mouse)
	fmt.Fprintf(&b, "theme = %q\n", c.theme)
	if old, err := os.ReadFile(path); err == nil {
		keepUnmanaged(&b, string(old))
	}
	return store.WriteFile(path, []byte(b.String()))
}

// keepUnmanaged copies old's lines into b, minus blank lines and the managed
// keys saveConfig has just written. A managed array written across several
// lines is skipped up to its closing bracket, so no orphaned lines are left.
func keepUnmanaged(b *strings.Builder, old string) {
	inArray := false
	for ln := range strings.SplitSeq(old, "\n") {
		if inArray {
			inArray = !strings.Contains(ln, "]")
			continue
		}
		k, v, isKV := strings.Cut(ln, "=")
		if isKV && managedConfigKeys[strings.TrimSpace(k)] {
			v = strings.TrimSpace(v)
			inArray = strings.HasPrefix(v, "[") && !strings.Contains(v, "]")
			continue
		}
		if strings.TrimSpace(ln) == "" {
			continue
		}
		b.WriteString(ln + "\n")
	}
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
	modeTags
	modeDue
	modeDefer
	modeLink
	modeFilter
	modeDetail
	modeHelp
	modeArchive
	modeBoards
	modeSettings
	modeSettingEdit
	modeBoardNew
	modeBoardDir    // capture/edit a board's working dir (creation, or 'e' in the detail view)
	modeBoardDetail // board detail: name, dir, source + archive paths, counts
	modeBoardRename
	modeConfirmDelete
	modeTheme // quick theme picker over the board
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
	viewTag                      // grouped under the item's first tag
	viewTable                    // flat bubbles/table
	viewLane                     // kanban columns, one per configured status
	viewBoard                    // grouped by source board (global view only)
)

// viewCount is how many views `v` cycles through: every view on a board, plus
// viewBoard in the read-only global view. Keep viewBoard last in the iota so the
// two counts stay a simple modulus.
const (
	viewCount       = int(viewBoard)
	viewCountGlobal = viewCount + 1
)

var viewName = map[viewMode]string{viewCategory: "category", viewPriority: "priority", viewTag: "tag", viewTable: "table", viewLane: "lane", viewBoard: "board"}

type model struct {
	path          string
	items         []todo.Item
	archived      []todo.Item // loaded from archive.md, searched when filtering
	arcRows       []todo.Item // archive browse set (modeArchive); all boards' when global
	arcCur        int         // cursor into arcRows
	cursor        int         // index into the VISIBLE subset, not items
	lane          int         // active column in viewLane (index into statuses)
	filter        string
	mode          mode
	view          viewMode
	input         textinput.Model
	note          textarea.Model // multi-line editor for the note field (modeNote)
	noteEditing   bool           // a change was captured this note session (one undo snapshot)
	noteRef       rowRef         // the row the note editor opened on; a filter can hide it mid-edit
	w             int
	height        int
	past          [][]todo.Item     // undo stack (snapshots before each mutation)
	future        [][]todo.Item     // redo stack (snapshots undone)
	dirty         bool              // cached: board differs from disk; recomputed per event
	saved         string            // fingerprint of the board as last saved to disk
	lastEdit      time.Time         // when the last mutation happened (debounce autosave)
	autosaveEvery time.Duration     // idle gap before autosaving; 0 disables
	lastMod       time.Time         // todo file mtime we last saw
	base          []todo.Item       // the board as last read from or written to disk (merge base)
	helpScroll    int               // scroll offset in the help page
	categories    []string          // configured categories (tab-cycle in category mode)
	catIdx        int               // cursor into categories while cycling
	statuses      []string          // configured statuses, ordered, "done" last (Tab-cycle in list)
	density       density           // spacing mode
	global        bool              // read-only aggregate across all boards
	board         string            // the board to return to when leaving global
	projRows      []store.Board     // board list for the picker (modeBoards)
	projCounts    [][2]int          // open/total per projRows entry, counted once per listing
	projCur       int               // cursor into projRows
	projArchived  bool              // picker is showing archived boards (unarchive-only)
	projNotice    string            // transient picker error (e.g. invalid/duplicate board name)
	keyNotice     string            // key.<action> overrides that were ignored; cleared on the next key
	pending       string            // first key of a sequence awaiting its second (see chordKey)
	projPending   string            // board whose working dir is being set (modeBoardDir)
	projDirEdit   bool              // dir editor opened from the detail view (empty clears, return to detail); false = creation flow (empty skips)
	fieldEdit     bool              // a field editor was opened from the item detail view, so save/cancel returns there instead of the list
	settingsCur   int               // cursor into the settings rows (modeSettings)
	themeWas      string            // theme to restore when the picker is cancelled (modeTheme)
	hideFooter    bool              // hide the list footer (help grid + version line)
	light         bool              // the terminal reported a light background
	kbdEnhanced   bool              // the terminal tells shift+enter from enter (Kitty keyboard protocol)
	mouse         bool              // mouse reporting on (config: mouse = true)
	lastClick     rowClickMsg       // the previous row click, for double-click
	theme         string            // active built-in theme
	themeKV       map[string]string // theme.<token> overrides from config.toml
	sty           *styles           // coloured styles for theme + background; nil = defaults
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
		hideFooter: m.hideFooter,
		mouse:      m.mouse,
		theme:      m.theme,
		themeKV:    m.themeKV,
	}
}

// resort orders items for the active view.
func (m *model) resort() {
	switch m.view {
	case viewBoard:
		todo.SortBySource(m.items)
	case viewTag:
		todo.SortByTag(m.items)
	default:
		todo.Sort(m.items, m.view == viewPriority)
	}
}

// fingerprint is an order-independent snapshot of board content, so the saved
// indicator reflects real differences from disk, not row order (switching view
// reorders items but changes nothing on disk). Naive per-item serialise+sort;
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

// newModel builds the initial board for the given board ("" = default).
func newModel(board string) model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.SetVirtualCursor(false) // View places the real cursor (IME, screen readers)
	na := textarea.New()
	na.SetVirtualCursor(false)
	na.ShowLineNumbers = false
	na.Prompt = ""
	na.CharLimit = 0 // notes can be long
	p := store.TodoPathFor(board)
	cfg := loadConfig(store.ConfigPath())
	m := model{
		path:          p,
		board:         board,
		input:         ti,
		note:          na,
		view:          cfg.view,
		density:       cfg.density,
		categories:    cfg.categories,
		statuses:      cfg.statuses,
		autosaveEvery: time.Duration(cfg.autosave) * time.Second,
		hideFooter:    cfg.hideFooter,
		mouse:         cfg.mouse,
		theme:         cfg.theme,
		themeKV:       cfg.themeKV,
		keyNotice:     strings.Join(applyKeys(cfg.keyKV), "; "),
	}
	m.applyTheme()
	m.reload()
	if slices.ContainsFunc(m.items, func(it todo.Item) bool { return it.ID == "" }) {
		m.save() // backfill ids now: merging another writer's edits matches items by id
	}
	return m
}

// reload replaces the board with what's on disk and makes that the merge base.
func (m *model) reload() {
	m.load(store.FileModTime(m.path), store.Load(m.path), store.Load(store.ArchivePath(m.path)))
}

// load installs a board read from disk at mod as the new clean baseline.
func (m *model) load(mod time.Time, items, archived []todo.Item) {
	m.lastMod = mod
	m.items = items
	m.archived = archived
	m.base = append([]todo.Item{}, todo.Clone(m.items)...) // non-nil: an empty board is a base too
	m.resort()
	m.clamp()
	m.markSaved()
}

// save writes the board under the board lock. If another process (the CLI, an
// agent) wrote the file since we last read it, its edits are merged in by item
// id first rather than overwritten. Errors are dropped: there's no mid-render
// recovery, and the board stays dirty in memory.
func (m *model) save() {
	err := store.WithLock(m.path, func() error {
		if m.base != nil && store.FileModTime(m.path).After(m.lastMod) {
			m.items = todo.Merge(m.base, m.items, store.Load(m.path))
			m.resort()
			m.clamp()
		}
		if err := store.Save(m.path, m.items); err != nil {
			return err
		}
		m.lastMod = store.FileModTime(m.path) // still under the lock: no other write in between
		m.base = append([]todo.Item{}, todo.Clone(m.items)...)
		return nil
	})
	if err == nil {
		m.markSaved()
	}
}

// setBoards fills the picker rows and counts each board's items once, rather
// than re-reading every board file on every render.
func (m *model) setBoards(rows []store.Board) {
	m.projRows = rows
	m.projCounts = make([][2]int, len(rows))
	for i, b := range rows {
		open, total := store.BoardCounts(b.Path)
		m.projCounts[i] = [2]int{open, total}
	}
}

// loadGlobal replaces the model's items with the read-only aggregate across
// every board, grouped by board. Shared by --all launch and the A toggle.
func (m *model) loadGlobal() {
	m.global = true
	m.items = store.LoadAll()
	m.archived = nil
	m.view = viewBoard
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
		nm := newModel(m.board)
		nm.adoptTerminal(*m)
		nm.filter, nm.density, nm.hideFooter = m.filter, m.density, m.hideFooter
		nm.clamp()
		*m = nm
		return
	}
	if m.dirty {
		m.save()
	}
	m.loadGlobal()
}

// Run builds the board for a board (or the global view), applies an initial
// filter, and runs it to exit.
func Run(filter, board string, global bool) error {
	m := newModel(board)
	if global {
		m.loadGlobal()
	}
	m.filter = filter
	m.clamp()
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
