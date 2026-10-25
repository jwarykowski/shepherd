package tui

import (
	"os"
	"shepherd/internal/todo"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"

	"github.com/charmbracelet/x/ansi"
)

// TestKeymapNoClashes pins one key per action and no single key shadowing a
// sequence in the defaults.
func TestKeymapNoClashes(t *testing.T) {
	km := defaultKeys
	if msg, _, _ := km.clash(); msg != "" {
		t.Fatal(msg)
	}
}

// TestKeySequences pins that g waits for a second key, shows what can follow,
// runs the pair, and that an unknown pair or esc just cancels.
func TestKeySequences(t *testing.T) {
	m := model{input: textinput.New(), w: 80, items: []todo.Item{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	m = drive(m, "g")
	if m.pending != "g" || m.mode != modeList {
		t.Fatalf("g: pending %q mode %d", m.pending, m.mode)
	}
	if v := ansi.Strip(m.render()); !strings.Contains(v, "b boards") || !strings.Contains(v, "t theme") {
		t.Fatalf("sequence menu missing:\n%s", v)
	}
	m = drive(m, "f")
	if m.pending != "" || !m.hideFooter {
		t.Fatalf("g f: pending %q hideFooter %v", m.pending, m.hideFooter)
	}
	m = drive(m, "G")
	if m.cursor != 2 {
		t.Fatalf("G: cursor %d", m.cursor)
	}
	m = drive(m, "g", "g")
	if m.cursor != 0 {
		t.Fatalf("gg: cursor %d", m.cursor)
	}
	m = drive(m, "g", "esc", "g", "z")
	if m.pending != "" || m.mode != modeList || len(m.items) != 3 {
		t.Fatalf("cancel: pending %q mode %d", m.pending, m.mode)
	}
}

// TestKeyOverrides pins that key.<action> lines rebind keys, relabel the
// footer, and that bad overrides fall back to the default with a notice.
func TestKeyOverrides(t *testing.T) {
	t.Cleanup(func() { applyKeys(nil) })
	notes := applyKeys(map[string]string{
		"key.due":     `"D"`,
		"key.defer":   `"y"`, // swaps with copy
		"key.copy":    `"s"`,
		"key.prio":    `"1, 2, 3"`,
		"key.boards":  `"space b"`,
		"key.tags":    `"d"`,     // clashes with nothing once due moved
		"key.edit":    `"x"`,     // clashes with delete
		"key.quit":    `"Q"`,     // reserved action
		"key.add":     `"esc"`,   // reserved key
		"key.nope":    `"z"`,     // unknown action
		"key.note":    `"v"`,     // clashes with view
		"key.footer":  `"g a"`,   // clashes with global
		"key.save":    `"g w"`,   // fine: a new sequence under g
		"key.archive": `"a b c"`, // three-key sequences aren't supported
	})
	if keys.Due.Keys()[0] != "D" || keys.Due.Help().Key != "D" || keys.Prio.Help().Key != "1/2/3" || keys.Save.Help().Key != "gw" {
		t.Fatalf("overrides not applied: due %v prio %q save %q", keys.Due.Keys(), keys.Prio.Help().Key, keys.Save.Help().Key)
	}
	if keys.Defer.Keys()[0] != "y" || keys.Copy.Keys()[0] != "s" {
		t.Fatalf("swap rejected: defer %v copy %v", keys.Defer.Keys(), keys.Copy.Keys())
	}
	if keys.Tags.Keys()[0] != "d" {
		t.Fatalf("tags = %v", keys.Tags.Keys())
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"key.edit", "key.quit", "key.add", "key.nope", "key.note", "key.footer", "key.archive", "key.boards"} {
		if !strings.Contains(joined, want+" ignored") {
			t.Errorf("no notice for %s:\n%s", want, joined)
		}
	}
	if keys.Edit.Keys()[0] != "e" || keys.Boards.Keys()[0] != "g b" {
		t.Fatalf("rejected overrides not reverted: edit %v boards %v", keys.Edit.Keys(), keys.Boards.Keys())
	}

	m := model{input: textinput.New(), items: []todo.Item{{Text: "a"}}}
	m = drive(m, "2")
	if m.items[0].Prio != 'M' {
		t.Fatalf("2 should set medium priority, got %c", m.items[0].Prio)
	}
	m = drive(m, "D")
	if m.mode != modeDue {
		t.Fatalf("D should open the due editor, mode %d", m.mode)
	}
}

// TestKeyNotice pins that ignored overrides show in the footer until the next key.
func TestKeyNotice(t *testing.T) {
	t.Cleanup(func() { applyKeys(nil) })
	m := model{input: textinput.New(), w: 120, keyNotice: strings.Join(applyKeys(map[string]string{"key.nope": "z"}), "; ")}
	if !strings.Contains(ansi.Strip(m.render()), "key.nope ignored: unknown action") {
		t.Fatalf("notice not shown:\n%s", ansi.Strip(m.render()))
	}
	if m = drive(m, "j"); m.keyNotice != "" {
		t.Fatal("notice kept after a key")
	}
}

// TestClassicKeys pins that the README's old-layout block applies cleanly.
func TestClassicKeys(t *testing.T) {
	t.Cleanup(func() { applyKeys(nil) })
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, block, _ := strings.Cut(string(readme), "To keep the old layout")
	block, _, _ = strings.Cut(block[strings.Index(block, "```toml")+7:], "```")
	kv := map[string]string{}
	for ln := range strings.SplitSeq(strings.TrimSpace(block), "\n") {
		k, v, _ := strings.Cut(ln, "=")
		kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if notes := applyKeys(kv); len(kv) < 10 || len(notes) > 0 {
		t.Fatalf("classic block: %d keys, notes %v", len(kv), notes)
	}
	m := drive(model{input: textinput.New(), items: []todo.Item{{Text: "a"}}}, "d")
	if m.mode != modeDetail {
		t.Fatalf("classic d should open detail, mode %d", m.mode)
	}
}
