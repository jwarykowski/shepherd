package drover

import (
	"encoding/json"
	"errors"
	"testing"

	"shepherd/internal/drover/drovertest"
	"shepherd/internal/todo"
)

func TestSendReleasesAHeldRun(t *testing.T) {
	calls := drovertest.Fake(t, `[{"id":"held1","status":"hold","subject":"item1"}]`)
	runs, err := Send(Task{Item: todo.Item{ID: "item1", Text: "fix login"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "held1" {
		t.Fatalf("runs = %+v", runs)
	}
	if got := calls(); got[len(got)-1] != "go --subject item1 --json" {
		t.Fatalf("calls = %q", got)
	}
}

func TestSendStartsARunWithTheItemsData(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	t.Setenv("SHEPHERD_CONFIG", t.TempDir()+"/config.toml") // no board dirs
	it := todo.Item{ID: "item1", Text: "backfill old orders", Note: "batch it\n10k rows", Link: "https://x/1"}
	parent := todo.Item{ID: "p1", Text: "move checkout to Postgres"}
	runs, err := Send(Task{Item: it, Parent: &parent, Board: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "new1" {
		t.Fatalf("runs = %+v", runs)
	}
	got := calls()
	if last := got[len(got)-1]; last != "run --subject item1 --on shepherd.added --data-json --json" {
		t.Fatalf("call %q: data belongs on stdin, not in argv", last)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(drovertest.Stdin()), &data); err != nil {
		t.Fatalf("stdin %q: %v", drovertest.Stdin(), err)
	}
	want := map[string]string{"title": "backfill old orders", "board": "web", "note": "batch it\n10k rows",
		"url": "https://x/1", "parent": "move checkout to Postgres", "item_id": "item1"}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%s] = %q, want %q", k, data[k], v)
		}
	}
}

func TestStatusLogAndArchive(t *testing.T) {
	calls := drovertest.Fake(t, `[]`)
	if !Watching() {
		t.Fatal("fake drover says it's watching")
	}
	t.Setenv("DROVER_FAKE_WATCHING", "false")
	if Watching() {
		t.Fatal("should report not watching")
	}
	rows, err := Log("r1")
	if err != nil || len(rows) != 2 || rows[1].Kind != "result" {
		t.Fatalf("log = %+v, %v", rows, err)
	}
	if err := Archive("r1", "r2"); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got[len(got)-1] != "archive r1 r2" {
		t.Fatalf("calls = %q", got)
	}
}

func TestRunStatesAndOrder(t *testing.T) {
	drovertest.Fake(t, `[
		{"id":"old","status":"","done":true,"subject":"s1","completed":"2026-10-01T10:00:00Z"},
		{"id":"new","status":"queued","subject":"s1","created":"2026-10-12T10:00:00Z"}
	]`)
	runs, err := Runs()
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].ID != "new" || runs[0].State() != "queued" || runs[1].State() != "done" {
		t.Fatalf("runs = %+v", runs)
	}
	if BySubject(runs)["s1"].ID != "new" {
		t.Fatal("a queued run says more than a finished one")
	}
}

func TestBySubjectPrefersActiveRuns(t *testing.T) {
	m := BySubject([]Run{
		{ID: "a", Subject: "s1", Status: "running"},
		{ID: "b", Subject: "s1", Status: "hold"},
		{ID: "c", Subject: "s2", Status: "hold"},
		{ID: "d", Subject: "s3", Status: "go", Done: true},
		{ID: "e", Subject: "s4", Status: "blocked"},
	})
	if m["s1"].ID != "a" || !m["s2"].Waiting() || !m["s4"].Failed() || m["s3"].State() != "done" || len(m) != 4 {
		t.Fatalf("by subject = %+v", m)
	}
}

func TestMissingDrover(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if Installed() {
		t.Fatal("drover found on an empty PATH")
	}
	if _, err := Runs(); !errors.Is(err, ErrMissing) {
		t.Fatalf("err = %v", err)
	}
}
