package agent

import (
	"strings"
	"testing"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Add retry to API client!":   "add-retry-to-api-client",
		"  --weird__spacing--  ":     "weird-spacing",
		"":                           "item",
		"!!!":                        "item",
		strings.Repeat("abcde ", 10): "abcde-abcde-abcde-abcde-abcde-abcde-abcd",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrompt(t *testing.T) {
	if got := (Task{Item: todo.Item{Text: "fix it"}}).Prompt(); got != "fix it" {
		t.Errorf("no note: %q", got)
	}
	if got := (Task{Item: todo.Item{Text: "fix it", Note: "carefully"}}).Prompt(); got != "fix it\n\ncarefully" {
		t.Errorf("with note: %q", got)
	}
	parent := todo.Item{Text: "Webhooks", Note: "Stripe only"}
	sub := Task{Item: todo.Item{Text: "add tests"}, Parent: &parent}
	if got, want := sub.Prompt(), "add tests\n\nThis is one part of: Webhooks\n\nStripe only"; got != want {
		t.Errorf("subtask: %q, want %q", got, want)
	}
}

func TestTaskName(t *testing.T) {
	if got := (Task{Item: todo.Item{Text: "Add tests"}}).Name(); got != "add-tests" {
		t.Errorf("item: %q", got)
	}
	parent := todo.Item{Text: "Wire the payment provider webhooks"}
	sub := Task{Item: todo.Item{Text: "Add tests"}, Parent: &parent}
	if got := sub.Name(); got != "wire-the-payment-pro-add-tests" {
		t.Errorf("subtask: %q", got)
	}
}

func TestRepoDirDefaultBoard(t *testing.T) {
	// the default board's dir is stored under "default"; board "" must find it
	t.Setenv("SHEPHERD_TODO_FILE", t.TempDir()+"/todo.md")
	want := t.TempDir()
	if err := store.SetBoardDir("default", want); err != nil {
		t.Fatal(err)
	}
	var asked string
	prev := Run
	t.Cleanup(func() { Run = prev })
	Run = func(dir, name string, args ...string) ([]byte, error) {
		asked = dir
		return []byte("/repo\n"), nil
	}
	if _, err := RepoDir(""); err != nil {
		t.Fatal(err)
	}
	if asked != want {
		t.Fatalf("RepoDir(\"\") asked git in %q, want the default board's dir %q", asked, want)
	}
}
