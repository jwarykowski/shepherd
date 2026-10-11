package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shepherd/internal/todo"
)

func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHEPHERD_TODO_FILE", filepath.Join(dir, "todo.md"))
	path := filepath.Join(dir, "config.toml")
	t.Setenv("SHEPHERD_CONFIG", path)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseArgv(t *testing.T) {
	cases := map[string][]string{
		`["claude", "--", "{prompt}"]`:      {"claude", "--", "{prompt}"},
		`["sh", "-c", "echo a, b"]`:         {"sh", "-c", "echo a, b"}, // commas stay inside an argument
		`["say", "\"quoted\""] # a comment`: {"say", `"quoted"`},
		`[]`:                                nil,
	}
	for in, want := range cases {
		got, err := parseArgv(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("parseArgv(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`claude`, `[claude]`, `["a", b]`} {
		if _, err := parseArgv(bad); err == nil {
			t.Errorf("parseArgv(%s) should fail", bad)
		}
	}
}

func TestExpandAndPrompt(t *testing.T) {
	writeConfig(t, "")
	parent := todo.Item{Text: "ship the release", Note: "after the freeze"}
	task := Task{Item: todo.Item{ID: "abc", Text: "write notes", Branch: "todo/notes"}, Parent: &parent, Board: "web"}
	got := Expand([]string{"run", "{id}", "{board}", "{branch}", "{text}", "{prompt}"}, task)
	want := []string{"run", "abc", "web", "todo/notes", "write notes", "write notes\n\nThis is one part of: ship the release\n\nafter the freeze"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Expand = %q\nwant %q", got, want)
	}
	if b := (Task{}).board(); b != "default" {
		t.Fatalf("default board name = %q", b)
	}
}

func TestCmd(t *testing.T) {
	task := Task{Item: todo.Item{ID: "abc", Text: "fix it"}}

	writeConfig(t, "view = \"category\"\n")
	if _, err := Cmd(task); !errors.Is(err, ErrUnset) {
		t.Fatalf("unset handoff: err = %v, want ErrUnset", err)
	}

	writeConfig(t, `handoff = ["no-such-agent-xyz", "{prompt}"]`+"\n")
	if _, err := Cmd(task); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing command: err = %v", err)
	}

	writeConfig(t, `handoff = ["sh", "-c", "echo $1", "sh", "{prompt}"]`+"\n")
	cmd, err := Cmd(task)
	if err != nil {
		t.Fatal(err)
	}
	if got := cmd.Args[len(cmd.Args)-1]; got != "fix it" {
		t.Fatalf("prompt arg = %q", got)
	}
	var env []string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "SHEPHERD_") && !strings.HasPrefix(kv, "SHEPHERD_CONFIG") && !strings.HasPrefix(kv, "SHEPHERD_TODO_FILE") {
			env = append(env, kv)
		}
	}
	want := []string{"SHEPHERD_ITEM_ID=abc", "SHEPHERD_BOARD=default", "SHEPHERD_BOARD_DIR=", "SHEPHERD_PROMPT=fix it"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("env = %q, want %q", env, want)
	}
}
