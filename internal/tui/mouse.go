package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// doubleClick is the gap within which a second click on the same row opens it.
const doubleClick = 400 * time.Millisecond

// rowMark tags the start of a rendered row with its lane and cursor position. It
// is a zero-width OSC string like cursorMark: View strips it and keeps where it
// landed, so a click can be mapped back to the row under it.
func rowMark(lane, pos int) string {
	return fmt.Sprintf("\x1b]1337;shepherd-row=%d;%d\x07", lane, pos)
}

var rowMarkRE = regexp.MustCompile(`\x1b\]1337;shepherd-row=(\d+);(\d+)\x07`)

// rowHit is a clickable row: the screen cell its mark was at.
type rowHit struct {
	x, y, lane, pos int
}

// cutRowMarks removes every row mark from s and reports where each one was.
func cutRowMarks(s string) (string, []rowHit) {
	if !strings.Contains(s, "shepherd-row=") {
		return s, nil
	}
	var hits []rowHit
	lines := strings.Split(s, "\n")
	for y, ln := range lines {
		for {
			loc := rowMarkRE.FindStringSubmatchIndex(ln)
			if loc == nil {
				break
			}
			lane, _ := strconv.Atoi(ln[loc[2]:loc[3]])
			pos, _ := strconv.Atoi(ln[loc[4]:loc[5]])
			hits = append(hits, rowHit{x: ansi.StringWidth(ln[:loc[0]]), y: y, lane: lane, pos: pos})
			ln = ln[:loc[0]] + ln[loc[1]:]
		}
		lines[y] = ln
	}
	return strings.Join(lines, "\n"), hits
}

// rowClickMsg is a left click on a row, resolved from the last frame's marks.
type rowClickMsg struct {
	lane, pos int
	at        time.Time
}

// onMouse turns a left click on a marked row into a rowClickMsg: the row on
// that line whose mark is nearest to the left of the click (lanes put several
// on one line).
func onMouse(hits []rowHit) func(tea.MouseMsg) tea.Cmd {
	return func(msg tea.MouseMsg) tea.Cmd {
		c, ok := msg.(tea.MouseClickMsg)
		if !ok || c.Button != tea.MouseLeft {
			return nil
		}
		var best *rowHit
		for i, h := range hits {
			if h.y == c.Y && (best == nil || h.x <= c.X && h.x > best.x) {
				best = &hits[i]
			}
		}
		if best == nil {
			return nil
		}
		hit := *best
		return func() tea.Msg { return rowClickMsg{lane: hit.lane, pos: hit.pos, at: time.Now()} }
	}
}

// clickRow moves the cursor to a clicked row; a second click on it within
// doubleClick opens its detail view.
func (m model) clickRow(msg rowClickMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeList {
		return m, nil
	}
	again := msg.lane == m.lastClick.lane && msg.pos == m.lastClick.pos && msg.at.Sub(m.lastClick.at) <= doubleClick
	if m.view == viewLane {
		m.lane = msg.lane
	}
	m.cursor = msg.pos
	m.clamp()
	if again {
		m.lastClick = rowClickMsg{}
		return m.boardKey(chord(keys.Detail.Keys()[0]))
	}
	m.lastClick = msg
	return m, nil
}

// wheel scrolls the screens that move with j/k; text entry and the detail
// pages ignore it.
func (m model) wheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeList, modeHelp, modeArchive, modeBoards, modeSettings, modeTheme:
	default:
		return m, nil
	}
	if m.mode == modeList { // the board keys may be rebound
		switch msg.Button {
		case tea.MouseWheelDown:
			return m.boardKey(chord(keys.Down.Keys()[0]))
		case tea.MouseWheelUp:
			return m.boardKey(chord(keys.Up.Keys()[0]))
		}
	}
	switch msg.Button {
	case tea.MouseWheelDown:
		return m.dispatchKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	case tea.MouseWheelUp:
		return m.dispatchKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	}
	return m, nil
}
