# 🦯 Shepherd

An interactive todo board for [herdr](https://herdr.dev), in a split pane.
Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) — the TUI
owns input, resize, and redraw, so it stays rock-solid inside a herdr pane.
Backed by a plain markdown file: greppable, hand-editable, syncable.

## Keys (inside the panel)

| key | action |
|-----|--------|
| `j` / `↓`, `k` / `↑` | move |
| `space` / `enter` | toggle done |
| `h` / `m` / `l` | set priority high / medium / low |
| `g` | set category |
| `t` | set due date — `today`, `tomorrow`, `3d`/`2w`/`5m`/`1y`, or `DD/MM/YYYY` (empty clears) |
| `a` | add item (inline syntax below) |
| `u` | edit item text |
| `d` | open detail view |
| `v` | cycle view: category / priority / table |
| `/` | filter (text, note, category, due — also greps `archive.md`) |
| `U` / `ctrl+r` | undo / redo (multi-level) |
| `ctrl+e` | open the markdown file in `$EDITOR` |
| `x` | delete item |
| `c` | archive all done items to `archive.md` |
| `?` | full help page |
| `q` | save + quit |

In the detail view: `e` edit note · `space` toggle · `d`/`esc`/`q` back.

**Inline quick-add** — `a`, then one line: `deploy api @work !h due:tomorrow`.
`@word` sets category, `!h`/`!m`/`!l` priority, `due:<preset>` the due date;
everything else is the task text.

Items are ordered by **category, then priority, then soonest due**, grouped
under headers, with a colored priority label on the right. **Overdue** open
items are pinned to a `⚠ overdue` group at the top. New items get a `created`
timestamp; items with a note show `✎`; due items show a relative label
(`due 3d`, `overdue 2d` in red). The board reloads on-disk changes
automatically when you have no unsaved edits, so external edits (or a dotfile
sync) show up on their own.

## Launch filter

Start the board pre-filtered — useful for a per-project tab:

```sh
./bin/board --filter work      # or: SHEPHERD_FILTER=work ./bin/board
```

In a herdr manifest, give a project its own pane entrypoint:

```toml
[[panes]]
id = "board-work"
title = "todo: work"
placement = "tab"
command = ["./bin/board", "--filter", "work"]
```

## Storage

`$HERDR_PLUGIN_STATE_DIR/todo.md`, else `~/.config/shepherd/todo.md`.
Override with `HERDR_TODO_FILE`. Dates are stored ISO (`YYYY-MM-DD`) so they
sort correctly, but shown and entered British (`DD/MM/YYYY`). Metadata rides as
indented sub-lines:

```markdown
- [ ] (H) ship the release
  created: 10/07/2026 13:40
  due: 2026-07-15
  category: work
  note: block on the migration first
```

Archived (done) items are appended to a sibling `archive.md` in the same
directory when you press `c`.

## Config

Optional `config.toml`, next to `todo.md` (override with `SHEPHERD_CONFIG`):

```toml
view = "category"                          # category (default) | priority | table
density = "compact"                        # compact (default) | comfort
categories = ["work", "home", "personal"]  # tab-cycles in the category prompt
```

- `view` — default grouping/layout on launch (`v` still cycles at runtime).
- `density` — `comfort` adds outer padding and blank lines between rows.
- `categories` — press `tab` in the category prompt (`g`) to cycle through them.

## Install

Requires a Go toolchain to build.

Local dev, from a checkout:

```sh
go build -o bin/board .
herdr plugin link .
```

Published (public repo + GitHub topic `herdr-plugin`) — herdr runs the
`[[build]]` step for you:

```sh
herdr plugin install jwarykowski/shepherd
```

## Keybinding

```toml
[[keys.command]]
key = "prefix+shift+o"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open"
description = "shepherd board"
```

## Develop

```sh
go test ./...     # logic + model + view tests
go build -o bin/board .
```
