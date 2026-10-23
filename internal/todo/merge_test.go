package todo

import (
	"reflect"
	"testing"
)

func texts(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID + "=" + it.Text
	}
	return out
}

func TestMerge(t *testing.T) {
	base := []Item{{ID: "a", Text: "a"}, {ID: "b", Text: "b"}, {ID: "c", Text: "c"}, {ID: "d", Text: "d"}}
	ours := []Item{
		{ID: "a", Text: "a ours"}, // we changed a
		{ID: "b", Text: "b"},      // untouched by us; they changed it
		// we deleted c (they left it alone)
		{ID: "d", Text: "d"}, // they deleted d
		{Text: "new ours"},   // we added, no id yet
	}
	theirs := []Item{
		{ID: "a", Text: "a theirs"}, // both changed a: ours wins
		{ID: "b", Text: "b theirs"},
		{ID: "c", Text: "c"},
		{ID: "e", Text: "new theirs"},
	}
	got := texts(Merge(base, ours, theirs))
	want := []string{"a=a ours", "b=b theirs", "=new ours", "e=new theirs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestMergeKeepsTheirEditToOurDelete: a delete doesn't beat the other side's edit.
func TestMergeKeepsTheirEditToOurDelete(t *testing.T) {
	base := []Item{{ID: "a", Text: "a"}}
	got := texts(Merge(base, nil, []Item{{ID: "a", Text: "a edited"}}))
	if !reflect.DeepEqual(got, []string{"a=a edited"}) {
		t.Fatalf("got %v", got)
	}
}

// TestStatusCascades: status:done completes a parent's subs like done does, and
// a status change that leaves done untouched leaves the subs alone.
func TestStatusCascades(t *testing.T) {
	p := Item{Text: "p", Subs: []Item{{Text: "s1"}, {Text: "s2", Done: true}}}
	SetStatus(&p, "done")
	if !p.Subs[0].Done || !p.Subs[1].Done {
		t.Fatalf("status:done left subs open: %+v", p.Subs)
	}
	SetStatus(&p, "open")
	if p.Subs[0].Done || p.Subs[1].Done {
		t.Fatalf("status:open left subs done: %+v", p.Subs)
	}
	p.Subs[1].Done = true
	SetStatus(&p, "doing")
	if !p.Subs[1].Done {
		t.Fatal("an open-to-open status change reopened a sub")
	}
	CycleStatus(&p, []string{"open", "doing", "done"})
	if !p.Done || !p.Subs[0].Done {
		t.Fatalf("cycling to done didn't cascade: %+v", p)
	}
}

func TestReconcileParent(t *testing.T) {
	p := Item{Subs: []Item{{Done: true}}}
	ReconcileParent(&p)
	if !p.Done {
		t.Fatal("all subs done should complete the parent")
	}
	p.Subs = append(p.Subs, Item{})
	ReconcileParent(&p)
	if p.Done {
		t.Fatal("an open sub should reopen the parent")
	}
	solo := Item{Done: true}
	ReconcileParent(&solo)
	if !solo.Done {
		t.Fatal("a parent with no subs must be left alone")
	}
}

func TestRelativeMonthsClamp(t *testing.T) {
	cases := []struct{ today, in, want string }{
		{"2026-01-31", "month", "2026-02-28"},
		{"2026-01-31", "1m", "2026-02-28"},
		{"2024-01-31", "1m", "2024-02-29"},
		{"2024-02-29", "1y", "2025-02-28"},
		{"2026-03-31", "-1m", "2026-02-28"},
		{"2026-05-15", "2m", "2026-07-15"},
	}
	for _, c := range cases {
		pinToday(t, c.today)
		if got := ParseDue(c.in); got != c.want {
			t.Errorf("%s + %s = %s, want %s", c.today, c.in, got, c.want)
		}
	}
	if ValidDate("999999999999y") || ValidDate("1001d") {
		t.Error("an absurd relative date was accepted")
	}
}

func TestAddTagDropsCommas(t *testing.T) {
	var it Item
	AddTag(&it, "a,b")
	if !reflect.DeepEqual(it.Tags, []string{"a-b"}) {
		t.Fatalf("tags = %v", it.Tags)
	}
}

func TestStatusOfUnconfigured(t *testing.T) {
	st := []string{"open", "doing", "done"}
	if got := StatusOf(Item{Status: "blocked"}, st); got != "open" {
		t.Fatalf("StatusOf(blocked) = %q, want the first lane", got)
	}
}
