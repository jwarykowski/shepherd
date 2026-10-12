package drover

import (
	"errors"
	"strings"
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
	it := todo.Item{ID: "item1", Text: "fix login", Note: "see the logs", Link: "https://x/1"}
	runs, err := Send(Task{Item: it, Board: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "new1" {
		t.Fatalf("runs = %+v", runs)
	}
	got := calls()
	last := got[len(got)-1]
	for _, want := range []string{"run --subject item1 --on shepherd.added", "--data title=fix login", "--data board=web", "--data note=see the logs", "--data url=https://x/1"} {
		if !strings.Contains(last, want) {
			t.Errorf("call %q lacks %q", last, want)
		}
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
	if m["s1"].ID != "a" || !m["s2"].Waiting() || !m["s4"].Failed() || len(m) != 3 {
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
