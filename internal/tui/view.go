package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"shepherd/internal/store"
	"shepherd/internal/todo"
	"strconv"
	"strings"

	binding "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---- styles ----
var (
	dimStyle   = lipgloss.NewStyle().Faint(true)
	brandStyle = lipgloss.NewStyle().Bold(true)
	doneStyle  = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	countStyle = lipgloss.NewStyle().Faint(true)
	prioLabel  = map[byte]string{'H': "high", 'M': "medium", 'L': "low"}
)

// spread lays left flush-left and right flush-right across w columns, padding
// the middle (always at least one space, so a too-narrow pane still separates
// them). Measured with lipgloss.Width, so styled input counts its glyphs only.
func spread(w int, left, right string) string {
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m model) View() tea.View {
	content, hits := cutRowMarks(m.screen())
	content, at, found := cutCursorMark(content)
	v := tea.NewView(content)
	if found {
		v.Cursor = m.editorCursor(at)
	}
	if m.mouse {
		v.MouseMode = tea.MouseModeCellMotion
		v.OnMouse = onMouse(hits)
	}
	v.AltScreen = true
	v.ReportFocus = true
	v.WindowTitle = m.windowTitle()
	v.ProgressBar = m.progressBar()
	return v
}

// windowTitle names the board in the terminal/herdr tab: board, done/total,
// and a dot while there are unsaved edits.
func (m model) windowTitle() string {
	board := m.currentBoardName()
	if m.global {
		board = "all boards"
	}
	done, total := m.count()
	t := fmt.Sprintf("%s · %s · %d/%d", appName, board, done, total)
	if m.dirty {
		t += " ●"
	}
	return t
}

// noteHint is the note editor's footer; shift+enter only works where the
// terminal reports it apart from enter (Kitty keyboard protocol).
func (m model) noteHint() string {
	if m.kbdEnhanced {
		return "note: enter newline · shift+enter/esc done (saves as you type)"
	}
	return "note: enter newline · esc done (saves as you type)"
}

// progressBar mirrors done/total in the terminal's tab progress indicator
// (OSC 9;4); an empty board clears it.
func (m model) progressBar() *tea.ProgressBar {
	done, total := m.count()
	if total == 0 {
		return tea.NewProgressBar(tea.ProgressBarNone, 0)
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, done*100/total)
}

// cursorMark is a zero-width OSC string the text editors render in front of
// themselves, so View can find where they landed on screen and put the real
// terminal cursor there. Layout and truncation treat it like any escape code.
const cursorMark = "\x1b]1337;shepherd-cursor\x07"

// inputView is the shared text input, marked for the real cursor.
func (m model) inputView() string { return cursorMark + m.input.View() }

// noteView is the note editor, marked for the real cursor.
func (m model) noteView() string { return cursorMark + m.note.View() }

// cutCursorMark removes the cursor mark from s and reports the screen cell it
// was at.
func cutCursorMark(s string) (string, tea.Position, bool) {
	before, after, found := strings.Cut(s, cursorMark)
	if !found {
		return s, tea.Position{}, false
	}
	y := strings.Count(before, "\n")
	x := ansi.StringWidth(before[strings.LastIndex(before, "\n")+1:])
	return before + after, tea.Position{X: x, Y: y}, true
}

// editorCursor is the focused editor's own cursor, offset to where the editor
// sits on screen. Nil when the editor draws a virtual cursor or isn't focused.
func (m model) editorCursor(at tea.Position) *tea.Cursor {
	c := m.input.Cursor()
	if m.mode == modeNote {
		c = m.note.Cursor()
	}
	if c == nil {
		return nil
	}
	c.X += at.X
	c.Y += at.Y
	return c
}

// render is the whole screen as a string, the content behind View, without
// the cursor mark.
func (m model) render() string {
	s, _ := cutRowMarks(m.screen())
	s, _, _ = cutCursorMark(s)
	return s
}

// screen is render with the cursor mark still in place.
func (m model) screen() string {
	var content string
	switch {
	case m.mode == modeHelp:
		content = m.helpView()
	case m.mode == modeArchive:
		content = m.archiveView()
	case m.mode == modeBoardDetail || (m.mode == modeBoardDir && m.projDirEdit):
		content = m.boardDetailView()
	case m.mode == modeBoards || m.mode == modeBoardNew || m.mode == modeBoardDir || m.mode == modeBoardRename || m.mode == modeConfirmDelete:
		content = m.boardsView()
	case m.mode == modeSettings || m.mode == modeSettingEdit:
		content = m.settingsView()
	case m.mode == modeDetail || m.mode == modeNote || m.fieldEdit:
		// fieldEdit means an editor was opened from the detail view, so the item
		// stays on screen with the prompt in its footer instead of the list's.
		content = m.detailView()
	case m.view == viewTable:
		content = m.tableView()
	case m.view == viewLane:
		content = m.laneView()
	default:
		content = m.listView()
	}
	out := lipgloss.NewStyle().Padding(m.density.padY(), m.density.padX()).Render(content)
	if m.mode == modeTheme {
		out = m.themeOverlay(out)
	}
	return out
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
	h := max(m.height-2*m.density.padY(), 1)
	return h
}

// groupOf returns a stable group id (for change detection) and display label
// for an item under the active view; overdue items form a pinned top group.
func (m model) groupOf(it todo.Item) (id, label string) {
	if m.view == viewBoard {
		// group strictly by board (no overdue pin) so sources stay contiguous
		return "s" + it.Source, it.Source
	}
	if todo.Pinned(it) {
		return "\x00pin", "⚠ overdue"
	}
	if m.view == viewPriority {
		if lbl, ok := prioLabel[it.Prio]; ok {
			return fmt.Sprintf("p%d", todo.Rank(it.Prio)), lbl + " priority"
		}
		return "p9", "no priority"
	}
	if m.view == viewTag {
		if tag := todo.TagKey(it); tag != "" {
			return "t" + tag, "#" + tag
		}
		return "t\x01", "untagged"
	}
	if it.Category == "" {
		return "c\x01", "uncategorized"
	}
	return "c" + strings.ToLower(it.Category), it.Category
}

// groupCount returns done/total for the group an item belongs to. Pinned
// (overdue) items are excluded from their category/priority group (they show in
// the overdue group instead).
func (m model) groupCount(it todo.Item) (done, total int) {
	switch {
	case m.view == viewBoard:
		for _, x := range m.items {
			if x.Source == it.Source {
				total++
				if x.Done {
					done++
				}
			}
		}
	case todo.Pinned(it):
		for _, x := range m.items {
			if todo.Pinned(x) {
				total++
			}
		}
	case m.view == viewPriority:
		for _, x := range m.items {
			if !todo.Pinned(x) && x.Prio == it.Prio {
				total++
				if x.Done {
					done++
				}
			}
		}
	case m.view == viewTag:
		for _, x := range m.items {
			if !todo.Pinned(x) && todo.TagKey(x) == todo.TagKey(it) {
				total++
				if x.Done {
					done++
				}
			}
		}
	default:
		for _, x := range m.items {
			if !todo.Pinned(x) && x.Category == it.Category {
				total++
				if x.Done {
					done++
				}
			}
		}
	}
	return
}

// rowComplement is the flush-far-right label: the grouping axis the active view
// does *not* show in its headers. The priority view groups by priority, so its
// rows carry the category; every other view carries the priority. Empty when the
// item has no value for it.
func (m model) rowComplement(it todo.Item) string {
	if m.view == viewPriority {
		if it.Category == "" {
			return ""
		}
		return m.st().cat.Render(it.Category)
	}
	if lbl, ok := prioLabel[it.Prio]; ok {
		return m.st().prio[it.Prio].Render(lbl)
	}
	return ""
}

// rowContent renders one item's row at the given indent: the box (its shape the
// status — ○ open, ◐ named status, ✓ done), the text, then up to two flush-right
// values — the subtask progress or due/defer label, and the complement axis
// (see rowComplement) pinned far right. badge is the item's subtask progress, or
// "". Cursor highlight is applied by the caller.
func (m model) rowContent(it todo.Item, indent, badge string, isSub bool) string {
	w := m.width()
	box := "○"
	boxSt := m.st().box
	text := it.Text
	deferred := todo.Deferred(it)
	switch {
	case it.Done:
		box = "✓"
		text = doneStyle.Render(text)
	case it.Status != "":
		box = "◐"
		boxSt = m.st().prog
	case deferred:
		text = dimStyle.Render(text) // not started yet
	}
	if m.global && m.view != viewBoard && it.Source != "" {
		text += " " + dimStyle.Render("["+it.Source+"]")
	}
	text += claimSuffix(it)
	// In the tag view the group header names the grouping tag; show the item's
	// other tags after the text so a multi-tag item isn't misread as single-tag.
	if m.view == viewTag && len(it.Tags) > 1 {
		text += " " + dimStyle.Render("#"+strings.Join(it.Tags[1:], " #"))
	}
	// Progress/due slot: one value, so the column lines up row to row — subtask
	// progress when the item has subtasks, else the due/defer label. Nothing
	// urgent hides behind a badge: overdue parents are pinned to the ⚠ overdue group.
	label := ""
	switch {
	case badge != "":
		label = countStyle.Render(badge)
	case deferred:
		if lbl := todo.DeferLabel(it.Defer); lbl != "" {
			label = dimStyle.Render(lbl)
		}
	case it.Due != "" && (isSub || !todo.Pinned(it)):
		// parents hide the label when pinned to the ⚠ overdue group; subs have no
		// such group, so always show it (red when overdue).
		lbl, over := todo.DueLabel(it.Due)
		st := dimStyle
		if over {
			st = m.st().prio['H'] // red for due/overdue
		}
		label = st.Render(lbl)
	}
	if c := m.rowComplement(it); c != "" { // pinned far right, past the progress/due slot
		if label != "" {
			label += "  "
		}
		label += c
	}
	left := fmt.Sprintf("%s%s %s", indent, boxSt.Render(box), text)
	return spread(w, left, label)
}

func (m model) listView() string {
	w := m.width()
	vis := m.visible()
	var out []string // scrollable region, one entry per visual line
	cursorLine := 0
	if len(vis) == 0 {
		if m.filter != "" {
			out = append(out, dimStyle.Render("(no matches)"))
		} else {
			out = append(out, dimStyle.Render("(empty — press "+keys.Add.Help().Key+" to add)"))
		}
	}
	lastGroup := "\x00" // sentinel so the first group always prints a header
	for pos, r := range m.rows() {
		it := m.rowItem(r)
		indent, badge := "   ", ""
		if r.sub == -1 { // parent row: group header + subtask progress badge
			parent := m.items[r.item]
			if gid, label := m.groupOf(parent); gid != lastGroup {
				if lastGroup != "\x00" {
					out = append(out, "") // padding below the previous group
				}
				done, total := m.groupCount(parent)
				cnt := countStyle.Render(fmt.Sprintf("%d/%d", done, total))
				out = append(out, spread(w, m.st().cat.Render(label), cnt))
				lastGroup = gid
			}
			if d, t := todo.SubCount(it); t > 0 {
				badge = fmt.Sprintf("%d/%d", d, t)
			}
		} else {
			indent = "     " // subtasks indent one level under their parent
		}
		row := m.rowContent(it, indent, badge, r.sub >= 0)
		if pos == m.cursor {
			cursorLine = len(out)
			// full-width subtle highlight on the selected row; strip inner styles
			// first so their ANSI resets don't punch holes in the background.
			row = m.st().cursor.Width(w).Render(ansi.Truncate(ansi.Strip(row), w, "…"))
		}
		out = append(out, rowMark(0, pos)+row)
		if m.density == comfort {
			out = append(out, "") // roomier rows
		}
	}
	if am := m.archivedMatches(); len(am) > 0 {
		out = append(out, "", dimStyle.Render(fmt.Sprintf("archive · %d match", len(am))))
		for _, it := range am {
			out = append(out, "  "+dimStyle.Render(it.Text))
		}
	}

	footer := m.listFooter()
	out = m.windowRows(out, cursorLine, lines(footer))
	body := m.header() + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// scrollOffset is the top-of-viewport index that keeps cursorLine centred in
// a vh-tall viewport over total rows, clamped at both ends.
func scrollOffset(total, cursorLine, vh int) int {
	return min(max(cursorLine-vh/2, 0), total-vh)
}

// windowRows clips the list body to what fits between the header and footer,
// keeping the cursor line centred in the viewport (clamped at both ends). It
// returns rows unchanged when the terminal size is unknown or everything fits.
func (m model) windowRows(rows []string, cursorLine, footLines int) []string {
	ih := m.innerHeight()
	if ih == 0 {
		return rows // unknown size: let the terminal handle it
	}
	vh := max(ih-footLines-2, 1) // header line + frame's minimum pad line; at least the cursor row
	if len(rows) <= vh {
		return rows
	}
	off := scrollOffset(len(rows), cursorLine, vh)
	return rows[off : off+vh]
}

// archiveView renders the read-only archive browser: the board's archived items
// (or every board's, in the global view), windowed on the cursor.
func (m model) archiveView() string {
	w := m.width()
	var out []string
	cursorLine := 0
	if len(m.arcRows) == 0 {
		out = append(out, dimStyle.Render("no archived items"))
	}
	for i, it := range m.arcRows {
		left := "  " + m.st().box.Render("✓") + " " + dimStyle.Render(it.Text)
		row := left
		if m.global && it.Source != "" {
			row = spread(w, left, m.st().cat.Render("["+it.Source+"]"))
		}
		if i == m.arcCur {
			cursorLine = len(out)
			row = m.st().cursor.Width(w).Render(ansi.Truncate(ansi.Strip(row), w, "…"))
		}
		out = append(out, row)
	}
	footer := m.st().rule.Render(strings.Repeat("┈", w)) + "\n" +
		dimStyle.Render("browse archive · j/k scroll · esc back · q quit")
	out = m.windowRows(out, cursorLine, lines(footer))
	body := m.headerWith("archive", len(m.arcRows), len(m.arcRows)) + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// boardsView renders the board picker: one row per board with open/total
// counts, the current board marked, windowed on the cursor.
func (m model) boardsView() string {
	w := m.width()
	cur := m.board
	if cur == "" {
		cur = "default"
	}
	var out []string
	cursorLine := 0
	if len(m.projRows) == 0 {
		if m.projArchived {
			out = append(out, dimStyle.Render("no archived boards"))
		} else {
			out = append(out, dimStyle.Render("no boards"))
		}
	}
	for i, b := range m.projRows {
		open, total := m.boardCount(i)
		left := "  " + b.Name
		if !m.projArchived && b.Name == cur {
			left = m.st().box.Render("▸ ") + b.Name
		}
		cnt := countStyle.Render(fmt.Sprintf("%d/%d", total-open, total))
		row := spread(w, left, cnt)
		if i == m.projCur {
			cursorLine = len(out)
			row = m.st().cursor.Width(w).Render(ansi.Truncate(ansi.Strip(row), w, "…"))
		}
		out = append(out, row)
	}
	rule := m.st().rule.Render(strings.Repeat("┈", w))
	var footer string
	switch m.mode {
	case modeBoardNew:
		footer = rule + "\n" + m.inputView() + "  " + dimStyle.Render("(new board: enter=create esc=cancel)")
	case modeBoardDir:
		footer = rule + "\n" + m.inputView() + "  " + dimStyle.Render(fmt.Sprintf("(dir for %q: enter=save esc=skip)", m.projPending))
	case modeBoardRename:
		footer = rule + "\n" + m.inputView() + "  " + dimStyle.Render("(rename: enter=save esc=cancel)")
	case modeConfirmDelete:
		name := ""
		if b := m.selectedBoard(); b != nil {
			name = b.Name
		}
		footer = rule + "\n" + m.st().warn.Render(fmt.Sprintf("delete board %q and its archive? (y/n)", name))
	default:
		footer = rule + "\n"
		if m.projNotice != "" {
			footer += m.st().warn.Render(m.projNotice) + "\n"
		}
		footer += m.boardsHelp()
	}
	out = m.windowRows(out, cursorLine, lines(footer))
	title := "boards"
	if m.projArchived {
		title = "archived boards"
	}
	body := m.headerWith(title, 0, len(m.projRows)) + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// boardDetailView renders the read-only detail for the board under the picker
// cursor: its name, working dir (with an on-disk existence marker), source file
// and item counts. When the dir editor is open (modeBoardDir from here), the
// input replaces the help line as the footer.
func (m model) boardDetailView() string {
	w := m.width()
	b := m.selectedBoard()
	if b == nil {
		return m.frame(m.headerWith("board", 0, 0), dimStyle.Render("no board"))
	}
	open, total := store.BoardCounts(b.Path)
	dir := store.BoardDir(b.Name)

	field := func(k, v string) string {
		return dimStyle.Render(fmt.Sprintf("%-8s", k)) + v + "\n"
	}
	dirVal := dimStyle.Render("(not set)")
	if dir != "" {
		// shepherd stores the dir as a plain string; it can't detect a move or
		// rename, so surface whether the recorded path still exists on disk.
		mark := ""
		if _, err := os.Stat(expandHome(dir)); err != nil {
			mark = "  " + m.st().warn.Render("(missing)")
		}
		dirVal = dir + mark
	}

	var body strings.Builder
	body.WriteString(m.headerWith("board", total-open, total) + "\n\n")
	body.WriteString(field("name", brandStyle.Render(b.Name)))
	body.WriteString(field("dir", dirVal))
	body.WriteString(field("file", dimStyle.Render(b.Path)))
	body.WriteString(field("archive", dimStyle.Render(store.ArchivePath(b.Path))))
	body.WriteString(field("items", countStyle.Render(fmt.Sprintf("%d/%d", total-open, total))))

	rule := m.st().rule.Render(strings.Repeat("┈", w))
	var footer string
	if m.mode == modeBoardDir { // editing this board's dir
		footer = rule + "\n" + m.inputView() + "  " + dimStyle.Render("(dir: enter=save esc=cancel)")
	} else {
		footer = rule + "\n" + dimStyle.Render("e edit dir · esc back · q quit")
	}
	return m.frame(body.String(), footer)
}

// expandHome replaces a leading ~ with the user's home dir, for on-disk checks.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// settingsView renders the settings screen: one row per editable config field,
// the enum/bool rows (view/density/footer) cycled in place, the text rows opened
// in the shared input. Changes are written to config.toml as they are made.
func (m model) settingsView() string {
	w := m.width()
	cfg := m.currentConfig()
	den := "compact"
	if cfg.density == comfort {
		den = "comfort"
	}
	foot := "shown"
	if cfg.hideFooter {
		foot = "hidden"
	}
	rows := []struct{ k, v string }{
		{"view", viewName[cfg.view]},
		{"density", den},
		{"autosave", fmt.Sprintf("%ds", cfg.autosave)},
		{"categories", strings.Join(cfg.categories, ", ")},
		{"statuses", strings.Join(cfg.statuses, ", ")},
		{"footer", foot},
		{"mouse", map[bool]string{false: "off", true: "on"}[cfg.mouse]},
		{"theme", cfg.theme},
	}
	var out []string
	for i, r := range rows {
		val := r.v
		if val == "" {
			val = "(none)"
		}
		line := "  " + fmt.Sprintf("%-12s", r.k) + val
		if i == m.settingsCur {
			line = m.st().cursor.Width(w).Render(ansi.Truncate(ansi.Strip(line), w, "…"))
		}
		out = append(out, line)
	}

	// header without the done/total count that headerWith always appends.
	left := titleLeft()
	right := dimStyle.Render("settings")
	header := spread(w, left, right) + "\n" + m.st().rule.Render(strings.Repeat("┈", w))

	rule := m.st().rule.Render(strings.Repeat("┈", w))
	footer := rule + "\n"
	if m.mode == modeSettingEdit {
		footer += m.inputView() + "  " + dimStyle.Render("(enter=save esc=cancel)")
	} else {
		footer += dimStyle.Render("settings · j/k move · enter change · esc back · q quit")
	}
	body := header + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// boardsHelp renders the picker's key hints, dimming the board actions
// (rename/archive/delete) when the default board is selected — they don't apply
// to it.
func (m model) boardsHelp() string {
	tok := func(s string) string { return dimStyle.Render(s) }
	if m.projArchived {
		parts := []string{
			tok("j/k move"), tok("u unarchive"), tok("e live"),
			tok("esc back"), tok("q quit"),
		}
		return strings.Join(parts, dimStyle.Render(" · "))
	}
	onDefault := false
	if b := m.selectedBoard(); b != nil && b.Name == "default" {
		onDefault = true
	}
	action := func(s string) string {
		if onDefault {
			return doneStyle.Render(s) // faint + strikethrough: not applicable to default
		}
		return dimStyle.Render(s)
	}
	parts := []string{
		tok("j/k move"), tok("enter open"), tok("d detail"), tok("a new"),
		action("r rename"), action("A archive"), action("x delete"),
		tok("e archived"), tok("esc back"), tok("q quit"),
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}

// count returns the done and total item counts across the whole board.
func (m model) count() (done, total int) {
	for _, it := range m.items {
		if it.Done {
			done++
		}
	}
	return done, len(m.items)
}

// header renders the list/table title block: the active view flush-right.
func (m model) header() string {
	done, total := m.count()
	ctx := viewName[m.view]
	if m.global {
		ctx = "all · " + ctx
	}
	return m.headerWith(ctx, done, total)
}

// titleLeft is the left side of every header: the branded app name.
func titleLeft() string {
	return brandStyle.Render(appEmoji + " " + appName)
}

// headerWith renders the shared title block used by every view: the subtitle
// (left) with the context label + done/total count (and active filter)
// flush-right, then a full-width rule.
func (m model) headerWith(context string, done, total int) string {
	w := m.width()
	left := titleLeft()

	right := dimStyle.Render(context) + "  " + countStyle.Render(fmt.Sprintf("%d/%d", done, total))
	if m.filter != "" || m.mode == modeFilter {
		right = m.st().match.Render("/"+m.filter) + "  " + right
	}
	if !m.global { // global view is read-only; no save state to show
		save := dimStyle.Render("● saved")
		if m.dirty {
			save = m.st().warn.Render("● unsaved")
		}
		right = right + "  " + save
	}
	return spread(w, left, right) + "\n" +
		m.st().rule.Render(strings.Repeat("┈", w))
}

// Version is the running build's version, set by main from the embedded
// manifest so the footer and `--version` agree. "dev" until wired.
var Version = "dev"

const (
	repoName = "jwarykowski/shepherd"
	repoURL  = "https://github.com/jwarykowski/shepherd"
)

// osc8 wraps text in a terminal hyperlink (OSC 8); terminals without support
// just render the text.
func osc8(text, url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// bottomBar is the very last footer line: linked repo name flush-left, version
// flush-right, filling the full width.
func (m model) bottomBar() string {
	left := dimStyle.Render(repoName)
	right := dimStyle.Render("v" + Version)
	// Link the version to its GitHub release; skip when unbuilt ("dev"/"unknown").
	// The link is added after spreading so the invisible OSC 8 bytes don't count
	// toward the gap.
	gap := max(m.width()-lipgloss.Width(left)-lipgloss.Width(right), 1)
	if Version != "dev" && Version != "unknown" && Version != "" {
		right = osc8(right, repoURL+"/releases/tag/v"+Version)
	}
	return osc8(left, repoURL) + strings.Repeat(" ", gap) + right
}

// listFooter: a full-width rule, then either the active input line or the
// grouped multi-line key help, and always the repo/version line at the bottom.
func (m model) listFooter() string {
	rule := m.st().rule.Render(strings.Repeat("┈", m.width()))
	switch m.mode {
	case modeFilter:
		return rule + "\n" + m.inputView() + "  " + dimStyle.Render("(filter: enter=apply esc=clear)")
	case modeAdd, modeAddSub, modeEdit, modeCategory, modeTags, modeDue, modeDefer, modeLink:
		return rule + "\n" + m.inputPrompt()
	default:
		grid := m.helpGrid()
		switch {
		case m.pending != "":
			grid = m.chordMenu()
		case m.keyNotice != "":
			grid = m.st().warn.Render(ansi.Truncate(m.keyNotice, m.width(), "…")) + "\n" + grid
		case m.hideFooter: // keep the repo/version line; drop the help grid
			return rule + "\n" + m.bottomBar()
		}
		return rule + "\n" + grid + "\n" + rule + "\n" + m.bottomBar()
	}
}

// chordMenu lists what can follow the pending first key of a sequence.
func (m model) chordMenu() string {
	var parts []string
	for _, nb := range keys.bindings() {
		for _, k := range nb.b.Keys() {
			if rest, ok := strings.CutPrefix(k, m.pending+" "); ok {
				desc := nb.b.Help().Desc
				if desc == "" { // the second half of a pair (j/k, gg/G)
					desc = nb.name
				}
				parts = append(parts, rest+" "+dimStyle.Render(desc))
			}
		}
	}
	return ansi.Wordwrap(m.pending+" … "+strings.Join(parts, " · ")+" · "+dimStyle.Render("esc cancel"), m.width(), " ")
}

// inputPrompt is the active field editor's line: the text input and what enter
// and esc will do. Shared by the list and detail footers, so an editor opened
// from either place reads the same.
func (m model) inputPrompt() string {
	verb := map[mode]string{modeAdd: "add", modeAddSub: "subtask", modeEdit: "edit", modeCategory: "category", modeTags: "tags", modeDue: "due", modeDefer: "defer", modeLink: "link"}[m.mode]
	return m.inputView() + "  " + dimStyle.Render("("+verb+": enter=save esc=cancel)")
}

// keyCol is one labelled column of a footer key grid: a header over its
// {key, label} hints.
type keyCol struct {
	head    string
	entries []keyEntry
}

// helpGrid is the list footer's key hints.
func (m model) helpGrid() string {
	cols := []keyCol{
		{"move", entries(keys.Up, keys.Top, keys.LaneLeft, keys.Toggle, keys.Detail, keys.View, keys.Global, keys.ArchiveView, keys.Boards, keys.Footer)},
		{"edit", entries(keys.Add, keys.AddSub, keys.Edit, keys.Status, keys.Delete, keys.Sweep, keys.Archive)},
		{"fields", entries(keys.Prio, keys.Category, keys.Tags, keys.Due, keys.Defer, keys.Link, keys.Open, keys.Copy)},
		{"board", entries(keys.Save, keys.Editor, keys.Undo, keys.Redo, keys.Filter, keys.Settings, keys.Theme, keys.Help, keys.Quit)},
	}

	// In the read-only global view most actions are inert; dim them so only the
	// keys that do something (navigate / inspect / leave) read as live.
	globalActive := keySet(keys.Up, keys.Top, keys.Detail, keys.View, keys.Filter, keys.Global, keys.ArchiveView, keys.Open, keys.Copy, keys.Boards, keys.Footer, keys.Theme, keys.Help, keys.Quit)

	// On a subtask row category is parent-only (subs share the parent's board);
	// dim it. Archive takes whole items only, so it's inert on a subtask too.
	// Due / defer / link / status all work on subtasks. `o` opens the link, so dim
	// it too when this subtask has none.
	onSub := !m.global && m.selRef().sub >= 0
	subInert := keySet(keys.Category, keys.Tags, keys.Archive)
	if onSub && m.rowItem(m.selRef()).Link == "" {
		subInert[keys.Open.Help().Key] = true
	}
	return m.keyGrid(cols, func(key string) bool {
		return (m.global && !globalActive[key]) || (onSub && subInert[key])
	})
}

func (m model) detailGrid() string {
	cols := []keyCol{
		{"fields", []keyEntry{{keys.Edit, "text"}, {b: keys.Prio}, {b: keys.Category}, {b: keys.Tags}}},
		{"dates", entries(keys.Due, keys.Defer)},
		{"item", entries(keys.Note, keys.Link, keys.Status, keys.Toggle)},
		{"go", []keyEntry{{keys.Open, "open link"}, {keys.Copy, "copy title"}, {b: keys.Back}, {b: keys.Quit}}},
	}

	// Same inert rules as the list footer: the global aggregate is read-only, and
	// category/tags are parent-only on a subtask.
	globalActive := keySet(keys.Open, keys.Copy, keys.Back, keys.Quit)
	onSub := !m.global && m.selRef().sub >= 0
	subInert := keySet(keys.Category, keys.Tags)
	return m.keyGrid(cols, func(key string) bool {
		return (m.global && !globalActive[key]) || (onSub && subInert[key])
	})
}

func (m model) keyGrid(cols []keyCol, dim func(key string) bool) string {
	rows := 0
	rendered := make([][]string, len(cols))
	widths := make([]int, len(cols))
	for i, c := range cols {
		keyW := 0
		for _, e := range c.entries {
			if kw := lipgloss.Width(e.label()); kw > keyW {
				keyW = kw
			}
		}
		lines := []string{m.st().cat.Render(c.head)}
		w := lipgloss.Width(lines[0])
		for _, e := range c.entries {
			key := e.label() + strings.Repeat(" ", keyW-lipgloss.Width(e.label()))
			if dim(e.label()) {
				key = dimStyle.Render(key)
			}
			line := key + " " + dimStyle.Render(e.text())
			if lw := lipgloss.Width(line); lw > w {
				w = lw
			}
			lines = append(lines, line)
		}
		rendered[i], widths[i] = lines, w
		if len(lines) > rows {
			rows = len(lines)
		}
	}

	total := 0
	for _, w := range widths {
		total += w
	}
	gap := 2
	if extra := m.width() - total; extra > gap*(len(cols)-1) {
		gap = extra / (len(cols) - 1)
	}

	var b strings.Builder
	for r := range rows {
		for i := range cols {
			cell := ""
			if r < len(rendered[i]) {
				cell = rendered[i][r]
			}
			b.WriteString(cell + strings.Repeat(" ", widths[i]-lipgloss.Width(cell)))
			if i < len(cols)-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		if r < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// tableView renders the flat bubbles/table view. Nav still comes from our own
// j/k (m.cursor); the table is driven read-only via SetCursor.
func (m model) tableView() string {
	w := m.width()
	vis := m.visible()
	catW, dueW, projW := 12, 11, 0
	if m.global {
		projW = 12
	}
	taskW := max(
		// marks + fixed cols + cell padding
		w-(2+2+catW+dueW+projW+8), 10)
	cols := []table.Column{
		{Title: "✓", Width: 1},
		{Title: "!", Width: 1},
		{Title: "task", Width: taskW},
	}
	if m.global {
		cols = append(cols, table.Column{Title: "board", Width: projW})
	}
	cols = append(cols,
		table.Column{Title: "category", Width: catW},
		table.Column{Title: "due", Width: dueW},
	)
	rows := make([]table.Row, 0, len(vis))
	for pos, r := range m.rows() {
		it := m.rowItem(r)
		box := "○"
		if it.Done {
			box = "✓"
		} else if it.Status != "" {
			box = "◐"
		}
		p := " "
		if it.Prio != 0 {
			p = strings.ToLower(string(it.Prio))
		}
		due := ""
		if it.Due != "" {
			due, _ = todo.DueLabel(it.Due)
		}
		task, cat := it.Text, it.Category
		if it.Claimed != "" {
			task += " (" + it.Claimed + ")" // plain: the table measures cells without ANSI
		}
		if r.sub >= 0 { // subtask: indent the task cell, inherit the parent's board/category columns
			task = "  " + task
			cat = ""
		} else if d, t := todo.SubCount(it); t > 0 {
			task = fmt.Sprintf("%s (%d/%d)", task, d, t)
		}
		row := table.Row{rowMark(0, pos) + box, p, task}
		if m.global {
			row = append(row, it.Source)
		}
		row = append(row, cat, due)
		rows = append(rows, row)
	}
	head, footer := m.header(), m.listFooter()
	// derive table height from the actual header/footer sizes rather than a
	// hard-coded constant; the height includes the table's column-header row.
	overhead := lines(head) + lines(footer)
	h := max(m.innerHeight()-overhead, 4)
	// the table is rebuilt every frame, so its viewport never scrolls: window
	// the rows here, keeping the cursor row on screen.
	off := max(scrollOffset(len(rows), m.cursor, h-1), 0)
	rows = rows[off:min(off+h-1, len(rows))]
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithHeight(h),
		table.WithWidth(w),
	)
	t.SetCursor(m.cursor - off)
	st := table.DefaultStyles()
	// plain reverse-video highlight (matches the list cursor); drop the
	// bubbles default pink foreground.
	st.Selected = lipgloss.NewStyle().Bold(true).Reverse(true)
	t.SetStyles(st)
	return m.frame(head+"\n"+t.View(), footer)
}

// claimSuffix names who claimed an item (see `shepherd claim`), or "".
func claimSuffix(it todo.Item) string {
	if it.Claimed == "" {
		return ""
	}
	return " " + dimStyle.Render("("+it.Claimed+")")
}

// laneCard renders one lane-view card line: a priority marker plus the title,
// truncated to width w. Status is implied by the column, so unlike rowContent
// there's no box glyph.
func (m model) laneCard(it todo.Item, w int) string {
	marker, markSt := " ", dimStyle
	if _, ok := prioLabel[it.Prio]; ok {
		marker, markSt = string(it.Prio), m.st().prio[it.Prio]
	}
	text := it.Text
	if it.Done {
		text = doneStyle.Render(text)
	}
	text += claimSuffix(it)
	return ansi.Truncate(markSt.Render(marker)+" "+text, w, "…")
}

// laneView renders the kanban board: one column per configured status. All
// columns are windowed together against the active lane's cursor position, so
// the whole board scrolls in lockstep as the cursor moves within its column.
const laneGap = 2 // blank columns between lanes, so headers/cards don't touch

func (m model) laneView() string {
	w := m.width()
	lanes := m.statuses
	if len(lanes) == 0 { // config invariant, but test model{} literals often skip it
		lanes = []string{""}
	}
	colW := max((w-laneGap*(len(lanes)-1))/len(lanes), 8)
	active := max(m.lane, 0)
	if active >= len(lanes) {
		active = len(lanes) - 1
	}
	cur := max(m.cursor, 0)

	headers := make([]string, len(lanes))
	cardLines := make([][]string, len(lanes))
	for i, status := range lanes {
		rows := m.laneRows(status)
		headers[i] = spread(colW, m.st().cat.Render(status), countStyle.Render(strconv.Itoa(len(rows))))
		lines := make([]string, len(rows))
		for j, r := range rows {
			line := m.laneCard(m.rowItem(r), colW)
			if i == active && j == cur {
				line = m.st().cursor.Width(colW).Render(ansi.Strip(line))
			}
			lines[j] = rowMark(i, j) + line
		}
		cardLines[i] = lines
	}

	footer := m.listFooter()
	vh := 0
	if ih := m.innerHeight(); ih > 0 {
		vh = max(ih-lines(footer)-3, 1) // m.header (2 lines) + this column's own header line
	}
	off := 0
	if vh > 0 && len(cardLines[active]) > vh {
		off = scrollOffset(len(cardLines[active]), cur, vh)
	}

	cols := make([]string, len(lanes))
	for i, visible := range cardLines {
		if vh > 0 {
			start := min(off, len(visible))
			end := min(start+vh, len(visible))
			visible = visible[start:end]
		}
		if len(visible) == 0 {
			visible = []string{dimStyle.Render("(empty)")}
		}
		cols[i] = lipgloss.NewStyle().Width(colW).Render(headers[i] + "\n" + strings.Join(visible, "\n"))
	}
	gap := lipgloss.NewStyle().Width(laneGap).Render("")
	parts := make([]string, 0, len(cols)*2-1)
	for i, c := range cols {
		if i > 0 {
			parts = append(parts, gap)
		}
		parts = append(parts, c)
	}
	body := m.header() + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return m.frame(body, footer)
}

// helpBody returns the full help content as individual (already-wrapped) lines.
func (m model) helpBody() []string {
	w := m.width()
	wrap := lipgloss.NewStyle().Width(w - 2)
	var out []string
	sec := func(s string) { out = append(out, m.st().cat.Render(s)) }
	line := func(s string) {
		for ln := range strings.SplitSeq(wrap.Render(s), "\n") {
			out = append(out, "  "+ln)
		}
	}
	blank := func() { out = append(out, "") }

	k := func(b binding.Binding) string { return b.Help().Key }
	line("An interactive todo board backed by a plain markdown file. Runs standalone in any terminal, or as a herdr plugin pane. Changes save on quit, autosave after a short idle pause, or on demand with " + k(keys.Save) + "; the header shows ● unsaved / ● saved. The board reloads external edits automatically when you have nothing unsaved.")
	blank()
	sec("adding")
	line(k(keys.Add) + " — add. Inline syntax: text @category #tag (tags:a,b replaces the set) !h|!m|!l due:tomorrow defer:3d link:https://… status:name note:the rest of the line")
	line("a new item inherits the selected item's category (then a category filter's), so adding under a group stays in it; an inline @category overrides")
	line(k(keys.Edit) + " — edit the selected item's (or subtask's) text")
	line(k(keys.AddSub) + " — add a subtask to the selected item (same !prio / due: syntax)")
	blank()
	sec("organise")
	line(k(keys.Prio) + " — set priority high/medium/low (same key again clears; works on subtasks too)")
	line(k(keys.Category) + " — set category · " + k(keys.Tags) + " — set tags (space- or comma-separated; empty clears) · " + k(keys.Due) + " — set due date · " + k(keys.Defer) + " — set start (defer) date")
	line(k(keys.Link) + " — set link · " + k(keys.Open) + " — open the link in the browser")
	line(k(keys.Copy) + " — copy the selected item's title to the clipboard (through the terminal, OSC 52)")
	line(k(keys.Toggle) + " — toggle done · " + k(keys.Status) + " — cycle status · " + k(keys.Delete) + " — delete")
	line("rows carry two flush-right values: subtask progress (else the due/defer label), then whichever grouping axis the headers don't already name — priority in the category and tag views, category in the priority view. The box shape is the status (○ open, ◐ named status, ✓ done)")
	line(k(keys.Sweep) + " — archive all done items · " + k(keys.Archive) + " — archive the selected item (whole items only, not subtasks)")
	line("subtasks: completing a parent completes its subtasks; completing the last subtask completes the parent")
	blank()
	sec("due dates")
	line("today · tomorrow · a weekday (fri: the next one) · Nd/Nw/Nm/Ny (e.g. 3d, 2w) · DD-MM-YYYY. Anything unrecognised clears the date here; the command API rejects it. Overdue items are pinned to a group at the top.")
	blank()
	sec("view & find")
	line(k(keys.Up) + " — move · " + k(keys.Top) + " — top/end")
	line(k(keys.View) + " — cycle view: category / priority / tag / table / lane (the global view adds a board grouping instead of lane). The tag view groups by an item's first tag (untagged last) and shows its other tags on the row")
	line("lane view: one column per configured status — a kanban board. " + k(keys.LaneLeft) + " switches the active column, " + k(keys.Status) + " moves the selected card into the next one; every other item key still acts on the card under the cursor. Not available in the global view, since boards can configure different statuses")
	line(k(keys.Filter) + " — filter text, note, category, tags, due (also greps the archive)")
	line(k(keys.Global) + " — toggle the read-only global view across all boards (esc to leave)")
	line(k(keys.ArchiveView) + " — browse the archive (read-only; all boards in the global view; esc to leave)")
	line(k(keys.Boards) + " — boards: switch, create, rename, archive")
	line(k(keys.Detail) + " — detail view; the same field keys work there and return to it · " + k(keys.Help) + " — this help")
	line(k(keys.Footer) + " — hide/show the footer help grid (the repo/version line stays; config: hidefooter = true starts hidden)")
	line(k(keys.Settings) + " — settings (also ,) · " + k(keys.Theme) + " — theme picker with a live preview")
	line("mouse (config: mouse = true, or settings): wheel scrolls, click selects a row or card, double-click opens its detail")
	line("theme (config: theme = ansi|catppuccin|dracula|gruvbox|nord|mono): theme.<token> = colour overrides one colour")
	blank()
	sec("history & files")
	line(k(keys.Undo) + " — undo · " + k(keys.Redo) + " — redo (multi-level)")
	line(k(keys.Save) + " — save now · autosave runs after idle (config: autosave = seconds, 0 disables)")
	line(k(keys.Editor) + " — open the markdown file in $EDITOR")
	line(k(keys.Quit) + " — save + quit")
	blank()
	sec("keys")
	line("Keys like gb are a sequence: press g, then b (the footer lists what can follow). Rebind any key in config.toml with key.<action> = \"x\" (see the README).")
	return out
}

// helpViewport is how many body lines fit between the header and footer.
func (m model) helpViewport() int {
	ih := m.innerHeight()
	if ih == 0 {
		return len(m.helpBody()) // unknown size: show everything
	}
	vh := max(
		// subtitle + rule + blank, footer rule + hint
		ih-5, 1)
	return vh
}

// helpMaxScroll is the largest valid scroll offset.
func (m model) helpMaxScroll() int {
	if n := len(m.helpBody()) - m.helpViewport(); n > 0 {
		return n
	}
	return 0
}

// helpView renders the scrollable help page.
func (m model) helpView() string {
	w := m.width()
	body := m.helpBody()
	vh := m.helpViewport()
	off := m.helpScroll
	if off > m.helpMaxScroll() {
		off = m.helpMaxScroll()
	}
	end := min(off+vh, len(body))

	done, total := m.count()
	var b strings.Builder
	b.WriteString(m.headerWith("help", done, total) + "\n\n")
	b.WriteString(strings.Join(body[off:end], "\n"))

	hint := "esc to close"
	if off > 0 {
		hint = "↑ " + hint
	}
	if end < len(body) {
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
	ref := m.selRef()
	if ref.item < 0 {
		return "no item"
	}
	it := m.rowItem(ref)
	// count reflects the item's own category, not the whole board
	cdone, ctotal := 0, 0
	for _, x := range m.items {
		if x.Category == it.Category {
			ctotal++
			if x.Done {
				cdone++
			}
		}
	}
	var b strings.Builder
	b.WriteString(m.headerWith("detail", cdone, ctotal) + "\n\n")

	status := "open"
	if it.Done {
		status = "done"
	} else if it.Status != "" {
		status = it.Status
	}
	prio := "none"
	if lbl, ok := prioLabel[it.Prio]; ok {
		prio = m.st().prio[it.Prio].Render(lbl)
	}
	created := it.Created
	if created == "" {
		created = dimStyle.Render("—")
	}

	field := func(k, v string) string {
		return dimStyle.Render(fmt.Sprintf("%-10s", k)) + v + "\n"
	}
	category := it.Category
	if category == "" {
		category = dimStyle.Render("uncategorized")
	} else {
		category = m.st().cat.Render(category)
	}
	b.WriteString(field("task", it.Text))
	if ref.sub >= 0 {
		b.WriteString(field("parent", m.items[ref.item].Text))
	}
	b.WriteString(field("status", status))
	if it.Claimed != "" {
		b.WriteString(field("claimed", it.Claimed))
	}
	b.WriteString(field("priority", prio))
	b.WriteString(field("category", category))
	tags := dimStyle.Render("—")
	if len(it.Tags) > 0 {
		tags = m.st().cat.Render("#" + strings.Join(it.Tags, " #"))
	}
	b.WriteString(field("tags", tags))
	if m.global && it.Source != "" {
		b.WriteString(field("board", m.st().cat.Render(it.Source)))
	}
	if it.Defer != "" {
		deferral := todo.DisplayDate(it.Defer)
		if lbl := todo.DeferLabel(it.Defer); lbl != "" {
			deferral += "  " + dimStyle.Render(lbl)
		}
		b.WriteString(field("defer", deferral))
	}
	due := dimStyle.Render("—")
	if it.Due != "" {
		lbl, over := todo.DueLabel(it.Due)
		st := dimStyle
		if over {
			st = m.st().prio['H']
		}
		due = fmt.Sprintf("%s  %s", todo.DisplayDate(it.Due), st.Render(lbl))
	}
	b.WriteString(field("due", due))
	if it.Link != "" {
		b.WriteString(field("link", m.st().match.Render(it.Link)))
	}
	b.WriteString(field("created", created))
	if it.Completed != "" {
		b.WriteString(field("completed", it.Completed))
	}
	b.WriteString("\n" + dimStyle.Render("note") + "\n")
	switch {
	case m.mode == modeNote:
		b.WriteString(m.noteView() + "\n")
	case it.Note != "":
		b.WriteString(lipgloss.NewStyle().Width(m.width()).Render(it.Note) + "\n")
	default:
		b.WriteString(dimStyle.Render("(none — press "+keys.Note.Help().Key+" to add)") + "\n")
	}

	rule := dimStyle.Render(strings.Repeat("─", m.width()))
	var help string
	switch {
	case m.mode == modeNote:
		help = rule + "\n" + dimStyle.Render(m.noteHint())
	case m.fieldEdit: // editing a field from here: the prompt replaces the grid
		help = rule + "\n" + m.inputPrompt()
	case m.pending != "":
		help = rule + "\n" + m.chordMenu()
	default:
		// the list's field editors work here too and come back here when done, so
		// the footer is the list's grid narrowed to one item.
		help = rule + "\n" + m.detailGrid()
	}
	return m.frame(b.String(), help)
}

// boardCount is the open/total count for picker row i, read from disk only if
// the rows were set without setBoards.
func (m model) boardCount(i int) (open, total int) {
	if i < len(m.projCounts) {
		return m.projCounts[i][0], m.projCounts[i][1]
	}
	return store.BoardCounts(m.projRows[i].Path)
}
