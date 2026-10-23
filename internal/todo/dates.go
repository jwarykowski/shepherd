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

// ParseTime parses a created/completed stamp (tsFormat "02-01-2006 15:04").
// ok is false if the string is empty or malformed.
func ParseTime(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(tsFormat, ts)
	return t, err == nil
}

// DisplayDate renders an ISO date as day-month-year DD-MM-YYYY; raw if unparseable.
func DisplayDate(iso string) string {
	if t, err := time.Parse(dateFormat, iso); err == nil {
		return t.Format(dmyDate)
	}
	return iso
}

// weekdays maps day names, full and short, to their time.Weekday.
var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

// ParseDue resolves preset keywords and relative forms to a YYYY-MM-DD date.
// Presets: today, tomorrow, week/next week, month/next month, a weekday name
// (the next one after today, so "friday" on a Friday is a week away), and
// "+Nd". Empty clears; an already-valid date passes through; anything else
// clears (ValidDate reports whether s would resolve).
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
		return addMonths(base, 1).Format(dateFormat)
	}
	if wd, ok := weekdays[s]; ok {
		days := (int(wd)-int(base.Weekday())+6)%7 + 1 // 1..7: never today
		return base.AddDate(0, 0, days).Format(dateFormat)
	}
	// relative: <n><unit>, unit d/w/m/y, optional leading + (e.g. 3d, 2w, 5m, 1y)
	if r := strings.TrimPrefix(s, "+"); len(r) >= 2 {
		if n, err := strconv.Atoi(r[:len(r)-1]); err == nil && n >= -maxRelative && n <= maxRelative {
			switch r[len(r)-1] {
			case 'd':
				return base.AddDate(0, 0, n).Format(dateFormat)
			case 'w':
				return base.AddDate(0, 0, 7*n).Format(dateFormat)
			case 'm':
				return addMonths(base, n).Format(dateFormat)
			case 'y':
				return addMonths(base, 12*n).Format(dateFormat)
			}
		}
	}
	// explicit dates: accept DMY or ISO, normalise to ISO on disk
	if t, err := time.Parse(dmyDate, s); err == nil {
		return t.Format(dateFormat)
	}
	if t, err := time.Parse(dateFormat, s); err == nil {
		return t.Format(dateFormat)
	}
	return "" // unrecognised: clear rather than store garbage
}

// ValidDate reports whether s is a date ParseDue resolves, or empty (which
// clears). It lets callers reject a typo instead of silently clearing the date.
func ValidDate(s string) bool {
	return strings.TrimSpace(s) == "" || ParseDue(s) != ""
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

// SetDone flips an item's done state and maintains the completion timestamp:
// stamped when marked done, cleared when reopened.
func SetDone(it *Item, done bool) {
	if it.Done == done {
		return // idempotent: re-marking a done/open item keeps its original stamp
	}
	it.Done = done
	if done {
		it.Completed = Now()
	} else {
		it.Completed = ""
	}
}

// Deferred reports whether an open item's start/defer date is still in the
// future — i.e. it hasn't begun yet.
func Deferred(it Item) bool {
	if it.Done || it.Defer == "" {
		return false
	}
	d, err := time.Parse(dateFormat, it.Defer)
	if err != nil {
		return false
	}
	t, err := time.Parse(dateFormat, Today())
	if err != nil {
		return false
	}
	return d.After(t)
}

// DeferLabel renders how long until a defer/start date is reached ("starts Nd");
// empty once it's today or past, or if unparseable.
func DeferLabel(iso string) string {
	d, err := time.Parse(dateFormat, iso)
	if err != nil {
		return ""
	}
	t, err := time.Parse(dateFormat, Today())
	if err != nil {
		return ""
	}
	days := int(d.Sub(t).Hours() / 24)
	if days <= 0 {
		return ""
	}
	return fmt.Sprintf("starts %dd", days)
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

// maxRelative bounds a relative date's count, so 999999999y is rejected rather
// than overflowing into a garbage year.
const maxRelative = 1000

// addMonths moves t by n months, clamping to the target month's last day so
// Jan 31 + 1 month is Feb 28/29, not Mar 3.
func addMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(t.Day(), last)-1)
}
