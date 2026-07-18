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
| `shepherd list --filter <q> [--json]` | list only matching items; real indexes kept |
| `shepherd projects [--json] [--archived]` | list boards with done/total counts (`--archived` lists archived boards); JSON marks the current board (`"current": true`) |
| `shepherd project rename\|delete\|archive\|unarchive <name>` | whole-board actions (delete needs `--force`, `--dry-run` previews; archive stashes under `projects/archived/`); default board is not actionable |
| `shepherd stats --json [--all]` | board metrics (JSON numbers; drop `--json` for charts; `--legend` explains them) |
| `shepherd add "<text>" [--json]` | add an item |
| `shepherd sub <ref> "<text>" [--json]` | add a subtask to an item |
| `shepherd edit <ref> "<tokens>" [--json]` | the single setter — merge @category/!prio/due:/defer:/link:/status:/note:/text onto an item (or subtask); bare key clears, note: takes the rest |
| `shepherd done <ref>... [--json]` / `undone <ref>...` | (un)complete one or more items/subtasks (shorthand for `edit … status:done`/`status:open`) |
| `shepherd rm <ref>... [--dry-run] [--json]` | remove one or more items/subtasks (`--dry-run`/`-n` previews) |

A `<ref>` is either an item's **stable id** (the `id` field from `list --json`)
or its 1-based index. **Prefer the id**: the index shifts whenever the board
reorders (a new high-priority task, a due date crossing into overdue), so an
index read in one call can point at a different item by the next — the id never
does. The index stays as a human convenience. `done`/`undone`/`rm` take several
refs at once and apply them as one atomic write.

Mutating verbs are safe to repeat: re-marking a done item keeps its original
completion stamp, so a retried call after a timeout won't corrupt state. They're
also safe to run concurrently — each mutation serialises under a board lock, so
parallel agents never lose one another's writes.

`--json` on a mutating verb echoes the resulting item(s) in the same shape as
`list --json` (so you needn't re-list to confirm) and reports failures as a
`{"error":…}` object on stdout instead of text on stderr.

Exit codes: `0` success · `2` usage/input error (bad flag, unknown command,
unknown ref) · `1` runtime/IO failure. `-q`/`--quiet` drops a mutation's
confirmation line, never the requested data.

## Subtasks

Items can hold one level of subtasks. `shepherd sub <ref> "<text>"` adds one
(same quick-add tokens as `add`). Address a subtask by its own id, or as `n.m`
(subtask `m` of item `n`), in `done`/`undone`/`rm`. Completion cascades both ways: completing a
parent completes its subtasks, and completing the last subtask completes the
parent. `list --json` nests them under each item's `subtasks` array (each with
a 1-based `index` within the parent). `stats` counts top-level items only.

## Projects

Each project has its own board: `--project <name>` (or `$SHEPHERD_PROJECT`)
targets `~/.config/shepherd/projects/<name>.md`; with no project you're on the
default board. Flags follow the verb — `shepherd list --project web`,
`shepherd add "…" --project web`, `shepherd done 2 --project web`.

`shepherd list --all` reads across every board and is **read-only**; its
indexes are aggregate, **not** valid for `done`/`rm`. To act on an item you
found via `--all`, mutate with the same `--project` as its board — the item's
`id` works directly, or re-list that board for its local index.

## Adding

`add` accepts quick-add tokens in the text:
`shepherd add "renew passport @home !h due:+2w defer:1w link:https://gov.uk"`

- `@category` · `!h`/`!m`/`!l` priority · `due:<today|tomorrow|+3d|15-07-2026>`
- `defer:<same date forms>` — start/defer date (item shown but not "started"
  until then) · `link:<url>` — a reference URL
- `status:<name>` — set a status · `note:<text>` — a note (holds spaces, takes
  the rest of the line, so put it last)

`list --json` includes `id` (the stable handle — use it to address the item),
`completed` (timestamp set when an item is marked done), `defer`, `link`, and
`status` per item.

## Statuses

Items carry a status. `done` is terminal (`done`/`undone`, or `[x]` on disk).
Between open and done there can be named intermediate statuses (e.g.
`in-progress`), configured as an ordered `statuses` list in `config.toml` with
`done` always last. `list --json` reports the current one in the `status` field
— empty when the item is plain open or done, else the named status (e.g.
`"in-progress"`). On disk an intermediate status is a `status:` line under the
item; open and done items carry none.

Set a status with `shepherd edit <n> "status:<name>"` — any name is accepted
(like a free-form `@category`); `status:done` marks the item done and
`status:open` clears it back to plain open. `done`/`undone` are shorthands for
the two terminal ends. In the interactive board, `tab` cycles through the
configured list.

## Notes

- Data file: `todo.md` under `$XDG_CONFIG_HOME/shepherd/` (defaults to
  `~/.config/shepherd/`), or `projects/<name>.md` there when a project is
  selected with `--project <name>` (or `$SHEPHERD_PROJECT`). Flags follow the
  verb, e.g. `shepherd list --project web`. Override the exact file with
  `$SHEPHERD_TODO_FILE`. Dates stored ISO.
- If `shepherd` isn't found, it isn't installed —
  `brew install jwarykowski/tap/shepherd`.
- An open board picks up your changes within ~2s.
