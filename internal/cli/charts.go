package cli

import (
	"fmt"
	"strings"
	"time"

	"shepherd/internal/todo"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
)

// Priority colors mirror tui/view.go: H red, M yellow, L faint. Kept here (not
// cross-imported) so the tui package stays UI-only.
const (
	colHigh = "1"
	colMed  = "3"
	colLow  = "8"
	colOK   = "2"
	colDim  = "240"
	colInfo = "4"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	faintStyle = lipgloss.NewStyle().Faint(true)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colDim)).
			Padding(0, 1)
)

type barRow struct {
	label string
	value int
	color string
}

// renderStats lays the charts out as a responsive grid sized to width.
func renderStats(s todo.Stats, title string, width int) string {
	if width < 20 {
		width = 20
	}

	var b strings.Builder
	left := titleStyle.Render(title)
	right := faintStyle.Render(fmt.Sprintf("%d/%d done · %d%%", s.Done, s.Total, pct(s.CompletionRate)))
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(left + strings.Repeat(" ", gap) + right + "\n")
	b.WriteString(faintStyle.Render(strings.Repeat("─", width)) + "\n\n")

	due := panel("due", hbar([]barRow{
		{"overdue", s.Overdue, colHigh},
		{"today", s.DueToday, colMed},
		{"this week", s.DueWeek, colOK},
		{"no date", s.NoDue, colDim},
		{"deferred", s.Deferred, colInfo},
	}, innerW(width)), width)

	prio := panel("open by priority", hbar([]barRow{
		{"!H", s.Prio.H, colHigh},
		{"!M", s.Prio.M, colMed},
		{"!L", s.Prio.L, colLow},
		{"none", s.Prio.None, colDim},
	}, innerW(width)), width)

	catRows := make([]barRow, 0, len(s.ByCategory))
	for _, c := range s.ByCategory {
		catRows = append(catRows, barRow{c.Name, c.Open, colInfo})
	}
	cat := panel("open by category", hbar(catRows, innerW(width)), width)

	statusRows := make([]barRow, 0, len(s.ByStatus))
	for _, st := range s.ByStatus {
		c := colInfo
		switch st.Name {
		case "done":
			c = colOK
		case "open":
			c = colDim
		}
		statusRows = append(statusRows, barRow{st.Name, st.Count, c})
	}
	status := panel("by status", hbar(statusRows, innerW(width)), width)

	thr := panel(fmt.Sprintf("done/day 30d · 7d:%d 30d:%d", s.Done7, s.Done30),
		spark(s.DonePerDay, innerW(width)), width)

	b.WriteString(due + "\n")
	b.WriteString(prio + "\n")
	b.WriteString(cat + "\n")
	b.WriteString(status + "\n")
	b.WriteString(thr + "\n")

	if len(s.ByProject) > 1 {
		b.WriteString(panel("open by project", projectBars(s, innerW(width)), width) + "\n")
	}

	b.WriteString(panel(fmt.Sprintf("backlog · created vs done 14d · net %+d/mo", s.Net30),
		trend(s.CreatedShort, s.DoneShort, innerW(width)), width) + "\n")

	b.WriteString(faintStyle.Render(fmt.Sprintf(
		" aging  oldest %dd · avg %.1fd · stale>30d %d · cycle %.1fd",
		s.OldestOpenDays, s.AvgOpenDays, s.StaleOpen, s.AvgCycleDays)))
	return b.String()
}

func pct(r float64) int { return int(r*100 + 0.5) }

// innerW is the content width inside a panel (border + padding eat 4 cols).
func innerW(panelW int) int {
	w := panelW - 4
	if w < 8 {
		w = 8
	}
	return w
}

func panel(title, body string, width int) string {
	inner := titleStyle.Render(title) + "\n" + body
	return panelStyle.Width(width - 2).Render(inner)
}

// hbar renders labelled horizontal bars scaled to the row max: a fixed label
// column, a colored block bar, then the count. Bars are hand-drawn — ntcharts'
// horizontal barchart reversed row order and emitted mid-bar artifacts for this
// single-value-per-row case.
func hbar(rows []barRow, width int) string {
	if len(rows) == 0 {
		return faintStyle.Render("(none)")
	}
	valW, max := 1, 0
	for _, r := range rows {
		if r.value > max {
			max = r.value
		}
		if n := len(fmt.Sprint(r.value)); n > valW {
			valW = n
		}
	}
	// Grow the label column to the longest label, but reserve a readable bar so
	// one very long name can't starve every bar down to the floor.
	const minBar = 10
	maxLabel := width - minBar - valW - 2
	if maxLabel < 8 {
		maxLabel = 8
	}
	labelW := 0
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = clip(r.label, maxLabel)
		if w := lipgloss.Width(labels[i]); w > labelW {
			labelW = w
		}
	}
	barW := width - labelW - valW - 2
	if barW < 4 {
		barW = 4
	}

	lines := make([]string, len(rows))
	for i, r := range rows {
		fill := 0
		if max > 0 && r.value > 0 {
			fill = int(float64(r.value)/float64(max)*float64(barW) + 0.5)
			if fill < 1 {
				fill = 1
			}
		}
		bar := lipgloss.NewStyle().Foreground(lipgloss.Color(r.color)).Render(strings.Repeat("━", fill)) +
			faintStyle.Render(strings.Repeat("┄", barW-fill))
		lines[i] = fmt.Sprintf("%s %s %*d", padR(labels[i], labelW), bar, valW, r.value)
	}
	return strings.Join(lines, "\n")
}

// clip shortens a label to n display columns, ending with … when cut.
func clip(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// padR right-pads s with spaces to n display columns.
func padR(s string, n int) string {
	if p := n - lipgloss.Width(s); p > 0 {
		return s + strings.Repeat(" ", p)
	}
	return s
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
	base := time.Now().Truncate(24*time.Hour).AddDate(0, 0, -(n - 1))
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
	tc.SetDataSetStyle("created", lipgloss.NewStyle().Foreground(lipgloss.Color(colInfo)))
	tc.SetDataSetStyle("done", lipgloss.NewStyle().Foreground(lipgloss.Color(colOK)))
	for i := 0; i < n; i++ {
		d := base.AddDate(0, 0, i)
		tc.PushDataSet("created", timeserieslinechart.TimePoint{Time: d, Value: float64(created[i])})
		tc.PushDataSet("done", timeserieslinechart.TimePoint{Time: d, Value: float64(done[i])})
	}
	tc.DrawBrailleAll()
	legend := lipgloss.NewStyle().Foreground(lipgloss.Color(colInfo)).Render("created") + " " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colOK)).Render("done")
	return tc.View() + "\n" + faintStyle.Render(legend)
}

func projectBars(s todo.Stats, width int) string {
	rows := make([]barRow, 0, len(s.ByProject))
	names := make([]string, 0, len(s.ByProject))
	for n := range s.ByProject {
		names = append(names, n)
	}
	// stable order: most open first, name tiebreak
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := s.ByProject[names[i]], s.ByProject[names[j]]
			if b.Open > a.Open || (b.Open == a.Open && names[j] < names[i]) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, n := range names {
		rows = append(rows, barRow{n, s.ByProject[n].Open, colInfo})
	}
	return hbar(rows, width)
}
