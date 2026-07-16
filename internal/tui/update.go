package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
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
	m.past = append(m.past, todo.Clone(m.items))
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
		if todo.Match(it, q) {
			idx = append(idx, i)
		}
	}
	return idx
}

// rowRef identifies a navigable row: a parent item (sub == -1) or one of its
// subtasks (sub is an index into m.items[item].Subs).
type rowRef struct {
	item int
	sub  int
}

// rows is the flat, cursor-indexed list of visible rows: each visible parent
// followed by its subtasks. The cursor is an index into this slice.
func (m model) rows() []rowRef {
	rs := make([]rowRef, 0, len(m.items))
	for _, i := range m.visible() {
		rs = append(rs, rowRef{i, -1})
		for s := range m.items[i].Subs {
			rs = append(rs, rowRef{i, s})
		}
	}
	return rs
}

// selRef is the row under the cursor, or {-1,-1} when there are no rows.
func (m model) selRef() rowRef {
	rs := m.rows()
	if len(rs) == 0 {
		return rowRef{-1, -1}
	}
	if m.cursor >= len(rs) {
		return rs[len(rs)-1]
	}
	return rs[m.cursor]
}

// rowItem returns the item a row points at: the parent, or the subtask.
func (m model) rowItem(r rowRef) todo.Item {
	if r.item < 0 {
		return todo.Item{}
	}
	if r.sub == -1 {
		return m.items[r.item]
	}
	return m.items[r.item].Subs[r.sub]
}

// rowText is the display text for a row (parent or subtask).
func (m model) rowText(r rowRef) string { return m.rowItem(r).Text }

// rowPtr returns a pointer to the item a row points at, for in-place mutation.
func (m *model) rowPtr(r rowRef) *todo.Item {
	if r.sub == -1 {
		return &m.items[r.item]
	}
	return &m.items[r.item].Subs[r.sub]
}

// sameItem matches two items by their (near-unique) identity fields, ignoring
// Subs/order, so the cursor can be re-placed after a resort. Item has a slice
// field and is not comparable, so == can't be used.
func sameItem(a, b todo.Item) bool {
	return a.Text == b.Text && a.Created == b.Created && a.Category == b.Category &&
		a.Prio == b.Prio && a.Due == b.Due && a.Done == b.Done
}

// archivedMatches returns archived items matching the active filter (read-only).
func (m model) archivedMatches() []todo.Item {
	if m.filter == "" {
		return nil
	}
	q := strings.ToLower(m.filter)
	var out []todo.Item
	for _, it := range m.archived {
		if todo.Match(it, q) {
			out = append(out, it)
		}
	}
	return out
}

// sel is the real items index of the parent under the cursor, or -1 if there
// are no rows. Detail/note/field editors act on this parent; list-level sub
// operations use selRef instead.
func (m model) sel() int {
	return m.selRef().item
}

func (m *model) clamp() {
	n := len(m.rows())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// place moves the cursor onto the parent row for a given item value (used after
// a sort re-orders the list). Lands on the parent row, not a subtask.
func (m *model) place(target todo.Item) {
	for p, r := range m.rows() {
		if r.sub == -1 && sameItem(m.items[r.item], target) {
			m.cursor = p
			return
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.height = msg.Width, msg.Height
		if m.mode == modeNote {
			m.note.SetWidth(m.width()) // keep the note editor sized while open
		}
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
		case modeAdd, modeAddSub, modeEdit, modeCategory, modeDue, modeDefer, modeLink, modeFilter, modeProjectRename, modeProjectNew:
			res, cmd = m.updateInput(msg)
		case modeConfirmDelete:
			res, cmd = m.updateConfirmDelete(msg)
		case modeNote:
			res, cmd = m.updateNote(msg)
		case modeDetail:
			res, cmd = m.updateDetail(msg)
		case modeHelp:
			res, cmd = m.updateHelp(msg)
		case modeArchive:
			res, cmd = m.updateArchive(msg)
		case modeProjects:
			res, cmd = m.updateProjects(msg)
		case modeSettings:
			res, cmd = m.updateSettings(msg)
		case modeSettingEdit:
			res, cmd = m.updateSettingEdit(msg)
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
	rows := m.rows()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit // no save: the aggregate must never be written back
	case "A", "esc":
		m.toggleGlobal()
	case "j", "down":
		if m.cursor < len(rows)-1 {
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
	case "e":
		m.enterArchive()
	case "p":
		m.enterProjects()
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
	rows := m.rows()
	ref := m.selRef()
	idx := ref.item
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
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case " ", "enter":
		if idx >= 0 {
			m.beforeMutate()
			if ref.sub == -1 {
				todo.SetParentDone(&m.items[idx], !m.items[idx].Done)
			} else {
				todo.SetSubDone(&m.items[idx], ref.sub, !m.items[idx].Subs[ref.sub].Done)
			}
		}
	case "tab":
		if idx >= 0 {
			m.beforeMutate()
			if ref.sub == -1 {
				todo.CycleStatus(&m.items[idx], m.statuses)
			} else {
				todo.CycleSubStatus(&m.items[idx], ref.sub, m.statuses)
			}
		}
	case "d":
		if idx >= 0 {
			m.mode = modeDetail
		}
	case "S":
		if idx >= 0 {
			m.mode = modeAddSub
			m.input.SetValue("")
			m.input.Placeholder = "subtask text  !h|!m|!l  due:tomorrow"
			m.input.Focus()
		}
	case "x":
		if idx >= 0 {
			m.beforeMutate()
			if ref.sub == -1 {
				m.items = append(m.items[:idx], m.items[idx+1:]...)
			} else {
				p := &m.items[idx]
				p.Subs = append(p.Subs[:ref.sub], p.Subs[ref.sub+1:]...)
				if len(p.Subs) > 0 {
					todo.SetDone(p, todo.AllSubsDone(p))
				}
			}
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
			m.future = append(m.future, todo.Clone(m.items))
			m.items = m.past[n-1]
			m.past = m.past[:n-1]
			m.lastEdit = time.Now()
			m.clamp()
		}
	case "ctrl+r":
		if n := len(m.future); n > 0 {
			m.past = append(m.past, todo.Clone(m.items))
			m.items = m.future[n-1]
			m.future = m.future[:n-1]
			m.lastEdit = time.Now()
			m.clamp()
		}
	case "h", "m", "l":
		if idx >= 0 {
			p := strings.ToUpper(msg.String())[0]
			if ref.sub >= 0 { // subtask priority: set in place, no resort
				m.beforeMutate()
				sub := &m.items[idx].Subs[ref.sub]
				if sub.Prio == p {
					sub.Prio = 0
				} else {
					sub.Prio = p
				}
				break
			}
			m.beforeMutate()
			cur := m.items[idx]
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
			m.input.SetValue(m.rowText(ref))
			m.input.Placeholder = ""
			m.input.Focus()
		}
	case "g":
		if idx >= 0 && ref.sub == -1 { // field editors are parent-level
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
			m.input.SetValue(m.rowItem(ref).Due)
			m.input.Placeholder = "today · tomorrow · 3d · 2w · 5m · 1y · DD-MM-YYYY"
			m.input.Focus()
		}
	case "s":
		if idx >= 0 {
			m.mode = modeDefer
			m.input.SetValue(m.rowItem(ref).Defer)
			m.input.Placeholder = "start/defer: today · tomorrow · 3d · 2w · DD-MM-YYYY"
			m.input.Focus()
		}
	case "L":
		if idx >= 0 {
			m.mode = modeLink
			m.input.SetValue(m.rowItem(ref).Link)
			m.input.Placeholder = "link (url)"
			m.input.Focus()
		}
	case "o":
		if idx >= 0 {
			return m, openLink(m.rowItem(ref).Link)
		}
	case "e":
		m.enterArchive()
	case "p":
		m.enterProjects()
	case ",":
		m.mode = modeSettings
		m.settingsCur = 0
	case "ctrl+e":
		return m, m.openEditor()
	}
	return m, nil
}

// enterProjects opens the board picker, listing every board with the current
// one (default when unnamed) under the cursor.
func (m *model) enterProjects() {
	m.projRows = store.Boards()
	m.projCur = 0
	m.projArchived = false
	m.projNotice = ""
	cur := m.project
	if cur == "" {
		cur = "default"
	}
	for i, b := range m.projRows {
		if b.Name == cur {
			m.projCur = i
			break
		}
	}
	m.mode = modeProjects
}

// updateProjects handles keys in the board picker: navigate, enter to jump to
// the selected board (flushing any unsaved edits first), esc to return.
func (m model) updateProjects(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if !m.global && m.dirty {
			_ = store.Save(m.path, m.items)
		}
		return m, tea.Quit
	case "esc":
		m.mode = modeList
	case "j", "down":
		if m.projCur < len(m.projRows)-1 {
			m.projCur++
		}
	case "k", "up":
		if m.projCur > 0 {
			m.projCur--
		}
	case "e": // toggle between live and archived boards
		m.projArchived = !m.projArchived
		m.projNotice = ""
		m.projCur = 0
		if m.projArchived {
			m.projRows = store.ArchivedBoards()
		} else {
			m.projRows = store.Boards()
		}
	case "u": // unarchive the selected board (archived view only)
		if !m.projArchived {
			break
		}
		if b := m.selectedBoard(); b != nil {
			if err := store.UnarchiveBoard(b.Name); err != nil {
				m.projNotice = err.Error()
			} else {
				m.projArchived = false
				m.projRows = store.Boards()
				for i, x := range m.projRows { // land on the restored board
					if x.Name == b.Name {
						m.projCur = i
						break
					}
				}
			}
		}
	case "enter":
		if m.projArchived { // archived boards aren't loadable; unarchive first
			break
		}
		if len(m.projRows) == 0 {
			m.mode = modeList
			return m, nil
		}
		name := m.projRows[m.projCur].Name
		proj := name
		if name == "default" {
			proj = ""
		}
		if !m.global && m.dirty {
			_ = store.Save(m.path, m.items)
		}
		nm := newModel(proj) // jump: rebuild the board fresh, like toggleGlobal
		nm.w, nm.height, nm.density = m.w, m.height, m.density
		nm.clamp()
		return nm, nil
	case "a": // create a new board
		if m.projArchived {
			break
		}
		m.projNotice = ""
		m.mode = modeProjectNew
		m.input.SetValue("")
		m.input.Placeholder = "new board name"
		m.input.Focus()
	case "r": // rename the selected board (not the default)
		if m.projArchived {
			break
		}
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			m.projNotice = ""
			m.mode = modeProjectRename
			m.input.SetValue(b.Name)
			m.input.Placeholder = "new board name"
			m.input.Focus()
		}
	case "x": // delete the selected board (confirmed)
		if m.projArchived {
			break
		}
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			m.mode = modeConfirmDelete
		}
	case "A": // archive the selected board into projects/archived/
		if m.projArchived {
			break
		}
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			if err := store.ArchiveBoard(b.Name); err != nil {
				m.projNotice = err.Error()
			} else {
				return m.afterBoardChange(b.Name, ""), nil
			}
		}
	}
	return m, nil
}

// selectedBoard is the board row under the picker cursor, or nil when empty.
func (m model) selectedBoard() *store.Board {
	if m.projCur < 0 || m.projCur >= len(m.projRows) {
		return nil
	}
	return &m.projRows[m.projCur]
}

// currentBoardName is the name of the board currently open ("default" when the
// unnamed board).
func (m model) currentBoardName() string {
	if m.project == "" {
		return "default"
	}
	return m.project
}

// afterBoardChange refreshes the picker after a rename/delete/archive. If the
// board that changed is the one currently open, it re-opens that board under its
// new name (newName), or the default board when it was removed/archived
// (newName == ""), so the live model never points at a moved file.
func (m model) afterBoardChange(oldName, newName string) model {
	if oldName == m.currentBoardName() {
		proj := newName
		if proj == "default" {
			proj = ""
		}
		nm := newModel(proj)
		nm.w, nm.height, nm.density = m.w, m.height, m.density
		nm.enterProjects()
		return nm
	}
	m.projRows = store.Boards()
	if m.projCur >= len(m.projRows) {
		m.projCur = len(m.projRows) - 1
	}
	if m.projCur < 0 {
		m.projCur = 0
	}
	m.mode = modeProjects
	return m
}

// updateConfirmDelete handles the delete-board confirmation: y deletes the
// selected board, anything else cancels back to the picker.
func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if b := m.selectedBoard(); b != nil && b.Name != "default" {
			if err := store.DeleteBoard(b.Name); err == nil {
				return m.afterBoardChange(b.Name, ""), nil
			}
		}
		m.mode = modeProjects
	default:
		m.mode = modeProjects
	}
	return m, nil
}

// numSettings is the count of editable rows in the settings screen, in the
// order settingsView renders them: view, density, autosave, categories, statuses.
const numSettings = 5

func (m *model) saveSettings() { _ = saveConfig(store.ConfigPath(), m.currentConfig()) }

// updateSettings handles the settings screen: navigate rows, cycle the enum
// rows (view/density) in place, or open the editor on a text row. Every change
// applies to the live model and is written straight to config.toml.
func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if !m.global && m.dirty {
			_ = store.Save(m.path, m.items)
		}
		return m, tea.Quit
	case "esc":
		m.mode = modeList
	case "j", "down":
		if m.settingsCur < numSettings-1 {
			m.settingsCur++
		}
	case "k", "up":
		if m.settingsCur > 0 {
			m.settingsCur--
		}
	case "tab", " ":
		m.cycleSetting() // no-op on text rows
	case "enter", "l", "right":
		switch m.settingsCur {
		case 0, 1:
			m.cycleSetting()
		default:
			m.mode = modeSettingEdit
			m.input.SetValue(m.settingValue(m.settingsCur))
			m.input.Placeholder = m.settingPlaceholder(m.settingsCur)
			m.input.Focus()
		}
	}
	return m, nil
}

// cycleSetting advances an enum row (view or density) and persists.
func (m *model) cycleSetting() {
	switch m.settingsCur {
	case 0: // view: category -> priority -> table
		m.view = (m.view + 1) % 3
		m.resort()
		m.clamp()
		m.saveSettings()
	case 1: // density: compact <-> comfort
		if m.density == comfort {
			m.density = compact
		} else {
			m.density = comfort
		}
		m.saveSettings()
	}
}

// settingValue is the editor seed for a text setting row.
func (m model) settingValue(idx int) string {
	switch idx {
	case 2:
		return strconv.Itoa(int(m.autosaveEvery / time.Second))
	case 3:
		return strings.Join(m.categories, ", ")
	case 4:
		return strings.Join(m.statuses, ", ")
	}
	return ""
}

func (m model) settingPlaceholder(idx int) string {
	switch idx {
	case 2:
		return "autosave seconds (0 disables)"
	case 3:
		return "categories, comma-separated"
	case 4:
		return "statuses, comma-separated (done always kept last)"
	}
	return ""
}

// updateSettingEdit runs the shared text input for a text setting row, applying
// and saving on enter.
func (m model) updateSettingEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.mode = modeSettings
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		m.applySettingText(m.settingsCur, v)
		m.saveSettings()
		m.input.Blur()
		m.mode = modeSettings
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// applySettingText writes an edited text setting back onto the live model.
func (m *model) applySettingText(idx int, v string) {
	switch idx {
	case 2:
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.autosaveEvery = time.Duration(n) * time.Second
		}
	case 3:
		m.categories = parseCommaList(v, false)
	case 4:
		m.statuses = normalizeStatuses(parseCommaList(v, true))
	}
}

// parseCommaList splits a comma-separated list, trimming blanks; lower lowercases
// each entry (used for statuses).
func parseCommaList(v string, lower bool) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		s := strings.TrimSpace(p)
		if lower {
			s = strings.ToLower(s)
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// enterArchive opens the read-only archive browser. On a project board it shows
// that board's archive; in the global view it aggregates every board's archive.
func (m *model) enterArchive() {
	m.mode = modeArchive
	m.arcCur = 0
	if m.global {
		m.arcRows = store.LoadAllArchives()
	} else {
		m.arcRows = m.archived
	}
}

// updateArchive handles keys in the read-only archive browser: navigate, esc to
// return to the board, q to quit. No mutations.
func (m model) updateArchive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if !m.global {
			_ = store.Save(m.path, m.items)
		}
		return m, tea.Quit
	case "esc", "e":
		m.mode = modeList
	case "j", "down":
		if m.arcCur < len(m.arcRows)-1 {
			m.arcCur++
		}
	case "k", "up":
		if m.arcCur > 0 {
			m.arcCur--
		}
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
	ref := m.selRef()
	if ref.item < 0 {
		if s := msg.String(); s == "esc" || s == "q" || s == "d" {
			m.mode = modeList
		}
		return m, nil
	}
	p := m.rowPtr(ref)
	switch msg.String() {
	case "esc", "q", "d":
		m.mode = modeList
	case "e", "n":
		if !m.global {
			m.mode = modeNote
			m.noteEditing = false
			m.note.SetWidth(m.width())
			m.note.SetHeight(noteHeight)
			m.note.SetValue(p.Note)
			m.note.Focus()
		}
	case "space", " ":
		if !m.global {
			m.beforeMutate()
			if ref.sub == -1 {
				todo.SetParentDone(&m.items[ref.item], !m.items[ref.item].Done)
			} else {
				todo.SetSubDone(&m.items[ref.item], ref.sub, !m.items[ref.item].Subs[ref.sub].Done)
			}
		}
	case "o":
		return m, openLink(p.Link)
	}
	return m, nil
}

// updateNote drives the multi-line note editor. enter inserts a newline (the
// textarea owns it); edits save live to the item, so esc just closes. One undo
// snapshot is taken on the first change, giving a single entry per edit session.
func (m model) updateNote(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.note.Blur()
		m.noteEditing = false
		m.mode = modeDetail
		return m, nil
	}
	var cmd tea.Cmd
	m.note, cmd = m.note.Update(msg)
	if ref := m.selRef(); ref.item >= 0 {
		p := m.rowPtr(ref)
		if v := m.note.Value(); v != p.Note { // empty clears
			if !m.noteEditing {
				m.beforeMutate()
				m.noteEditing = true
			}
			p.Note = v
		}
	}
	return m, cmd
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ref := m.selRef()
	idx := ref.item
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
		case modeFilter:
			m.filter = ""
			m.clamp()
			m.mode = modeList
		case modeProjectRename, modeProjectNew:
			m.mode = modeProjects
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
		case modeAddSub:
			if it := todo.ParseQuickAdd(v); it.Text != "" && idx >= 0 {
				m.beforeMutate()
				p := &m.items[idx]
				p.Subs = append(p.Subs, it)
				todo.SetDone(p, todo.AllSubsDone(p)) // an open sub reopens the parent
				m.clamp()
			}
			m.mode = modeList
		case modeEdit:
			if v != "" && idx >= 0 {
				m.beforeMutate()
				if ref.sub == -1 {
					m.items[idx].Text = v
				} else {
					m.items[idx].Subs[ref.sub].Text = v
				}
			}
			m.mode = modeList
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
				if ref.sub == -1 { // parent due affects sort order; subs keep file order
					cur := m.items[idx]
					cur.Due = todo.ParseDue(v) // presets/relative resolved; empty clears
					m.items[idx] = cur
					m.resort()
					m.place(cur)
				} else {
					m.rowPtr(ref).Due = todo.ParseDue(v)
				}
			}
			m.mode = modeList
		case modeDefer:
			if idx >= 0 {
				m.beforeMutate()
				m.rowPtr(ref).Defer = todo.ParseDue(v) // presets/relative resolved; empty clears
			}
			m.mode = modeList
		case modeLink:
			if idx >= 0 {
				m.beforeMutate()
				m.rowPtr(ref).Link = v // empty clears
			}
			m.mode = modeList
		case modeFilter:
			m.mode = modeList // keep the filter applied
		case modeProjectRename:
			old := ""
			if b := m.selectedBoard(); b != nil {
				old = b.Name
			}
			m.input.Blur()
			if v != "" && v != old {
				if err := store.RenameBoard(old, v); err != nil {
					m.projNotice = err.Error()
				} else {
					return m.afterBoardChange(old, v), nil
				}
			}
			m.mode = modeProjects // no-op / invalid name: return to the picker
			return m, nil
		case modeProjectNew:
			m.input.Blur()
			m.mode = modeProjects
			if v != "" {
				if err := store.CreateBoard(v); err != nil {
					m.projNotice = err.Error()
				} else {
					m.projRows = store.Boards()
					for i, b := range m.projRows { // land on the new board
						if b.Name == v {
							m.projCur = i
							break
						}
					}
				}
			}
			return m, nil
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
