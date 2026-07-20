package todo

// SetStatus sets an item to a named status, applying the same terminal rules as
// the cycle: "done" marks it done (stamping completion), "open" or "" clears to
// the implicit default, and any other name is stored as the intermediate status.
func SetStatus(it *Item, name string) {
	switch name {
	case "done":
		SetDone(it, true)
		it.Status = ""
	case "", "open":
		SetDone(it, false)
		it.Status = ""
	default:
		SetDone(it, false)
		it.Status = name
	}
}

// The agentic hand-off vocabulary is hold → go → running → done. hold and go are
// the human's to set (authorization); running and done are the agent's.

// AgenticLocked reports whether an agentic item's status is owned by the agent
// (running or done). Once set, the board must not change it — the agent drives
// the rest of the lifecycle.
func AgenticLocked(it Item) bool {
	return it.Done || it.Status == "running"
}

// ToggleAgenticStatus flips an agentic item between the two human-settable
// hand-off states, hold and go. Anything not already "go" (hold, unset, stale)
// becomes "go"; "go" becomes "hold". Callers must check AgenticLocked first —
// running/done are the agent's to set, not the board's.
func ToggleAgenticStatus(it *Item) {
	if it.Status == "go" {
		SetStatus(it, "hold")
	} else {
		SetStatus(it, "go")
	}
}

// CycleStatus advances an item to the next status in the configured order,
// wrapping around. statuses is the ordered list from config with "done" last
// (e.g. ["open", "in-progress", "done"]). The terminal "done" state is owned by
// Done/SetDone; Status only ever holds a non-terminal name, and the first
// status is left implicit (empty) so defaults aren't persisted.
func CycleStatus(it *Item, statuses []string) {
	if len(statuses) == 0 {
		return
	}
	cur := 0
	if it.Done {
		cur = len(statuses) - 1 // "done" is last after config normalization
	} else if it.Status != "" {
		for i, s := range statuses {
			if s == it.Status {
				cur = i
				break
			}
		}
	}
	next := (cur + 1) % len(statuses)
	target := statuses[next]
	switch {
	case target == "done":
		SetDone(it, true)
		it.Status = ""
	case next == 0:
		SetDone(it, false)
		it.Status = "" // first status is the implicit default
	default:
		SetDone(it, false)
		it.Status = target
	}
}
