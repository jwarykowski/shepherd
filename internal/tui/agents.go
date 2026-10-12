package tui

// agents.go: the board's view of drover. Rows carry a mark for their item's
// run (waiting, queued, running, failed, or review once a run is done and the
// item isn't), @ sends the selected item to the coding agent, and R opens the
// agents view: every run on every board and source, with its log and an
// archive. See internal/drover.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"shepherd/internal/drover"
	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// runsEvery is how often the board asks drover what it's doing.
const runsEvery = 3 * time.Second

// runsMsg carries drover's runs (live, and finished within drover.Recent) and
// whether drover is running. installed is false when drover isn't on the PATH.
// poll marks the periodic fetch, which schedules the next one; a refresh after
// @ doesn't, so there's only ever one polling loop.
type runsMsg struct {
	runs      []drover.Run
	watching  bool
	installed bool
	poll      bool
}

// sentMsg is sent when @ has handed an item (or retried a run) through drover.
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
		return warnStyle.Render("waiting")
	case "queued":
		return dimStyle.Render("queued")
	case "running":
		return progStyle.Render("running")
	case "failed":
		return failStyle.Render("failed")
	case "done":
		if !it.Done {
			return boxStyle.Render("review")
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
			return "drover isn't running, so nothing will run: shepherd agents start"
		}
	}
	return ""
}

// sendToAgent hands the selected item or subtask to drover (@): a run drover
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

// sent reports how @ went, and refreshes the marks straight away.
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
	{"waiting", "waiting"}, {"failed", "failed"}, {"running", "running"}, {"queued", "queued"}, {"done", "done today"},
}

// agentRows is the agents view's selectable runs, in display order: by state,
// then runs of standalone tasks and sources before subtask runs, which sit
// together under their parent. Finished runs show for the day they finished.
func (m model) agentRows() []drover.Run {
	today := time.Now().Format("2006-01-02")
	var out []drover.Run
	for _, g := range agentGroups {
		var solo, subs []drover.Run
		for _, r := range m.allRuns {
			if r.State() != g.state {
				continue
			}
			if g.state == "done" && r.When().Local().Format("2006-01-02") != today {
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
	return m, loadRuns
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

func stateStyle(state string) func(...string) string {
	switch state {
	case "waiting":
		return warnStyle.Render
	case "running":
		return progStyle.Render
	case "failed":
		return failStyle.Render
	case "done":
		return boxStyle.Render
	}
	return dimStyle.Render
}

// agentsView renders every run, grouped by state, or the archive (e).
func (m model) agentsView() string {
	w := m.width()
	var out []string
	cursorLine := 0
	row := func(i int, r drover.Run, indent string) {
		title, gone := m.runTitle(r)
		if gone {
			title += " " + dimStyle.Render("(task removed)")
		}
		st := r.State()
		left := indent + stateStyle(st)(stateGlyph[st]) + " " + title
		if r.External() {
			left += " " + warnStyle.Render("external")
		}
		right := dimStyle.Render(m.runWhere(r)) + "  " + dimStyle.Render(runDetail(r))
		line := spread(w, left, right)
		if ansi.StringWidth(line) > w {
			line = spread(w, left, dimStyle.Render(m.runWhere(r)))
		}
		if i == m.agCur {
			cursorLine = len(out)
			line = cursorStyle.Width(w).Render(ansi.Strip(line))
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
			out = append(out, dimStyle.Render("no runs: press @ on a task to send it to your agent"))
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
				out = append(out, stateStyle(st)(label))
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

	rule := ruleStyle.Render(strings.Repeat("┈", w))
	var foot []string
	if d := m.droverDown(); d != "" {
		foot = append(foot, failStyle.Render(d))
	}
	if m.notice != "" {
		foot = append(foot, warnStyle.Render(m.notice))
	}
	keys := "@ go/retry · l log · o open · enter task · C archive · e archive view · esc back"
	if m.agArchive {
		keys = "@ run again · l log · o open · enter task · e live runs · esc back"
	}
	foot = append(foot, dimStyle.Render(keys))
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
		for _, g := range agentGroups[:4] {
			n := 0
			for _, r := range m.allRuns {
				if r.State() == g.state {
					n++
				}
			}
			if n > 0 {
				parts = append(parts, stateStyle(g.state)(fmt.Sprintf("%d %s", n, g.state)))
			}
		}
		if len(parts) > 0 {
			right += "  " + strings.Join(parts, dimStyle.Render(" · "))
		}
	} else {
		right += "  " + countStyle.Render(fmt.Sprint(count))
	}
	return spread(w, titleLeft(), right) + "\n" + ruleStyle.Render(strings.Repeat("┈", w))
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

func (m model) updateAgents(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	n := len(m.agentRows())
	if m.agArchive {
		n = len(m.agArchiveRuns)
	}
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc", "R":
		if m.agArchive {
			m.agArchive, m.agCur = false, 0
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "j", "down":
		if m.agCur < n-1 {
			m.agCur++
		}
		return m, nil
	case "k", "up":
		if m.agCur > 0 {
			m.agCur--
		}
		return m, nil
	case "e":
		m.agArchive, m.agCur = !m.agArchive, 0
		if m.agArchive {
			return m, func() tea.Msg {
				runs, err := drover.AllRuns()
				var done []drover.Run
				for _, r := range runs {
					if r.Done {
						done = append(done, r)
					}
				}
				return archiveRunsMsg{runs: done, err: err}
			}
		}
		return m, nil
	}
	r, ok := m.selectedRun()
	if !ok {
		return m, nil
	}
	switch msg.String() {
	case "@":
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
	case "l", "enter":
		if msg.String() == "enter" {
			if bi, isItem := m.itemIdx[r.Subject]; isItem {
				return m.jumpTo(bi)
			}
		}
		return m.openLog(r, modeAgents)
	case "o":
		if u := r.URL(); u != "" {
			return m, openLink(u)
		}
		m.notice = "this run has no link"
	case "C":
		if m.agArchive {
			return m, nil
		}
		if r.Running() {
			m.notice = "still running: archive it once it finishes"
			return m, nil
		}
		if r.Done {
			m.notice = "finished runs are archived already; they leave this list tomorrow"
			return m, nil
		}
		m.notice = "archived: " + r.Text
		return m, func() tea.Msg { return sentMsg{text: r.Text, err: drover.Archive(r.ID)} }
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
	head := spread(w, titleLeft(), dimStyle.Render("log")+"  "+stateStyle(st)(st)) + "\n" +
		ruleStyle.Render(strings.Repeat("┈", w))
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
		out = append(out, failStyle.Render(m.logErr))
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
			text = boxStyle.Render(text)
		case "stderr":
			text = failStyle.Render(text)
		}
		out = append(out, dimStyle.Render(l.Time)+"  "+ansi.Truncate(text, w-10, "…"))
	}
	footer := ruleStyle.Render(strings.Repeat("┈", w)) + "\n" + dimStyle.Render("j/k scroll · o open link · esc back")
	if m.logScroll > len(out)-1 {
		m.logScroll = len(out) - 1
	}
	out = m.windowRows(out[m.logScroll:], 0, lines(footer))
	return m.frame(head+"\n"+strings.Join(out, "\n"), footer)
}

func (m model) updateLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "esc", "l":
		m.mode = m.logBack
	case "j", "down":
		m.logScroll++
	case "k", "up":
		if m.logScroll > 0 {
			m.logScroll--
		}
	case "o":
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
		b.WriteString(dimStyle.Render("(none — press @ to send it to your agent)") + "\n")
		return b.String()
	}
	const shown = 5
	for i, r := range runs {
		if i == shown {
			b.WriteString(dimStyle.Render(fmt.Sprintf("… %d more in the agents view (R, then e)", len(runs)-shown)) + "\n")
			break
		}
		st := r.State()
		when := ""
		if t := r.When(); !t.IsZero() {
			when = ago(t)
		}
		line := stateStyle(st)(fmt.Sprintf("%s %-8s", stateGlyph[st], st)) + " " + dimStyle.Render(fmt.Sprintf("%-9s", when)) + " " + r.Note
		b.WriteString(ansi.Truncate(line, m.width(), "…") + "\n")
	}
	return b.String()
}
