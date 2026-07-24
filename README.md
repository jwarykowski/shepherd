# 𓋾 Shepherd

Your todos herded. An interactive todo board that runs standalone in any
terminal, or as a [herdr](https://herdr.dev) plugin in a split, tab, overlay,
or zoomed pane. Backed by a plain markdown file: greppable, hand-editable,
syncable.

No setup required — everything defaults under `~/.config/shepherd/` (or
`$XDG_CONFIG_HOME/shepherd/` when that is set):

| What | Path |
|------|------|
| Default board | `~/.config/shepherd/todo.md` |
| Named board | `~/.config/shepherd/boards/<name>.md` — via `--board <name>` |
| Archive | sibling of the board: `archive.md` / `<name>-archive.md` |
| Config (optional, shared) | `~/.config/shepherd/config.toml` |

Overrides: `$SHEPHERD_TODO_FILE` (exact board file), `$SHEPHERD_CONFIG` (config
file). See [storage](#storage).

- [install](#install)
- [usage](#usage)
- [subtasks](#subtasks)
- [boards](#boards)
- [global view](#global-view)
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
| `space` / `enter` | toggle done (on a parent, cascades to its subtasks; the last subtask done completes the parent) |
| `tab` | cycle status (open → in-progress → done → open); see `statuses` config |
| `h` / `m` / `l` | set priority high / medium / low |
| `g` | set category |
| `t` | set due date — `today`, `tomorrow`, `3d`/`2w`/`5m`/`1y`, or `DD-MM-YYYY` (empty clears) |
| `s` | set defer/start date (same formats as due; item shows dimmed with `starts Nd` until then) |
| `L` | set a reference link (url) |
| `o` | open the selected item's link in the browser |
| `a` | add item (inline syntax below) |
| `S` | add a subtask to the selected item |
| `u` | edit item (or subtask) text |
| `d` | open detail view (shows every field) |
| `v` | cycle view: category / priority / table |
| `A` | toggle the [global view](#global-view) across all boards |
| `p` | open the board picker — every board with done/total counts; `enter` jumps, `a` creates a board, `r` renames, `A` archives, `x` deletes (confirmed) the selected board (rename/archive/delete don't apply to the default board); `e` toggles the archived-boards view where `u` unarchives the selected board |
| `e` | browse the archive (read-only; all boards in the global view; `esc` to leave) |
| `/` | filter (text/note/category/due/defer/link — also greps `archive.md`) |
| `U` / `ctrl+r` | undo / redo (multi-level) |
| `w` | save now (the header shows `● unsaved` / `● saved`) |
| `ctrl+e` | open the markdown file in `$EDITOR` |
| `,` | open settings — edit view, density, autosave, categories, statuses; changes save to `config.toml` |
| `x` | delete item |
| `c` | sweep all done items to `archive.md` |
| `C` | archive the selected item to `archive.md` (whole items only — dimmed on a subtask row) |
| `?` | full help page |
| `q` | save + quit |

In the detail view: `e` edit note · `space` toggle · `o` open link · `d`/`esc`/`q` back.

**Inline quick-add** — `a`, then one line:
`deploy api @work !h due:tomorrow defer:1w link:https://…`. `@word` sets
category, `!h`/`!m`/`!l` priority, `due:<preset>` the due date, `defer:<preset>`
a start/defer date, `link:<url>` a reference, `status:<name>` a status, and
`note:<text>` a note (holds spaces, takes the rest of the line — put it last);
everything else is the task text.

Items are ordered by **category, then priority, then soonest due**, grouped
under headers, with a coloured priority label flush right. **Overdue** open
items are pinned to a `⚠ overdue` group at the top. New items get a `created`
timestamp; due items show a relative label
(`due 3d`, `overdue 2d` in red). Edits save on quit, autosave after a short
idle pause (`autosave` seconds, default 60; `0` disables), or on demand with
`w`; the header shows `● unsaved` / `● saved`. The board reloads on-disk changes
automatically when you have no unsaved edits, so external edits (or a dotfile
sync) show up on their own.

## subtasks

Any item can hold **one level** of subtasks — the steps that make up a task.
Each subtask is a full item (its own done state, priority, due date, …); it just
lives nested under its parent.

On the board, subtasks render indented beneath their parent, and the parent
shows a `done/total` badge:

```
○ ship cli edit                    1/3   high
    ○ parse tokens                     medium
    ✓ wire into Run
    ○ update usage
```

- `S` — add a subtask to the selected item (same quick-add syntax as `a`).
- On a subtask row every per-item key works as it does on a parent: `space`
  toggle, `tab` status, `u` text, `h`/`m`/`l` priority, `t` due, `s` defer,
  `L` link, `o` open link, `x` delete. Overdue/defer labels show on the row.
- `d` opens the subtask's detail view — its own fields plus a `parent` line
  naming the task it belongs to; edit its note there with `e`, same as a parent.
- `g` (category) is the one exception — it's parent-only, since a subtask shares
  its parent's board; it's dimmed in the footer on a subtask row. Set a
  subtask's fields at creation from the CLI too:
  `shepherd sub <n> "text @home !h due:tomorrow defer:1w link:https://…"`.

**Completion cascades both ways:** completing a parent completes all its
subtasks, and completing the last open subtask completes the parent (reopening
one reopens the parent). From the CLI:

```sh
shepherd sub 2 "chop onions !m"   # add subtask to item 2
shepherd done 2.1                 # complete subtask 1 of item 2
shepherd done 2                   # complete item 2 and all its subtasks
shepherd rm 2.1                   # remove just that subtask
```

`list --json` nests them under each item's `subtasks` array (each with a 1-based
`index` within the parent). `stats` counts top-level items only — subtasks are
decomposition, not separate board work.

## boards

Each named board gets its own file at
`~/.config/shepherd/boards/<name>.md`; with no board selected you're on the
default `todo.md`. `config.toml` is shared across every board.

```sh
shepherd --board web            # open the web board
SHEPHERD_BOARD=web shepherd     # same, via env
```

Names are a simple slug — letters, digits, `.` `_` `-`. The archive is
per-board (`boards/web.md` → `boards/web-archive.md`); see
[storage](#storage). This also works from the command API (below) and as a
herdr pane entrypoint:

```toml
[[panes]]
id = "shepherd-work"
title = "todo: work"
placement = "tab"
command = ["./bin/shepherd", "--board", "work"]
```

## global view

See every board at once — the default plus all named boards — in one **read-only**
view. Launch with `--all`, or press `A` from any board to toggle in and out
(your board is saved first, and `A` again drops you back on it).

```sh
shepherd --all        # aggregate of every board
```

`v` cycles four groupings: **board → category → priority → table**. In the
board grouping each board is a header; in the others every row carries a
`[board]` tag (the table gets a `board` column). It's read-only by design —
editing stays on the focused board, so the aggregate is never written back.
`/` filters across boards (including by board name).

The command API mirrors it: `shepherd list --all` (see [command api](#command-api)).

## launch filter

`--board` gives a board its own file; `--filter` is a saved *view* over one
board — start it pre-filtered by text/note/category/due/defer/link:

```sh
./bin/shepherd --filter work      # or: SHEPHERD_FILTER=work ./bin/shepherd
```

When the filter names a category (one you've configured or already use), items
you add while it's active inherit that category — so a task added on a
`--filter work` board lands in `work` and stays in view. An inline `@category`
still overrides; a filter that isn't a category leaves new items uncategorized.
The two combine: `shepherd --board web --filter '!h'`.

`shepherd --version` prints the version and exits.

## command api

For scripts and agentic tools that can't drive the TUI. A leading non-flag
argument switches shepherd from the board to a one-shot command that reads or
mutates a board file and exits — the binary owns the file format, so writes are
always valid. Items carry a stable `id` (in `list --json`); mutating verbs take
either that id or the 1-based `list` index. Agents should use the id — the index
shifts as the board reorders, the id never does. Mutations serialise under a
board lock, so parallel processes never lose one another's writes.

```sh
shepherd list [--json]              # show items with their index
shepherd list --all [--json]        # aggregate across every board (read-only)
shepherd list --filter home         # only items matching the query, real indexes kept
shepherd watch                      # stream board changes as NDJSON until killed
shepherd watch --interval 500ms     # poll faster (default 1s)
shepherd boards [--json]            # list boards with done/total counts (* = current)
shepherd boards --archived          # list archived boards instead
shepherd board rename web webapp    # rename a board (and its archive sibling)
shepherd board archive webapp       # stash a board under boards/archived/ (reversible)
shepherd board unarchive webapp     # restore an archived board
shepherd board delete webapp --force  # delete a board (and its archive sibling)
shepherd board delete webapp --dry-run # preview the delete without writing
shepherd stats [--json] [--all]     # board metrics as charts (--json = numbers)
shepherd stats --legend             # explain every chart and the aging numbers
shepherd stats --no-color           # charts without ANSI color
shepherd add "buy milk @home !h due:tomorrow"
shepherd add "buy milk" -q          # add quietly (no confirmation line)
shepherd add "buy milk" --json      # echo the new item (incl. its id) as JSON
shepherd sub 2 "chop onions !m"     # add a subtask to item 2 (id or index)
shepherd edit 2 "@work !h due:friday" # merge tokens onto item 2 (2.1 edits a subtask)
shepherd done 2                     # mark item 2 done (cascades to its subtasks)
shepherd done 2 5 7                 # mark several done in one atomic write
shepherd done 2f3a…c1 --json        # mark by id; echo the result as JSON
shepherd undone 2.1                 # reopen subtask 1 (also reopens the parent)
shepherd edit 2 "status:in-progress" # set item 2's status (status:done|open recognised)
shepherd edit 2 "note:waiting on infra" # set item 2's note (edit 2 "note:" clears it)
shepherd rm 2                       # remove item 2 (rm 2.1 removes just the subtask)
shepherd rm 2 5 --dry-run           # preview removing several without writing
shepherd archive 2                  # move item 2 off the board into archive.md (whole items only)
```

Global flags (any command):

- `-q`, `--quiet` — silence a mutation's confirmation line (`list`/`stats` data is never suppressed).
- `--no-input` — accepted for script-compat; this API never prompts.
- `-h`, `--help` — print a command's flags, exit 0.

Exit codes: `0` success · `2` usage/input error (bad flag, unknown command,
unknown ref) · `1` runtime/IO failure. A mistyped command suggests the
closest real one. `stats` drops colour when stdout isn't a terminal, `$NO_COLOR`
is set, or `$TERM=dumb`; `--no-color` forces it off.

`done`/`undone`/`rm` take one or more refs (id or index) and apply them as a
single atomic write; they're safe to repeat, since re-marking a done item keeps
its original completion stamp. A subtask is addressed by its own id or a dotted
`n.m` reference (subtask `m` of item `n`); see [subtasks](#subtasks) for the
cascade rules. `--json` on any mutating verb echoes the resulting item(s) in the
`list --json` shape and reports failures as `{"error":…}` on stdout.

`edit <n[.m]> "<tokens>"` sets only the fields its tokens carry — `@category`,
`!h`/`!m`/`!l`, `due:`, `defer:`, `link:`, `status:`, `note:` — and replaces the
text only when plain words are present. A bare key clears its field
(`edit 2 "@ due:"`); `note:` holds spaces and takes the rest of the line, so put
it last (`edit 2 "!h note:call the bank"`).

`list --filter <q>` matches text/note/category/due/defer/link and keeps each
item's real board index, so `done`/`rm` on a filtered listing still hit the
right item.

`watch` streams a board's changes as NDJSON until killed — a coordinating agent
reacts instead of polling. The first line is a `{"type":"snapshot","items":[…]}`
baseline, then one line per change keyed by the item's stable id:
`{"type":"added|updated|removed","item":{…}}` (item shape = `list --json`).
Change detection is mtime polling (`--interval`, default `1s`); it's read-only
and never blocks a writer.

Flags go **after** the verb. Add `--board <name>` (or set `$SHEPHERD_BOARD`)
to target a named board instead of the default:

```sh
shepherd add "ship v2 @work !h" --board web
shepherd list --board web
```

`add` accepts the same quick-add tokens as the board: `@category`, `!h`/`!m`/`!l`
priority, `due:<today|tomorrow|+3d|15-07-2026>`, `defer:`, `link:`, `status:`,
and `note:` (takes the rest of the line). Agents should read with
`list --json` (stable machine shape) and mutate with `add`/`edit`/`done`/`rm`;
an open board picks up the change within ~2s. `edit` is the single setter for
every field, including `status:` (any name, like a free-form `@category`;
`status:done`/`status:open` are the terminal/default ends, with `done`/`undone`
as shorthands) — the `status` field appears in `list --json`.
`list --all --json` adds a `board` field per item so you can tell which board
each came from.

`stats` summarises a board as terminal charts — completion, due/urgency,
priority load, throughput and backlog trend (drawn with
[ntcharts](https://github.com/NimbleMarkets/ntcharts)). Done-based counts include
the archive. `--all` aggregates every board and adds a by-board breakdown;
`--json` emits the raw numbers (no charts) for scripts.

```json
[
  { "id": "019f7390…d901", "index": 1, "done": false, "priority": "H",
    "text": "buy milk", "category": "home", "created": "10-07-2026 13:40",
    "defer": "2026-07-11", "due": "2026-07-15", "link": "https://…",
    "note": "", "completed": "" }
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

Optional `config.toml` at `$XDG_CONFIG_HOME/shepherd/config.toml` (defaults to
`~/.config/shepherd/config.toml`) — shared across every board (override with
`SHEPHERD_CONFIG`):

```toml
view = "category"                          # category (default) | priority | table
density = "compact"                        # compact (default) | comfort
autosave = 60                              # seconds idle before writing; 0 disables
categories = ["work", "home", "personal"]  # tab-cycles in the category prompt
statuses = ["open", "in-progress", "done"] # tab cycles item status in the list
```

- `view` — default grouping/layout on launch (`v` still cycles at runtime).
- `density` — `comfort` adds outer padding and blank lines between rows.
- `autosave` — idle seconds before an unsaved board is written to disk (default 60); `0` disables it, so only `w` and quit save.
- `categories` — press `tab` in the category prompt (`g`) to cycle through them.
- `statuses` — ordered list `tab` cycles through in the list; `done` is always kept and forced last. Defaults to `["open", "done"]`. Intermediate statuses persist as a `status:` line and show a `◐` glyph; the stats page (board and `shepherd stats`) breaks items down by status in this order.

Edit these in the running board with `,` (settings): `tab` cycles `view`/`density`, `enter` edits `autosave`/`categories`/`statuses`, and each change is written straight back to `config.toml`. shepherd owns the file — a settings save rewrites the managed keys.

herdr pane placement (`placement` / `direction`) lives in the same file — see
[herdr integration](#herdr-integration).

## storage

Layout is the table at the top: the default `todo.md`, a shared `config.toml`,
and one `boards/<name>.md` per named board (selected with
`--board`/`$SHEPHERD_BOARD`). Override the exact board file with
`$SHEPHERD_TODO_FILE`.

Dates are stored ISO (`YYYY-MM-DD`) so they sort correctly, but shown and
entered day-month-year / DMY (`DD-MM-YYYY`). Metadata rides as indented
sub-lines:

```markdown
- [ ] (H) ship the release
  id: 019f7390ff8b0e369b6ed5540057c717
  created: 10-07-2026 13:40
  defer: 2026-07-11
  due: 2026-07-15
  category: work
  status: in-progress
  link: https://github.com/org/repo/pull/1
  note: block on the migration first
```

`id` is a stable, opaque handle minted when an item is first written (a legacy
board without ids gets them the next time shepherd saves it); it's how scripts
and agents address an item across reorderings. `completed` (a timestamp) is
added automatically when an item is marked done and cleared if it's reopened.

[Subtasks](#subtasks) nest as further-indented checklist lines — two spaces for
the `- [ ]`, four for their own metadata — written after the parent's metadata:

```markdown
- [ ] (H) ship cli edit
  created: 10-07-2026 13:40
  category: shepherd
  - [ ] (M) parse tokens
  - [x] wire into Run
    due: 2026-07-15
```

`status` is the named intermediate status (`tab` cycles it — see the `statuses`
config). It rides as a sub-line only on non-done items with a status past the
first; a plain open item (`- [ ]`) and a done item (`- [x]`) carry no `status:`
line, so existing files stay unchanged.

### archive

Pressing `c` sweeps every done item off the board and **appends** it to a
sibling archive file (`todo.md` → `archive.md`, `boards/web.md` →
`boards/web-archive.md`, created on first use). `C` archives just the
selected item instead, whatever its status — whole items only, so it's a no-op
(and dimmed in the legend) on a subtask row, matching the `shepherd archive
<ref>` CLI. Same markdown format as `todo.md`, so it's greppable and
hand-editable. It's append-only — shepherd never rewrites or prunes it; trim it
yourself if it grows. The board doesn't display the archive, but `/` filtering
also greps it and shows matches in a separate section, so archived items stay
findable.

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

Set the `config.toml` defaults (in the shared
`~/.config/shepherd/config.toml`):

```toml
placement = "split"     # split (default) | tab | overlay | zoomed
direction = "right"     # split only: right (default) | left | up | down
```

`open-split` / `open-tab` / `open-overlay` / `open-zoomed` force one placement
regardless of config; `SHEPHERD_PLACEMENT` / `SHEPHERD_DIRECTION` override too.

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
