package tui

import binding "charm.land/bubbles/v2/key"

// keys is the board's keymap: the list, global and detail handlers dispatch on
// these bindings and the footer grids render their help, so a key and its hint
// can't drift apart.
var keys = struct {
	Up, Down, LaneLeft, LaneRight, Toggle, Detail, View, Global, ArchiveView, Boards, Footer binding.Binding
	Add, AddSub, Edit, Status, Delete, Sweep, Archive                                        binding.Binding
	Prio, Category, Tags, Due, Defer, Link, Open, Copy, Note                                 binding.Binding
	Save, Editor, Undo, Redo, Filter, Settings, Help, Back, Quit                             binding.Binding
}{
	Up:          binding.NewBinding(binding.WithKeys("k", "up"), binding.WithHelp("j/k", "move")),
	Down:        binding.NewBinding(binding.WithKeys("j", "down")),
	LaneLeft:    binding.NewBinding(binding.WithKeys("left"), binding.WithHelp("←/→", "lane")),
	LaneRight:   binding.NewBinding(binding.WithKeys("right")),
	Toggle:      binding.NewBinding(binding.WithKeys("space"), binding.WithHelp("space", "toggle")),
	Detail:      binding.NewBinding(binding.WithKeys("d"), binding.WithHelp("d", "detail")),
	View:        binding.NewBinding(binding.WithKeys("v"), binding.WithHelp("v", "view")),
	Global:      binding.NewBinding(binding.WithKeys("A"), binding.WithHelp("A", "global")),
	ArchiveView: binding.NewBinding(binding.WithKeys("e"), binding.WithHelp("e", "archive")),
	Boards:      binding.NewBinding(binding.WithKeys("b"), binding.WithHelp("b", "boards")),
	Footer:      binding.NewBinding(binding.WithKeys("F"), binding.WithHelp("F", "footer")),

	Add:     binding.NewBinding(binding.WithKeys("a"), binding.WithHelp("a", "add")),
	AddSub:  binding.NewBinding(binding.WithKeys("S"), binding.WithHelp("S", "sub")),
	Edit:    binding.NewBinding(binding.WithKeys("u"), binding.WithHelp("u", "edit")),
	Status:  binding.NewBinding(binding.WithKeys("tab"), binding.WithHelp("tab", "status")),
	Delete:  binding.NewBinding(binding.WithKeys("x"), binding.WithHelp("x", "del")),
	Sweep:   binding.NewBinding(binding.WithKeys("c"), binding.WithHelp("c", "sweep")),
	Archive: binding.NewBinding(binding.WithKeys("C"), binding.WithHelp("C", "arch")),

	Prio:     binding.NewBinding(binding.WithKeys("h", "m", "l"), binding.WithHelp("h/m/l", "prio")),
	Category: binding.NewBinding(binding.WithKeys("g"), binding.WithHelp("g", "cat")),
	Tags:     binding.NewBinding(binding.WithKeys("T"), binding.WithHelp("T", "tags")),
	Due:      binding.NewBinding(binding.WithKeys("t"), binding.WithHelp("t", "due")),
	Defer:    binding.NewBinding(binding.WithKeys("s"), binding.WithHelp("s", "defer")),
	Link:     binding.NewBinding(binding.WithKeys("L"), binding.WithHelp("L", "link")),
	Open:     binding.NewBinding(binding.WithKeys("o"), binding.WithHelp("o", "open")),
	Copy:     binding.NewBinding(binding.WithKeys("y"), binding.WithHelp("y", "copy")),
	Note:     binding.NewBinding(binding.WithKeys("n"), binding.WithHelp("n", "note")),

	Save:     binding.NewBinding(binding.WithKeys("w"), binding.WithHelp("w", "save")),
	Editor:   binding.NewBinding(binding.WithKeys("ctrl+e"), binding.WithHelp("^e", "editor")),
	Undo:     binding.NewBinding(binding.WithKeys("U"), binding.WithHelp("U", "undo")),
	Redo:     binding.NewBinding(binding.WithKeys("ctrl+r"), binding.WithHelp("^r", "redo")),
	Filter:   binding.NewBinding(binding.WithKeys("/"), binding.WithHelp("/", "filter")),
	Settings: binding.NewBinding(binding.WithKeys(","), binding.WithHelp(",", "settings")),
	Help:     binding.NewBinding(binding.WithKeys("?"), binding.WithHelp("?", "help")),
	Back:     binding.NewBinding(binding.WithKeys("esc"), binding.WithHelp("esc", "back")),
	Quit:     binding.NewBinding(binding.WithKeys("q"), binding.WithHelp("q", "quit")),
}

// keyEntry is one footer-grid cell: a binding's key hint beside desc, or the
// binding's own description when desc is empty.
type keyEntry struct {
	b    binding.Binding
	desc string
}

func (e keyEntry) label() string { return e.b.Help().Key }

func (e keyEntry) text() string {
	if e.desc != "" {
		return e.desc
	}
	return e.b.Help().Desc
}

// entries wraps bindings as grid cells with their own descriptions.
func entries(bs ...binding.Binding) []keyEntry {
	out := make([]keyEntry, len(bs))
	for i, b := range bs {
		out[i] = keyEntry{b: b}
	}
	return out
}

// keySet is the set of hint labels for bs, for dimming inert grid cells.
func keySet(bs ...binding.Binding) map[string]bool {
	out := make(map[string]bool, len(bs))
	for _, b := range bs {
		out[b.Help().Key] = true
	}
	return out
}
