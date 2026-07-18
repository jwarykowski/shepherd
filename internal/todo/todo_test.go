package todo

import (
	"testing"
	"time"
)

func pinToday(t *testing.T, iso string) {
	t.Helper()
	Today = func() string { return iso }
	t.Cleanup(func() { Today = func() string { return time.Now().Format(dateFormat) } })
}

func TestSortStable(t *testing.T) {
	items := []Item{{Text: "a"}, {Prio: 'L', Text: "b"}, {Prio: 'H', Text: "c"}, {Prio: 'H', Text: "d"}}
	Sort(items, false)
	got := ""
	for _, it := range items {
		got += it.Text
	}
	if got != "cdba" { // H,H (stable), L, none
		t.Fatalf("want cdba, got %s", got)
	}
}

func TestSortByCategoryThenPrio(t *testing.T) {
	items := []Item{
		{Text: "a1", Category: "alpha", Prio: 'L'},
		{Text: "b1", Category: "beta", Prio: 'H'},
		{Text: "a2", Category: "alpha", Prio: 'H'},
		{Text: "u1"}, // uncategorized -> last
		{Text: "b2", Category: "beta", Prio: 'M'},
	}
	Sort(items, false)
	got := ""
	for _, it := range items {
		got += it.Text + " "
	}
	if got != "a2 a1 b1 b2 u1 " {
		t.Fatalf("category+prio order wrong: %q", got)
	}
}

func TestSortBySource(t *testing.T) {
	items := []Item{
		{Text: "w2", Source: "web", Category: "z"},
		{Text: "a1", Source: "api"},
		{Text: "w1", Source: "web", Category: "a"},
	}
	SortBySource(items)
	got := ""
	for _, it := range items {
		got += it.Text + " "
	}
	// grouped by source (api<web), category order within a source
	if got != "a1 w1 w2 " {
		t.Fatalf("source order wrong: %q", got)
	}
}

func TestSetDone(t *testing.T) {
	Now = func() string { return "10-07-2026 09:00" }
	t.Cleanup(func() { Now = func() string { return time.Now().Format(tsFormat) } })
	it := Item{Text: "x"}
	SetDone(&it, true)
	if !it.Done || it.Completed != "10-07-2026 09:00" {
		t.Fatalf("mark done: %+v", it)
	}
	SetDone(&it, false)
	if it.Done || it.Completed != "" {
		t.Fatalf("reopen should clear completed: %+v", it)
	}
}

func TestDeferred(t *testing.T) {
	pinToday(t, "2026-07-10")
	cases := []struct {
		it   Item
		want bool
	}{
		{Item{Defer: "2026-07-15"}, true},              // future
		{Item{Defer: "2026-07-10"}, false},             // today = started
		{Item{Defer: "2026-07-01"}, false},             // past
		{Item{Defer: "2026-07-15", Done: true}, false}, // done never deferred
		{Item{}, false},                                // no defer date
	}
	for _, c := range cases {
		if got := Deferred(c.it); got != c.want {
			t.Fatalf("Deferred(%+v) = %v, want %v", c.it, got, c.want)
		}
	}
}

func TestDeferLabel(t *testing.T) {
	pinToday(t, "2026-07-10")
	if got := DeferLabel("2026-07-13"); got != "starts 3d" {
		t.Fatalf("future label = %q, want starts 3d", got)
	}
	if got := DeferLabel("2026-07-10"); got != "" {
		t.Fatalf("today label = %q, want empty", got)
	}
	if got := DeferLabel("garbage"); got != "" {
		t.Fatalf("unparseable label = %q, want empty", got)
	}
}

func TestQuickAddDeferLink(t *testing.T) {
	pinToday(t, "2026-07-10")
	it := ParseQuickAdd("read spec defer:3d link:https://ex.com/pr/1")
	if it.Text != "read spec" || it.Defer != "2026-07-13" || it.Link != "https://ex.com/pr/1" {
		t.Fatalf("defer/link quick-add wrong: %+v", it)
	}
}

func TestQuickAddAgentic(t *testing.T) {
	it := ParseQuickAdd("deploy release agentic status:hold")
	if it.Text != "deploy release" || !it.Agentic || it.Status != "hold" {
		t.Fatalf("agentic quick-add wrong: %+v", it)
	}
	// The flag is reversible and leaves other fields alone.
	ApplyEdit(&it, "agentic:false")
	if it.Agentic || it.Status != "hold" || it.Text != "deploy release" {
		t.Fatalf("agentic:false should clear only the flag: %+v", it)
	}
}

func TestSortByPriorityView(t *testing.T) {
	items := []Item{
		{Text: "a", Category: "z", Prio: 'L'},
		{Text: "b", Category: "a", Prio: 'H'},
	}
	Sort(items, true) // priority first: H before L regardless of category
	if items[0].Text != "b" {
		t.Fatalf("priority-view sort wrong: %+v", items)
	}
}

func TestDueSort(t *testing.T) {
	items := []Item{
		{Text: "late", Due: "2026-07-20"},
		{Text: "none"},
		{Text: "soon", Due: "2026-07-11"},
	}
	Sort(items, false) // same cat+prio: soonest due first, no-due last
	if got := items[0].Text + items[1].Text + items[2].Text; got != "soonlatenone" {
		t.Fatalf("due sort wrong: %q", got)
	}
}

func TestOverduePinSort(t *testing.T) {
	pinToday(t, "2026-07-10")
	items := []Item{
		{Text: "future", Category: "work", Due: "2026-08-01"},
		{Text: "late", Category: "home", Due: "2026-07-01"}, // overdue
		{Text: "plain", Category: "work"},
	}
	Sort(items, false)
	if items[0].Text != "late" {
		t.Fatalf("overdue not pinned first: %+v", items)
	}
	if !Pinned(items[0]) || Pinned(items[2]) {
		t.Fatalf("Pinned wrong: %+v", items)
	}
}

func TestDueLabel(t *testing.T) {
	pinToday(t, "2026-07-10")
	cases := []struct {
		due     string
		want    string
		overdue bool
	}{
		{"2026-07-08", "overdue 2d", true},
		{"2026-07-10", "due today", true},
		{"2026-07-11", "due 1d", false},
		{"2026-07-13", "due 3d", false},
		{"garbage", "garbage", false},
	}
	for _, c := range cases {
		got, over := DueLabel(c.due)
		if got != c.want || over != c.overdue {
			t.Fatalf("DueLabel(%q) = (%q,%v), want (%q,%v)", c.due, got, over, c.want, c.overdue)
		}
	}
}

func TestParseDue(t *testing.T) {
	pinToday(t, "2026-07-10")
	cases := map[string]string{
		"":           "",
		"today":      "2026-07-10",
		"tomorrow":   "2026-07-11",
		"week":       "2026-07-17",
		"next month": "2026-08-10",
		"2w":         "2026-07-24", // relative, no leading +
		"+3d":        "2026-07-13",
		"1m":         "2026-08-10",
		"1y":         "2027-07-10",
		"25-12-2026": "2026-12-25", // DMY input -> ISO storage
		"2026-12-25": "2026-12-25",
		"whatever":   "", // unrecognized -> blank
	}
	for in, want := range cases {
		if got := ParseDue(in); got != want {
			t.Fatalf("ParseDue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayDate(t *testing.T) {
	if got := DisplayDate("2026-07-15"); got != "15-07-2026" {
		t.Errorf("DisplayDate ISO = %q, want 15-07-2026", got)
	}
	if got := DisplayDate("not-a-date"); got != "not-a-date" {
		t.Errorf("DisplayDate passthrough = %q", got)
	}
}

func TestQuickAdd(t *testing.T) {
	pinToday(t, "2026-07-10")
	it := ParseQuickAdd("ship the thing @work !h due:tomorrow")
	if it.Text != "ship the thing" || it.Category != "work" || it.Prio != 'H' || it.Due != "2026-07-11" {
		t.Fatalf("quick-add parse wrong: %+v", it)
	}
	it = ParseQuickAdd("buy !x milk") // unrecognized tokens stay in text
	if it.Text != "buy !x milk" {
		t.Fatalf("bad token should stay: %q", it.Text)
	}
}

func TestApplyEdit(t *testing.T) {
	pinToday(t, "2026-07-10")
	it := Item{Text: "original", Prio: 'L'}
	ApplyEdit(&it, "!h due:tomorrow @home") // token-only: text preserved
	if it.Text != "original" || it.Prio != 'H' || it.Category != "home" || it.Due != "2026-07-11" {
		t.Fatalf("token-only edit wrong: %+v", it)
	}
	ApplyEdit(&it, "new text") // plain words replace text, other fields untouched
	if it.Text != "new text" || it.Prio != 'H' || it.Category != "home" {
		t.Fatalf("text edit wrong: %+v", it)
	}
	if !Match(it, "home") || Match(it, "zzz") {
		t.Fatal("Match wrong on category")
	}

	// status: and note: tokens set their fields; note swallows the rest of the line.
	ApplyEdit(&it, "status:in-progress note:call the bank first")
	if it.Status != "in-progress" || it.Done || it.Note != "call the bank first" {
		t.Fatalf("status/note edit wrong: %+v", it)
	}
	if it.Text != "new text" {
		t.Fatalf("note: should not have altered text: %q", it.Text)
	}

	// bare key tokens clear their fields; note: at the end clears the note.
	ApplyEdit(&it, "@ ! due: defer: link: status: note:")
	if it.Category != "" || it.Prio != 0 || it.Due != "" || it.Defer != "" || it.Link != "" || it.Status != "" || it.Note != "" {
		t.Fatalf("clear tokens did not reset fields: %+v", it)
	}
	if it.Text != "new text" {
		t.Fatalf("clear-only edit should preserve text: %q", it.Text)
	}

	// status:done marks the item done (terminal), like the status verb.
	ApplyEdit(&it, "status:done")
	if !it.Done || it.Status != "" {
		t.Fatalf("status:done should mark done: %+v", it)
	}
}
