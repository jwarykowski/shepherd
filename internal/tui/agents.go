package tui

// agents.go: the board's view of drover. Items drover is holding show as
// waiting, items it's working on as running, and items whose run stopped short
// as failed; @ sends the selected item to the coding agent, or retries it (see
// internal/drover).

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"shepherd/internal/drover"
	"shepherd/internal/store"
)

// runsEvery is how often the board asks drover what it's doing.
const runsEvery = 3 * time.Second

// runsMsg carries drover's live runs, keyed by board item id; nil when drover
// isn't installed or didn't answer.
type runsMsg map[string]drover.Run

// sentMsg is sent when @ has handed an item to drover.
type sentMsg struct {
	text string
	runs []drover.Run
	err  error
}

// loadRuns asks drover for its runs now.
func loadRuns() tea.Msg {
	if !drover.Installed() {
		return runsMsg(nil)
	}
	runs, err := drover.Runs()
	if err != nil {
		return runsMsg(nil)
	}
	return runsMsg(drover.BySubject(runs))
}

// pollRuns asks drover for its runs after runsEvery.
func pollRuns() tea.Cmd {
	return tea.Tick(runsEvery, func(time.Time) tea.Msg { return loadRuns() })
}

// runLabel is the mark drover's state puts on an item's row, or "".
func (m model) runLabel(id string) string {
	r, ok := m.runs[id]
	switch {
	case !ok || id == "":
		return ""
	case r.Waiting():
		return warnStyle.Render("waiting")
	case r.Failed():
		return failStyle.Render("failed")
	default:
		return progStyle.Render("running")
	}
}

// sendToAgent hands the selected item or subtask to drover (@): a run drover
// holds for it is released, otherwise one starts now. The board is saved first
// so a new item has the id drover tracks it by.
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
	if it.Done {
		m.notice = "that item is already done"
		return m, nil
	}
	if !drover.Installed() {
		m.notice = drover.ErrMissing.Error()
		return m, nil
	}
	m.notice = "sending to your agent…"
	task := drover.Task{Item: it, Board: m.board}
	return m, func() tea.Msg {
		runs, err := drover.Send(task)
		return sentMsg{text: it.Text, runs: runs, err: err}
	}
}

// sent reports how @ went, and refreshes the run marks straight away.
func (m model) sent(msg sentMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.notice = msg.err.Error()
		if !drover.Installed() {
			break
		}
		m.notice += " (shepherd agents setup sets drover up)"
	case len(msg.runs) > 0 && msg.runs[0].Status == "running":
		m.notice = "already running: " + msg.text
	default:
		m.notice = "sent to your agent: " + msg.text
	}
	return m, loadRuns
}
