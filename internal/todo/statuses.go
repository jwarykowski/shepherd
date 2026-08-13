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

// StatusOf is an item's effective status name against the configured order:
// "done" (last after normalization) when Done, the first status when Status is
// left implicit (empty), else Status itself.
func StatusOf(it Item, statuses []string) string {
	if len(statuses) == 0 {
		return ""
	}
	if it.Done {
		return statuses[len(statuses)-1]
	}
	if it.Status == "" {
		return statuses[0]
	}
	return it.Status
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
	for i, s := range statuses {
		if s == StatusOf(*it, statuses) {
			cur = i
			break
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
