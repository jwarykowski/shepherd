package todo

import "testing"

// TestCompute pins today and feeds items covering every bucket, asserting the
// non-trivial counting/aging logic.
func TestCompute(t *testing.T) {
	pinToday(t, "2026-07-13")

	items := []Item{
		// open, overdue, high, infra
		{Prio: 'H', Text: "a", Category: "infra", Created: "20-06-2026 09:00", Due: "2026-07-10"},
		// open, due today, high
		{Prio: 'H', Text: "b", Created: "12-07-2026 09:00", Due: "2026-07-13"},
		// open, due this week, med
		{Prio: 'M', Text: "c", Created: "11-07-2026 09:00", Due: "2026-07-16"},
		// open, no due, stale (created > 30d ago), med
		{Prio: 'M', Text: "d", Created: "01-05-2026 09:00"},
		// open, deferred (future start), low, no due
		{Prio: 'L', Text: "e", Created: "12-07-2026 09:00", Defer: "2026-07-20"},
		// done last 7d, cycle 3d
		{Done: true, Text: "f", Category: "infra", Created: "08-07-2026 09:00", Completed: "11-07-2026 09:00"},
		// done 18d ago (in 30d window, not 7d), cycle 5d
		{Done: true, Text: "g", Created: "20-06-2026 09:00", Completed: "25-06-2026 09:00"},
	}

	s := Compute(items)

	eq := func(name string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	eq("total", s.Total, 7)
	eq("open", s.Open, 5)
	eq("done", s.Done, 2)
	if s.CompletionRate != 2.0/7.0 {
		t.Errorf("completion = %v", s.CompletionRate)
	}
	eq("overdue", s.Overdue, 1)
	eq("dueToday", s.DueToday, 1)
	eq("dueWeek", s.DueWeek, 1)
	eq("noDue", s.NoDue, 2) // d and e
	eq("deferred", s.Deferred, 1)
	eq("prioH", s.Prio.H, 2)
	eq("prioM", s.Prio.M, 2)
	eq("prioL", s.Prio.L, 1)
	eq("prioNone", s.Prio.None, 0)
	eq("done7", s.Done7, 1)
	eq("done30", s.Done30, 2)
	eq("stale", s.StaleOpen, 1) // d, created 01-05
	eq("oldestOpen", s.OldestOpenDays, 73)
	// open ages: b/c/e in 1-3d, a ~23d in 8-30d, d ~73d >30d
	if want := []int{0, 3, 0, 1, 1}; !equalInts(s.OpenAgeDist, want) {
		t.Errorf("openAgeDist = %v, want %v", s.OpenAgeDist, want)
	}
	eq("net30", s.Net30, s.Created30-s.Done30)
	// cycle: f 3d, g 5d -> avg 4
	if s.AvgCycleDays != 4.0 {
		t.Errorf("avgCycle = %v, want 4", s.AvgCycleDays)
	}
	// series: one done ~2d ago and one ~18d ago land in the 30-day sparkline
	sum := 0
	for _, v := range s.DonePerDay {
		sum += v
	}
	eq("donePerDay sum", sum, 2)
	if len(s.DonePerDay) != 30 || len(s.DoneShort) != 14 || len(s.CreatedShort) != 14 {
		t.Errorf("series lengths off: %d %d %d", len(s.DonePerDay), len(s.DoneShort), len(s.CreatedShort))
	}
	// by-category: infra (1 open) present, sorted; no Source so ByProject nil
	if len(s.ByCategory) == 0 || s.ByProject != nil {
		t.Errorf("category/project: %+v / %v", s.ByCategory, s.ByProject)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAgeBucket pins the open-age bucket boundaries.
func TestAgeBucket(t *testing.T) {
	cases := []struct {
		days, want int
	}{
		{0, 0}, {1, 1}, {3, 1}, {4, 2}, {7, 2}, {8, 3}, {30, 3}, {31, 4}, {365, 4},
	}
	for _, c := range cases {
		if got := AgeBucket(c.days); got != c.want {
			t.Errorf("AgeBucket(%d) = %d, want %d", c.days, got, c.want)
		}
	}
}

// TestComputeByProject checks Source drives the by-project aggregate.
func TestComputeByProject(t *testing.T) {
	pinToday(t, "2026-07-13")
	items := []Item{
		{Text: "a", Source: "web", Created: "12-07-2026 09:00"},
		{Text: "b", Source: "web", Created: "12-07-2026 09:00"},
		{Done: true, Text: "c", Source: "web", Created: "10-07-2026 09:00", Completed: "12-07-2026 09:00"},
		{Text: "d", Source: "api", Created: "12-07-2026 09:00"},
	}
	s := Compute(items)
	if s.ByProject["web"].Open != 2 || s.ByProject["web"].Done != 1 {
		t.Errorf("web = %+v", s.ByProject["web"])
	}
	if s.ByProject["api"].Open != 1 {
		t.Errorf("api = %+v", s.ByProject["api"])
	}
}
