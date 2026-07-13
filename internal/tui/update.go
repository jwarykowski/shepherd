package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

func (m model) Init() tea.Cmd { return tick() }

// beforeMutate pushes the current state onto the undo stack, clears the redo
// stack (a fresh edit invalidates the redo future), and marks the model dirty.
func (m *model) beforeMutate() {
	m.past = append(m.past, append([]todo.Item(nil), m.items...))
	if len(m.past) > histCap {
		m.past = m.past[len(m.past)-histCap:]
	}
	m.future = nil
	m.lastEdit = time.Now()
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
	_ = store.Save(m.path, m.items)
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command("sh", "-c", editor+` "$0"`, m.path)
	return tea.ExecProcess(c, func(error) tea.Msg { return editorDoneMsg{} })
}

// matchItem reports whether an item matches a lowercased filter query.
func matchItem(it todo.Item, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(it.Text), q) ||
		strings.Contains(strings.ToLower(it.Note), q) ||
		strings.Contains(strings.ToLower(it.Category), q) ||
		strings.Contains(strings.ToLower(it.Due), q) ||
		strings.Contains(strings.ToLower(it.Defer), q) ||
		strings.Contains(strings.ToLower(it.Link), q)
}

// filterCategory returns the active filter when it exactly names a known
// category (configured or already in use), so a new item added under a
// category filter inherits it and stays visible. Empty for a non-category
// filter (text/due/partial), leaving the item uncategorized.
func (m model) filterCategory() string {
	if m.filter == "" {
		return ""
	}
	f := strings.ToLower(m.filter)
	for _, c := range m.categories {
		if strings.ToLower(c) == f {
			return f
		}
	}
	for _, it := range m.items {
		if strings.ToLower(it.Category) == f {
			return f
		}
	}
	return ""
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
func (m model) archivedMatches() []todo.Item {
	if m.filter == "" {
		return nil
	}
	q := strings.ToLower(m.filter)
	var out []todo.Item
	for _, it := range m.archived {
		if matchItem(it, q) {
			out = append(out, it)
		}
	}
	return out
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
func (m *model) place(target todo.Item) {
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
		m.items = store.Load(m.path)
		m.archived = store.Load(store.ArchivePath(m.path))
		m.resort()
		m.clamp()
		m.lastMod = store.FileModTime(m.path)
		m.markSaved()
		return m, nil
	case tickMsg:
		// only auto-reload external edits when we have no pending in-memory
		// changes, so a dotfile sync can't clobber the user's work.
		if m.global {
			if mt := store.BoardsLatestMod(); mt.After(m.lastMod) {
				m.items = store.LoadAll()
				m.resort()
				m.clamp()
				m.lastMod = mt
				m.markSaved()
			}
		} else if m.dirty {
			// debounced autosave: flush once the user has paused editing.
			if m.autosaveEvery > 0 && time.Since(m.lastEdit) >= m.autosaveEvery {
				_ = store.Save(m.path, m.items)
				m.lastMod = store.FileModTime(m.path)
				m.markSaved()
			}
		} else {
			if mt := store.FileModTime(m.path); mt.After(m.lastMod) {
				m.items = store.Load(m.path)
				m.archived = store.Load(store.ArchivePath(m.path))
				m.resort()
				m.clamp()
				m.lastMod = mt
				m.markSaved()
			}
		}
		return m, tick()
	case tea.KeyMsg:
		var res tea.Model
		var cmd tea.Cmd
		switch m.mode {
		case modeAdd, modeEdit, modeNote, modeCategory, modeDue, modeDefer, modeLink, modeFilter:
			res, cmd = m.updateInput(msg)
		case modeDetail:
			res, cmd = m.updateDetail(msg)
		case modeHelp:
			res, cmd = m.updateHelp(msg)
		default:
			if m.global {
				res, cmd = m.updateGlobal(msg)
			} else {
				res, cmd = m.updateList(msg)
			}
		}
		if nm, ok := res.(model); ok {
			nm.refreshDirty() // one compare per event, not per render
			return nm, cmd
		}
		return res, cmd
	}
	return m, nil
}

// updateGlobal handles keys in the read-only global view. It deliberately has
// no mutation cases — read-only is structural here, not a per-case guard — and
// never saves the aggregate to disk.
func (m model) updateGlobal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.visible()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit // no save: the aggregate must never be written back
	case "A", "esc":
		m.toggleGlobal()
	case "j", "down":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "v":
		var cur todo.Item
		has := m.sel() >= 0
		if has {
			cur = m.items[m.sel()]
		}
		m.view = (m.view + 1) % 4
		m.resort()
		if has {
			m.place(cur)
		}
		m.clamp()
	case "d":
		if m.sel() >= 0 {
			m.mode = modeDetail
		}
	case "o":
		if m.sel() >= 0 {
			return m, openLink(m.items[m.sel()].Link)
		}
	case "?":
		m.mode = modeHelp
	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.Placeholder = "filter"
		m.input.Focus()
	}
	return m, nil
}

// openLink launches the OS browser on url (open on macOS, xdg-open elsewhere),
// detached. A no-op when url is empty. The exec error is dropped: there's no
// board-side recovery and the TUI must not block on a spawn.
func openLink(url string) tea.Cmd {
	if url == "" {
		return nil
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	_ = exec.Command(opener, url).Start()
	return nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.visible()
	idx := m.sel()
	switch msg.String() {
	case "q", "ctrl+c":
		_ = store.Save(m.path, m.items)
		return m, tea.Quit
	case "w":
		_ = store.Save(m.path, m.items)
		m.markSaved()
		m.lastMod = store.FileModTime(m.path)
		return m, nil
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
			todo.SetDone(&m.items[idx], !m.items[idx].Done)
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
		var done, kept []todo.Item
		for _, it := range m.items {
			if it.Done {
				done = append(done, it)
			} else {
				kept = append(kept, it)
			}
		}
		if len(done) > 0 {
			m.beforeMutate()
			_ = store.AppendArchive(m.path, done)
			m.archived = append(m.archived, done...)
			m.items = kept
			m.clamp()
		}
	case "U":
		if n := len(m.past); n > 0 {
			m.future = append(m.future, append([]todo.Item(nil), m.items...))
			m.items = m.past[n-1]
			m.past = m.past[:n-1]
			m.lastEdit = time.Now()
			m.clamp()
		}
	case "ctrl+r":
		if n := len(m.future); n > 0 {
			m.past = append(m.past, append([]todo.Item(nil), m.items...))
			m.items = m.future[n-1]
			m.future = m.future[:n-1]
			m.lastEdit = time.Now()
			m.clamp()
		}
	case "h", "m", "l":
		if idx >= 0 {
			m.beforeMutate()
			cur := m.items[idx]
			p := strings.ToUpper(msg.String())[0]
			if cur.Prio == p { // same priority again clears it
				cur.Prio = 0
			} else {
				cur.Prio = p
			}
			m.items[idx] = cur
			m.resort()
			m.place(cur)
		}
	case "v":
		var cur todo.Item
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
	case "A":
		m.toggleGlobal()
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
			m.input.SetValue(m.items[idx].Text)
			m.input.Placeholder = ""
			m.input.Focus()
		}
	case "g":
		if idx >= 0 {
			m.mode = modeCategory
			m.input.SetValue(m.items[idx].Category)
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
			m.input.SetValue(m.items[idx].Due)
			m.input.Placeholder = "today · tomorrow · 3d · 2w · 5m · 1y · DD-MM-YYYY"
			m.input.Focus()
		}
	case "s":
		if idx >= 0 {
			m.mode = modeDefer
			m.input.SetValue(m.items[idx].Defer)
			m.input.Placeholder = "start/defer: today · tomorrow · 3d · 2w · DD-MM-YYYY"
			m.input.Focus()
		}
	case "L":
		if idx >= 0 {
			m.mode = modeLink
			m.input.SetValue(m.items[idx].Link)
			m.input.Placeholder = "link (url)"
			m.input.Focus()
		}
	case "o":
		if idx >= 0 {
			return m, openLink(m.items[idx].Link)
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
		if idx >= 0 && !m.global {
			m.mode = modeNote
			m.input.SetValue(m.items[idx].Note)
			m.input.Placeholder = "note"
			m.input.Focus()
		}
	case "space", " ":
		if idx >= 0 && !m.global {
			m.beforeMutate()
			todo.SetDone(&m.items[idx], !m.items[idx].Done)
		}
	case "o":
		if idx >= 0 {
			return m, openLink(m.items[idx].Link)
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
			if it := todo.ParseQuickAdd(v); it.Text != "" {
				if it.Category == "" {
					it.Category = m.filterCategory()
				}
				m.beforeMutate()
				m.items = append(m.items, it)
				m.resort()
				m.place(it)
			}
			m.mode = modeList
		case modeEdit:
			if v != "" && idx >= 0 {
				m.beforeMutate()
				m.items[idx].Text = v
			}
			m.mode = modeList
		case modeNote:
			if idx >= 0 {
				m.beforeMutate()
				m.items[idx].Note = v // empty clears
			}
			m.mode = modeDetail
		case modeCategory:
			if idx >= 0 {
				m.beforeMutate()
				cur := m.items[idx]
				cur.Category = strings.ToLower(v) // empty clears
				m.items[idx] = cur
				m.resort()
				m.place(cur)
			}
			m.mode = modeList
		case modeDue:
			if idx >= 0 {
				m.beforeMutate()
				cur := m.items[idx]
				cur.Due = todo.ParseDue(v) // presets/relative resolved; empty clears
				m.items[idx] = cur
				m.resort()
				m.place(cur)
			}
			m.mode = modeList
		case modeDefer:
			if idx >= 0 {
				m.beforeMutate()
				m.items[idx].Defer = todo.ParseDue(v) // presets/relative resolved; empty clears
			}
			m.mode = modeList
		case modeLink:
			if idx >= 0 {
				m.beforeMutate()
				m.items[idx].Link = v // empty clears
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
