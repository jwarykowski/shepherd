package cli

import (
	"fmt"
	"image/color"
	"shepherd/internal/theme"
	"shepherd/internal/todo"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/linechart/timeserieslinechart"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"
	"github.com/charmbracelet/x/ansi"
)

// palette colours the charts: the configured theme (see theme.Resolve), set by
// cmdStats before rendering; the default theme until then.
var palette = theme.Resolve(theme.Default, false, nil)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	faintStyle = lipgloss.NewStyle().Faint(true)
)

// fg colours s with c, or leaves it plain for a nil (colourless) token.
func fg(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Foreground(c)
}

// lowColor is the !L bar: the theme's low priority, else its muted colour.
func lowColor() color.Color {
	if palette.PrioLow != nil {
		return palette.PrioLow
	}
	return palette.Muted
}

type barRow struct {
	label string
	value int
	color color.Color
}

// renderStats lays the charts out as a responsive grid sized to width.
func renderStats(s todo.Stats, title string, width int) string {
	if width < 20 {
		width = 20
	}

	var b strings.Builder
	left := titleStyle.Render(title)
	right := faintStyle.Render(fmt.Sprintf("%d/%d done · %d%%", s.Done, s.Total, pct(s.CompletionRate)))
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	b.WriteString(left + strings.Repeat(" ", gap) + right + "\n")
	b.WriteString(faintStyle.Render(strings.Repeat("─", width)) + "\n\n")

	due := panel("due", hbar([]barRow{
		{"overdue", s.Overdue, palette.PrioHigh},
		{"today", s.DueToday, palette.Warn},
		{"this week", s.DueWeek, palette.OK},
		{"no date", s.NoDue, palette.Rule},
		{"deferred", s.Deferred, palette.Info},
	}, innerW(width)), width)

	prio := panel("open by priority", hbar([]barRow{
		{"!H", s.Prio.H, palette.PrioHigh},
		{"!M", s.Prio.M, palette.Warn},
		{"!L", s.Prio.L, lowColor()},
		{"none", s.Prio.None, palette.Rule},
	}, innerW(width)), width)

	catRows := make([]barRow, 0, len(s.ByCategory))
	for _, c := range s.ByCategory {
		catRows = append(catRows, barRow{c.Name, c.Open, palette.Info})
	}
	cat := panel("open by category", hbar(catRows, innerW(width)), width)

	statusRows := make([]barRow, 0, len(s.ByStatus))
	for _, st := range s.ByStatus {
		c := palette.Info
		switch st.Name {
		case "done":
			c = palette.OK
		case "open":
			c = palette.Rule
		}
		statusRows = append(statusRows, barRow{st.Name, st.Count, c})
	}
	status := panel("by status", hbar(statusRows, innerW(width)), width)

	// Labels must line up with todo.AgeBucket's boundaries.
	ageLabels := []string{"<1d", "1-3d", "4-7d", "8-30d", ">30d"}
	ageRows := make([]barRow, 0, len(s.OpenAgeDist))
	for i, c := range s.OpenAgeDist {
		col := palette.Info
		if i == len(ageLabels)-1 {
			col = palette.PrioHigh // >30d is stale
		}
		ageRows = append(ageRows, barRow{ageLabels[i], c, col})
	}
	age := panel("open by age", hbar(ageRows, innerW(width)), width)

	thr := panel(fmt.Sprintf("done/day 30d · 7d:%d 30d:%d", s.Done7, s.Done30),
		spark(s.DonePerDay, innerW(width)), width)

	b.WriteString(due + "\n")
	b.WriteString(prio + "\n")
	b.WriteString(cat + "\n")
	b.WriteString(age + "\n")
	b.WriteString(status + "\n")
	b.WriteString(thr + "\n")

	if len(s.ByBoard) > 1 {
		b.WriteString(panel("open by board", boardBars(s, innerW(width)), width) + "\n")
	}

	b.WriteString(panel(fmt.Sprintf("backlog · created vs done 14d · net %+d/mo", s.Net30),
		trend(s.CreatedShort, s.DoneShort, innerW(width)), width) + "\n")

	b.WriteString(faintStyle.Render(fmt.Sprintf(
		" backlog health  oldest open %dd · avg open %.1fd · stale >30d %d · time to done %.1fd",
		s.OldestOpenDays, s.AvgOpenDays, s.StaleOpen, s.AvgCycleDays)))
	return b.String()
}

// statsLegend explains every chart and the aging line, in the order they render.
func statsLegend() string {
	title := titleStyle.Render("shepherd stats · legend")
	rule := faintStyle.Render(strings.Repeat("─", lipgloss.Width("shepherd stats · legend")))
	entry := func(name, desc string) string {
		return titleStyle.Render(name) + "\n  " + desc
	}
	sections := []string{
		title + "\n" + rule,
		entry("header", "N/N done · % — items completed out of all items (done incl. archived)."),
		entry("due", "open items by urgency: overdue · due today · due within 7 days · no due date · deferred (start date in the future). Not exclusive — an item can count in more than one."),
		entry("open by priority", "open items by !H / !M / !L, or none."),
		entry("open by category", "open items per @category; (none) is uncategorised."),
		entry("open by age", "open items by how long ago they were created: <1d · 1-3d · 4-7d · 8-30d · >30d (>30d flagged red — the stale tail)."),
		entry("by status", "all items by status: open, any named status (e.g. in-progress), and done."),
		entry("done/day 30d", "sparkline of items completed per day over 30 days; 7d/30d are the totals."),
		entry("open by board", "open items per board (only with --all)."),
		entry("backlog · created vs done 14d", "two lines over 14 days — items created vs completed; net +/-per month is the 30-day created-minus-done balance (positive = backlog growing)."),
		entry("backlog health", "oldest open — age of the oldest open item · avg open — mean age of open items · stale >30d — open items created over 30 days ago · time to done — mean create→complete time for finished items."),
	}
	return strings.Join(sections, "\n\n")
}

func pct(r float64) int { return int(r*100 + 0.5) }

// innerW is the content width inside a panel (border + padding eat 4 cols).
func innerW(panelW int) int {
	w := max(panelW-4, 8)
	return w
}

func panel(title, body string, width int) string {
	inner := titleStyle.Render(title) + "\n" + body
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if palette.Rule != nil {
		st = st.BorderForeground(palette.Rule)
	}
	return st.Width(width).Render(inner) // lipgloss v2: Width includes the border
}

// hbar renders labelled horizontal bars scaled to the row max: a fixed label
// column, a coloured block bar, then the count. Bars are hand-drawn — ntcharts'
// horizontal barchart reversed row order and emitted mid-bar artefacts for this
// single-value-per-row case.
func hbar(rows []barRow, width int) string {
	if len(rows) == 0 {
		return faintStyle.Render("(none)")
	}
	valW, most := 1, 0
	for _, r := range rows {
		if r.value > most {
			most = r.value
		}
		if n := len(strconv.Itoa(r.value)); n > valW {
			valW = n
		}
	}
	// Grow the label column to the longest label, but reserve a readable bar so
	// one very long name can't starve every bar down to the floor.
	const minBar = 10
	maxLabel := max(width-minBar-valW-2, 8)
	labelW := 0
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = ansi.Truncate(r.label, maxLabel, "…")
		if w := lipgloss.Width(labels[i]); w > labelW {
			labelW = w
		}
	}
	barW := max(width-labelW-valW-2, 4)

	lines := make([]string, len(rows))
	for i, r := range rows {
		fill := 0
		if most > 0 && r.value > 0 {
			fill = max(int(float64(r.value)/float64(most)*float64(barW)+0.5), 1)
		}
		bar := fg(lipgloss.NewStyle(), r.color).Render(strings.Repeat("━", fill)) +
			faintStyle.Render(strings.Repeat("┄", barW-fill))
		lines[i] = fmt.Sprintf("%s %s %*d", lipgloss.NewStyle().Width(labelW).Render(labels[i]), bar, valW, r.value)
	}
	return strings.Join(lines, "\n")
}

func spark(vals []int, width int) string {
	sl := sparkline.New(width, 1)
	f := make([]float64, len(vals))
	for i, v := range vals {
		f[i] = float64(v)
	}
	sl.PushAll(f)
	sl.Draw()
	return sl.View()
}

// trend draws two braille series (created, done) over the same day span.
func trend(created, done []int, width int) string {
	n := len(created)
	if n == 0 {
		return ""
	}
	// real recent dates so the x-axis labels are meaningful (display only).
	// local "today" (todo.Today), not Truncate's UTC midnight, so labels match Compute's days.
	base, err := time.Parse("2006-01-02", todo.Today())
	if err != nil {
		base = time.Now()
	}
	base = base.AddDate(0, 0, -(n - 1))
	maxY := 1.0
	for _, v := range append(append([]int{}, created...), done...) {
		if float64(v) > maxY {
			maxY = float64(v)
		}
	}
	tc := timeserieslinechart.New(width, 8)
	tc.SetTimeRange(base, base.AddDate(0, 0, n-1))
	tc.SetViewTimeRange(base, base.AddDate(0, 0, n-1))
	tc.SetYRange(0, maxY)
	tc.SetViewYRange(0, maxY)
	tc.SetDataSetStyle("created", fg(lipgloss.NewStyle(), palette.Info))
	tc.SetDataSetStyle("done", fg(lipgloss.NewStyle(), palette.OK))
	for i := range n {
		d := base.AddDate(0, 0, i)
		tc.PushDataSet("created", timeserieslinechart.TimePoint{Time: d, Value: float64(created[i])})
		tc.PushDataSet("done", timeserieslinechart.TimePoint{Time: d, Value: float64(done[i])})
	}
	tc.DrawBrailleAll()
	legend := fg(lipgloss.NewStyle(), palette.Info).Render("created") + " " +
		fg(lipgloss.NewStyle(), palette.OK).Render("done")
	return tc.View() + "\n" + faintStyle.Render(legend)
}

func boardBars(s todo.Stats, width int) string {
	rows := make([]barRow, 0, len(s.ByBoard))
	names := make([]string, 0, len(s.ByBoard))
	for n := range s.ByBoard {
		names = append(names, n)
	}
	// stable order: most open first, name tiebreak
	sort.Slice(names, func(i, j int) bool {
		if a, b := s.ByBoard[names[i]].Open, s.ByBoard[names[j]].Open; a != b {
			return a > b
		}
		return names[i] < names[j]
	})
	for _, n := range names {
		rows = append(rows, barRow{n, s.ByBoard[n].Open, palette.Info})
	}
	return hbar(rows, width)
}
