package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"shepherd/internal/store"
)

// handoffBoard points shepherd at a temp board and config, with the given
// config.toml, and stubs the command runner to record argv instead of running.
func handoffBoard(t *testing.T, config string) (string, *[]string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.md")
	t.Setenv("SHEPHERD_TODO_FILE", path)
	t.Setenv("SHEPHERD_CONFIG", filepath.Join(dir, "config.toml"))
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	var ran []string
	orig := runHandoff
	runHandoff = func(c *exec.Cmd) error { ran = c.Args; return nil }
	t.Cleanup(func() { runHandoff = orig })
	return path, &ran
}

func TestHandoffRunsTheConfiguredCommand(t *testing.T) {
	path, ran := handoffBoard(t, `handoff = ["sh", "-c", "true", "sh", "{prompt}", "{id}"]`+"\n")
	cmdAdd([]string{"ship it note:after the freeze"}, "", &bytes.Buffer{})
	cmdSub([]string{"1", "write notes"}, "", &bytes.Buffer{})
	items := store.Load(path)

	if code := cmdHandoff([]string{"1.1"}, "", &bytes.Buffer{}); code != 0 {
		t.Fatalf("handoff exit %d", code)
	}
	sub := store.Load(path)[0].Subs[0]
	want := []string{"sh", "-c", "true", "sh", "write notes\n\nThis is one part of: ship it\n\nafter the freeze", items[0].Subs[0].ID}
	if !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %q\nwant %q", *ran, want)
	}
	if sub.Status != "in-progress" {
		t.Fatalf("subtask status = %q, want in-progress", sub.Status)
	}
}

// An unset or missing command is a usage error that leaves the item as it was
// and runs nothing.
func TestHandoffFailsBeforeTouchingTheItem(t *testing.T) {
	for name, config := range map[string]string{
		"unset":   "view = \"category\"\n",
		"missing": `handoff = ["no-such-agent-xyz", "{prompt}"]` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			path, ran := handoffBoard(t, config)
			cmdAdd([]string{"ship it"}, "", &bytes.Buffer{})
			if code := cmdHandoff([]string{"1"}, "", &bytes.Buffer{}); code != 2 {
				t.Fatalf("exit %d, want 2", code)
			}
			if it := store.Load(path)[0]; it.Status != "" {
				t.Fatalf("status changed to %q", it.Status)
			}
			if *ran != nil {
				t.Fatalf("ran %q", *ran)
			}
		})
	}
	_, _ = handoffBoard(t, "")
	if code := cmdHandoff(nil, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("no ref: exit %d, want 2", code)
	}
}

func TestBranchToken(t *testing.T) {
	path, _ := handoffBoard(t, "")
	cmdAdd([]string{"ship it branch:todo/ship-it"}, "", &bytes.Buffer{})
	if it := store.Load(path)[0]; it.Branch != "todo/ship-it" || it.Text != "ship it" {
		t.Fatalf("branch not parsed: %+v", it)
	}
	var out bytes.Buffer
	cmdList([]string{"--json", "--filter", "todo/ship"}, "", &out)
	var got []itemJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 1 || got[0].Branch != "todo/ship-it" {
		t.Fatalf("list --json = %s (%v)", out.String(), err)
	}
	cmdEdit([]string{"1", "branch:"}, "", &bytes.Buffer{})
	if it := store.Load(path)[0]; it.Branch != "" {
		t.Fatalf("bare branch: didn't clear: %+v", it)
	}
}
