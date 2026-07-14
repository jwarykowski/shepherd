package todo

import "testing"

func TestCycleStatus(t *testing.T) {
	statuses := []string{"open", "in-progress", "done"}

	it := &Item{}
	CycleStatus(it, statuses) // open -> in-progress
	if it.Done || it.Status != "in-progress" {
		t.Fatalf("open->in-progress wrong: %+v", *it)
	}
	CycleStatus(it, statuses) // in-progress -> done
	if !it.Done || it.Status != "" || it.Completed == "" {
		t.Fatalf("in-progress->done wrong: %+v", *it)
	}
	CycleStatus(it, statuses) // done -> open (wrap, reopen)
	if it.Done || it.Status != "" || it.Completed != "" {
		t.Fatalf("done->open wrap wrong: %+v", *it)
	}
}

func TestCycleStatusTwoState(t *testing.T) {
	statuses := []string{"open", "done"} // default config, must behave like a toggle
	it := &Item{}
	CycleStatus(it, statuses)
	if !it.Done {
		t.Fatalf("open->done wrong: %+v", *it)
	}
	CycleStatus(it, statuses)
	if it.Done || it.Status != "" {
		t.Fatalf("done->open wrong: %+v", *it)
	}
}

func TestSetStatus(t *testing.T) {
	it := &Item{}
	SetStatus(it, "in-progress")
	if it.Done || it.Status != "in-progress" {
		t.Fatalf("set in-progress wrong: %+v", *it)
	}
	SetStatus(it, "done")
	if !it.Done || it.Status != "" || it.Completed == "" {
		t.Fatalf("set done wrong: %+v", *it)
	}
	SetStatus(it, "open")
	if it.Done || it.Status != "" || it.Completed != "" {
		t.Fatalf("set open wrong: %+v", *it)
	}
}

func TestCycleStatusEmpty(t *testing.T) {
	it := &Item{Status: "x"}
	CycleStatus(it, nil) // no-op, must not panic
	if it.Status != "x" {
		t.Fatalf("empty statuses should be a no-op: %+v", *it)
	}
}
