// herdr-todo — interactive todo board for herdr, as a Bubble Tea TUI.
// Bubble Tea owns the alt-screen, input, and redraw loop, so none of the
// terminal wrangling that plagued the shell version lives here.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tsFormat    = "02-01-2006 15:04"
	appSubtitle = "your todos herded"
	padX        = 2
	padY        = 1
)

// now / today are package vars so tests can pin them.
var (
	now   = func() string { return time.Now().Format(tsFormat) }
	today = func() string { return time.Now().Format(dateFormat) }
)

const (
	dateFormat = "2006-01-02" // ISO on disk: sorts lexically, so due ordering works
	dmyDate    = "02-01-2006" // day-month-year (DMY), for display + input
)

// displayDate renders an ISO date as day-month-year DD-MM-YYYY; raw if unparseable.
func displayDate(iso string) string {
	if t, err := time.Parse(dateFormat, iso); err == nil {
		return t.Format(dmyDate)
	}
	return iso
}

type item struct {
	done     bool
	prio     byte // 'H','M','L', or 0 for none
	text     string
	category string
	created  string
	due      string // YYYY-MM-DD, or empty
	note     string
}

var (
	lineRE = regexp.MustCompile(`^- \[([ xX])\] (?:\(([HMLhml])\) )?(.*)$`)
	metaRE = regexp.MustCompile(`^  (created|note|category|due): (.*)$`)
)

func todoPath() string {
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

func configPath() string {
	if p := os.Getenv("SHEPHERD_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(todoPath()), "config.toml")
}

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

func load(path string) []item {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []item
	for _, ln := range strings.Split(string(data), "\n") {
		if m := lineRE.FindStringSubmatch(ln); m != nil {
			it := item{done: m[1] != " ", text: m[3]}
			if m[2] != "" {
				it.prio = strings.ToUpper(m[2])[0]
			}
			items = append(items, it)
			continue
		}
		if m := metaRE.FindStringSubmatch(ln); m != nil && len(items) > 0 {
			last := &items[len(items)-1]
			switch m[1] {
			case "created":
				last.created = m[2]
			case "category":
				last.category = strings.ToLower(m[2])
			case "due":
				last.due = m[2]
			case "note":
				last.note = m[2]
			}
		}
	}
	return items
}

func serialize(items []item) string {
	var b strings.Builder
	for _, it := range items {
		box := " "
		if it.done {
			box = "x"
		}
		tag := ""
		if it.prio != 0 {
			tag = fmt.Sprintf("(%c) ", it.prio)
		}
		fmt.Fprintf(&b, "- [%s] %s%s\n", box, tag, it.text)
		if it.created != "" {
			fmt.Fprintf(&b, "  created: %s\n", it.created)
		}
		if it.due != "" {
			fmt.Fprintf(&b, "  due: %s\n", it.due)
		}
		if it.category != "" {
			fmt.Fprintf(&b, "  category: %s\n", it.category)
		}
		if it.note != "" {
			fmt.Fprintf(&b, "  note: %s\n", strings.ReplaceAll(it.note, "\n", " "))
		}
	}
	return b.String()
}

func save(path string, items []item) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(serialize(items)), 0o644)
}

func archivePath(todo string) string {
	return filepath.Join(filepath.Dir(todo), "archive.md")
}

// appendArchive appends done items to a sibling archive.md (create if absent).
func appendArchive(todo string, items []item) error {
	f, err := os.OpenFile(archivePath(todo), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(serialize(items))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func fileModTime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func rank(p byte) int {
	switch p {
	case 'H':
		return 0
	case 'M':
		return 1
	case 'L':
		return 2
	}
	return 3
}

// catKey sorts named categories alphabetically, uncategorized last.
func catKey(c string) string {
	if c == "" {
		return "￿"
	}
	return strings.ToLower(c)
}

// dueKey sorts soonest-first; no due date sorts last.
func dueKey(d string) string {
	if d == "" {
		return "9999-99-99"
	}
	return d
}

// parseDue resolves preset keywords and relative forms to a YYYY-MM-DD date.
// Presets: today, tomorrow, week/next week, month/next month, and "+Nd".
// Empty clears; an already-valid date passes through; anything else is kept
// raw (dueLabel shows it un-flagged).
func parseDue(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	base, err := time.Parse(dateFormat, today())
	if err != nil {
		return s
	}
	switch s {
	case "today":
		return base.Format(dateFormat)
	case "tomorrow", "tmr", "tom":
		return base.AddDate(0, 0, 1).Format(dateFormat)
	case "week", "next week":
		return base.AddDate(0, 0, 7).Format(dateFormat)
	case "month", "next month":
		return base.AddDate(0, 1, 0).Format(dateFormat)
	}
	// relative: <n><unit>, unit d/w/m/y, optional leading + (e.g. 3d, 2w, 5m, 1y)
	if r := strings.TrimPrefix(s, "+"); len(r) >= 2 {
		if n, err := strconv.Atoi(r[:len(r)-1]); err == nil {
			switch r[len(r)-1] {
			case 'd':
				return base.AddDate(0, 0, n).Format(dateFormat)
			case 'w':
				return base.AddDate(0, 0, 7*n).Format(dateFormat)
			case 'm':
				return base.AddDate(0, n, 0).Format(dateFormat)
			case 'y':
				return base.AddDate(n, 0, 0).Format(dateFormat)
			}
		}
	}
	// explicit dates: accept DMY or ISO, normalize to ISO on disk
	if t, err := time.Parse(dmyDate, s); err == nil {
		return t.Format(dateFormat)
	}
	if t, err := time.Parse(dateFormat, s); err == nil {
		return t.Format(dateFormat)
	}
	return "" // unrecognized: clear rather than store garbage
}

// dueLabel renders a due date relative to today, and whether it's due/overdue.
func dueLabel(due string) (string, bool) {
	d, err := time.Parse(dateFormat, due)
	if err != nil {
		return due, false // unparseable — show raw, don't flag
	}
	t, err := time.Parse(dateFormat, today())
	if err != nil {
		return due, false
	}
	days := int(d.Sub(t).Hours() / 24)
	switch {
	case days < 0:
		return fmt.Sprintf("overdue %dd", -days), true
	case days == 0:
		return "due today", true
	case days == 1:
		return "due 1d", false
	default:
		return fmt.Sprintf("due %dd", days), false
	}
}

// pinned reports whether an item is surfaced to the top: open and past due.
func pinned(it item) bool {
	if it.done || it.due == "" {
		return false
	}
	d, err := time.Parse(dateFormat, it.due)
	if err != nil {
		return false
	}
	t, err := time.Parse(dateFormat, today())
	if err != nil {
		return false
	}
	return d.Before(t)
}

// Sort: overdue pinned first, then category, then priority, then soonest due.
func sortItems(items []item) {
	sort.SliceStable(items, func(i, j int) bool {
		if pi, pj := pinned(items[i]), pinned(items[j]); pi != pj {
			return pi
		}
		ci, cj := catKey(items[i].category), catKey(items[j].category)
		if ci != cj {
			return ci < cj
		}
		if ri, rj := rank(items[i].prio), rank(items[j].prio); ri != rj {
			return ri < rj
		}
		return dueKey(items[i].due) < dueKey(items[j].due)
	})
}

// sortByPriority: overdue pinned first, then priority, then category, then due.
func sortByPriority(items []item) {
	sort.SliceStable(items, func(i, j int) bool {
		if pi, pj := pinned(items[i]), pinned(items[j]); pi != pj {
			return pi
		}
		if ri, rj := rank(items[i].prio), rank(items[j].prio); ri != rj {
			return ri < rj
		}
		ci, cj := catKey(items[i].category), catKey(items[j].category)
		if ci != cj {
			return ci < cj
		}
		return dueKey(items[i].due) < dueKey(items[j].due)
	})
}

// ---- styles ----
var (
	dimStyle    = lipgloss.NewStyle().Faint(true)
	doneStyle   = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	boxStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	matchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	catStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	countStyle  = lipgloss.NewStyle().Faint(true)
	prioStyles  = map[byte]lipgloss.Style{
		'H': lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		'M': lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		'L': lipgloss.NewStyle().Faint(true),
	}
	prioLabel = map[byte]string{'H': "high", 'M': "medium", 'L': "low"}
)

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
	items      []item
	archived   []item // loaded from archive.md, searched when filtering
	cursor     int    // index into the VISIBLE subset, not items
	filter     string
	mode       mode
	view       viewMode
	input      textinput.Model
	w          int
	height     int
	past       [][]item  // undo stack (snapshots before each mutation)
	future     [][]item  // redo stack (snapshots undone)
	dirty      bool      // in-memory changes not yet saved
	lastMod    time.Time // todo file mtime we last saw
	helpScroll int       // scroll offset in the help page
	categories []string  // configured categories (tab-cycle in category mode)
	catIdx     int       // cursor into categories while cycling
	density    density   // spacing mode
}

// resort orders items for the active view.
func (m *model) resort() {
	if m.view == viewPriority {
		sortByPriority(m.items)
	} else {
		sortItems(m.items)
	}
}

// parseQuickAdd splits an add line into text plus @category, !h/!m/!l priority,
// and due:<preset> tokens. Unrecognized tokens stay part of the text.
func parseQuickAdd(s string) item {
	it := item{created: now()}
	var words []string
	for _, tok := range strings.Fields(s) {
		switch {
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			it.category = strings.ToLower(tok[1:])
		case strings.HasPrefix(tok, "!") && len(tok) == 2 && strings.ContainsRune("hHmMlL", rune(tok[1])):
			it.prio = strings.ToUpper(tok[1:])[0]
		case strings.HasPrefix(tok, "due:") && len(tok) > 4:
			it.due = parseDue(tok[4:])
		default:
			words = append(words, tok)
		}
	}
	it.text = strings.Join(words, " ")
	return it
}

// histCap bounds the undo/redo depth so history can't grow unbounded.
// 100 is plenty for a todo list; raise if anyone ever hits it.
const histCap = 100

func newModel() model {
	ti := textinput.New()
	ti.Prompt = "› "
	p := todoPath()
	cfg := loadConfig(configPath())
	m := model{
		path:       p,
		items:      load(p),
		archived:   load(archivePath(p)),
		input:      ti,
		lastMod:    fileModTime(p),
		view:       cfg.view,
		density:    cfg.density,
		categories: cfg.categories,
	}
	m.resort()
	return m
}

func (m model) Init() tea.Cmd { return tick() }

// beforeMutate pushes the current state onto the undo stack, clears the redo
// stack (a fresh edit invalidates the redo future), and marks the model dirty.
func (m *model) beforeMutate() {
	m.past = append(m.past, append([]item(nil), m.items...))
	if len(m.past) > histCap {
		m.past = m.past[len(m.past)-histCap:]
	}
	m.future = nil
	m.dirty = true
}

// editorDoneMsg is sent when the external editor exits.
type editorDoneMsg struct{}

// tickMsg drives the periodic external-change check.
type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// openEditor saves the list, then hands the terminal to $EDITOR (fallback vi)
// on the markdown file. sh -c so EDITOR may carry flags (e.g. "code -w").
func (m model) openEditor() tea.Cmd {
	_ = save(m.path, m.items)
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command("sh", "-c", editor+` "$0"`, m.path)
	return tea.ExecProcess(c, func(error) tea.Msg { return editorDoneMsg{} })
}

// matchItem reports whether an item matches a lowercased filter query.
func matchItem(it item, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(it.text), q) ||
		strings.Contains(strings.ToLower(it.note), q) ||
		strings.Contains(strings.ToLower(it.category), q) ||
		strings.Contains(strings.ToLower(it.due), q)
}

// visible returns the item indices matching the current filter, in order.
func (m model) visible() []int {
	idx := make([]int, 0, len(m.items))
	q := strings.ToLower(m.filter)
	for i, it := range m.items {
		if matchItem(it, q) {
			idx = append(idx, i)
		}
	}
	return idx
}

// archivedMatches returns archived items matching the active filter (read-only).
func (m model) archivedMatches() []item {
	if m.filter == "" {
		return nil
	}
	q := strings.ToLower(m.filter)
	var out []item
	for _, it := range m.archived {
		if matchItem(it, q) {
			out = append(out, it)
		}
	}
	return out
}

// groupOf returns a stable group id (for change detection) and display label
// for an item under the active view; overdue items form a pinned top group.
func (m model) groupOf(it item) (id, label string) {
	if pinned(it) {
		return "\x00pin", "⚠ overdue"
	}
	if m.view == viewPriority {
		if lbl, ok := prioLabel[it.prio]; ok {
			return fmt.Sprintf("p%d", rank(it.prio)), lbl + " priority"
		}
		return "p9", "no priority"
	}
	if it.category == "" {
		return "c\x01", "uncategorized"
	}
	return "c" + catKey(it.category), it.category
}

// groupCount returns done/total for the group an item belongs to. Pinned items
// are excluded from their category/priority group (they show in overdue).
func (m model) groupCount(it item) (done, total int) {
	switch {
	case pinned(it):
		for _, x := range m.items {
			if pinned(x) {
				total++
			}
		}
	case m.view == viewPriority:
		for _, x := range m.items {
			if !pinned(x) && x.prio == it.prio {
				total++
				if x.done {
					done++
				}
			}
		}
	default:
		for _, x := range m.items {
			if !pinned(x) && x.category == it.category {
				total++
				if x.done {
					done++
				}
			}
		}
	}
	return
}

// sel is the real items index under the cursor, or -1 if the visible list is empty.
func (m model) sel() int {
	v := m.visible()
	if len(v) == 0 {
		return -1
	}
	if m.cursor >= len(v) {
		return v[len(v)-1]
	}
	return v[m.cursor]
}

func (m *model) clamp() {
	n := len(m.visible())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// place moves the cursor onto the visible position of a given item value
// (used after a sort re-orders the list).
func (m *model) place(target item) {
	for p, i := range m.visible() {
		if m.items[i] == target {
			m.cursor = p
			return
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.height = msg.Width, msg.Height
		return m, nil
	case editorDoneMsg:
		// Reload from disk — the file may have changed under us.
		m.items = load(m.path)
		m.archived = load(archivePath(m.path))
		m.resort()
		m.clamp()
		m.lastMod = fileModTime(m.path)
		m.dirty = false
		return m, nil
	case tickMsg:
		// only auto-reload external edits when we have no pending in-memory
		// changes, so a dotfile sync can't clobber the user's work.
		if !m.dirty {
			if mt := fileModTime(m.path); mt.After(m.lastMod) {
				m.items = load(m.path)
				m.archived = load(archivePath(m.path))
				m.resort()
				m.clamp()
				m.lastMod = mt
			}
		}
		return m, tick()
	case tea.KeyMsg:
		switch m.mode {
		case modeAdd, modeEdit, modeNote, modeCategory, modeDue, modeFilter:
			return m.updateInput(msg)
		case modeDetail:
			return m.updateDetail(msg)
		case modeHelp:
			return m.updateHelp(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.visible()
	idx := m.sel()
	switch msg.String() {
	case "q", "ctrl+c":
		_ = save(m.path, m.items)
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case " ", "enter":
		if idx >= 0 {
			m.beforeMutate()
			m.items[idx].done = !m.items[idx].done
		}
	case "d":
		if idx >= 0 {
			m.mode = modeDetail
		}
	case "x":
		if idx >= 0 {
			m.beforeMutate()
			m.items = append(m.items[:idx], m.items[idx+1:]...)
			m.clamp()
		}
	case "c":
		var done, kept []item
		for _, it := range m.items {
			if it.done {
				done = append(done, it)
			} else {
				kept = append(kept, it)
			}
		}
		if len(done) > 0 {
			m.beforeMutate()
			_ = appendArchive(m.path, done)
			m.archived = append(m.archived, done...)
			m.items = kept
			m.clamp()
		}
	case "U":
		if n := len(m.past); n > 0 {
			m.future = append(m.future, append([]item(nil), m.items...))
			m.items = m.past[n-1]
			m.past = m.past[:n-1]
			m.dirty = true
			m.clamp()
		}
	case "ctrl+r":
		if n := len(m.future); n > 0 {
			m.past = append(m.past, append([]item(nil), m.items...))
			m.items = m.future[n-1]
			m.future = m.future[:n-1]
			m.dirty = true
			m.clamp()
		}
	case "h", "m", "l":
		if idx >= 0 {
			m.beforeMutate()
			cur := m.items[idx]
			p := strings.ToUpper(msg.String())[0]
			if cur.prio == p { // same priority again clears it
				cur.prio = 0
			} else {
				cur.prio = p
			}
			m.items[idx] = cur
			m.resort()
			m.place(cur)
		}
	case "v":
		var cur item
		has := idx >= 0
		if has {
			cur = m.items[idx]
		}
		m.view = (m.view + 1) % 3
		m.resort()
		if has {
			m.place(cur)
		}
		m.clamp()
	case "?":
		m.mode = modeHelp
	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.Placeholder = "filter"
		m.input.Focus()
	case "esc":
		m.filter = ""
		m.clamp()
	case "a":
		m.mode = modeAdd
		m.input.SetValue("")
		m.input.Placeholder = "todo text  @category  !h|!m|!l  due:tomorrow"
		m.input.Focus()
	case "u":
		if idx >= 0 {
			m.mode = modeEdit
			m.input.SetValue(m.items[idx].text)
			m.input.Placeholder = ""
			m.input.Focus()
		}
	case "g":
		if idx >= 0 {
			m.mode = modeCategory
			m.input.SetValue(m.items[idx].category)
			m.input.Placeholder = "category"
			if len(m.categories) > 0 {
				m.input.Placeholder = "category · tab: " + strings.Join(m.categories, "/")
			}
			m.catIdx = 0
			m.input.Focus()
		}
	case "t":
		if idx >= 0 {
			m.mode = modeDue
			m.input.SetValue(m.items[idx].due)
			m.input.Placeholder = "today · tomorrow · 3d · 2w · 5m · 1y · DD-MM-YYYY"
			m.input.Focus()
		}
	case "ctrl+e":
		return m, m.openEditor()
	}
	return m, nil
}

func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = modeList
		m.helpScroll = 0
	case "j", "down":
		if m.helpScroll < m.helpMaxScroll() {
			m.helpScroll++
		}
	case "k", "up":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case "g", "home":
		m.helpScroll = 0
	case "G", "end":
		m.helpScroll = m.helpMaxScroll()
	}
	return m, nil
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	idx := m.sel()
	switch msg.String() {
	case "esc", "q", "d":
		m.mode = modeList
	case "e", "n":
		if idx >= 0 {
			m.mode = modeNote
			m.input.SetValue(m.items[idx].note)
			m.input.Placeholder = "note"
			m.input.Focus()
		}
	case "space", " ":
		if idx >= 0 {
			m.beforeMutate()
			m.items[idx].done = !m.items[idx].done
		}
	}
	return m, nil
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	idx := m.sel()
	if msg.String() == "tab" && m.mode == modeCategory && len(m.categories) > 0 {
		m.input.SetValue(m.categories[m.catIdx])
		m.input.CursorEnd()
		m.catIdx = (m.catIdx + 1) % len(m.categories)
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.input.Blur()
		switch m.mode {
		case modeNote:
			m.mode = modeDetail
		case modeFilter:
			m.filter = ""
			m.clamp()
			m.mode = modeList
		default:
			m.mode = modeList
		}
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		switch m.mode {
		case modeAdd:
			if it := parseQuickAdd(v); it.text != "" {
				m.beforeMutate()
				m.items = append(m.items, it)
				m.resort()
				m.place(it)
			}
			m.mode = modeList
		case modeEdit:
			if v != "" && idx >= 0 {
				m.beforeMutate()
				m.items[idx].text = v
			}
			m.mode = modeList
		case modeNote:
			if idx >= 0 {
				m.beforeMutate()
				m.items[idx].note = v // empty clears
			}
			m.mode = modeDetail
		case modeCategory:
			if idx >= 0 {
				m.beforeMutate()
				cur := m.items[idx]
				cur.category = strings.ToLower(v) // empty clears
				m.items[idx] = cur
				m.resort()
				m.place(cur)
			}
			m.mode = modeList
		case modeDue:
			if idx >= 0 {
				m.beforeMutate()
				cur := m.items[idx]
				cur.due = parseDue(v) // presets/relative resolved; empty clears
				m.items[idx] = cur
				m.resort()
				m.place(cur)
			}
			m.mode = modeList
		case modeFilter:
			m.mode = modeList // keep the filter applied
		}
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.mode == modeFilter { // live filter
		m.filter = m.input.Value()
		m.clamp()
	}
	return m, cmd
}

func (m model) View() string {
	var content string
	switch {
	case m.mode == modeHelp:
		content = m.helpView()
	case m.mode == modeDetail || m.mode == modeNote:
		content = m.detailView()
	case m.view == viewTable:
		content = m.tableView()
	default:
		content = m.listView()
	}
	return lipgloss.NewStyle().Padding(m.density.padY(), m.density.padX()).Render(content)
}

// width is the inner content width, i.e. the pane minus horizontal padding.
func (m model) width() int {
	w := m.w
	if w == 0 {
		w = 40
	}
	w -= 2 * m.density.padX()
	if w < 10 {
		w = 10
	}
	return w
}

// innerHeight is the pane height minus vertical padding (0 = unknown).
func (m model) innerHeight() int {
	if m.height == 0 {
		return 0
	}
	h := m.height - 2*m.density.padY()
	if h < 1 {
		h = 1
	}
	return h
}

func (m model) listView() string {
	w := m.width()
	vis := m.visible()
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	if len(vis) == 0 {
		if m.filter != "" {
			b.WriteString(dimStyle.Render("(no matches)") + "\n")
		} else {
			b.WriteString(dimStyle.Render("(empty — press a to add)") + "\n")
		}
	}
	lastGroup := "\x00" // sentinel so the first group always prints a header
	for pos, i := range vis {
		it := m.items[i]
		if gid, label := m.groupOf(it); gid != lastGroup {
			if lastGroup != "\x00" {
				b.WriteString("\n") // padding below the previous group
			}
			done, total := m.groupCount(it)
			cnt := countStyle.Render(fmt.Sprintf("%d/%d", done, total))
			left := catStyle.Render(label)
			gap := w - lipgloss.Width(left) - lipgloss.Width(cnt)
			if gap < 1 {
				gap = 1
			}
			b.WriteString(left + strings.Repeat(" ", gap) + cnt + "\n")
			lastGroup = gid
		}
		box := "○"
		text := it.text
		if it.done {
			box = "✓"
			text = doneStyle.Render(text)
		}
		mark := " "
		if pos == m.cursor {
			mark = cursorStyle.Render(" ")
		}
		// right cluster: due (left) then priority label flush far-right.
		// Overdue rows live under the ⚠ overdue group, so don't repeat "overdue" on the line.
		label := ""
		if it.due != "" && !pinned(it) {
			lbl, over := dueLabel(it.due)
			st := dimStyle
			if over {
				st = prioStyles['H'] // red for due/overdue
			}
			label = st.Render(lbl)
		}
		if lbl, ok := prioLabel[it.prio]; ok {
			if label != "" {
				label += "  "
			}
			label += prioStyles[it.prio].Render(lbl)
		}
		left := fmt.Sprintf("%s  %s %s", mark, boxStyle.Render(box), text) // 2-space indent under header
		gap := w - lipgloss.Width(left) - lipgloss.Width(label)
		if gap < 1 {
			gap = 1
		}
		b.WriteString(left + strings.Repeat(" ", gap) + label + "\n")
		if m.density == comfort {
			b.WriteString("\n") // roomier rows
		}
	}
	if am := m.archivedMatches(); len(am) > 0 {
		b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("archive · %d match", len(am))) + "\n")
		for _, it := range am {
			b.WriteString("  " + doneStyle.Render(it.text) + "\n")
		}
	}
	return m.frame(b.String(), m.listFooter())
}

// count returns the done and total item counts across the whole board.
func (m model) count() (done, total int) {
	for _, it := range m.items {
		if it.done {
			done++
		}
	}
	return done, len(m.items)
}

// header renders the list/table title block: the active view flush-right.
func (m model) header() string {
	done, total := m.count()
	return m.headerWith(viewName[m.view], done, total)
}

// headerWith renders the shared title block used by every view: the subtitle
// (left) with the context label + done/total count (and active filter)
// flush-right, then a full-width rule.
func (m model) headerWith(context string, done, total int) string {
	w := m.width()
	left := dimStyle.Render(appSubtitle)

	right := dimStyle.Render(context) + "  " + countStyle.Render(fmt.Sprintf("%d/%d", done, total))
	if m.filter != "" || m.mode == modeFilter {
		right = matchStyle.Render("/"+m.filter) + "  " + right
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right + "\n" +
		dimStyle.Render(strings.Repeat("─", w))
}

// listFooter: a full-width rule, then either the active input line or the
// grouped multi-line key help.
func (m model) listFooter() string {
	rule := dimStyle.Render(strings.Repeat("─", m.width()))
	switch m.mode {
	case modeFilter:
		return rule + "\n" + m.input.View() + "  " + dimStyle.Render("(filter: enter=apply esc=clear)")
	case modeAdd, modeEdit, modeCategory, modeDue:
		verb := map[mode]string{modeAdd: "add", modeEdit: "edit", modeCategory: "category", modeDue: "due"}[m.mode]
		return rule + "\n" + m.input.View() + "  " + dimStyle.Render("("+verb+": enter=save esc=cancel)")
	default:
		return rule + "\n" + m.helpGrid()
	}
}

// helpGrid renders the key hints as an aligned 3-column table.
func (m model) helpGrid() string {
	cells := [][2]string{
		{"move", "j/k"}, {"toggle", "space"}, {"detail", "d"},
		{"add", "a"}, {"edit", "u"}, {"filter", "/"},
		{"prio", "h/m/l"}, {"due", "t"}, {"cat", "g"},
		{"view", "v"}, {"undo", "U"}, {"redo", "^r"},
		{"del", "x"}, {"arch", "c"}, {"editor", "^e"},
		{"help", "?"}, {"quit", "q"},
	}
	var b strings.Builder
	for i, c := range cells {
		fmt.Fprintf(&b, "%-7s%-7s", c[0], c[1])
		if i%3 == 2 && i != len(cells)-1 {
			b.WriteByte('\n')
		}
	}
	return dimStyle.Render(b.String())
}

// tableView renders the flat bubbles/table view. Nav still comes from our own
// j/k (m.cursor); the table is driven read-only via SetCursor.
func (m model) tableView() string {
	w := m.width()
	vis := m.visible()
	catW, dueW := 12, 11
	taskW := w - (2 + 2 + catW + dueW + 8) // marks + fixed cols + cell padding
	if taskW < 10 {
		taskW = 10
	}
	cols := []table.Column{
		{Title: "✓", Width: 1},
		{Title: "!", Width: 1},
		{Title: "task", Width: taskW},
		{Title: "category", Width: catW},
		{Title: "due", Width: dueW},
	}
	rows := make([]table.Row, 0, len(vis))
	for _, i := range vis {
		it := m.items[i]
		box := "○"
		if it.done {
			box = "✓"
		}
		p := " "
		if it.prio != 0 {
			p = strings.ToLower(string(it.prio))
		}
		due := ""
		if it.due != "" {
			due, _ = dueLabel(it.due)
		}
		rows = append(rows, table.Row{box, p, it.text, it.category, due})
	}
	head, footer := m.header(), m.listFooter()
	// derive table height from the actual header/footer sizes (+1 for the
	// table's own column-header row) rather than a hard-coded constant.
	overhead := lines(head) + lines(footer) + 1
	h := m.innerHeight() - overhead
	if h < 3 {
		h = 3
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithHeight(h),
	)
	t.SetCursor(m.cursor)
	st := table.DefaultStyles()
	// plain reverse-video highlight (matches the list cursor); drop the
	// bubbles default pink foreground.
	st.Selected = lipgloss.NewStyle().Bold(true).Reverse(true)
	t.SetStyles(st)
	return m.frame(head+"\n"+t.View(), footer)
}

// helpBody returns the full help content as individual (already-wrapped) lines.
func (m model) helpBody() []string {
	w := m.width()
	wrap := lipgloss.NewStyle().Width(w - 2)
	var lines []string
	sec := func(s string) { lines = append(lines, catStyle.Render(s)) }
	line := func(s string) {
		for _, ln := range strings.Split(wrap.Render(s), "\n") {
			lines = append(lines, "  "+ln)
		}
	}
	blank := func() { lines = append(lines, "") }

	line("An interactive todo board in a herdr pane, backed by a plain markdown file. Changes save on quit; the board reloads external edits automatically when you have nothing unsaved.")
	blank()
	sec("adding")
	line("a — add. Inline syntax: text @category !h|!m|!l due:tomorrow")
	line("u — edit the selected item's text")
	blank()
	sec("organise")
	line("h/m/l — set priority high/medium/low (same key again clears)")
	line("g — set category · t — set due date")
	line("space — toggle done · x — delete · c — archive done")
	blank()
	sec("due dates")
	line("today · tomorrow · Nd/Nw/Nm/Ny (e.g. 3d, 2w) · DD-MM-YYYY. Anything unrecognised clears the date. Overdue items are pinned to a group at the top.")
	blank()
	sec("view & find")
	line("v — cycle view: category / priority / table")
	line("/ — filter text, note, category, due (also greps the archive)")
	line("d — detail view · ? — this help")
	blank()
	sec("history & files")
	line("U — undo · ^r — redo (multi-level)")
	line("^e — open the markdown file in $EDITOR")
	line("q — save + quit")
	return lines
}

// helpViewport is how many body lines fit between the header and footer.
func (m model) helpViewport() int {
	ih := m.innerHeight()
	if ih == 0 {
		return len(m.helpBody()) // unknown size: show everything
	}
	vh := ih - 5 // subtitle + rule + blank, footer rule + hint
	if vh < 1 {
		vh = 1
	}
	return vh
}

// helpMaxScroll is the largest valid scroll offset.
func (m model) helpMaxScroll() int {
	if max := len(m.helpBody()) - m.helpViewport(); max > 0 {
		return max
	}
	return 0
}

// helpView renders the scrollable help page.
func (m model) helpView() string {
	w := m.width()
	lines := m.helpBody()
	vh := m.helpViewport()
	off := m.helpScroll
	if off > m.helpMaxScroll() {
		off = m.helpMaxScroll()
	}
	end := off + vh
	if end > len(lines) {
		end = len(lines)
	}

	done, total := m.count()
	var b strings.Builder
	b.WriteString(m.headerWith("help", done, total) + "\n\n")
	b.WriteString(strings.Join(lines[off:end], "\n"))

	hint := "enter to close"
	if off > 0 {
		hint = "↑ " + hint
	}
	if end < len(lines) {
		hint += " · ↓ more"
	}
	footer := dimStyle.Render(strings.Repeat("─", w)) + "\n" +
		dimStyle.Render("scroll j/k · "+hint)
	return m.frame(b.String(), footer)
}

// lines counts the visual lines in a rendered string.
func lines(s string) int { return strings.Count(s, "\n") + 1 }

// frame pins the footer to the bottom of the pane, padding the middle.
func (m model) frame(body, footer string) string {
	bodyLines := strings.Count(body, "\n")
	footLines := strings.Count(footer, "\n") + 1
	ih := m.innerHeight()
	pad := ih - bodyLines - footLines
	if ih == 0 || pad < 1 {
		pad = 1
	}
	return body + strings.Repeat("\n", pad) + footer
}

func (m model) detailView() string {
	idx := m.sel()
	if idx < 0 {
		return "no item"
	}
	it := m.items[idx]
	// count reflects the item's own category, not the whole board
	cdone, ctotal := 0, 0
	for _, x := range m.items {
		if x.category == it.category {
			ctotal++
			if x.done {
				cdone++
			}
		}
	}
	var b strings.Builder
	b.WriteString(m.headerWith("detail", cdone, ctotal) + "\n\n")

	status := "open"
	if it.done {
		status = "done"
	}
	prio := "none"
	if lbl, ok := prioLabel[it.prio]; ok {
		prio = prioStyles[it.prio].Render(lbl)
	}
	created := it.created
	if created == "" {
		created = dimStyle.Render("—")
	}

	field := func(k, v string) string {
		return dimStyle.Render(fmt.Sprintf("%-9s", k)) + v + "\n"
	}
	category := it.category
	if category == "" {
		category = dimStyle.Render("uncategorized")
	} else {
		category = catStyle.Render(category)
	}
	b.WriteString(field("task", it.text))
	b.WriteString(field("status", status))
	b.WriteString(field("priority", prio))
	b.WriteString(field("category", category))
	due := dimStyle.Render("—")
	if it.due != "" {
		lbl, over := dueLabel(it.due)
		st := dimStyle
		if over {
			st = prioStyles['H']
		}
		due = fmt.Sprintf("%s  %s", displayDate(it.due), st.Render(lbl))
	}
	b.WriteString(field("due", due))
	b.WriteString(field("created", created))
	b.WriteString("\n" + dimStyle.Render("note") + "\n")
	if m.mode == modeNote {
		b.WriteString(m.input.View() + "\n")
	} else if it.note != "" {
		b.WriteString(it.note + "\n")
	} else {
		b.WriteString(dimStyle.Render("(none — press e to add)") + "\n")
	}

	rule := dimStyle.Render(strings.Repeat("─", m.width()))
	var help string
	if m.mode == modeNote {
		help = rule + "\n" + dimStyle.Render("note: enter=save · esc=cancel")
	} else {
		help = rule + "\n" + dimStyle.Render("e edit note   space toggle   d/esc/q back")
	}
	return m.frame(b.String(), help)
}

func main() {
	// A leading non-flag arg switches to the command API (see cli.go);
	// bare `shepherd` and `shepherd --filter …` stay the interactive board.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		os.Exit(runCLI(os.Args[1], os.Args[2:]))
	}

	filter := flag.String("filter", os.Getenv("SHEPHERD_FILTER"), "start with this filter applied (matches text/note/category/due)")
	flag.Parse()

	m := newModel()
	m.filter = *filter
	m.clamp()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
