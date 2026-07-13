package todo

import (
	"sort"
	"time"
)

// PrioCount is the open-item count per priority (H/M/L and none).
type PrioCount struct {
	H    int `json:"h"`
	M    int `json:"m"`
	L    int `json:"l"`
	None int `json:"none"`
}

// CatCount is one category's open-item count, for the by-category chart.
type CatCount struct {
	Name string `json:"name"`
	Open int    `json:"open"`
}

// ProjectStat is one board's open/done counts, for the --all by-project chart.
type ProjectStat struct {
	Open int `json:"open"`
	Done int `json:"done"`
}

// Stats is the computed summary of a board (or the aggregate of all boards).
// Done-based counts include archived items; open-item counts do not (archives
// hold no open items). All relative-date math keys off Today()/Now().
type Stats struct {
	Total          int     `json:"total"`
	Open           int     `json:"open"`
	Done           int     `json:"done"`
	CompletionRate float64 `json:"completion_rate"` // 0..1, done/total

	// Due & urgency (open items). Not a partition — an item can land in more
	// than one bucket; these are urgency indicators, not disjoint slices.
	Overdue  int `json:"overdue"`
	DueToday int `json:"due_today"`
	DueWeek  int `json:"due_week"` // 1..7 days out
	NoDue    int `json:"no_due"`
	Deferred int `json:"deferred"`

	Prio PrioCount `json:"priority"` // open items

	// Throughput & aging.
	Done7          int     `json:"done_7d"`
	Done30         int     `json:"done_30d"`
	Created7       int     `json:"created_7d"`
	Created30      int     `json:"created_30d"`
	Net30          int     `json:"net_30d"` // created_30d - done_30d
	OldestOpenDays int     `json:"oldest_open_days"`
	AvgOpenDays    float64 `json:"avg_open_days"`
	StaleOpen      int     `json:"stale_open"` // open, created > 30d ago
	AvgCycleDays   float64 `json:"avg_cycle_days"`

	// Series, oldest -> newest.
	DonePerDay   []int `json:"done_per_day"`       // 30 days, for the sparkline
	CreatedShort []int `json:"created_per_day_14"` // 14 days, trend
	DoneShort    []int `json:"done_per_day_14"`    // 14 days, trend

	ByCategory []CatCount             `json:"by_category"`          // open, desc by count
	ByProject  map[string]ProjectStat `json:"by_project,omitempty"` // only in --all
}

// Compute buckets the full item set (open + done, incl. archived) into Stats.
func Compute(items []Item) Stats {
	today, err := time.Parse(dateFormat, Today())
	if err != nil {
		today = time.Now()
	}
	daysSince := func(t time.Time) int {
		return int(today.Sub(t.Truncate(24*time.Hour)).Hours() / 24)
	}

	s := Stats{
		DonePerDay:   make([]int, 30),
		CreatedShort: make([]int, 14),
		DoneShort:    make([]int, 14),
	}
	cats := map[string]int{}
	var sumOpenAge, sumCycle float64
	var openAged, cycled int

	for _, it := range items {
		s.Total++
		if it.Done {
			s.Done++
		} else {
			s.Open++
		}

		// created-based windows + open aging
		if ct, ok := ParseTime(it.Created); ok {
			d := daysSince(ct)
			if d >= 0 {
				if d < 7 {
					s.Created7++
				}
				if d < 30 {
					s.Created30++
					if idx := 13 - d; idx >= 0 && idx < 14 {
						s.CreatedShort[idx]++
					}
				}
				if !it.Done {
					sumOpenAge += float64(d)
					openAged++
					if d > s.OldestOpenDays {
						s.OldestOpenDays = d
					}
					if d > 30 {
						s.StaleOpen++
					}
				}
			}
		}

		if it.Done {
			// completed-based windows + cycle time
			if pt, ok := ParseTime(it.Completed); ok {
				d := daysSince(pt)
				if d >= 0 {
					if d < 7 {
						s.Done7++
					}
					if d < 30 {
						s.Done30++
						if idx := 13 - d; idx >= 0 && idx < 14 {
							s.DoneShort[idx]++
						}
					}
					if idx := 29 - d; idx >= 0 && idx < 30 {
						s.DonePerDay[idx]++
					}
				}
				if ct, ok := ParseTime(it.Created); ok {
					if days := pt.Sub(ct).Hours() / 24; days >= 0 {
						sumCycle += days
						cycled++
					}
				}
			}
		} else {
			// open-only: urgency + priority + category
			if Pinned(it) {
				s.Overdue++
			}
			switch dueDays(it.Due, today) {
			case 0:
				s.DueToday++
			case 1, 2, 3, 4, 5, 6, 7:
				s.DueWeek++
			}
			if it.Due == "" {
				s.NoDue++
			}
			if Deferred(it) {
				s.Deferred++
			}
			switch it.Prio {
			case 'H':
				s.Prio.H++
			case 'M':
				s.Prio.M++
			case 'L':
				s.Prio.L++
			default:
				s.Prio.None++
			}
			name := it.Category
			if name == "" {
				name = "(none)"
			}
			cats[name]++
		}

		if it.Source != "" {
			if s.ByProject == nil {
				s.ByProject = map[string]ProjectStat{}
			}
			ps := s.ByProject[it.Source]
			if it.Done {
				ps.Done++
			} else {
				ps.Open++
			}
			s.ByProject[it.Source] = ps
		}
	}

	if s.Total > 0 {
		s.CompletionRate = float64(s.Done) / float64(s.Total)
	}
	if openAged > 0 {
		s.AvgOpenDays = sumOpenAge / float64(openAged)
	}
	if cycled > 0 {
		s.AvgCycleDays = sumCycle / float64(cycled)
	}
	s.Net30 = s.Created30 - s.Done30

	s.ByCategory = make([]CatCount, 0, len(cats))
	for n, c := range cats {
		s.ByCategory = append(s.ByCategory, CatCount{Name: n, Open: c})
	}
	sort.Slice(s.ByCategory, func(i, j int) bool {
		if s.ByCategory[i].Open != s.ByCategory[j].Open {
			return s.ByCategory[i].Open > s.ByCategory[j].Open
		}
		return s.ByCategory[i].Name < s.ByCategory[j].Name
	})
	return s
}

// dueDays returns whole days from today until an ISO due date, or -1 if the
// date is empty/unparseable (so it matches no urgency bucket).
func dueDays(due string, today time.Time) int {
	if due == "" {
		return -1
	}
	d, err := time.Parse(dateFormat, due)
	if err != nil {
		return -1
	}
	return int(d.Sub(today).Hours() / 24)
}
