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
| `shepherd add "<text>"` | add an item |
| `shepherd done <n>` / `undone <n>` | (un)complete item n |
| `shepherd rm <n>` | remove item n |

Indexes are 1-based and match `list` order. Read with `--json`, act by index.

## Adding

`add` accepts quick-add tokens in the text:
`shepherd add "renew passport @home !h due:+2w"`

- `@category` · `!h`/`!m`/`!l` priority · `due:<today|tomorrow|+3d|15-07-2026>`

## Notes

- Data file: `$HERDR_TODO_FILE`, else `$HERDR_PLUGIN_STATE_DIR/todo.md`, else
  `~/.config/shepherd/todo.md`. Dates stored ISO.
- If `shepherd` isn't found, it isn't installed —
  `brew install jwarykowski/tap/shepherd`.
- An open board picks up your changes within ~2s.
