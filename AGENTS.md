# Agent guide

This machine has `shepherd`, a CLI todo board backed by a markdown file. Manage
the user's todos through it — never hand-edit the todo file; the binary owns the
format.

- `shepherd list --json` — read all items (machine-readable; prefer this)
- `shepherd add "buy milk @home !h due:tomorrow"` — add an item
- `shepherd done <n>` / `shepherd undone <n>` — (un)complete item n
- `shepherd rm <n>` — remove item n

Indexes are 1-based and match `list` order. Quick-add tokens: `@category`,
`!h`/`!m`/`!l` priority, `due:<today|tomorrow|+3d|15-07-2026>`. Run
`shepherd help` for the full contract.
