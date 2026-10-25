package tui

// agents.go: the board's view of drover. Rows carry a mark for their item's
// run (waiting, queued, running, failed, or review once a run is done and the
// item isn't), r sends the selected item to the coding agent, and gr opens the
// agents view: every run on every board and source, with its log and an
// archive. See internal/drover.

import (
	"fmt"
	"shepherd/internal/drover"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"strconv"
	"strings"
	"time"

	binding "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// runsEvery is how often the board asks drover what it's doing.
const runsEvery = 3 * time.Second

// runsMsg carries drover's runs (live, and finished within drover.Recent) and
// whether drover is running. installed is false when drover isn't on the PATH.
// poll marks the periodic fetch, which schedules the next one; a refresh after
// r doesn't, so there's only ever one polling loop.
type runsMsg struct {
	runs      []drover.Run
	watching  bool
	installed bool
	poll      bool
}

// sentMsg is sent when r has handed an item (or retried a run) through drover.
type sentMsg struct {
	text string
	runs []drover.Run
	err  error
}

// logMsg carries a run's log for the log view.
type logMsg struct {
	rows []drover.LogRow
	err  error
}

// archiveRunsMsg carries every run drover keeps, for the agents archive.
// takeOverMsg is sent when a taken-over conversation ends.
type takeOverMsg struct{ err error }

// actionsMsg carries the actions' labels.
type actionsMsg struct{ names map[string]string }

func loadActions() tea.Msg {
	acts, err := drover.Actions()
	if err != nil {
		return actionsMsg{}
	}
	names := map[string]string{}
	for _, a := range acts {
		label := a.Name
		if a.Source != "" {
			label = strings.TrimPrefix(label, a.Source+": ")
		}
		names[a.ID] = label
	}
	return actionsMsg{names: names}
}

type archiveRunsMsg struct {
	runs []drover.Run
	err  error
}

// fetchRuns asks drover for its runs and whether it's watching.
func fetchRuns(poll bool) tea.Msg {
	if !drover.Installed() {
		return runsMsg{poll: poll}
	}
	runs, err := drover.Runs()
	if err != nil {
		return runsMsg{installed: true, poll: poll}
	}
	return runsMsg{runs: runs, watching: drover.Watching(), installed: true, poll: poll}
}

// loadRuns refreshes the runs now, outside the polling loop.
func loadRuns() tea.Msg { return fetchRuns(false) }

// firstRuns starts the polling loop.
func firstRuns() tea.Msg { return fetchRuns(true) }

// pollRuns asks drover for its runs after runsEvery, keeping the loop going.
func pollRuns() tea.Cmd {
	return tea.Tick(runsEvery, func(time.Time) tea.Msg { return fetchRuns(true) })
}

// gotRuns stores a fetch and, for the polling loop, schedules the next one.
func (m model) gotRuns(msg runsMsg) (tea.Model, tea.Cmd) {
	m.allRuns, m.watching, m.droverOn = msg.runs, msg.watching, msg.installed
	m.runs = drover.BySubject(msg.runs)
	if m.mode == modeAgents {
		m.indexItems()
		m.clampAgents()
	}
	if msg.poll {
		return m, pollRuns()
	}
	return m, nil
}

// runLabel is the mark an item's run puts on its row, or "". A finished run
// marks an open item for review: there's work (often a pull request) to look at.
func (m model) runLabel(it todo.Item) string {
	r, ok := m.runs[it.ID]
	if !ok || it.ID == "" {
		return ""
	}
	switch r.State() {
	case "waiting":
		return m.st().warn.Render("waiting")
	case "queued":
		return dimStyle.Render("queued")
	case "running":
		return m.st().prog.Render("running")
	case "failed":
		return m.st().prio['H'].Render("failed")
	case "done":
		if !it.Done {
			return m.st().box.Render("review")
		}
	}
	return ""
}

// droverDown is the warning shown when runs are waiting on a drover that isn't
// running, or "" when there's nothing to warn about.
func (m model) droverDown() string {
	if !m.droverOn || m.watching {
		return ""
	}
	for _, r := range m.allRuns {
		if r.Waiting() || r.Active() {
			return "agents aren't running, so nothing will start: shepherd agents start"
		}
	}
	return ""
}

// sendToAgent hands the selected item or subtask to drover (r): a run drover
// holds for it is released, a failed one retried, otherwise one starts now. A
// subtask goes with its parent, so the agent knows what it's one part of. The
// board is saved first so a new item has the id drover tracks it by.
func (m model) sendToAgent() (tea.Model, tea.Cmd) {
	if m.global {
		m.notice = "open the item's board to send it to your agent"
		return m, nil
	}
	ref := m.selRef()
	if ref.item < 0 {
		return m, nil
	}
	_ = store.Save(m.path, m.items)
	m.markSaved()
	m.lastMod = store.FileModTime(m.path)
	it := m.rowItem(ref)
	var parent *todo.Item
	if ref.sub >= 0 {
		p := m.items[ref.item]
		parent = &p
	}
	return m.send(drover.Task{Item: it, Parent: parent, Board: m.board})
}

func (m model) send(task drover.Task) (tea.Model, tea.Cmd) {
	if task.Item.Done {
		m.notice = "that item is already done"
		return m, nil
	}
	if !drover.Installed() {
		m.notice = drover.ErrMissing.Error()
		return m, nil
	}
	m.notice = "sending to your agent…"
	return m, func() tea.Msg {
		runs, err := drover.Send(task)
		return sentMsg{text: task.Item.Text, runs: runs, err: err}
	}
}

// sent reports how r went, and refreshes the marks straight away.
func (m model) sent(msg sentMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.notice = msg.err.Error()
		if drover.Installed() && strings.Contains(m.notice, "no action") {
			m.notice += " (shepherd agents setup adds one)"
		}
	case len(msg.runs) > 0 && msg.runs[0].Running():
		m.notice = "already running: " + msg.text
	default:
		m.notice = "sent to your agent: " + msg.text
	}
	if d := m.droverDown(); d != "" && msg.err == nil {
		m.notice += " · " + d
	}
	return m, loadRuns
}

// ---- the agents view (R) ----------------------------------------------------

// boardItem is where a run's subject lives on the boards: the item, its parent
// when it's a subtask, and the board.
type boardItem struct {
	item   todo.Item
	parent *todo.Item
	board  string
}

// indexItems maps every item and subtask on every board by id, so the agents
// view shows each run under its task's current title, grouped under its parent.
func (m *model) indexItems() {
	idx := map[string]boardItem{}
	add := func(items []todo.Item, board func(todo.Item) string) {
		for i := range items {
			it := items[i]
			idx[it.ID] = boardItem{item: it, board: board(it)}
			for _, s := range it.Subs {
				p := items[i]
				idx[s.ID] = boardItem{item: s, parent: &p, board: board(it)}
			}
		}
	}
	add(store.LoadAll(), func(it todo.Item) string { return it.Source })
	if !m.global { // the open board as it stands, unsaved edits and all
		here := m.board
		if here == "" {
			here = "default"
		}
		add(m.items, func(todo.Item) string { return here })
	}
	delete(idx, "")
	m.itemIdx = idx
}

// agentGroups is the agents view's order: what needs you first.
var agentGroups = []struct{ state, label string }{
	{"waiting", "waiting"}, {"failed", "failed"}, {"running", "running"}, {"queued", "queued"}, {"done", "done"},
}

// agentRows is the agents view's selectable runs, in display order: by state,
// then runs of standalone tasks and sources before subtask runs, which sit
// together under their parent. A thread shows only its newest run. Finished
// runs stay until they're archived.
func (m model) agentRows() []drover.Run {
	heads, _ := threadHeads(m.allRuns)
	var out []drover.Run
	for _, g := range agentGroups {
		var solo, subs []drover.Run
		for _, r := range heads {
			if r.State() != g.state {
				continue
			}
			if bi, ok := m.itemIdx[r.Subject]; ok && bi.parent != nil {
				subs = append(subs, r)
			} else {
				solo = append(solo, r)
			}
		}
		// stable: subtask runs of one parent stay together, newest first within it
		parentOf := func(r drover.Run) string { return m.itemIdx[r.Subject].parent.ID }
		for i := 1; i < len(subs); i++ {
			for j := i; j > 0 && parentOf(subs[j]) < parentOf(subs[j-1]); j-- {
				subs[j], subs[j-1] = subs[j-1], subs[j]
			}
		}
		out = append(out, solo...)
		out = append(out, subs...)
	}
	return out
}

func (m *model) clampAgents() {
	n := len(m.agentRows())
	if m.agArchive {
		n = len(m.agArchiveRuns)
	}
	if m.agCur >= n {
		m.agCur = n - 1
	}
	if m.agCur < 0 {
		m.agCur = 0
	}
}

// openAgents switches to the agents view.
func (m model) openAgents() (tea.Model, tea.Cmd) {
	m.mode = modeAgents
	m.agArchive = false
	m.agCur = 0
	m.indexItems()
	return m, tea.Batch(loadRuns, loadActions)
}

// threadHeads is each thread's newest live run, with how many live runs each
// thread has; runs from before threads stand alone. runs are newest first.
func threadHeads(runs []drover.Run) ([]drover.Run, map[string]int) {
	var out []drover.Run
	count := map[string]int{}
	for _, r := range runs {
		if r.Archived {
			continue
		}
		if r.Thread != "" {
			count[r.Thread]++
			if count[r.Thread] > 1 {
				continue
			}
		}
		out = append(out, r)
	}
	return out, count
}

// runTitle is the run's task's current title when it's a board item, the run's
// own text otherwise, and says so when the task has gone.
func (m model) runTitle(r drover.Run) (title string, gone bool) {
	if bi, ok := m.itemIdx[r.Subject]; ok {
		return bi.item.Text, false
	}
	if r.Source == "cli" || strings.HasPrefix(r.Source, "shepherd/") {
		return r.Text, true // it was a board item, and isn't any more
	}
	return r.Text, false
}

// runWhere is the run's origin: the board and ref of its task, or its source.
func (m model) runWhere(r drover.Run) string {
	if bi, ok := m.itemIdx[r.Subject]; ok {
		if bi.board != "" {
			return bi.board
		}
		return "board"
	}
	if v := r.Data["via"]; v != "" {
		return v
	}
	return strings.TrimPrefix(r.Source, "shepherd/")
}

// runDetail is the right-hand text of a run's row: why it stopped, what it
// produced, or when it started.
func runDetail(r drover.Run) string {
	switch r.State() {
	case "failed", "done":
		if r.Note != "" {
			return r.Note
		}
	}
	if t := r.When(); !t.IsZero() {
		return ago(t)
	}
	return ""
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return t.Local().Format("15:04")
	}
	return t.Local().Format("2 Jan")
}

var stateGlyph = map[string]string{"waiting": "○", "queued": "◌", "running": "●", "failed": "✗", "done": "✓"}

func (m model) stateStyle(state string) func(...string) string {
	switch state {
	case "waiting":
		return m.st().warn.Render
	case "running":
		return m.st().prog.Render
	case "failed":
		return m.st().prio['H'].Render
	case "done":
		return m.st().box.Render
	}
	return dimStyle.Render
}

// agentsView renders every run, grouped by state, or the archive (e).
func (m model) agentsView() string {
	w := m.width()
	var out []string
	cursorLine := 0
	heads, inThread := threadHeads(m.allRuns)
	threadsOn := map[string]int{} // a subject's threads: more than one names each by its action
	for _, r := range heads {
		threadsOn[r.Subject]++
	}
	row := func(i int, r drover.Run, indent string) {
		title, gone := m.runTitle(r)
		if gone {
			title += " " + dimStyle.Render("(task removed)")
		}
		st := r.State()
		left := indent + m.stateStyle(st)(stateGlyph[st]) + " " + title
		if name := m.actionNames[r.Action]; name != "" && threadsOn[r.Subject] > 1 && !m.agArchive {
			left += " " + dimStyle.Render("· "+name)
		}
		if r.External() {
			left += " " + m.st().warn.Render("external")
		}
		if n := inThread[r.Thread]; n > 1 && !m.agArchive {
			left += " " + dimStyle.Render(fmt.Sprintf("· %d runs", n))
		}
		if r.Events > 0 {
			left += " " + dimStyle.Render(fmt.Sprintf("· +%d event%s", r.Events, plural(r.Events)))
		}
		right := dimStyle.Render(m.runWhere(r)) + "  " + dimStyle.Render(runDetail(r))
		line := spread(w, left, right)
		if ansi.StringWidth(line) > w {
			line = spread(w, left, dimStyle.Render(m.runWhere(r)))
		}
		if i == m.agCur {
			cursorLine = len(out)
			line = m.st().cursor.Width(w).Render(ansi.Strip(line))
		}
		out = append(out, line)
	}

	var title string
	var count int
	if m.agArchive {
		title, count = "agents · archive", len(m.agArchiveRuns)
		if len(m.agArchiveRuns) == 0 {
			out = append(out, dimStyle.Render("nothing archived"))
		}
		for i, r := range m.agArchiveRuns {
			row(i, r, "  ")
		}
	} else {
		rows := m.agentRows()
		title, count = "agents", len(rows)
		if len(rows) == 0 {
			out = append(out, dimStyle.Render("no runs: press "+keys.Run.Help().Key+" on a task to send it to your agent"))
		}
		i, lastState, lastParent := 0, "", ""
		for _, r := range rows {
			st := r.State()
			if st != lastState {
				label := st
				for _, g := range agentGroups {
					if g.state == st {
						label = g.label
					}
				}
				out = append(out, m.stateStyle(st)(label))
				lastState, lastParent = st, ""
			}
			indent := "  "
			if bi, ok := m.itemIdx[r.Subject]; ok && bi.parent != nil {
				if bi.parent.ID != lastParent {
					out = append(out, "  "+dimStyle.Render(bi.parent.Text))
					lastParent = bi.parent.ID
				}
				indent = "    "
			} else {
				lastParent = ""
			}
			row(i, r, indent)
			i++
		}
	}

	rule := m.st().rule.Render(strings.Repeat("┈", w))
	var foot []string
	if d := m.droverDown(); d != "" {
		foot = append(foot, m.st().prio['H'].Render(d))
	}
	if m.notice != "" {
		foot = append(foot, m.st().warn.Render(m.notice))
	}
	k := func(b binding.Binding) string { return b.Help().Key }
	hint := k(keys.Run) + " go/retry · l log · " + k(keys.Open) + " open · t take over · enter task · X archive · C clear finished · e archive · " + k(keys.Back) + " back"
	if m.agArchive {
		hint = k(keys.Run) + " run again · l log · " + k(keys.Open) + " open · enter task · e live runs · " + k(keys.Back) + " back"
	}
	foot = append(foot, dimStyle.Render(hint))
	footer := rule + "\n" + strings.Join(foot, "\n")
	out = m.windowRows(out, cursorLine, lines(footer))
	body := m.agentsHeader(title, count) + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// agentsHeader is the title line with a count per state.
func (m model) agentsHeader(title string, count int) string {
	w := m.width()
	right := dimStyle.Render(title)
	if !m.agArchive {
		var parts []string
		heads, _ := threadHeads(m.allRuns)
		for _, g := range agentGroups[:4] {
			n := 0
			for _, r := range heads {
				if r.State() == g.state {
					n++
				}
			}
			if n > 0 {
				parts = append(parts, m.stateStyle(g.state)(fmt.Sprintf("%d %s", n, g.state)))
			}
		}
		if len(parts) > 0 {
			right += "  " + strings.Join(parts, dimStyle.Render(" · "))
		}
	} else {
		right += "  " + countStyle.Render(strconv.Itoa(count))
	}
	return spread(w, titleLeft(), right) + "\n" + m.st().rule.Render(strings.Repeat("┈", w))
}

func (m model) selectedRun() (drover.Run, bool) {
	rows := m.agentRows()
	if m.agArchive {
		rows = m.agArchiveRuns
	}
	if m.agCur < 0 || m.agCur >= len(rows) {
		return drover.Run{}, false
	}
	return rows[m.agCur], true
}

func (m model) updateAgents(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	n := len(m.agentRows())
	if m.agArchive {
		n = len(m.agArchiveRuns)
	}
	switch {
	case binding.Matches(msg, keys.Quit):
		return m.quit()
	case binding.Matches(msg, keys.Back):
		if m.agArchive {
			m.agArchive, m.agCur = false, 0
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case binding.Matches(msg, keys.Down):
		if m.agCur < n-1 {
			m.agCur++
		}
		return m, nil
	case binding.Matches(msg, keys.Up):
		if m.agCur > 0 {
			m.agCur--
		}
		return m, nil
	case msg.String() == "e":
		m.agArchive, m.agCur = !m.agArchive, 0
		if m.agArchive {
			return m, func() tea.Msg {
				runs, err := drover.AllRuns()
				var archived []drover.Run
				for _, r := range runs {
					if r.Archived {
						archived = append(archived, r)
					}
				}
				return archiveRunsMsg{runs: archived, err: err}
			}
		}
		return m, nil
	}
	if msg.String() == "C" && !m.agArchive {
		var ids []string
		for _, r := range m.agentRows() {
			if r.Done {
				ids = append(ids, r.ID)
			}
		}
		if len(ids) == 0 {
			m.notice = "no finished runs to clear"
			return m, nil
		}
		return m, archiveCmd(fmt.Sprintf("cleared %d finished run%s into the archive", len(ids), plural(len(ids))), ids...)
	}
	r, ok := m.selectedRun()
	if !ok {
		return m, nil
	}
	switch {
	case binding.Matches(msg, keys.Run):
		if bi, isItem := m.itemIdx[r.Subject]; isItem {
			if r.Running() || r.Queued() {
				m.notice = "already " + r.State() + ": " + bi.item.Text
				return m, nil
			}
			return m.send(drover.Task{Item: bi.item, Parent: bi.parent, Board: boardName(bi.board)})
		}
		if r.Running() || r.Queued() {
			m.notice = "already " + r.State() + ": " + r.Text
			return m, nil
		}
		m.notice = "sending to your agent…"
		return m, func() tea.Msg { return sentMsg{text: r.Text, err: drover.Retry(r)} }
	case msg.String() == "l" || msg.String() == "enter":
		if msg.String() == "enter" {
			if bi, isItem := m.itemIdx[r.Subject]; isItem {
				return m.jumpTo(bi)
			}
		}
		return m.openLog(r, modeAgents)
	case binding.Matches(msg, keys.Open):
		if u := r.URL(); u != "" {
			return m, openLink(u)
		}
		m.notice = "this run has no link"
	case msg.String() == "t":
		if r.Running() {
			m.notice = "still running: take over once it stops"
			return m, nil
		}
		c := r.TakeOver()
		for _, o := range m.allRuns { // a waiting run carries on its thread's conversation
			if c == nil && r.Thread != "" && o.Thread == r.Thread {
				c = o.TakeOver()
			}
		}
		if c == nil {
			m.notice = "no conversation to take over: the run hasn't started, or its worktree is gone"
			return m, nil
		}
		return m, tea.ExecProcess(c, func(err error) tea.Msg { return takeOverMsg{err: err} })
	case msg.String() == "X":
		if m.agArchive {
			return m, nil
		}
		if r.Running() {
			m.notice = "still running: archive it once it finishes"
			return m, nil
		}
		title, _ := m.runTitle(r)
		return m, archiveCmd("archived: "+title, r.ID)
	}
	return m, nil
}

// boardName turns an item's board label back into the board argument: the
// default board is "".
func boardName(label string) string {
	if label == "default" {
		return ""
	}
	return label
}

// jumpTo opens the run's task in its detail view, when it's on this board.
func (m model) jumpTo(bi boardItem) (tea.Model, tea.Cmd) {
	if boardName(bi.board) != m.board || m.global {
		m.notice = fmt.Sprintf("that task is on the %s board: b to switch boards", bi.board)
		return m, nil
	}
	for i, r := range m.rows() {
		if m.rowItem(r).ID == bi.item.ID {
			m.view = viewCategory
			m.filter = ""
			m.cursor = i
			m.mode = modeDetail
			m.detailBack = modeAgents
			return m, nil
		}
	}
	m.notice = "can't find that task on the board"
	return m, nil
}

// ---- a run's log (l) --------------------------------------------------------

func (m model) openLog(r drover.Run, back mode) (tea.Model, tea.Cmd) {
	title, _ := m.runTitle(r)
	m.logRun, m.logTitle, m.logBack = r, title, back
	m.logRows, m.logErr, m.logScroll = nil, "", 0
	m.mode = modeAgentLog
	return m, func() tea.Msg {
		rows, err := drover.Log(r.ID)
		return logMsg{rows: rows, err: err}
	}
}

func (m model) logView() string {
	w := m.width()
	r := m.logRun
	st := r.State()
	head := spread(w, titleLeft(), dimStyle.Render("log")+"  "+m.stateStyle(st)(st)) + "\n" +
		m.st().rule.Render(strings.Repeat("┈", w))
	var out []string
	out = append(out, m.logTitle)
	meta := []string{m.runWhere(r)}
	if t := r.When(); !t.IsZero() {
		meta = append(meta, ago(t))
	}
	if r.Note != "" {
		meta = append(meta, r.Note)
	}
	out = append(out, dimStyle.Render(strings.Join(meta, " · ")), "")
	switch {
	case m.logErr != "":
		out = append(out, m.st().prio['H'].Render(m.logErr))
	case m.logRows == nil:
		out = append(out, dimStyle.Render("loading…"))
	case len(m.logRows) == 0:
		out = append(out, dimStyle.Render("no log yet: nothing has run"))
	}
	for _, l := range m.logRows {
		text := l.Text
		switch l.Kind {
		case "tool":
			text = dimStyle.Render("› " + text)
		case "result":
			text = m.st().box.Render(text)
		case "stderr":
			text = m.st().prio['H'].Render(text)
		}
		out = append(out, dimStyle.Render(l.Time)+"  "+ansi.Truncate(text, w-10, "…"))
	}
	footer := m.st().rule.Render(strings.Repeat("┈", w)) + "\n" + dimStyle.Render("j/k scroll · o open link · esc back")
	if m.logScroll > len(out)-1 {
		m.logScroll = len(out) - 1
	}
	out = m.windowRows(out[m.logScroll:], 0, lines(footer))
	return m.frame(head+"\n"+strings.Join(out, "\n"), footer)
}

func (m model) updateLog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case binding.Matches(msg, keys.Quit):
		return m.quit()
	case binding.Matches(msg, keys.Back), msg.String() == "l":
		m.mode = m.logBack
	case binding.Matches(msg, keys.Down):
		m.logScroll++
	case binding.Matches(msg, keys.Up):
		if m.logScroll > 0 {
			m.logScroll--
		}
	case binding.Matches(msg, keys.Open):
		if u := m.logRun.URL(); u != "" {
			return m, openLink(u)
		}
	}
	return m, nil
}

// itemRuns is an item's runs, newest first, for its detail view.
func (m model) itemRuns(id string) []drover.Run {
	var out []drover.Run
	for _, r := range m.allRuns {
		if id != "" && r.Subject == id {
			out = append(out, r)
		}
	}
	return out
}

// detailRuns is the detail view's runs section: the item's runs, newest first,
// each with its state, when, and what it said. Empty without drover.
func (m model) detailRuns(it todo.Item) string {
	if !m.droverOn {
		return ""
	}
	runs := m.itemRuns(it.ID)
	var b strings.Builder
	b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("runs (%d)", len(runs))) + "\n")
	if len(runs) == 0 {
		b.WriteString(dimStyle.Render("(none — press "+keys.Run.Help().Key+" to send it to your agent)") + "\n")
		return b.String()
	}
	const shown = 5
	for i, r := range runs {
		if i == shown {
			b.WriteString(dimStyle.Render(fmt.Sprintf("… %d more in the agents view (%s, then e)", len(runs)-shown, keys.Runs.Help().Key)) + "\n")
			break
		}
		st := r.State()
		when := ""
		if t := r.When(); !t.IsZero() {
			when = ago(t)
		}
		line := m.stateStyle(st)(fmt.Sprintf("%s %-8s", stateGlyph[st], st)) + " " + dimStyle.Render(fmt.Sprintf("%-9s", when)) + " " + r.Note
		if r.Result != "" {
			line += " " + dimStyle.Render(r.Result)
		}
		b.WriteString(ansi.Truncate(line, m.width(), "…") + "\n")
	}
	return b.String()
}

// archivedMsg reports an archive from the agents view.
type archivedMsg struct {
	notice string
	err    error
}

// archiveCmd archives runs in the background, then refreshes the view.
func archiveCmd(notice string, ids ...string) tea.Cmd {
	return func() tea.Msg { return archivedMsg{notice: notice, err: drover.Archive(ids...)} }
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
