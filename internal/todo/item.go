// Package todo is shepherd's domain layer: the Item type and the pure logic
// over it (quick-add parsing, dates, ordering). No I/O, no UI, no third-party
// dependencies.
package todo

import "strings"

// Item is a single todo entry.
type Item struct {
	Done     bool
	Prio     byte // 'H', 'M', 'L', or 0 for none
	Text     string
	Category string
	Created  string
	Due      string // YYYY-MM-DD, or empty
	Note     string
}

// ParseQuickAdd splits an add line into text plus @category, !h/!m/!l priority,
// and due:<preset> tokens. Unrecognized tokens stay part of the text.
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
		default:
			words = append(words, tok)
		}
	}
	it.Text = strings.Join(words, " ")
	return it
}
