package tui

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	binding "charm.land/bubbles/v2/key"
)

// keyMap is the board's bindings: the list, global and detail handlers dispatch
// on them and the footer grids render their help, so a key and its hint can't
// drift apart. A key like "g b" is a two-key sequence (see chordKey).
type keyMap struct {
	Up, Down, Top, Bottom, LaneLeft, LaneRight, Toggle, Detail, View, Global, ArchiveView, Boards, Footer binding.Binding
	Add, AddSub, Edit, Status, Delete, Sweep, Archive                                                     binding.Binding
	Prio, Category, Tags, Due, Defer, Link, Open, Copy, Note                                              binding.Binding
	Run, Runs, RunLog                                                                                     binding.Binding
	Save, Editor, Undo, Redo, Filter, Settings, Theme, Help, Back, Quit                                   binding.Binding
}

func bind(desc string, ks ...string) binding.Binding {
	return binding.NewBinding(binding.WithKeys(ks...), binding.WithHelp("", desc))
}

// defaultKeys is the built-in keymap; key.<action> lines in config.toml
// override it (see applyKeys).
var defaultKeys = keyMap{
	Up:          bind("move", "k", "up"),
	Down:        bind("", "j", "down"),
	Top:         bind("top/end", "g g", "home"),
	Bottom:      bind("", "G", "end"),
	LaneLeft:    bind("lane", "left"),
	LaneRight:   bind("", "right"),
	Toggle:      bind("toggle", "space"),
	Detail:      bind("detail", "enter"),
	View:        bind("view", "v"),
	Global:      bind("all boards", "g a"),
	ArchiveView: bind("archive", "g h"),
	Boards:      bind("boards", "g b"),
	Footer:      bind("footer", "g f"),

	Add:     bind("add", "a"),
	AddSub:  bind("sub", "A"),
	Edit:    bind("edit", "e"),
	Status:  bind("status", "tab"),
	Delete:  bind("del", "x"),
	Sweep:   bind("clear done", "C"),
	Archive: bind("arch", "X"),

	Prio:     bind("prio", "h", "m", "l"),
	Category: bind("cat", "c"),
	Tags:     bind("tags", "t"),
	Due:      bind("due", "d"),
	Defer:    bind("start", "s"),
	Link:     bind("link", "L"),
	Open:     bind("open", "o"),
	Copy:     bind("copy", "y"),
	Note:     bind("note", "n"),

	Run:    bind("run", "r"),
	Runs:   bind("runs", "g r"),
	RunLog: bind("run log", "g l"),

	Save:     bind("save", "w"),
	Editor:   bind("editor", "ctrl+e"),
	Undo:     bind("undo", "u"),
	Redo:     bind("redo", "ctrl+r"),
	Filter:   bind("filter", "/"),
	Settings: bind("settings", "g s", ","),
	Theme:    bind("theme", "g t"),
	Help:     bind("help", "?"),
	Back:     bind("back", "esc"),
	Quit:     bind("quit", "q"),
}

var keys = withLabels(defaultKeys)

// reservedKeys can't be rebound: they back out and quit from every screen.
var reservedKeys = map[string]bool{"esc": true, "q": true, "ctrl+c": true}

// chord is a completed two-key sequence ("g b"), matched like a key press.
type chord string

func (c chord) String() string { return string(c) }

// bindings lists km's bindings with their config names (key.<name>), in
// declaration order.
func (km *keyMap) bindings() []namedBinding {
	v := reflect.ValueOf(km).Elem()
	out := make([]namedBinding, v.NumField())
	for i := range out {
		out[i] = namedBinding{snake(v.Type().Field(i).Name), v.Field(i).Addr().Interface().(*binding.Binding)}
	}
	return out
}

// byName is the binding configured as key.<name>.
func (km keyMap) byName(name string) *binding.Binding {
	for _, nb := range km.bindings() {
		if nb.name == name {
			return nb.b
		}
	}
	return nil
}

type namedBinding struct {
	name string
	b    *binding.Binding
}

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// prettyKey is a key as the footer shows it: "g b" -> "gb", "ctrl+e" -> "^e".
func prettyKey(k string) string {
	switch k {
	case "left":
		return "←"
	case "right":
		return "→"
	case "up":
		return "↑"
	case "down":
		return "↓"
	}
	k = strings.ReplaceAll(k, " ", "")
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return "^" + rest
	}
	return k
}

// withLabels sets each binding's help key from its first key; pairs shown as
// one hint (j/k, ←/→, gg/G) and the three priority keys read together.
func withLabels(km keyMap) keyMap {
	first := func(b binding.Binding) string { return prettyKey(b.Keys()[0]) }
	for _, nb := range km.bindings() {
		nb.b.SetHelp(first(*nb.b), nb.b.Help().Desc)
	}
	km.Up.SetHelp(first(km.Down)+"/"+first(km.Up), km.Up.Help().Desc)
	km.Top.SetHelp(first(km.Top)+"/"+first(km.Bottom), km.Top.Help().Desc)
	km.LaneLeft.SetHelp(first(km.LaneLeft)+"/"+first(km.LaneRight), km.LaneLeft.Help().Desc)
	ps := make([]string, len(km.Prio.Keys()))
	for i, k := range km.Prio.Keys() {
		ps[i] = prettyKey(k)
	}
	km.Prio.SetHelp(strings.Join(ps, "/"), km.Prio.Help().Desc)
	return km
}

// clash describes a key in km bound to two actions, or a single key that is
// also the first key of a sequence (making the sequence unreachable), and
// names the two actions; "" when there is none.
func (km *keyMap) clash() (msg, a, b string) {
	owner := map[string]string{}
	for _, nb := range km.bindings() {
		for _, k := range nb.b.Keys() {
			if prev, ok := owner[k]; ok {
				return fmt.Sprintf("%q is bound to both %s and %s", k, prev, nb.name), prev, nb.name
			}
			owner[k] = nb.name
		}
	}
	for _, nb := range km.bindings() {
		for _, k := range nb.b.Keys() {
			if head, _, ok := strings.Cut(k, " "); ok {
				if other, single := owner[head]; single {
					return fmt.Sprintf("%q (%s) blocks the sequence %q (%s)", head, other, k, nb.name), other, nb.name
				}
			}
		}
	}
	return "", "", ""
}

// applyKeys rebuilds keys from the defaults plus key.<action> overrides from
// config.toml, and returns a notice for each override it had to ignore.
func applyKeys(kv map[string]string) []string {
	km := defaultKeys
	byName := map[string]*binding.Binding{}
	for _, nb := range km.bindings() {
		byName[nb.name] = nb.b
	}
	names := make([]string, 0, len(kv))
	for k := range kv {
		if name, ok := strings.CutPrefix(k, "key."); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var notes []string
	set := map[string]bool{}
	for _, name := range names {
		b, ok := byName[name]
		var ks []string
		raw := strings.Trim(kv["key."+name], `"`)
		if strings.TrimSpace(raw) == "," { // a lone comma is the key, not a separator
			raw = "comma"
		}
		for part := range strings.SplitSeq(raw, ",") {
			if k := strings.Join(strings.Fields(part), " "); k != "" {
				ks = append(ks, strings.ReplaceAll(k, "comma", ","))
			}
		}
		reason := ""
		switch {
		case !ok:
			reason = "unknown action"
		case name == "back" || name == "quit":
			reason = "esc and q can't be rebound"
		case len(ks) == 0:
			reason = "no keys"
		case name == "prio" && len(ks) != 3:
			reason = "prio takes three keys: high, medium, low"
		}
		for _, k := range ks {
			if reservedKeys[k] || reservedKeys[strings.Fields(k)[0]] || len(strings.Fields(k)) > 2 {
				reason = fmt.Sprintf("%q can't be used", k)
			}
		}
		if reason == "" {
			b.SetKeys(ks...)
			set[name] = true
		} else {
			notes = append(notes, fmt.Sprintf("key.%s ignored: %s", name, reason))
		}
	}
	// Checked once all are set, so two actions can swap keys. The defaults
	// never clash, so each pass reverts at least one override.
	for {
		msg, a, b := km.clash()
		if msg == "" {
			break
		}
		for _, name := range []string{a, b} {
			if set[name] {
				byName[name].SetKeys(defaultKeys.byName(name).Keys()...)
				delete(set, name)
				notes = append(notes, fmt.Sprintf("key.%s ignored: %s", name, msg))
			}
		}
	}
	keys = withLabels(km)
	return notes
}

// chordPrefix reports whether s starts a key sequence in the keymap.
func chordPrefix(s string) bool {
	for _, nb := range keys.bindings() {
		for _, k := range nb.b.Keys() {
			if strings.HasPrefix(k, s+" ") {
				return true
			}
		}
	}
	return false
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
