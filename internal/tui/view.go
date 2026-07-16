package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// ---- styles ----
var (
	dimStyle    = lipgloss.NewStyle().Faint(true)
	doneStyle   = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	cursorStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})
	boxStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	progStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	matchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	catStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	countStyle  = lipgloss.NewStyle().Faint(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	ruleStyle   = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("240"))
	prioStyles  = map[byte]lipgloss.Style{
		'H': lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		'M': lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		'L': lipgloss.NewStyle().Faint(true),
	}
	prioLabel = map[byte]string{'H': "high", 'M': "medium", 'L': "low"}
)

func (m model) View() string {
	var content string
	switch {
	case m.mode == modeHelp:
		content = m.helpView()
	case m.mode == modeArchive:
		content = m.archiveView()
	case m.mode == modeProjects:
		content = m.projectsView()
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

// groupOf returns a stable group id (for change detection) and display label
// for an item under the active view; overdue items form a pinned top group.
func (m model) groupOf(it todo.Item) (id, label string) {
	if m.view == viewProject {
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
	if it.Category == "" {
		return "c\x01", "uncategorized"
	}
	return "c" + strings.ToLower(it.Category), it.Category
}

// groupCount returns done/total for the group an item belongs to. Pinned items
// are excluded from their category/priority group (they show in overdue).
func (m model) groupCount(it todo.Item) (done, total int) {
	switch {
	case m.view == viewProject:
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

// rowContent renders one item's row (box + text + flush-right due/prio/status
// cluster) at the given indent, with an optional subtask-progress badge after
// the text. Cursor highlight is applied by the caller.
func (m model) rowContent(it todo.Item, indent, badge string, isSub bool) string {
	w := m.width()
	box := "○"
	boxSt := boxStyle
	text := it.Text
	deferred := todo.Deferred(it)
	if it.Done {
		box = "✓"
		text = doneStyle.Render(text)
	} else if it.Status != "" {
		box = "◐"
		boxSt = progStyle
	} else if deferred {
		text = dimStyle.Render(text) // not started yet
	}
	if m.global && m.view != viewProject && it.Source != "" {
		text += " " + dimStyle.Render("["+it.Source+"]")
	}
	// right cluster: due (left) then priority label flush far-right.
	// Overdue rows live under the ⚠ overdue group, so don't repeat "overdue" on the line.
	label := ""
	if deferred {
		if lbl := todo.DeferLabel(it.Defer); lbl != "" {
			label = dimStyle.Render(lbl)
		}
	} else if it.Due != "" && (isSub || !todo.Pinned(it)) {
		// parents hide the label when pinned to the ⚠ overdue group; subs have no
		// such group, so always show it (red when overdue).
		lbl, over := todo.DueLabel(it.Due)
		st := dimStyle
		if over {
			st = prioStyles['H'] // red for due/overdue
		}
		label = st.Render(lbl)
	}
	if badge != "" { // subtask progress, flush-right just left of priority
		if label != "" {
			label += "  "
		}
		label += countStyle.Render(badge)
	}
	if lbl, ok := prioLabel[it.Prio]; ok {
		if label != "" {
			label += "  "
		}
		label += prioStyles[it.Prio].Render(lbl)
	}
	if it.Status != "" { // intermediate status, flush-right ahead of due/prio
		s := progStyle.Render(it.Status)
		if label != "" {
			label = s + "  " + label
		} else {
			label = s
		}
	}
	left := fmt.Sprintf("%s%s %s", indent, boxSt.Render(box), text)
	gap := w - lipgloss.Width(left) - lipgloss.Width(label)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + label
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
			out = append(out, dimStyle.Render("(empty — press a to add)"))
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
				left := catStyle.Render(label)
				gap := w - lipgloss.Width(left) - lipgloss.Width(cnt)
				if gap < 1 {
					gap = 1
				}
				out = append(out, left+strings.Repeat(" ", gap)+cnt)
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
			row = cursorStyle.Width(w).Render(ansi.Strip(row))
		}
		out = append(out, row)
		if m.density == comfort {
			out = append(out, "") // roomier rows
		}
	}
	if am := m.archivedMatches(); len(am) > 0 {
		out = append(out, "", dimStyle.Render(fmt.Sprintf("archive · %d match", len(am))))
		for _, it := range am {
			out = append(out, "  "+doneStyle.Render(it.Text))
		}
	}

	footer := m.listFooter()
	out = m.windowRows(out, cursorLine, lines(footer))
	body := m.header() + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// windowRows clips the list body to what fits between the header and footer,
// keeping the cursor line centered in the viewport (clamped at both ends). It
// returns rows unchanged when the terminal size is unknown or everything fits.
func (m model) windowRows(rows []string, cursorLine, footLines int) []string {
	ih := m.innerHeight()
	if ih == 0 {
		return rows // unknown size: let the terminal handle it
	}
	vh := ih - footLines - 2 // header line + frame's minimum pad line
	if vh < 1 || len(rows) <= vh {
		return rows
	}
	off := cursorLine - vh/2
	if off < 0 {
		off = 0
	}
	if off > len(rows)-vh {
		off = len(rows) - vh
	}
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
		left := "  " + boxStyle.Render("✓") + " " + dimStyle.Render(it.Text)
		row := left
		if m.global && it.Source != "" {
			tag := catStyle.Render("[" + it.Source + "]")
			gap := w - lipgloss.Width(left) - lipgloss.Width(tag)
			if gap < 1 {
				gap = 1
			}
			row = left + strings.Repeat(" ", gap) + tag
		}
		if i == m.arcCur {
			cursorLine = len(out)
			row = cursorStyle.Width(w).Render(ansi.Strip(row))
		}
		out = append(out, row)
	}
	footer := ruleStyle.Render(strings.Repeat("┈", w)) + "\n" +
		dimStyle.Render("browse archive · j/k scroll · esc back · q quit")
	out = m.windowRows(out, cursorLine, lines(footer))
	body := m.headerWith("archive", len(m.arcRows), len(m.arcRows)) + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
}

// projectsView renders the board picker: one row per board with open/total
// counts, the current board marked, windowed on the cursor.
func (m model) projectsView() string {
	w := m.width()
	cur := m.project
	if cur == "" {
		cur = "default"
	}
	var out []string
	cursorLine := 0
	if len(m.projRows) == 0 {
		out = append(out, dimStyle.Render("no boards"))
	}
	for i, b := range m.projRows {
		open, total := store.BoardCounts(b.Path)
		left := "  " + b.Name
		if b.Name == cur {
			left = boxStyle.Render("▸ ") + b.Name
		}
		cnt := countStyle.Render(fmt.Sprintf("%d open · %d total", open, total))
		gap := w - lipgloss.Width(left) - lipgloss.Width(cnt)
		if gap < 1 {
			gap = 1
		}
		row := left + strings.Repeat(" ", gap) + cnt
		if i == m.projCur {
			cursorLine = len(out)
			row = cursorStyle.Width(w).Render(ansi.Strip(row))
		}
		out = append(out, row)
	}
	footer := ruleStyle.Render(strings.Repeat("┈", w)) + "\n" +
		dimStyle.Render("boards · j/k move · enter open · esc back · q quit")
	out = m.windowRows(out, cursorLine, lines(footer))
	body := m.headerWith("boards", 0, len(m.projRows)) + "\n" + strings.Join(out, "\n")
	return m.frame(body, footer)
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
	if !m.global { // global view is read-only; no save state to show
		save := dimStyle.Render("● saved")
		if m.dirty {
			save = warnStyle.Render("● unsaved")
		}
		right = right + "  " + save
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right + "\n" +
		ruleStyle.Render(strings.Repeat("┈", w))
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
	gap := m.width() - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return osc8(left, repoURL) + strings.Repeat(" ", gap) + right
}

// listFooter: a full-width rule, then either the active input line or the
// grouped multi-line key help, and always the repo/version line at the bottom.
func (m model) listFooter() string {
	rule := ruleStyle.Render(strings.Repeat("┈", m.width()))
	switch m.mode {
	case modeFilter:
		return rule + "\n" + m.input.View() + "  " + dimStyle.Render("(filter: enter=apply esc=clear)")
	case modeAdd, modeAddSub, modeEdit, modeCategory, modeDue, modeDefer, modeLink:
		verb := map[mode]string{modeAdd: "add", modeAddSub: "subtask", modeEdit: "edit", modeCategory: "category", modeDue: "due", modeDefer: "defer", modeLink: "link"}[m.mode]
		return rule + "\n" + m.input.View() + "  " + dimStyle.Render("("+verb+": enter=save esc=cancel)")
	default:
		return rule + "\n" + m.helpGrid() + "\n" + rule + "\n" + m.bottomBar()
	}
}

// helpGrid renders the key hints as labelled sections spread across the full
// width: one column per section, each a header over "key label" rows, with the
// leftover width shared as gaps so the block spans the whole pane.
func (m model) helpGrid() string {
	type entry struct{ key, label string }
	cols := []struct {
		head    string
		entries []entry
	}{
		{"move", []entry{{"j/k", "move"}, {"space", "toggle"}, {"d", "detail"}, {"v", "view"}, {"/", "filter"}, {"A", "global"}, {"e", "archive"}, {"p", "boards"}}},
		{"edit", []entry{{"a", "add"}, {"S", "sub"}, {"u", "edit"}, {"tab", "status"}, {"x", "del"}, {"c", "arch"}}},
		{"fields", []entry{{"h/m/l", "prio"}, {"g", "cat"}, {"t", "due"}, {"s", "defer"}, {"L", "link"}, {"o", "open"}}},
		{"board", []entry{{"w", "save"}, {"^e", "editor"}, {"U", "undo"}, {"^r", "redo"}, {"?", "help"}, {"q", "quit"}}},
	}

	// In the read-only global view most actions are inert; dim them so only the
	// keys that do something (navigate / inspect / leave) read as live.
	globalActive := map[string]bool{"j/k": true, "d": true, "v": true, "/": true, "A": true, "e": true, "o": true, "p": true, "?": true, "q": true}

	// On a subtask row category is parent-only (subs share the parent's board);
	// dim it. Due / defer / link / status all work on subtasks. `o` opens the
	// link, so dim it too when this subtask has none.
	onSub := !m.global && m.selRef().sub >= 0
	subInert := map[string]bool{"g": true}
	if onSub && m.rowItem(m.selRef()).Link == "" {
		subInert["o"] = true
	}

	rows := 0
	rendered := make([][]string, len(cols))
	widths := make([]int, len(cols))
	for i, c := range cols {
		keyW := 0
		for _, e := range c.entries {
			if len(e.key) > keyW {
				keyW = len(e.key)
			}
		}
		lines := []string{catStyle.Render(c.head)}
		w := lipgloss.Width(lines[0])
		for _, e := range c.entries {
			key := fmt.Sprintf("%-*s", keyW, e.key)
			if (m.global && !globalActive[e.key]) || (onSub && subInert[e.key]) {
				key = dimStyle.Render(key)
			}
			line := key + " " + dimStyle.Render(e.label)
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
	for r := 0; r < rows; r++ {
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
	taskW := w - (2 + 2 + catW + dueW + projW + 8) // marks + fixed cols + cell padding
	if taskW < 10 {
		taskW = 10
	}
	cols := []table.Column{
		{Title: "✓", Width: 1},
		{Title: "!", Width: 1},
		{Title: "task", Width: taskW},
	}
	if m.global {
		cols = append(cols, table.Column{Title: "project", Width: projW})
	}
	cols = append(cols,
		table.Column{Title: "category", Width: catW},
		table.Column{Title: "due", Width: dueW},
	)
	rows := make([]table.Row, 0, len(vis))
	for _, r := range m.rows() {
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
		if r.sub >= 0 { // subtask: indent the task cell, inherit the parent's board/category columns
			task = "  " + task
			cat = ""
		} else if d, t := todo.SubCount(it); t > 0 {
			task = fmt.Sprintf("%s (%d/%d)", task, d, t)
		}
		row := table.Row{box, p, task}
		if m.global {
			row = append(row, it.Source)
		}
		row = append(row, cat, due)
		rows = append(rows, row)
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
	var out []string
	sec := func(s string) { out = append(out, catStyle.Render(s)) }
	line := func(s string) {
		for _, ln := range strings.Split(wrap.Render(s), "\n") {
			out = append(out, "  "+ln)
		}
	}
	blank := func() { out = append(out, "") }

	line("An interactive todo board in a herdr pane, backed by a plain markdown file. Changes save on quit, autosave after a short idle pause, or on demand with w; the header shows ● unsaved / ● saved. The board reloads external edits automatically when you have nothing unsaved.")
	blank()
	sec("adding")
	line("a — add. Inline syntax: text @category !h|!m|!l due:tomorrow defer:3d link:https://…")
	line("u — edit the selected item's (or subtask's) text")
	line("S — add a subtask to the selected item (same !prio / due: syntax)")
	blank()
	sec("organise")
	line("h/m/l — set priority high/medium/low (same key again clears; works on subtasks too)")
	line("g — set category · t — set due date · s — set defer/start date")
	line("L — set link · o — open the link in the browser")
	line("space — toggle done · tab — cycle status · x — delete · c — archive done")
	line("subtasks: completing a parent completes its subtasks; completing the last subtask completes the parent")
	blank()
	sec("due dates")
	line("today · tomorrow · Nd/Nw/Nm/Ny (e.g. 3d, 2w) · DD-MM-YYYY. Anything unrecognised clears the date. Overdue items are pinned to a group at the top.")
	blank()
	sec("view & find")
	line("v — cycle view: category / priority / table")
	line("/ — filter text, note, category, due (also greps the archive)")
	line("A — toggle the read-only global view across all boards (esc/A to leave)")
	line("e — browse the archive (read-only; all boards in the global view; esc to leave)")
	line("d — detail view · ? — this help")
	blank()
	sec("history & files")
	line("U — undo · ^r — redo (multi-level)")
	line("w — save now · autosave runs after idle (config: autosave = seconds, 0 disables)")
	line("^e — open the markdown file in $EDITOR")
	line("q — save + quit")
	return out
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
	body := m.helpBody()
	vh := m.helpViewport()
	off := m.helpScroll
	if off > m.helpMaxScroll() {
		off = m.helpMaxScroll()
	}
	end := off + vh
	if end > len(body) {
		end = len(body)
	}

	done, total := m.count()
	var b strings.Builder
	b.WriteString(m.headerWith("help", done, total) + "\n\n")
	b.WriteString(strings.Join(body[off:end], "\n"))

	hint := "enter to close"
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
		prio = prioStyles[it.Prio].Render(lbl)
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
		category = catStyle.Render(category)
	}
	b.WriteString(field("task", it.Text))
	if ref.sub >= 0 {
		b.WriteString(field("parent", m.items[ref.item].Text))
	}
	b.WriteString(field("status", status))
	b.WriteString(field("priority", prio))
	b.WriteString(field("category", category))
	if m.global && it.Source != "" {
		b.WriteString(field("board", catStyle.Render(it.Source)))
	}
	if it.Defer != "" {
		defer_ := todo.DisplayDate(it.Defer)
		if lbl := todo.DeferLabel(it.Defer); lbl != "" {
			defer_ += "  " + dimStyle.Render(lbl)
		}
		b.WriteString(field("defer", defer_))
	}
	due := dimStyle.Render("—")
	if it.Due != "" {
		lbl, over := todo.DueLabel(it.Due)
		st := dimStyle
		if over {
			st = prioStyles['H']
		}
		due = fmt.Sprintf("%s  %s", todo.DisplayDate(it.Due), st.Render(lbl))
	}
	b.WriteString(field("due", due))
	if it.Link != "" {
		b.WriteString(field("link", matchStyle.Render(it.Link)))
	}
	b.WriteString(field("created", created))
	if it.Completed != "" {
		b.WriteString(field("completed", it.Completed))
	}
	b.WriteString("\n" + dimStyle.Render("note") + "\n")
	if m.mode == modeNote {
		b.WriteString(m.note.View() + "\n")
	} else if it.Note != "" {
		b.WriteString(lipgloss.NewStyle().Width(m.width()).Render(it.Note) + "\n")
	} else {
		b.WriteString(dimStyle.Render("(none — press e to add)") + "\n")
	}

	rule := dimStyle.Render(strings.Repeat("─", m.width()))
	var help string
	if m.mode == modeNote {
		help = rule + "\n" + dimStyle.Render("note: enter newline · esc done (saves as you type)")
	} else {
		help = rule + "\n" + dimStyle.Render("e edit note   space toggle   o open link   d/esc/q back")
	}
	return m.frame(b.String(), help)
}
