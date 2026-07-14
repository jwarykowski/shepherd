// Package todo is shepherd's domain layer: the Item type and the pure logic
// over it (quick-add parsing, dates, ordering). No I/O, no UI, no third-party
// dependencies.
package todo

import "strings"

// Item is a single todo entry.
type Item struct {
	Done      bool
	Status    string // named non-terminal status (e.g. "in-progress"); empty = first/default status; ignored when Done
	Prio      byte   // 'H', 'M', 'L', or 0 for none
	Text      string
	Category  string
	Created   string
	Completed string // timestamp the item was marked done, or empty
	Defer     string // YYYY-MM-DD start/defer date, or empty
	Due       string // YYYY-MM-DD, or empty
	Note      string
	Link      string // reference URL, or empty
	// Source is the board an item came from in an aggregated (global) view,
	// e.g. "web" or "default". Derived from the filename; never serialized.
	Source string
	// Subs are one level of nested subtasks. A subtask is a full Item, but its
	// own Subs and Source are never serialized — subtasks don't nest, and they
	// inherit the parent's board. A slice field makes Item non-comparable; use
	// Clone/positional lookups, never == or reflect.DeepEqual on Item.
	Subs []Item
}

// ApplyEdit applies quick-add tokens onto an existing item, touching only the
// fields present in s: @category, !h/!m/!l priority, due:<preset>,
// defer:<preset>, link:<url>. Text is replaced only when s carries plain
// (non-token) words, so a token-only edit leaves the text alone. Unrecognized
// tokens count as plain words.
func ApplyEdit(it *Item, s string) {
	var words []string
	for _, tok := range strings.Fields(s) {
		switch {
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			it.Category = strings.ToLower(tok[1:])
		case strings.HasPrefix(tok, "!") && len(tok) == 2 && strings.ContainsRune("hHmMlL", rune(tok[1])):
			it.Prio = strings.ToUpper(tok[1:])[0]
		case strings.HasPrefix(tok, "due:") && len(tok) > 4:
			it.Due = ParseDue(tok[4:])
		case strings.HasPrefix(tok, "defer:") && len(tok) > 6:
			it.Defer = ParseDue(tok[6:])
		case strings.HasPrefix(tok, "link:") && len(tok) > 5:
			it.Link = tok[5:]
		default:
			words = append(words, tok)
		}
	}
	if len(words) > 0 {
		it.Text = strings.Join(words, " ")
	}
}

// ParseQuickAdd builds a new item from an add line, splitting text from
// @category, !h/!m/!l priority, due:/defer:/link: tokens. Unrecognized tokens
// stay part of the text.
func ParseQuickAdd(s string) Item {
	it := Item{Created: Now()}
	ApplyEdit(&it, s)
	return it
}

// Match reports whether an item matches filter query q, which the caller has
// already lowercased. Empty q matches everything. Searches text, note,
// category, due, defer, and link.
func Match(it Item, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(it.Text), q) ||
		strings.Contains(strings.ToLower(it.Note), q) ||
		strings.Contains(strings.ToLower(it.Category), q) ||
		strings.Contains(strings.ToLower(it.Due), q) ||
		strings.Contains(strings.ToLower(it.Defer), q) ||
		strings.Contains(strings.ToLower(it.Link), q)
}
