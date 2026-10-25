# keys

Keys like `gb` are a sequence: press `g`, then `b`. After `g` the footer lists
what can follow; `esc` cancels. Every key can be rebound (see
[key bindings](#key-bindings)).

## board

| key | action |
|-----|--------|
| `j` / `↓`, `k` / `↑` | move |
| `gg` / `G` | jump to the top / bottom |
| `enter` | open detail view (shows every field) |
| `space` | toggle done (on a parent, cascades to its subtasks; the last subtask done completes the parent) |
| `tab` | cycle status (open, in-progress, done, open); see `statuses` in [configuration](configuration.md) |
| `a` | add item ([quick-add](#quick-add)); inherits the selected item's category unless the text names one |
| `A` | add a subtask to the selected item |
| `e` | edit item (or subtask) text |
| `x` | delete item |
| `X` | archive the selected item to `archive.md` (whole items only; dimmed on a subtask row) |
| `C` | clear done: sweep all done items to `archive.md` |
| `h` / `m` / `l` | set priority high / medium / low |
| `c` | set category |
| `t` | set tags (space- or comma-separated; empty clears) |
| `d` | set due date: `today`, `tomorrow`, a weekday (`fri`, `friday`: the next one, never today), `3d`/`2w`/`5m`/`1y`, or `DD-MM-YYYY` (empty clears) |
| `s` | set start (defer) date (same formats as due; the item shows dimmed with `starts Nd` until then) |
| `L` | set a reference link (url) |
| `o` | open the selected item's link in the browser |
| `y` | copy the selected item's title to the clipboard (OSC 52, so it works over SSH and in tmux with `set-clipboard on`) |
| `v` | cycle view: category / priority / tag / table / lane |
| `←`/`→` | switch the active column in the [lane view](boards.md#lane-view) |
| `/` | filter (text/note/category/tags/due/defer/link; also greps `archive.md`) |
| `u` / `ctrl+r` | undo / redo (multi-level; an archive sweep saves the board and clears history) |
| `w` | save now (the header shows `● unsaved` / `● saved`) |
| `ctrl+e` | open the markdown file in `$EDITOR` |
| `gb` | board picker: every board with done/total counts; `enter` jumps, `a` creates, `r` renames, `A` archives, `x` deletes (confirmed), `d` shows detail (name, dir, paths, counts); rename/archive/delete don't apply to the default board; `e` toggles the archived-boards view, where `u` unarchives |
| `ga` | toggle the [global view](boards.md#global-view) across all boards |
| `gh` | browse the archive (read-only; all boards in the global view; `esc` to leave) |
| `gs` or `,` | settings: view, density, autosave, categories, statuses, footer, mouse, theme; changes save to `config.toml` |
| `gt` | theme picker: `j`/`k` preview each theme live, `enter` keeps it (saved to `config.toml`), `esc` restores the old one |
| `gf` | hide / show the footer help grid (the `jwarykowski/shepherd` · version line stays); `hidefooter` config sets the default |
| `r` | run: send the item to your coding agent ([Shepherd Pro](agents.md#send-tasks-to-your-agent-shepherd-pro)) |
| `gr` | the agents view: every run, its log, take over |
| `?` | full help page |
| `q` | save + quit |

Items sort by category, then priority, then soonest due, grouped under headers.
Overdue open items are pinned to a `⚠ overdue` group at the top. New items get a
`created` timestamp; due items show a relative label (`due 3d`, `overdue 2d` in
red).

Rows carry two flush-right values: the subtask progress (`1/2`) when the item
has subtasks, else the due/defer label; then, pinned far right, whichever
grouping axis the headers don't already name. The category view shows the
priority there, the priority view the category, the tag view the priority.
Status takes no width: the box shape carries it (`○` open, `◐` a named status,
`✓` done).

Edits save on quit, after an idle pause (`autosave` seconds, default 60; `0`
disables), or with `w`. With no unsaved edits, the board reloads on-disk
changes by itself, so external edits (or a dotfile sync) show up.

## quick-add

`a`, then one line: `deploy api @work #api !h due:tomorrow defer:1w link:https://…`

| token | sets |
|-------|------|
| `@word` | category |
| `#word` | adds a tag (`tags:a,b` sets the whole set) |
| `!h` `!m` `!l` | priority |
| `due:<preset>` | due date |
| `defer:<preset>` | start (defer) date |
| `link:<url>` | a reference link |
| `status:<name>` | a status |
| `note:<text>` | a note; holds spaces and takes the rest of the line, so put it last |

Everything else is the task text.

## detail view

The detail view is also an editor. The same field keys work there (`e` text,
`h`/`m`/`l` priority, `c` category, `t` tags, `d` due, `s` start, `L` link,
`tab` status), and saving returns to the detail view rather than the list.
Plus `n` edit note (`esc`, or `shift+enter` in terminals with the Kitty
keyboard protocol, finishes), `space` toggle, `o` open link, `y` copy title,
`esc` back, `q` save + quit. With agents, it lists the item's runs: `r` sends
it, `gl` opens the latest run's log. Its footer is the board's key grid narrowed
to the keys that act on one item (`fields` · `dates` · `item` · `agent` · `go`).

## key bindings

Rebind any board key in `config.toml` with `key.<action> = "<keys>"`. Separate
several keys with commas; a space makes a two-key sequence:

```toml
key.due    = "t"           # one key
key.tags   = "T"
key.up     = "k, up"       # several keys
key.boards = "g b, B"      # a sequence and a single key
key.prio   = "1, 2, 3"     # always three: high, medium, low
```

Actions: `up`, `down`, `top`, `bottom`, `lane_left`, `lane_right`, `toggle`,
`detail`, `view`, `global`, `archive_view`, `boards`, `footer`, `add`,
`add_sub`, `edit`, `status`, `delete`, `sweep`, `archive`, `prio`, `category`,
`tags`, `due`, `defer`, `link`, `open`, `copy`, `note`, `save`, `editor`,
`undo`, `redo`, `filter`, `settings`, `theme`, `help`, `run`, `runs`,
`run_log`. Key names are as Bubble
Tea reports them: a character (`x`, `X`, `/`), `space`, `enter`, `tab`,
`up`/`down`/`left`/`right`, `home`/`end`, `ctrl+<key>`, `alt+<key>`, and
`comma` for `,` alongside other keys (`"comma, g s"`).

Overrides are checked when the board starts, so two actions can swap keys. One
that names an unknown action, has no keys, uses `esc`, `q` or `ctrl+c`, or
clashes with another binding (including a single key that is also the start of
a sequence) is ignored, and the footer says so until the next key. `esc` and
`q` always back out and quit. The footer and `?` help show your bindings.
Pickers and prompts (boards, settings, archive, text entry) keep their fixed
keys.

To keep the old layout (0.20 and earlier), paste this:

```toml
key.detail = "d"
key.edit = "u"
key.undo = "U"
key.add_sub = "S"
key.archive = "C"
key.sweep = "c"
key.category = "g"
key.tags = "T"
key.due = "t"
key.boards = "b"
key.global = "A"
key.archive_view = "e"
key.footer = "F"
key.theme = "ctrl+t"
key.top = "home"
key.settings = ","
key.run = "@"
key.runs = "R"
key.run_log = "r"
```
