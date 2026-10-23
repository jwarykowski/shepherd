package todo

import "reflect"

// Merge folds another writer's edits (theirs) into ours, both descended from
// base, matching items by ID. An item only one side changed takes that side's
// version; when both changed it, ours wins. Items either side added are kept,
// and an item either side deleted is dropped unless the other side changed it.
// Items without an ID can't be matched and count as added.
func Merge(base, ours, theirs []Item) []Item {
	b, o, t := byID(base), byID(ours), byID(theirs)
	out := make([]Item, 0, len(ours)+len(theirs))
	for _, it := range ours {
		was, inBase := b[it.ID]
		now, inTheirs := t[it.ID]
		switch {
		case !inBase:
			out = append(out, it)
		case !inTheirs:
			if !reflect.DeepEqual(it, was) {
				out = append(out, it)
			}
		case reflect.DeepEqual(it, was):
			out = append(out, now)
		default:
			out = append(out, it)
		}
	}
	for _, it := range theirs {
		was, inBase := b[it.ID]
		_, inOurs := o[it.ID]
		switch {
		case !inBase && !inOurs:
			out = append(out, it)
		case inBase && !inOurs && !reflect.DeepEqual(it, was):
			out = append(out, it) // we deleted it, they changed it: keep their edit
		}
	}
	return out
}

func byID(items []Item) map[string]Item {
	m := make(map[string]Item, len(items))
	for _, it := range items {
		if it.ID != "" {
			m[it.ID] = it
		}
	}
	return m
}
