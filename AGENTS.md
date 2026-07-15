# Agent guide

This machine has `shepherd`, a CLI todo board backed by a markdown file. Manage
the user's todos through it — never hand-edit the todo file; the binary owns the
format.

- `shepherd list --json` — read all items (machine-readable; prefer this)
- `shepherd list --all --json` — read across every board; adds a `project` field per item
- `shepherd stats [--json] [--all]` — board metrics (charts, or `--json` numbers)
- `shepherd add "buy milk @home !h due:tomorrow"` — add an item
- `shepherd sub <n> "<text>"` — add a subtask to item n (same quick-add tokens)
- `shepherd edit <n[.m]> "<tokens>"` — merge tokens onto item n (or subtask m); only the given fields change. Tokens: `@category`, `!prio`, `due:`, `defer:`, `link:`, `status:`, `note:`, and text. A bare key clears its field; `note:` takes the rest of the line
- `shepherd list --filter <q>` — list only matching items (text/note/category/due/defer/link), keeping their real indexes for done/rm
- `shepherd done <n[.m]>` / `shepherd undone <n[.m]>` — (un)complete item n, or its subtask m
- `shepherd status <n[.m]> <name>` — set item n's (or subtask m's) status (`in-progress`; `done`/`open` recognised)
- `shepherd note <n[.m]> "<text>"` — set item n's (or subtask m's) free-text note; empty value clears it
- `shepherd rm <n[.m]>` — remove item n, or just its subtask m

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
`shepherd status <n> <name>` (any name accepted; `done`/`open` recognised as the
terminal/default ends); `tab` cycles the configured list in the interactive board.

Boards are per-project: add `--project <name>` after the verb to target a
project's board (`shepherd list --project web`, `shepherd add "…" --project web`,
`shepherd done 2 --project web`), else the default board is used.

`shepherd list --all` reads across every board and is read-only; its indexes are
aggregate and **not** valid for `done`/`rm`. To act on an item seen via `--all`,
re-list that board (`list --project <name>`) and use its index with the same
`--project`. Run `shepherd help` for the full contract.
