# 𓋾 Shepherd

Your todos herded. An interactive todo board for [herdr](https://herdr.dev),
opened in a split, tab, overlay, or zoomed pane. Backed by a plain markdown
file: greppable, hand-editable, syncable.

## Keys

| key | action |
|-----|--------|
| `j` / `↓`, `k` / `↑` | move |
| `space` / `enter` | toggle done |
| `h` / `m` / `l` | set priority high / medium / low |
| `g` | set category |
| `t` | set due date — `today`, `tomorrow`, `3d`/`2w`/`5m`/`1y`, or `DD-MM-YYYY` (empty clears) |
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
under headers, with a colored priority label flush right. **Overdue** open
items are pinned to a `⚠ overdue` group at the top. New items get a `created`
timestamp; due items show a relative label
(`due 3d`, `overdue 2d` in red). The board reloads on-disk changes
automatically when you have no unsaved edits, so external edits (or a dotfile
sync) show up on their own.

## Launch filter

Start the board pre-filtered — useful for a per-project tab:

```sh
./bin/shepherd --filter work      # or: SHEPHERD_FILTER=work ./bin/shepherd
```

In a herdr manifest, give a project its own pane entrypoint:

```toml
[[panes]]
id = "shepherd-work"
title = "todo: work"
placement = "tab"
command = ["./bin/shepherd", "--filter", "work"]
```

## Command API

For scripts and agentic tools that can't drive the TUI. A leading non-flag
argument switches shepherd from the board to a one-shot command that reads or
mutates `todo.md` and exits — the binary owns the file format, so writes are
always valid. Indexes are 1-based and match `list` order.

```sh
shepherd list [--json]              # show items with their index
shepherd add "buy milk @home !h due:tomorrow"
shepherd done 2                     # mark item 2 done
shepherd undone 2                   # mark item 2 not done
shepherd rm 2                       # remove item 2
```

`add` accepts the same quick-add tokens as the board: `@category`, `!h`/`!m`/`!l`
priority, `due:<today|tomorrow|+3d|15-07-2026>`. Agents should read with
`list --json` (stable machine shape) and mutate with `add`/`done`/`rm`; an open
board picks up the change within ~2s.

```json
[
  { "index": 1, "done": false, "priority": "H", "text": "buy milk",
    "category": "home", "created": "10-07-2026 13:40", "due": "2026-07-15" }
]
```

## Storage

`$HERDR_PLUGIN_STATE_DIR/todo.md`, else `~/.config/shepherd/todo.md`.
Override with `HERDR_TODO_FILE`. Dates are stored ISO (`YYYY-MM-DD`) so they
sort correctly, but shown and entered day-month-year / DMY (`DD-MM-YYYY`).
Metadata rides as indented sub-lines:

```markdown
- [ ] (H) ship the release
  created: 10-07-2026 13:40
  due: 2026-07-15
  category: work
  note: block on the migration first
```

### Archive

Pressing `c` moves every done item off the board and **appends** it to a
sibling `archive.md` (same directory as `todo.md`, created on first use). Same
markdown format as `todo.md`, so it's greppable and hand-editable. It's
append-only — shepherd never rewrites or prunes it; trim it yourself if it
grows. The board doesn't display the archive, but `/` filtering also greps it
and shows matches in a separate section, so archived items stay findable.

## Config

Optional `config.toml`, next to `todo.md` (override with `SHEPHERD_CONFIG`):

```toml
view = "category"                          # category (default) | priority | table
density = "compact"                        # compact (default) | comfort
categories = ["work", "home", "personal"]  # tab-cycles in the category prompt
placement = "split"                        # split (default) | tab | overlay | zoomed
direction = "right"                        # split only: right (default) | left | up | down
```

- `view` — default grouping/layout on launch (`v` still cycles at runtime).
- `density` — `comfort` adds outer padding and blank lines between rows.
- `categories` — press `tab` in the category prompt (`g`) to cycle through them.
- `placement` / `direction` — how `.open` opens the pane. `.open` reads these;
  `.open-split` / `.open-tab` / `.open-overlay` / `.open-zoomed` force one
  regardless of config. `SHEPHERD_PLACEMENT` / `SHEPHERD_DIRECTION` override too.

## Install

Requires a Go toolchain to build.

Local dev, from a checkout:

```sh
go build -o bin/shepherd .
herdr plugin link .
```

Published (public repo + GitHub topic `herdr-plugin`) — herdr runs the
`[[build]]` step for you:

```sh
herdr plugin install jwarykowski/shepherd
```

## Placement

The board opens as a `split`, `tab`, `overlay`, or `zoomed` pane. Five actions:

| Action         | Placement                        |
| -------------- | -------------------------------- |
| `open`         | from config (`split` if unset)   |
| `open-split`   | split                            |
| `open-tab`     | tab                              |
| `open-overlay` | overlay                          |
| `open-zoomed`  | zoomed                           |

`open` resolves placement from (highest first): env
`SHEPHERD_PLACEMENT`/`SHEPHERD_DIRECTION` → `config.toml` → `split`/`right`.
`direction` (`right`/`left`/`up`/`down`) applies to `split` only.

## Keybindings

```toml
[[keys.command]]
key = "prefix+shift+o"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open"          # placement from config
description = "shepherd"

[[keys.command]]
key = "prefix+shift+t"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open-tab"
description = "shepherd (tab)"

[[keys.command]]
key = "prefix+shift+y"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open-overlay"
description = "shepherd (overlay)"

[[keys.command]]
key = "prefix+shift+z"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open-zoomed"
description = "shepherd (zoomed)"
```

## Develop

See [CONTRIBUTING.md](CONTRIBUTING.md).
