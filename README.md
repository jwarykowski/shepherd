# 𓋾 Shepherd

Your todos herded. An interactive todo board that runs standalone in any
terminal, or as a [herdr](https://herdr.dev) plugin in a split, tab, overlay,
or zoomed pane. Backed by a plain markdown file: greppable, hand-editable,
syncable.

- [install](#install)
- [usage](#usage)
- [launch filter](#launch-filter)
- [command api](#command-api)
- [agentic tools](#agentic-tools)
- [configuration](#configuration)
- [storage](#storage)
- [herdr integration](#herdr-integration)
- [develop](#develop)

## install

Requires a Go toolchain to build.

Homebrew (recommended):

```sh
brew install jwarykowski/tap/shepherd
```

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

## usage

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

## launch filter

Start the board pre-filtered — useful for a per-project tab:

```sh
./bin/shepherd --filter work      # or: SHEPHERD_FILTER=work ./bin/shepherd
```

When the filter names a category (one you've configured or already use), items
you add while it's active inherit that category — so a task added on a
`--filter work` board lands in `work` and stays in view. An inline `@category`
still overrides; a filter that isn't a category leaves new items uncategorized.

In a herdr manifest, give a project its own pane entrypoint:

```toml
[[panes]]
id = "shepherd-work"
title = "todo: work"
placement = "tab"
command = ["./bin/shepherd", "--filter", "work"]
```

## command api

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

## agentic tools

The command API is the whole integration — any agent that can run a shell drives
shepherd with the same verbs. Discovery is layered:

- **Any tool:** `shepherd help` prints the contract. That's the universal
  fallback; nothing else is required.
- **Claude Code:** symlink the bundled skill in once —
  ```sh
  ln -s "$PWD/skills/shepherd" ~/.claude/skills/shepherd
  ```
  Available in every project; Claude invokes it when a request relates to your
  todos (the skill's description is what it matches on).
- **Cursor / Codex / Zed / others:** paste [`AGENTS.md`](AGENTS.md) into the
  tool's global rules / instructions slot.

All three point at the same `shepherd` CLI — no per-tool server, no MCP.

## configuration

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

## storage

Everything lives under `~/.config/shepherd`: the default board `todo.md`, a
shared `config.toml`, and one file per project at `projects/<name>.md`. Pick a
project with `--project <name>` (or `$SHEPHERD_PROJECT`); unset uses the default
board. `config.toml` is shared across every board. Point at an exact file with
`$SHEPHERD_TODO_FILE` if you need to. In the command API, flags follow the
verb: `shepherd add "…" --project web`, `shepherd list --project web`.

Dates are stored ISO (`YYYY-MM-DD`) so they sort correctly, but shown and
entered day-month-year / DMY (`DD-MM-YYYY`). Metadata rides as indented
sub-lines:

```markdown
- [ ] (H) ship the release
  created: 10-07-2026 13:40
  due: 2026-07-15
  category: work
  note: block on the migration first
```

### archive

Pressing `c` moves every done item off the board and **appends** it to a
sibling archive file (`todo.md` → `archive.md`, `projects/web.md` →
`projects/web-archive.md`, created on first use). Same
markdown format as `todo.md`, so it's greppable and hand-editable. It's
append-only — shepherd never rewrites or prunes it; trim it yourself if it
grows. The board doesn't display the archive, but `/` filtering also greps it
and shows matches in a separate section, so archived items stay findable.

## herdr integration

### placement

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

### keybindings

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

## develop

See [CONTRIBUTING.md](CONTRIBUTING.md).
