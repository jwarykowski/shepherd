---
name: shepherd
description: >
  Read and manage the user's shepherd todo board from the command line. Use
  whenever the user mentions their todos, to-do list, task board, or
  "shepherd" — e.g. "what's on my todo list", "add a todo to…", "add a task",
  "mark that done", "what's overdue", "clear my done items". Shepherd is a
  local CLI backed by a markdown file; manage it with the `shepherd` command,
  never by hand-editing the file.
---

# Shepherd todo board

Shepherd is an installed CLI (`shepherd`) backed by a markdown file. Manage it
through the command API — the binary owns the format, so never hand-edit the
todo file.

Run `shepherd help` for the authoritative command list. Summary:

| Command | Does |
| --- | --- |
| `shepherd list --json` | read all items (machine shape — prefer this) |
| `shepherd list --all --json` | read across every board; adds a `project` field |
| `shepherd add "<text>"` | add an item |
| `shepherd done <n>` / `undone <n>` | (un)complete item n |
| `shepherd rm <n>` | remove item n |

Indexes are 1-based and match `list` order. Read with `--json`, act by index.

## Projects

Each project has its own board: `--project <name>` (or `$SHEPHERD_PROJECT`)
targets `~/.config/shepherd/projects/<name>.md`; with no project you're on the
default board. Flags follow the verb — `shepherd list --project web`,
`shepherd add "…" --project web`, `shepherd done 2 --project web`.

`shepherd list --all` reads across every board and is **read-only**; its
indexes are aggregate, **not** valid for `done`/`rm`. To act on an item you
found via `--all`, re-list that board (`list --project <name> --json`) and use
*that* board's index, mutating with the same `--project`.

## Adding

`add` accepts quick-add tokens in the text:
`shepherd add "renew passport @home !h due:+2w defer:1w link:https://gov.uk"`

- `@category` · `!h`/`!m`/`!l` priority · `due:<today|tomorrow|+3d|15-07-2026>`
- `defer:<same date forms>` — start/defer date (item shown but not "started"
  until then) · `link:<url>` — a reference URL

`list --json` includes `completed` (timestamp set when an item is marked done),
`defer`, and `link` per item.

## Notes

- Data file: `~/.config/shepherd/todo.md` by default, or
  `~/.config/shepherd/projects/<name>.md` when a project is selected with
  `--project <name>` (or `$SHEPHERD_PROJECT`). Flags follow the verb, e.g.
  `shepherd list --project web`. Override the exact file with
  `$SHEPHERD_TODO_FILE`. Dates stored ISO.
- If `shepherd` isn't found, it isn't installed —
  `brew install jwarykowski/tap/shepherd`.
- An open board picks up your changes within ~2s.
