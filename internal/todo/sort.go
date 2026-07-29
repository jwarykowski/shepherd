package todo

import (
	"cmp"
	"sort"
	"strings"
)

// Rank maps a priority byte to a sort order (H<M<L<none). Exported for the UI's
// group ordering.
func Rank(p byte) int {
	switch p {
	case 'H':
		return 0
	case 'M':
		return 1
	case 'L':
		return 2
	}
	return 3
}

// catKey sorts named categories alphabetically, uncategorized last.
func catKey(c string) string {
	if c == "" {
		return "￿"
	}
	return strings.ToLower(c)
}

// TagKey is an item's grouping tag: its first tag, or "" when untagged. Tags are
// multi-valued but a row belongs to exactly one group, so the first one wins —
// the rest ride along on the row. Exported for the UI's group ordering.
func TagKey(it Item) string {
	if len(it.Tags) == 0 {
		return ""
	}
	return it.Tags[0]
}

// dueKey sorts soonest-first; no due date sorts last.
func dueKey(d string) string {
	if d == "" {
		return "9999-99-99"
	}
	return d
}

// less is the shared item ordering: overdue pinned first, then
// category-then-priority (or priority-then-category when byPrio), then soonest due.
func less(a, b Item, byPrio bool) bool {
	if pa, pb := Pinned(a), Pinned(b); pa != pb {
		return pa
	}
	cat := cmp.Compare(catKey(a.Category), catKey(b.Category))
	prio := cmp.Compare(Rank(a.Prio), Rank(b.Prio))
	order := [2]int{cat, prio}
	if byPrio {
		order = [2]int{prio, cat}
	}
	for _, c := range order {
		if c != 0 {
			return c < 0
		}
	}
	return cmp.Compare(dueKey(a.Due), dueKey(b.Due)) < 0
}

// Sort orders items: overdue pinned first, then soonest due. The middle two
// keys are category then priority, or priority then category when byPrio (the
// priority view).
func Sort(items []Item, byPrio bool) {
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j], byPrio) })
}

// SortByTag orders items by their grouping tag (untagged last), then by the
// shared intra-group order, so each tag's items stay contiguous.
func SortByTag(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		// catKey's "untagged/uncategorized last" trick works for tags too.
		if a, b := catKey(TagKey(items[i])), catKey(TagKey(items[j])); a != b {
			return a < b
		}
		return less(items[i], items[j], false)
	})
}

// SortBySource orders items by Source first (the global board view), then by
// the shared intra-group order, so each board's items stay contiguous.
func SortBySource(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		return less(items[i], items[j], false)
	})
}
