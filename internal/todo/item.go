// Package todo is shepherd's domain layer: the Item type and the pure logic
// over it (quick-add parsing, dates, ordering). No I/O, no UI, no third-party
// dependencies.
package todo

import "strings"

// Item is a single todo entry.
type Item struct {
	Done      bool
	Prio      byte // 'H', 'M', 'L', or 0 for none
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
}

// ParseQuickAdd splits an add line into text plus @category, !h/!m/!l priority,
// due:<preset>, defer:<preset>, and link:<url> tokens. Unrecognized tokens stay
// part of the text.
func ParseQuickAdd(s string) Item {
	it := Item{Created: Now()}
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
	it.Text = strings.Join(words, " ")
	return it
}
