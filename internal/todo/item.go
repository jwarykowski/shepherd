// Package todo is shepherd's domain layer: the Item type and the pure logic
// over it (quick-add parsing, dates, ordering). No I/O, no UI, no third-party
// dependencies.
package todo

import "strings"

// Item is a single todo entry.
type Item struct {
	ID        string // stable opaque id (see NewID); agents address items by it, not by list position
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
	// Agentic marks a task raised and driven by an autonomous agent (e.g. drover).
	// The status lifecycle (hold/go/running) is only meaningful on such tasks; a
	// human toggles the status to hand work to, or hold it from, the agent.
	Agentic bool
	// Source is the board an item came from in an aggregated (global) view,
	// e.g. "web" or "default". Derived from the filename; never serialised.
	Source string
	// Subs are one level of nested subtasks. A subtask is a full Item, but its
	// own Subs and Source are never serialised — subtasks don't nest, and they
	// inherit the parent's board. A slice field makes Item non-comparable; use
	// Clone/positional lookups, never == or reflect.DeepEqual on Item.
	Subs []Item
}

// ApplyEdit applies quick-add tokens onto an existing item, touching only the
// fields present in s: @category, !h/!m/!l priority, due:<preset>,
// defer:<preset>, link:<url>, status:<name>, agentic, and note:<text>. A bare
// key token clears that field: "@", "!", "due:", "defer:", "link:", "status:"
// reset category / priority / due / defer / link / status respectively; a bare
// "agentic" sets the agentic flag and "agentic:false" clears it. Text is
// replaced only when s carries plain (non-token) words, so a token-only edit
// leaves the text alone. Unrecognised tokens count as plain words.
//
// note: is special — a note may contain spaces, so once seen it consumes the
// rest of the line as the note value (a bare trailing "note:" clears it). Put
// note: last in the edit string.
func ApplyEdit(it *Item, s string) {
	// note: swallows everything after it (spaces and all); pull it out first so
	// the remaining tokens split cleanly on whitespace.
	if i := noteIndex(s); i >= 0 {
		it.Note = strings.TrimSpace(s[i+len("note:"):])
		s = strings.TrimSpace(s[:i])
	}
	var words []string
	for _, tok := range strings.Fields(s) {
		switch {
		case tok == "@":
			it.Category = ""
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			it.Category = strings.ToLower(tok[1:])
		case tok == "!":
			it.Prio = 0
		case strings.HasPrefix(tok, "!") && len(tok) == 2 && strings.ContainsRune("hHmMlL", rune(tok[1])):
			it.Prio = strings.ToUpper(tok[1:])[0]
		case tok == "due:":
			it.Due = ""
		case strings.HasPrefix(tok, "due:") && len(tok) > 4:
			it.Due = ParseDue(tok[4:])
		case tok == "defer:":
			it.Defer = ""
		case strings.HasPrefix(tok, "defer:") && len(tok) > 6:
			it.Defer = ParseDue(tok[6:])
		case tok == "link:":
			it.Link = ""
		case strings.HasPrefix(tok, "link:") && len(tok) > 5:
			it.Link = tok[5:]
		case tok == "status:":
			SetStatus(it, "open")
		case strings.HasPrefix(tok, "status:") && len(tok) > 7:
			SetStatus(it, strings.ToLower(tok[7:]))
		case tok == "agentic", tok == "agentic:true":
			it.Agentic = true
		case tok == "agentic:false":
			it.Agentic = false
		default:
			words = append(words, tok)
		}
	}
	if len(words) > 0 {
		it.Text = strings.Join(words, " ")
	}
}

// noteIndex returns the byte offset of the "note:" token in s (at the start or
// after a space), or -1 if none. It marks where the free-text note begins.
func noteIndex(s string) int {
	if strings.HasPrefix(s, "note:") {
		return 0
	}
	if i := strings.Index(s, " note:"); i >= 0 {
		return i + 1
	}
	return -1
}

// ParseQuickAdd builds a new item from an add line, splitting text from
// @category, !h/!m/!l priority, due:/defer:/link: tokens. Unrecognised tokens
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
