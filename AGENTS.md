# Agent guide

This machine has `shepherd`, a CLI todo board backed by a markdown file. Manage
the user's todos through it — never hand-edit the todo file; the binary owns the
format.

- `shepherd list --json` — read all items (machine-readable; prefer this)
- `shepherd list --all --json` — read across every board; adds a `project` field per item
- `shepherd projects [--json] [--archived]` — list boards with done/total counts (`--archived` lists archived boards instead); JSON marks the current board with `"current": true`
- `shepherd project rename <old> <new>` / `archive <name>` / `unarchive <name>` / `delete <name> --force [--dry-run]` — whole-board actions (default board is not renamable/deletable/archivable; archive stashes under `projects/archived/`)
- `shepherd stats [--json] [--all] [--legend]` — board metrics (charts, or `--json` numbers; `--legend` explains each chart)
- `shepherd add "buy milk @home !h due:tomorrow"` — add an item
- `shepherd sub <n> "<text>"` — add a subtask to item n (same quick-add tokens)
- `shepherd edit <n[.m]> "<tokens>"` — merge tokens onto item n (or subtask m); only the given fields change. Tokens: `@category`, `!prio`, `due:`, `defer:`, `link:`, `status:`, `note:`, and text. A bare key clears its field; `note:` takes the rest of the line
- `shepherd list --filter <q>` — list only matching items (text/note/category/due/defer/link), keeping their real indexes for done/rm
- `shepherd done <n[.m]>` / `shepherd undone <n[.m]>` — (un)complete item n, or its subtask m
- `shepherd rm <n[.m]> [--dry-run]` — remove item n, or just its subtask m (`--dry-run`/`-n` previews without writing)

`edit` is the single setter for every field — status, note, category, priority,
due, defer, link, and text all change through its tokens (`edit 2 "status:in-progress"`,
`edit 2 "note:call the vendor"`); a bare `key:` clears. `done`/`undone` are the
only shorthands, for the terminal state.

Indexes are 1-based and match `list` order. Quick-add tokens (shared by `add`,
`sub`, `edit`): `@category`, `!h`/`!m`/`!l` priority,
`due:<today|tomorrow|+3d|15-07-2026>`, `defer:<same date forms>` (start/defer
date), `link:<url>`, `status:<name>`, and `note:<text>` (holds spaces, takes the
rest of the line — put it last). `list --json` reports `completed` (done
timestamp), `defer`, `link`, and `status` per item.

Subtasks nest one level under an item. `list --json` puts them in each item's
`subtasks` array (1-based within the parent); address them as `n.m`. Completion
cascades both ways: completing a parent completes all its subtasks, and
completing the last subtask completes the parent. Stats count top-level items
only — subtasks are decomposition, not separate board work.

Items have a status: `done` is terminal; between open and done there can be
named intermediate statuses (e.g. `in-progress`), configured as an ordered
`statuses` list in `config.toml` (`done` always last). `list --json`'s `status`
field is empty for a plain open or done item, else the named status. Set it with
`shepherd edit <n> "status:<name>"` (any name accepted; `status:done`/`status:open`
recognised as the terminal/default ends, and `done`/`undone` are shorthands for
them); `tab` cycles the configured list in the interactive board.

Boards are per-project: add `--project <name>` after the verb to target a
project's board (`shepherd list --project web`, `shepherd add "…" --project web`,
`shepherd done 2 --project web`), else the default board is used.

`shepherd list --all` reads across every board and is read-only; its indexes are
aggregate and **not** valid for `done`/`rm`. To act on an item seen via `--all`,
re-list that board (`list --project <name>`) and use its index with the same
`--project`.

Exit codes for scripting: `0` success, `2` usage/input error (bad flag, unknown
command, out-of-range index), `1` runtime/IO failure. `-q`/`--quiet` on a
mutation drops its confirmation line but never the requested data. Run
`shepherd help` for the full contract.
