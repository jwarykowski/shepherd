# boards

## named boards

Each named board gets its own file at `~/.config/shepherd/boards/<name>.md`;
with no board selected you're on the default `todo.md`. `config.toml` is shared
across every board.

```sh
shepherd --board web            # open the web board
SHEPHERD_BOARD=web shepherd     # same, via env
```

Names are a slug: letters, digits, `.` `_` `-`. The archive is per board
(`boards/web.md` -> `boards/web-archive.md`); see [storage](storage.md). This
also works from the [command api](command-api.md) and as a herdr pane
entrypoint:

```toml
[[panes]]
id = "shepherd-work"
title = "todo: work"
placement = "tab"
command = ["./bin/shepherd", "--board", "work"]
```

## global view

Every board at once (the default plus all named boards) in one read-only view.
Launch with `--all`, or press `ga` on any board to toggle in and out (your board
is saved first, and `ga` again drops you back on it).

```sh
shepherd --all        # aggregate of every board
```

`v` cycles five groupings: board, category, priority, tag, table. In the board
grouping each board is a header; in the others every row carries a `[board]`
tag (the table gets a `board` column). It's read-only by design: editing stays
on the focused board, so the aggregate is never written back. `/` filters across
boards (including by board name).

Only keys that read or navigate work here: `j`/`k`, `gg`/`G`, `enter`, `v`, `/`,
`gh`, `gb`, `o`, `y`, `gf`, `gt`, `?`, `ga`/`esc` to leave, `q` to quit. Every
editing key is inert and dimmed in the footer, on the board and in the detail
view alike.

The command API mirrors it with `shepherd list --all`.

## launch filter

`--board` gives a board its own file; `--filter` is a saved view over one
board. It starts pre-filtered by text/note/category/tags/due/defer/link:

```sh
shepherd --filter work      # or: SHEPHERD_FILTER=work shepherd
```

When the filter names a category (one you've configured or already use), items
you add while it's active inherit it, so a task added on a `--filter work`
board lands in `work` and stays in view. This is the fallback: the selected
item's own category is inherited first, and an inline `@category` overrides
both. A filter that isn't a category leaves new items uncategorised. The two
combine: `shepherd --board web --filter '!h'`.

## lane view

A kanban board: one column per configured `statuses` entry, reached by cycling
with `v`. `←`/`→` switch the active column; `j`/`k` move within it. `tab`
cycles the selected card's status as everywhere else, which in lane view also
moves it into the next column, cursor and all. Every other key still acts on
the card under the cursor (`h`/`m`/`l` priority, `c` category, `t` tags, `d`
due, `s` start, `L` link, `o` open, `y` copy, `e` edit, `enter` detail, `space`
toggle, `x` delete). Not available in the global view, since boards can each
configure different statuses.

## subtasks

Any item can hold one level of subtasks: the steps that make up a task. Each
subtask is a full item (its own done state, priority, due date, …) nested under
its parent. On the board they render indented beneath the parent, which shows a
`done/total` badge:

```
○ ship cli edit                    1/3   high
    ○ parse tokens                     medium
    ✓ wire into Run
    ○ update usage
```

- `A` adds a subtask to the selected item (same quick-add syntax as `a`).
- On a subtask row every per-item key works as on a parent: `space` toggle,
  `tab` status, `e` text, `h`/`m`/`l` priority, `d` due, `s` start, `L` link,
  `o` open link, `x` delete. Overdue/defer labels show on the row.
- `enter` opens the subtask's detail view: its own fields plus a `parent` line
  naming its task; `n` edits its note.
- `c` (category) is parent-only, since a subtask shares its parent's board; it's
  dimmed in the footer on a subtask row.

Completion cascades both ways: completing a parent completes all its subtasks,
and completing the last open subtask completes the parent (reopening one
reopens the parent). From the CLI:

```sh
shepherd sub 2 "chop onions !m"   # add subtask to item 2 (quick-add tokens work)
shepherd done 2.1                 # complete subtask 1 of item 2
shepherd done 2                   # complete item 2 and all its subtasks
shepherd rm 2.1                   # remove just that subtask
```

`list --json` nests them under each item's `subtasks` array (each with a 1-based
`index` within the parent). `stats` counts top-level items only: subtasks are
decomposition, not separate board work.
