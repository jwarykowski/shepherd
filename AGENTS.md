# Agent guide

This machine has `shepherd`, a CLI todo board backed by a markdown file. Manage
the user's todos through it — never hand-edit the todo file; the binary owns the
format.

- `shepherd list --json` — read all items (machine-readable; prefer this)
- `shepherd list --all --json` — read across every board; adds a `project` field per item
- `shepherd stats [--json] [--all]` — board metrics (charts, or `--json` numbers)
- `shepherd add "buy milk @home !h due:tomorrow"` — add an item
- `shepherd done <n>` / `shepherd undone <n>` — (un)complete item n
- `shepherd rm <n>` — remove item n

Indexes are 1-based and match `list` order. Quick-add tokens: `@category`,
`!h`/`!m`/`!l` priority, `due:<today|tomorrow|+3d|15-07-2026>`,
`defer:<same date forms>` (start/defer date), `link:<url>`. `list --json`
reports `completed` (done timestamp), `defer`, and `link` per item.

Boards are per-project: add `--project <name>` after the verb to target a
project's board (`shepherd list --project web`, `shepherd add "…" --project web`,
`shepherd done 2 --project web`), else the default board is used.

`shepherd list --all` reads across every board and is read-only; its indexes are
aggregate and **not** valid for `done`/`rm`. To act on an item seen via `--all`,
re-list that board (`list --project <name>`) and use its index with the same
`--project`. Run `shepherd help` for the full contract.
