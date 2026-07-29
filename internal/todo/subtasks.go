package todo

// Subtasks are one level of nested items. Completion cascades both ways: the
// helpers here are the single owner of that rule so the CLI and TUI can't
// diverge. A subtask carries the same fields as any Item; only its own Subs and
// Source are never persisted (see Item.Subs).

// SetParentDone sets a parent's done state and cascades it to every subtask.
// Completing a parent completes all its subs; reopening it reopens them.
func SetParentDone(p *Item, done bool) {
	SetDone(p, done)
	for i := range p.Subs {
		SetDone(&p.Subs[i], done)
	}
}

// SetSubDone sets one subtask's done state, then reconciles the parent: the
// parent is done exactly when it has subs and all of them are done. i is a
// bounds-checked index into p.Subs; out-of-range is a no-op.
func SetSubDone(p *Item, i int, done bool) {
	if i < 0 || i >= len(p.Subs) {
		return
	}
	SetDone(&p.Subs[i], done)
	SetDone(p, AllSubsDone(p))
}

// CycleSubStatus advances a subtask through the configured statuses, then
// reconciles the parent's done state.
func CycleSubStatus(p *Item, i int, statuses []string) {
	if i < 0 || i >= len(p.Subs) {
		return
	}
	CycleStatus(&p.Subs[i], statuses)
	SetDone(p, AllSubsDone(p))
}

// AllSubsDone reports whether an item has subtasks and every one is done. False
// for a subtask-less item, so it never forces a plain task done.
func AllSubsDone(p *Item) bool {
	if len(p.Subs) == 0 {
		return false
	}
	for i := range p.Subs {
		if !p.Subs[i].Done {
			return false
		}
	}
	return true
}

// SubCount returns done/total subtasks for an item.
func SubCount(it Item) (done, total int) {
	for i := range it.Subs {
		if it.Subs[i].Done {
			done++
		}
	}
	return done, len(it.Subs)
}

// Clone deep-copies items including their Subs and Tags, so a snapshot
// (undo/redo) can't be mutated through a shared slice. Item is non-comparable,
// so the outer append([]Item, ...) shallow copy is not enough on its own.
func Clone(items []Item) []Item {
	if items == nil {
		return nil
	}
	cloneOne := func(it Item) Item {
		if it.Tags != nil {
			it.Tags = append([]string(nil), it.Tags...)
		}
		return it
	}
	out := make([]Item, len(items))
	for i := range items {
		out[i] = cloneOne(items[i])
		if items[i].Subs != nil {
			out[i].Subs = make([]Item, len(items[i].Subs))
			for j, s := range items[i].Subs {
				out[i].Subs[j] = cloneOne(s)
			}
		}
	}
	return out
}
