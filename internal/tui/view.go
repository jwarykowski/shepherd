package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"shepherd/internal/todo"
)

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

// groupOf returns a stable group id (for change detection) and display label
// for an item under the active view; overdue items form a pinned top group.
func (m model) groupOf(it todo.Item) (id, label string) {
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
		text := it.Text
		if it.Done {
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
		if it.Due != "" && !todo.Pinned(it) {
			lbl, over := todo.DueLabel(it.Due)
			st := dimStyle
			if over {
				st = prioStyles['H'] // red for due/overdue
			}
			label = st.Render(lbl)
		}
		if lbl, ok := prioLabel[it.Prio]; ok {
			if label != "" {
				label += "  "
			}
			label += prioStyles[it.Prio].Render(lbl)
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
			b.WriteString("  " + doneStyle.Render(it.Text) + "\n")
		}
	}
	return m.frame(b.String(), m.listFooter())
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
		if it.Done {
			box = "✓"
		}
		p := " "
		if it.Prio != 0 {
			p = strings.ToLower(string(it.Prio))
		}
		due := ""
		if it.Due != "" {
			due, _ = todo.DueLabel(it.Due)
		}
		rows = append(rows, table.Row{box, p, it.Text, it.Category, due})
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
	idx := m.sel()
	if idx < 0 {
		return "no item"
	}
	it := m.items[idx]
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
		return dimStyle.Render(fmt.Sprintf("%-9s", k)) + v + "\n"
	}
	category := it.Category
	if category == "" {
		category = dimStyle.Render("uncategorized")
	} else {
		category = catStyle.Render(category)
	}
	b.WriteString(field("task", it.Text))
	b.WriteString(field("status", status))
	b.WriteString(field("priority", prio))
	b.WriteString(field("category", category))
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
	b.WriteString(field("created", created))
	b.WriteString("\n" + dimStyle.Render("note") + "\n")
	if m.mode == modeNote {
		b.WriteString(m.input.View() + "\n")
	} else if it.Note != "" {
		b.WriteString(it.Note + "\n")
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
