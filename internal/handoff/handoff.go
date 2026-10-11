// Package handoff passes a todo item (or one subtask) to whatever the user
// works it with: the `handoff` command in config.toml, run with the task filled
// into its arguments. shepherd doesn't know about agents, worktrees or pull
// requests; the command can start a coding agent, call drover, or run a script.
//
//	handoff = ["claude", "--", "{prompt}"]
//
// Depends on todo and store; used by the cli and tui frontends.
package handoff

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"shepherd/internal/store"
	"shepherd/internal/todo"
)

// ErrUnset is returned when config.toml has no handoff command.
var ErrUnset = errors.New(`no handoff command: set one in config.toml, e.g. handoff = ["claude", "--", "{prompt}"]`)

// Task is the work being handed off: an item, or a subtask with its parent
// (which gives the command context), on a board ("" is the default board).
type Task struct {
	Item   todo.Item
	Parent *todo.Item
	Board  string
}

// Prompt is the task as one block of text: the item text, then its note; for
// a subtask, followed by the parent it belongs to (text and note).
func (t Task) Prompt() string {
	parts := []string{t.Item.Text}
	if t.Item.Note != "" {
		parts = append(parts, t.Item.Note)
	}
	if t.Parent != nil {
		parts = append(parts, "This is one part of: "+t.Parent.Text)
		if t.Parent.Note != "" {
			parts = append(parts, t.Parent.Note)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (t Task) board() string {
	if t.Board == "" {
		return "default"
	}
	return t.Board
}

// Command reads the handoff argv from the shared config.toml.
func Command() ([]string, error) {
	raw := store.ScanKVFile(store.ConfigPath())["handoff"]
	if strings.TrimSpace(raw) == "" {
		return nil, ErrUnset
	}
	argv, err := parseArgv(raw)
	if err != nil {
		return nil, fmt.Errorf("handoff in config.toml: %w", err)
	}
	if len(argv) == 0 {
		return nil, ErrUnset
	}
	return argv, nil
}

// parseArgv reads a one-line TOML array of strings, e.g. ["a", "b c", "d,e"].
// Unlike store.ParseList it keeps commas and spaces inside the strings, since
// they're arguments, and it unquotes escapes.
func parseArgv(raw string) ([]string, error) {
	s := strings.TrimSpace(raw)
	if i := strings.LastIndex(s, "]"); i >= 0 {
		s = s[:i+1] // drop a trailing # comment
	}
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, errors.New(`want an array of strings, e.g. ["claude", "--", "{prompt}"]`)
	}
	s = strings.TrimSpace(s[1 : len(s)-1])
	var argv []string
	for s != "" {
		q, err := strconv.QuotedPrefix(s)
		if err != nil {
			return nil, errors.New("every argument must be a double-quoted string")
		}
		arg, err := strconv.Unquote(q)
		if err != nil {
			return nil, err
		}
		argv = append(argv, arg)
		s = strings.TrimSpace(s[len(q):])
		s = strings.TrimSpace(strings.TrimPrefix(s, ","))
	}
	return argv, nil
}

// Expand fills the task into argv's placeholders: {prompt}, {text}, {note},
// {id}, {board}, {dir} (the board's working directory) and {branch}.
func Expand(argv []string, t Task) []string {
	r := strings.NewReplacer(
		"{prompt}", t.Prompt(),
		"{text}", t.Item.Text,
		"{note}", t.Item.Note,
		"{id}", t.Item.ID,
		"{board}", t.board(),
		"{dir}", store.BoardDir(t.board()),
		"{branch}", t.Item.Branch,
	)
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = r.Replace(a)
	}
	return out
}

// LookPath is exec.LookPath, a var so tests can pretend a command exists.
var LookPath = exec.LookPath

// Cmd builds the handoff command for t: argv from config.toml, expanded, run
// in the board's working directory when it has one. The task also rides in the
// environment (SHEPHERD_ITEM_ID, SHEPHERD_BOARD, SHEPHERD_BOARD_DIR,
// SHEPHERD_PROMPT) for scripts. It fails before anything runs when the
// command is unset or not installed, so callers can leave the item alone.
func Cmd(t Task) (*exec.Cmd, error) {
	argv, err := Command()
	if err != nil {
		return nil, err
	}
	argv = Expand(argv, t)
	if _, err := LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("handoff command %q not found", argv[0])
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	dir := store.BoardDir(t.board())
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SHEPHERD_ITEM_ID="+t.Item.ID,
		"SHEPHERD_BOARD="+t.board(),
		"SHEPHERD_BOARD_DIR="+dir,
		"SHEPHERD_PROMPT="+t.Prompt(),
	)
	return cmd, nil
}
