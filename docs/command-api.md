# command api

For scripts and agents that can't drive the TUI. A leading non-flag argument
switches shepherd from the board to a one-shot command that reads or changes a
board file and exits. The binary owns the file format, so writes are always
valid. Items carry a stable `id` (in `list --json`); changing verbs take that id
or the 1-based `list` index. Agents should use the id: the index shifts as the
board reorders, the id never does. Writes serialise under a board lock, so
parallel processes never lose one another's changes.

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
shepherd edit 2 "#api #docs"        # add tags (tags:a,b replaces the set, tags: clears)
shepherd rm 2                       # remove item 2 (rm 2.1 removes just the subtask)
shepherd rm 2 5 --dry-run           # preview removing several without writing
shepherd archive 2                  # move item 2 off the board into archive.md (whole items only)
shepherd add --text "fix #123 crash" "@work" # text taken verbatim; tokens from the positional string
shepherd claim 2 --by agent-1       # take item 2 (sets claimed + in-progress; exit 1 if held or done)
shepherd claim 2 --release --by agent-1 # hand it back
shepherd edit 2 "!h" --if-rev 1a2b3c4d5e6f # write only if item 2 is unchanged since that rev
shepherd --version                  # print the version and exit
```

`--text`, `--if-rev` and `claim` are covered in [agents](agents.md#safe-writes);
`stats` in [stats](stats.md).

## flags and exit codes

Flags go after the verb. `--board <name>` (or `$SHEPHERD_BOARD`) targets a named
board instead of the default:

```sh
shepherd add "ship v2 @work !h" --board web
shepherd list --board web
```

- `-q`, `--quiet`: silence a write's confirmation line (`list`/`stats` data is
  never suppressed).
- `--no-input`: accepted for scripts; this API never prompts.
- `-h`, `--help`: print a command's flags, exit 0.

Exit codes: `0` success, `2` usage or input error (bad flag, unknown command,
unknown ref, a `due:`/`defer:` date it can't read), `1` runtime or IO failure. A
mistyped command suggests the closest real one.

## writing

`add` takes the same quick-add tokens as the board: `@category`, `#tag`
(`tags:a,b` replaces the set), `!h`/`!m`/`!l` priority,
`due:<today|tomorrow|friday|+3d|15-07-2026>`, `defer:`, `link:`, `status:`, and
`note:` (takes the rest of the line). An open board picks up a change within
about 2s.

`edit <n[.m]> "<tokens>"` is the one setter for every field. It sets only the
fields its tokens carry and replaces the text only when plain words are
present. A bare key clears its field (`edit 2 "@ due:"`); `note:` holds spaces
and takes the rest of the line, so put it last (`edit 2 "!h note:call the
bank"`). `status:` takes any name, like a free-form `@category`; `status:done`
and `status:open` are the ends, with `done`/`undone` as shorthands.

`done`/`undone`/`rm` take one or more refs (id or index) and apply them in one
atomic write. They're safe to repeat: re-marking a done item keeps its original
completion stamp. A subtask is addressed by its own id or as `n.m` (subtask `m`
of item `n`); see [subtasks](boards.md#subtasks) for the cascade rules.

`--json` on any write echoes the resulting item(s) in the `list --json` shape
and reports failures as `{"error":…}` on stdout.

## reading

`list --json` is the stable machine shape. `list --all --json` adds a `board`
field per item. Every item also carries a `rev` (see
[safe writes](agents.md#safe-writes)).

```json
[
  { "id": "019f7390…d901", "index": 1, "done": false, "priority": "H",
    "text": "buy milk", "category": "home", "tags": ["errand"],
    "created": "10-07-2026 13:40",
    "defer": "2026-07-11", "due": "2026-07-15", "link": "https://…",
    "note": "", "completed": "" }
]
```

`list --filter <q>` matches text/note/category/tags/due/defer/link and keeps each
item's real board index, so `done`/`rm` on a filtered listing still hit the
right item.

`watch` streams a board's changes as NDJSON until killed, so a coordinating
agent reacts instead of polling. The first line is a
`{"type":"snapshot","items":[…]}` baseline, then one line per change keyed by
the item's id: `{"type":"added|updated|removed","item":{…}}` (item shape as
`list --json`). It polls the file's mtime (`--interval`, default `1s`), only
reads, and never blocks a writer.
