package todo

import "testing"

func TestCascade(t *testing.T) {
	mk := func() *Item { return &Item{Text: "p", Subs: []Item{{Text: "a"}, {Text: "b"}}} }

	// complete parent -> all subs done
	p := mk()
	SetParentDone(p, true)
	if !p.Done || !p.Subs[0].Done || !p.Subs[1].Done {
		t.Fatalf("complete parent didn't cascade: %+v", *p)
	}
	// reopen parent -> all subs reopened
	SetParentDone(p, false)
	if p.Done || p.Subs[0].Done || p.Subs[1].Done {
		t.Fatalf("reopen parent didn't cascade: %+v", *p)
	}

	// check last sub -> parent done; uncheck one -> parent reopens
	p = mk()
	SetSubDone(p, 0, true)
	if p.Done {
		t.Fatalf("parent done with one sub open: %+v", *p)
	}
	SetSubDone(p, 1, true)
	if !p.Done || p.Completed == "" {
		t.Fatalf("all subs done should complete parent: %+v", *p)
	}
	SetSubDone(p, 0, false)
	if p.Done || p.Completed != "" {
		t.Fatalf("unchecking a sub should reopen parent: %+v", *p)
	}

	// out-of-range index is a no-op
	SetSubDone(p, 5, true)
	if p.Subs[0].Done {
		t.Fatalf("out-of-range SetSubDone mutated state")
	}

	// subtask-less parent behaves like a plain task
	plain := &Item{Text: "x"}
	SetParentDone(plain, true)
	if !plain.Done {
		t.Fatalf("plain task not marked done")
	}
	if AllSubsDone(plain) {
		t.Fatalf("subtask-less item reported all-subs-done")
	}
}

func TestSubStatus(t *testing.T) {
	statuses := []string{"open", "in-progress", "done"}
	p := &Item{Text: "p", Subs: []Item{{Text: "a"}, {Text: "b"}}}

	// an intermediate status leaves the sub open and the parent open
	SetSubStatus(p, 0, "in-progress")
	if p.Subs[0].Done || p.Subs[0].Status != "in-progress" || p.Done {
		t.Fatalf("in-progress sub wrong: %+v", *p)
	}
	// status "done" on the last open sub completes it; all done -> parent done
	SetSubStatus(p, 0, "done")
	SetSubStatus(p, 1, "done")
	if !p.Subs[0].Done || !p.Done {
		t.Fatalf("done sub status should cascade to parent: %+v", *p)
	}
	// cycling a sub back off done reopens it and the parent
	CycleSubStatus(p, 1, statuses) // done -> open (wraps)
	if p.Subs[1].Done || p.Done {
		t.Fatalf("cycling sub off done should reopen parent: %+v", *p)
	}
}

func TestClone(t *testing.T) {
	src := []Item{{Text: "p", Subs: []Item{{Text: "a"}}}}
	cp := Clone(src)
	cp[0].Subs[0].Done = true
	if src[0].Subs[0].Done {
		t.Fatalf("Clone shares the Subs slice")
	}
}
