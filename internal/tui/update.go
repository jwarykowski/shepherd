package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"shepherd/internal/drover"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"slices"
	"strings"
	"time"

	binding "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// copyText puts s on the system clipboard through the terminal (OSC 52), so it
// works over SSH and inside tmux/herdr panes. A package var so tests can record.
var copyText = tea.SetClipboard

func (m model) Init() tea.Cmd { return tea.Batch(tick(), tea.RequestBackgroundColor, firstRuns) }

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

// diskMsg is a board (or the global aggregate) that changed on disk, read by
// checkDisk off the Update loop.
type diskMsg struct {
	path            string
	global          bool
	mod             time.Time
	items, archived []todo.Item
}

// checkDisk stats the board (every board when global) and, if it's newer than
// the last read, loads it in the background as a diskMsg.
func (m model) checkDisk() tea.Cmd {
	path, global, last := m.path, m.global, m.lastMod
	return func() tea.Msg {
		if global {
			if mt := store.BoardsLatestMod(); mt.After(last) {
				return diskMsg{global: true, mod: mt, items: store.LoadAll()}
			}
			return nil
		}
		if mt := store.FileModTime(path); mt.After(last) {
			return diskMsg{path: path, mod: mt, items: store.Load(path), archived: store.Load(store.ArchivePath(path))}
		}
		return nil
	}
}

// applyDisk takes a background read unless it's gone stale: another board or
// view, an older file than the last read, or edits made meanwhile, which a
// reload would clobber (the next save merges them instead).
func (m model) applyDisk(msg diskMsg) model {
	if msg.global != m.global || (!msg.global && msg.path != m.path) || !msg.mod.After(m.lastMod) {
		return m
	}
	if msg.global {
		m.items = msg.items
		m.lastMod = msg.mod
		m.resort()
		m.clamp()
		m.markSaved()
		return m
	}
	if m.dirty || m.mode != modeList {
		return m
	}
	var sel *todo.Item
	if r := m.selRef(); r.item >= 0 {
		it := m.items[r.item]
		sel = &it
	}
	m.load(msg.mod, msg.items, msg.archived)
	if sel != nil {
		m.place(*sel) // keep the cursor on the same item, not the same row
	}
	return m
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
func (m *model) openEditor() tea.Cmd {
	m.save()
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

// addCategory is the category a new item inherits: the selected row's, so an
// item added over a categorised one joins that group instead of the
// uncategorised tail, else a category filter's (which would otherwise hide the
// new item). An explicit @category in the input beats both. idx is the selected
// row's item index, -1 when no row is selected.
func (m model) addCategory(idx int) string {
	if idx >= 0 && m.items[idx].Category != "" {
		return m.items[idx].Category
	}
	return m.filterCategory()
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

// laneRows is the visible rows whose effective status matches the given lane
// name — the per-column cursor space for viewLane. Built from rows() so it
// stays consistent with the active filter and subtask flattening.
func (m model) laneRows(status string) []rowRef {
	var out []rowRef
	for _, r := range m.rows() {
		if todo.StatusOf(m.rowItem(r), m.statuses) == status {
			out = append(out, r)
		}
	}
	return out
}

// currentLane is the status name of the active column in viewLane, clamped
// into range. Empty when m.statuses is empty (loadConfig's normalizeStatuses
// guarantees it isn't at runtime, but model{} test literals often skip it).
func (m model) currentLane() string {
	if len(m.statuses) == 0 {
		return ""
	}
	i := max(m.lane, 0)
	if i >= len(m.statuses) {
		i = len(m.statuses) - 1
	}
	return m.statuses[i]
}

// cursorRows is the row slice m.cursor indexes: the active lane's rows in
// viewLane, the full flat list otherwise. The single choke point for cursor
// bounds so j/k, selRef and clamp can't disagree on what's in range.
func (m model) cursorRows() []rowRef {
	if m.view == viewLane {
		return m.laneRows(m.currentLane())
	}
	return m.rows()
}

// selRef is the row under the cursor, or {-1,-1} when there are no rows. In
// viewLane the cursor indexes the active column's own rows, not the full list.
func (m model) selRef() rowRef {
	rs := m.cursorRows()
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

// prompt opens the shared text input in mode, seeded with value and showing
// placeholder — the single entry point for every field editor and picker prompt.
func (m *model) prompt(md mode, value, placeholder string) {
	m.mode = md
	m.input.SetValue(value)
	m.input.Placeholder = placeholder
	m.input.Focus()
}

// sameItem matches two items by ID, else by their (near-unique) identity fields, ignoring
// Subs/order, so the cursor can be re-placed after a resort. Item has a slice
// field and is not comparable, so == can't be used.
func sameItem(a, b todo.Item) bool {
	if a.ID != "" && b.ID != "" {
		return a.ID == b.ID
	}
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
	if m.view == viewLane {
		if m.lane < 0 || len(m.statuses) == 0 {
			m.lane = 0
		} else if m.lane >= len(m.statuses) {
			m.lane = len(m.statuses) - 1
		}
	}
	n := len(m.cursorRows())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// place moves the cursor onto the parent row for a given item value (used after
// a sort re-orders the list). Lands on the parent row, not a subtask. In
// viewLane it first switches to the item's own lane, so a card followed here
// after a status change stays under the cursor in its new column.
func (m *model) place(target todo.Item) {
	if m.view == viewLane {
		lane := todo.StatusOf(target, m.statuses)
		for i, s := range m.statuses {
			if s == lane {
				m.lane = i
				break
			}
		}
		for p, r := range m.laneRows(lane) {
			if r.sub == -1 && sameItem(m.items[r.item], target) {
				m.cursor = p
				return
			}
		}
		return
	}
	for p, r := range m.rows() {
		if r.sub == -1 && sameItem(m.items[r.item], target) {
			m.cursor = p
			return
		}
	}
}

// placeSub is place's subtask counterpart: follows the cursor onto a
// subtask's row after its status changes. Matched by (idx, sub) rather than
// by value — subtask text isn't guaranteed unique the way place's sameItem
// check assumes.
func (m *model) placeSub(idx, sub int) {
	if m.view != viewLane {
		return
	}
	lane := todo.StatusOf(m.items[idx].Subs[sub], m.statuses)
	for i, s := range m.statuses {
		if s == lane {
			m.lane = i
			break
		}
	}
	for p, r := range m.laneRows(lane) {
		if r.item == idx && r.sub == sub {
			m.cursor = p
			return
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.light = !msg.IsDark()
		m.applyTheme()
		return m, nil
	case tea.KeyboardEnhancementsMsg:
		m.kbdEnhanced = msg.SupportsKeyDisambiguation()
		return m, nil
	case tea.FocusMsg:
		// back in the pane: pick up agent/CLI edits now, not on the next tick
		return m, m.checkDisk()
	case tea.BlurMsg:
		// leaving the pane flushes edits, unless autosave is off
		if !m.global && m.dirty && m.autosaveEvery > 0 {
			m.save()
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.w, m.height = msg.Width, msg.Height
		if m.mode == modeNote {
			m.note.SetWidth(m.width()) // keep the note editor sized while open
		}
		return m, nil
	case editorDoneMsg:
		// Reload from disk — the file may have changed under us.
		m.reload()
		return m, nil
	case tickMsg:
		// only auto-reload external edits when we have no pending in-memory
		// changes, so a dotfile sync can't clobber the user's work.
		switch {
		case m.global:
			return m, tea.Batch(tick(), m.checkDisk())
		case m.mode != modeList:
			// leave the rows alone under an open editor or detail page, which
			// address the item by its row; catch up once back on the list.
		case m.dirty:
			// debounced autosave: flush once the user has paused editing.
			if m.autosaveEvery > 0 && time.Since(m.lastEdit) >= m.autosaveEvery {
				m.save()
			}
		default:
			return m, tea.Batch(tick(), m.checkDisk())
		}
		return m, tick()
	case diskMsg:
		return m.applyDisk(msg), nil
	case runsMsg:
		return m.gotRuns(msg)
	case logMsg:
		m.logRows = msg.rows
		if m.logRows == nil {
			m.logRows = []drover.LogRow{}
		}
		if msg.err != nil {
			m.logErr = msg.err.Error()
		}
		return m, nil
	case archivedMsg:
		m.notice = msg.notice
		if msg.err != nil {
			m.notice = msg.err.Error()
		}
		return m, loadRuns
	case takeOverMsg:
		m.notice = "back from the conversation"
		if msg.err != nil {
			m.notice = "take over: " + msg.err.Error()
		}
		return m, loadRuns
	case actionsMsg:
		m.actionNames = msg.names
		return m, nil
	case archiveRunsMsg:
		m.agArchiveRuns = msg.runs
		if msg.err != nil {
			m.notice = msg.err.Error()
		}
		return m, nil
	case sentMsg:
		return m.sent(msg)
	case tea.PasteMsg:
		return m.paste(msg)
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	case rowClickMsg:
		return m.clickRow(msg)
	case tea.KeyPressMsg:
		return m.dispatchKey(msg)
	}
	return m, nil
}

// updateGlobal handles keys in the read-only global view. It deliberately has
// no mutation cases — read-only is structural here, not a per-case guard — and
// never saves the aggregate to disk.
func (m model) updateGlobal(msg fmt.Stringer) (tea.Model, tea.Cmd) {
	rows := m.rows()
	switch {
	case binding.Matches(msg, keys.Quit):
		return m.quit()
	case binding.Matches(msg, keys.Back, keys.Global): // toggles both ways, like it does on a board
		m.toggleGlobal()
	case binding.Matches(msg, keys.Footer):
		m.hideFooter = !m.hideFooter
	case binding.Matches(msg, keys.Down):
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case binding.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case binding.Matches(msg, keys.Top):
		m.cursor = 0
	case binding.Matches(msg, keys.Bottom):
		m.cursor = max(len(rows)-1, 0)
	case binding.Matches(msg, keys.View):
		var cur todo.Item
		has := m.sel() >= 0
		if has {
			cur = m.items[m.sel()]
		}
		next := (m.view + 1) % viewMode(viewCountGlobal)
		if next == viewLane { // lane view is board-only: boards can configure
			next = (next + 1) % viewMode(viewCountGlobal) // different statuses
		}
		m.view = next
		m.resort()
		if has {
			m.place(cur)
		}
		m.clamp()
	case binding.Matches(msg, keys.Detail):
		if m.sel() >= 0 {
			m.mode, m.detailBack = modeDetail, modeList
		}
	case binding.Matches(msg, keys.Open):
		if m.sel() >= 0 {
			return m, openLink(m.items[m.sel()].Link)
		}
	case binding.Matches(msg, keys.Copy):
		if m.sel() >= 0 {
			return m, copyText(m.items[m.sel()].Text)
		}
	case binding.Matches(msg, keys.ArchiveView):
		m.enterArchive()
	case binding.Matches(msg, keys.Boards):
		m.enterBoards()
	case binding.Matches(msg, keys.Help):
		m.mode = modeHelp
	case binding.Matches(msg, keys.Theme):
		m.enterTheme()
	case binding.Matches(msg, keys.Filter):
		m.prompt(modeFilter, m.filter, "filter")
	}
	return m, nil
}

// openLink launches the OS browser on url (open on macOS, xdg-open elsewhere),
// detached. A no-op when url is empty. The exec error is dropped: there's no
// board-side recovery and the TUI must not block on a spawn. It returns a
// tea.Cmd so key handlers can return it, but has nothing to run afterwards.
func openLink(url string) tea.Cmd { //nolint:unparam // matches the handlers' return
	if url == "" {
		return nil
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if cmd := exec.Command(opener, url); cmd.Start() == nil {
		go func() { _ = cmd.Wait() }() // reap it, so no zombie per opened link
	}
	return nil
}

// quit is the single exit point for q and ctrl+c across every mode. It saves the
// board first, except in the read-only global aggregate, which must never be
// written back.
func (m model) quit() (tea.Model, tea.Cmd) {
	if !m.global && m.dirty {
		m.save()
	}
	return m, tea.Quit
}

func (m model) updateList(msg fmt.Stringer) (tea.Model, tea.Cmd) {
	rows := m.cursorRows()
	ref := m.selRef()
	idx := ref.item
	m.notice = ""
	switch {
	case binding.Matches(msg, keys.Run):
		return m.sendToAgent()
	case binding.Matches(msg, keys.Runs):
		return m.openAgents()
	case binding.Matches(msg, keys.Quit):
		return m.quit()
	case binding.Matches(msg, keys.Save):
		m.save()
		return m, nil
	case binding.Matches(msg, keys.Down):
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case binding.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case binding.Matches(msg, keys.Top):
		m.cursor = 0
	case binding.Matches(msg, keys.Bottom):
		m.cursor = max(len(rows)-1, 0)
	case binding.Matches(msg, keys.LaneLeft):
		if m.view == viewLane && m.lane > 0 {
			m.lane--
			m.clamp()
		}
	case binding.Matches(msg, keys.LaneRight):
		if m.view == viewLane && m.lane < len(m.statuses)-1 {
			m.lane++
			m.clamp()
		}
	case binding.Matches(msg, keys.Toggle):
		if idx >= 0 {
			m.beforeMutate()
			if ref.sub == -1 {
				todo.SetParentDone(&m.items[idx], !m.items[idx].Done)
				if m.view == viewLane { // follow the card into its new column
					m.place(m.items[idx])
				}
			} else {
				todo.SetSubDone(&m.items[idx], ref.sub, !m.items[idx].Subs[ref.sub].Done)
				m.placeSub(idx, ref.sub)
			}
			m.clamp()
		}
	case binding.Matches(msg, keys.Status):
		if idx >= 0 {
			m.beforeMutate()
			if ref.sub == -1 {
				todo.CycleStatus(&m.items[idx], m.statuses)
				if m.view == viewLane { // follow the card into its new column
					m.place(m.items[idx])
				}
			} else {
				todo.CycleSubStatus(&m.items[idx], ref.sub, m.statuses)
				m.placeSub(idx, ref.sub)
			}
			m.clamp()
		}
	case binding.Matches(msg, keys.Detail):
		if idx >= 0 {
			m.mode, m.detailBack = modeDetail, modeList
		}
	case binding.Matches(msg, keys.AddSub):
		if idx >= 0 {
			m.prompt(modeAddSub, "", "subtask text  !h|!m|!l  due:tomorrow")
		}
	case binding.Matches(msg, keys.Delete):
		if idx >= 0 {
			m.beforeMutate()
			if ref.sub == -1 {
				m.items = append(m.items[:idx], m.items[idx+1:]...)
			} else {
				p := &m.items[idx]
				p.Subs = append(p.Subs[:ref.sub], p.Subs[ref.sub+1:]...)
				todo.ReconcileParent(p)
			}
			m.clamp()
		}
	case binding.Matches(msg, keys.Sweep):
		var done, kept []todo.Item
		for _, it := range m.items {
			if it.Done {
				done = append(done, it)
			} else {
				kept = append(kept, it)
			}
		}
		if len(done) > 0 {
			_ = store.AppendArchive(m.path, done)
			m.archived = append(m.archived, done...)
			m.items = kept
			m.commitArchive()
		}
	case binding.Matches(msg, keys.Archive):
		// Archive just the selected item. Whole items only — subtasks live inside
		// their parent, so a subtask row is a no-op (and dimmed in the legend),
		// matching the CLI's `archive <ref>` which rejects subtask refs.
		if idx >= 0 && ref.sub == -1 {
			it := m.items[idx]
			_ = store.AppendArchive(m.path, []todo.Item{it}) // to disk before the board rewrite, so an interrupt re-archives rather than loses
			m.archived = append(m.archived, it)
			m.items = append(m.items[:idx], m.items[idx+1:]...)
			m.commitArchive()
		}
	case binding.Matches(msg, keys.Undo):
		if n := len(m.past); n > 0 {
			m.future = append(m.future, todo.Clone(m.items))
			m.items = m.past[n-1]
			m.past = m.past[:n-1]
			m.lastEdit = time.Now()
			m.resort() // the snapshot keeps the order of the view it was taken in
			m.clamp()
		}
	case binding.Matches(msg, keys.Redo):
		if n := len(m.future); n > 0 {
			m.past = append(m.past, todo.Clone(m.items))
			m.items = m.future[n-1]
			m.future = m.future[:n-1]
			m.lastEdit = time.Now()
			m.resort()
			m.clamp()
		}
	case binding.Matches(msg, keys.Prio):
		if idx >= 0 {
			p := "HML"[slices.Index(keys.Prio.Keys(), msg.String())]
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
	case binding.Matches(msg, keys.View):
		var cur todo.Item
		has := idx >= 0
		if has {
			cur = m.items[idx]
		}
		m.view = (m.view + 1) % viewMode(viewCount)
		m.resort()
		if has {
			m.place(cur)
		}
		m.clamp()
	case binding.Matches(msg, keys.Global):
		m.toggleGlobal()
	case binding.Matches(msg, keys.Footer):
		m.hideFooter = !m.hideFooter
	case binding.Matches(msg, keys.Help):
		m.mode = modeHelp
	case binding.Matches(msg, keys.Filter):
		m.prompt(modeFilter, m.filter, "filter")
	case binding.Matches(msg, keys.Back):
		m.filter = ""
		m.clamp()
	case binding.Matches(msg, keys.Add):
		m.prompt(modeAdd, "", "todo text  @category  !h|!m|!l  due:tomorrow")
	case binding.Matches(msg, keys.Edit):
		if idx >= 0 {
			m.prompt(modeEdit, m.rowText(ref), "")
		}
	case binding.Matches(msg, keys.Category):
		if idx >= 0 && ref.sub == -1 { // field editors are parent-level
			ph := "category"
			if len(m.categories) > 0 {
				ph = "category · tab: " + strings.Join(m.categories, "/")
			}
			m.catIdx = 0
			m.prompt(modeCategory, m.items[idx].Category, ph)
		}
	case binding.Matches(msg, keys.Tags):
		if idx >= 0 && ref.sub == -1 { // field editors are parent-level
			m.prompt(modeTags, strings.Join(m.items[idx].Tags, " "), "tags, space- or comma-separated")
		}
	case binding.Matches(msg, keys.Due):
		if idx >= 0 {
			m.prompt(modeDue, m.rowItem(ref).Due, "today · tomorrow · fri · 3d · 2w · 5m · 1y · DD-MM-YYYY")
		}
	case binding.Matches(msg, keys.Defer):
		if idx >= 0 {
			m.prompt(modeDefer, m.rowItem(ref).Defer, "start/defer: today · tomorrow · fri · 3d · 2w · DD-MM-YYYY")
		}
	case binding.Matches(msg, keys.Link):
		if idx >= 0 {
			m.prompt(modeLink, m.rowItem(ref).Link, "link (url)")
		}
	case binding.Matches(msg, keys.Open):
		if idx >= 0 {
			return m, openLink(m.rowItem(ref).Link)
		}
	case binding.Matches(msg, keys.Copy):
		if idx >= 0 {
			return m, copyText(m.rowText(ref))
		}
	case binding.Matches(msg, keys.ArchiveView):
		m.enterArchive()
	case binding.Matches(msg, keys.Boards):
		m.enterBoards()
	case binding.Matches(msg, keys.Settings):
		m.mode = modeSettings
		m.settingsCur = 0
	case binding.Matches(msg, keys.Theme):
		m.enterTheme()
	case binding.Matches(msg, keys.Editor):
		return m, m.openEditor()
	}
	return m, nil
}

// commitArchive finishes a sweep: AppendArchive already wrote the archive, so the
// board is saved to match and undo history is dropped — undoing would restore
// items that also stay in archive.md.
func (m *model) commitArchive() {
	m.save()
	m.past, m.future = nil, nil
	m.clamp()
}

// enterArchive opens the read-only archive browser. On a board board it shows
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
func (m model) updateArchive(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc":
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

func (m model) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc":
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

// textEntry reports whether the mode types into the shared single-line input.
func (m model) textEntry() bool {
	switch m.mode {
	case modeAdd, modeAddSub, modeEdit, modeCategory, modeTags, modeDue, modeDefer, modeLink, modeFilter, modeBoardRename, modeBoardNew, modeBoardDir, modeSettingEdit:
		return true
	}
	return false
}

// paste feeds bracketed-paste text to whichever editor is open; elsewhere a
// paste is ignored rather than read as key presses.
func (m model) paste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case m.mode == modeNote:
		m.note, cmd = m.note.Update(msg)
		m.syncNote()
	case m.textEntry():
		m.input, cmd = m.input.Update(msg)
		if m.mode == modeFilter {
			m.filter = m.input.Value()
			m.clamp()
		}
	}
	m.refreshDirty()
	return m, cmd
}

func (m model) updateInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		case modeBoardRename, modeBoardNew:
			m.mode = modeBoards
		case modeBoardDir: // dir edit/skip cancelled; leave the board's dir unchanged
			if m.projDirEdit { // opened from detail: return there
				m.projDirEdit = false
				m.mode = modeBoardDetail
			} else {
				m.mode = modeBoards
			}
		default:
			m.mode = m.afterFieldEdit()
		}
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		switch m.mode {
		case modeAdd:
			if it := todo.ParseQuickAdd(v); it.Text != "" {
				if it.Category == "" {
					it.Category = m.addCategory(idx)
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
				todo.ReconcileParent(p)
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
		case modeTags:
			if idx >= 0 {
				m.beforeMutate()
				cur := m.items[idx]
				// accept spaces as separators too; ParseTags lowercases, drops a
				// leading #, and dedups. Empty clears.
				cur.Tags = todo.ParseTags(strings.ReplaceAll(v, " ", ","))
				m.items[idx] = cur
				m.resort() // tags are the grouping axis in the tag view
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
		case modeBoardRename:
			old := ""
			if b := m.selectedBoard(); b != nil {
				old = b.Name
			}
			m.input.Blur()
			if v != "" && v != old {
				m.saveBeforeMove(old)
				if err := store.RenameBoard(old, v); err != nil {
					m.projNotice = err.Error()
				} else {
					return m.afterBoardChange(old, v), nil
				}
			}
			m.mode = modeBoards // no-op / invalid name: return to the picker
			return m, nil
		case modeBoardNew:
			if v == "" {
				m.input.Blur()
				m.mode = modeBoards
				return m, nil
			}
			if err := store.CreateBoard(v); err != nil {
				m.input.Blur()
				m.mode = modeBoards
				m.projNotice = err.Error()
				return m, nil
			}
			// board made — ask for its working dir before returning to the picker.
			m.projPending = v
			m.prompt(modeBoardDir, "", "working dir (optional, enter to skip)")
			return m, nil
		case modeBoardDir:
			m.input.Blur()
			// 'd' edit: empty clears the dir; creation flow: empty just skips it.
			if v != "" || m.projDirEdit {
				if err := store.SetBoardDir(m.projPending, v); err != nil {
					m.projNotice = err.Error()
				}
			}
			m.setBoards(store.Boards())
			for i, b := range m.projRows { // land on the board
				if b.Name == m.projPending {
					m.projCur = i
					break
				}
			}
			if m.projDirEdit { // opened from detail: return there to show the change
				m.projDirEdit = false
				m.mode = modeBoardDetail
			} else {
				m.mode = modeBoards
			}
			m.projPending = ""
			return m, nil
		}
		m.input.Blur()
		if m.mode == modeList { // a field editor just saved; the detail view gets it back
			m.mode = m.afterFieldEdit()
		}
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

// chordKey collects two-key sequences ("g b") on the board views: a key that
// starts one waits for the next, and the pair is matched as a single key.
func (m model) chordKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.keyNotice = ""
	if m.pending != "" {
		k := chord(m.pending + " " + msg.String())
		m.pending = ""
		return m.boardKey(k)
	}
	if chordPrefix(msg.String()) {
		m.pending = msg.String()
		return m, nil
	}
	return m.boardKey(msg)
}

// boardKey routes a key or completed sequence to the list, global or detail
// handler.
func (m model) boardKey(k fmt.Stringer) (tea.Model, tea.Cmd) {
	switch {
	case m.mode == modeDetail:
		return m.updateDetail(k)
	case m.global:
		return m.updateGlobal(k)
	}
	return m.updateList(k)
}

// dispatchKey routes a key press to the open mode's handler.
func (m model) dispatchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" { // universal abort, reaches text/detail/help/confirm too
		return m.quit()
	}
	var res tea.Model
	var cmd tea.Cmd
	switch m.mode {
	case modeAdd, modeAddSub, modeEdit, modeCategory, modeTags, modeDue, modeDefer, modeLink, modeFilter, modeBoardRename, modeBoardNew, modeBoardDir:
		res, cmd = m.updateInput(msg)
	case modeConfirmDelete:
		res, cmd = m.updateConfirmDelete(msg)
	case modeNote:
		res, cmd = m.updateNote(msg)
	case modeHelp:
		res, cmd = m.updateHelp(msg)
	case modeArchive:
		res, cmd = m.updateArchive(msg)
	case modeBoards:
		res, cmd = m.updateBoards(msg)
	case modeBoardDetail:
		res, cmd = m.updateBoardDetail(msg)
	case modeSettings:
		res, cmd = m.updateSettings(msg)
	case modeSettingEdit:
		res, cmd = m.updateSettingEdit(msg)
	case modeTheme:
		res, cmd = m.updateTheme(msg)
	case modeAgents:
		res, cmd = m.updateAgents(msg)
	case modeAgentLog:
		res, cmd = m.updateLog(msg)
	default: // list, global and detail views
		res, cmd = m.chordKey(msg)
	}
	if nm, ok := res.(model); ok {
		nm.refreshDirty() // one compare per event, not per render
		return nm, cmd
	}
	return res, cmd
}
