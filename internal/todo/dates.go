package todo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	tsFormat   = "02-01-2006 15:04"
	dateFormat = "2006-01-02" // ISO on disk: sorts lexically, so due ordering works
	dmyDate    = "02-01-2006" // day-month-year (DMY), for display + input
)

// Now / Today are package vars so tests can pin them.
var (
	Now   = func() string { return time.Now().Format(tsFormat) }
	Today = func() string { return time.Now().Format(dateFormat) }
)

// DisplayDate renders an ISO date as day-month-year DD-MM-YYYY; raw if unparseable.
func DisplayDate(iso string) string {
	if t, err := time.Parse(dateFormat, iso); err == nil {
		return t.Format(dmyDate)
	}
	return iso
}

// ParseDue resolves preset keywords and relative forms to a YYYY-MM-DD date.
// Presets: today, tomorrow, week/next week, month/next month, and "+Nd".
// Empty clears; an already-valid date passes through; anything else clears.
func ParseDue(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	base, err := time.Parse(dateFormat, Today())
	if err != nil {
		return s
	}
	switch s {
	case "today":
		return base.Format(dateFormat)
	case "tomorrow", "tmr", "tom":
		return base.AddDate(0, 0, 1).Format(dateFormat)
	case "week", "next week":
		return base.AddDate(0, 0, 7).Format(dateFormat)
	case "month", "next month":
		return base.AddDate(0, 1, 0).Format(dateFormat)
	}
	// relative: <n><unit>, unit d/w/m/y, optional leading + (e.g. 3d, 2w, 5m, 1y)
	if r := strings.TrimPrefix(s, "+"); len(r) >= 2 {
		if n, err := strconv.Atoi(r[:len(r)-1]); err == nil {
			switch r[len(r)-1] {
			case 'd':
				return base.AddDate(0, 0, n).Format(dateFormat)
			case 'w':
				return base.AddDate(0, 0, 7*n).Format(dateFormat)
			case 'm':
				return base.AddDate(0, n, 0).Format(dateFormat)
			case 'y':
				return base.AddDate(n, 0, 0).Format(dateFormat)
			}
		}
	}
	// explicit dates: accept DMY or ISO, normalize to ISO on disk
	if t, err := time.Parse(dmyDate, s); err == nil {
		return t.Format(dateFormat)
	}
	if t, err := time.Parse(dateFormat, s); err == nil {
		return t.Format(dateFormat)
	}
	return "" // unrecognized: clear rather than store garbage
}

// DueLabel renders a due date relative to today, and whether it's due/overdue.
func DueLabel(due string) (string, bool) {
	d, err := time.Parse(dateFormat, due)
	if err != nil {
		return due, false // unparseable — show raw, don't flag
	}
	t, err := time.Parse(dateFormat, Today())
	if err != nil {
		return due, false
	}
	days := int(d.Sub(t).Hours() / 24)
	switch {
	case days < 0:
		return fmt.Sprintf("overdue %dd", -days), true
	case days == 0:
		return "due today", true
	case days == 1:
		return "due 1d", false
	default:
		return fmt.Sprintf("due %dd", days), false
	}
}

// Pinned reports whether an item is surfaced to the top: open and past due.
func Pinned(it Item) bool {
	if it.Done || it.Due == "" {
		return false
	}
	d, err := time.Parse(dateFormat, it.Due)
	if err != nil {
		return false
	}
	t, err := time.Parse(dateFormat, Today())
	if err != nil {
		return false
	}
	return d.Before(t)
}
