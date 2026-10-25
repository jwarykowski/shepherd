# storage

Everything defaults under `~/.config/shepherd/` (or `$XDG_CONFIG_HOME/shepherd/`
when that is set):

| what | path |
|------|------|
| default board | `~/.config/shepherd/todo.md` |
| named board | `~/.config/shepherd/boards/<name>.md`, via `--board <name>` or `$SHEPHERD_BOARD` |
| archive | sibling of the board: `archive.md` / `<name>-archive.md` |
| config (optional, shared) | `~/.config/shepherd/config.toml` |

`$SHEPHERD_TODO_FILE` overrides the exact board file; while it's set, `--board`
and the board actions refuse to run. `$SHEPHERD_CONFIG` overrides the config
file. Board names are letters, digits, `.`, `_` and `-`; `default`, `todo`,
`archive` and names ending in `-archive` are reserved.

## format

A board is markdown. Dates are stored ISO (`YYYY-MM-DD`) so they sort, but
shown and entered day first (`DD-MM-YYYY`). Metadata rides as indented
sub-lines:

```markdown
- [ ] (H) ship the release
  id: 019f7390ff8b0e369b6ed5540057c717
  created: 10-07-2026 13:40
  defer: 2026-07-11
  due: 2026-07-15
  category: work
  tags: api, docs
  status: in-progress
  link: https://github.com/org/repo/pull/1
  note: block on the migration first
```

`id` is a stable, opaque handle minted when an item is first written (a legacy
board without ids gets them the next time shepherd saves it); it's how scripts
and agents address an item across reorderings. `completed` (a timestamp) is
added when an item is marked done and cleared if it's reopened. `claimed` names
who holds the item (see [safe writes](agents.md#safe-writes)).

[Subtasks](boards.md#subtasks) nest as further-indented checklist lines (two
spaces for the `- [ ]`, four for their own metadata), written after the
parent's metadata:

```markdown
- [ ] (H) ship cli edit
  created: 10-07-2026 13:40
  category: shepherd
  - [ ] (M) parse tokens
  - [x] wire into Run
    due: 2026-07-15
```

`status` is the named intermediate status (`tab` cycles it; see `statuses` in
[configuration](configuration.md)). It's written only on non-done items with a
status past the first; a plain open item (`- [ ]`) and a done item (`- [x]`)
carry no `status:` line, so existing files stay unchanged.

You can open the file in `$EDITOR` with `ctrl+e`, but let scripts and agents go
through the [command api](command-api.md).

## archive

`C` sweeps every done item off the board and appends it to a sibling archive
file (`todo.md` -> `archive.md`, `boards/web.md` -> `boards/web-archive.md`,
created on first use). `X` archives just the selected item instead, whatever
its status: whole items only, so it does nothing (and is dimmed in the footer)
on a subtask row, matching `shepherd archive <ref>`. Same markdown format as
the board, so it's greppable and hand-editable. It's append-only: shepherd
never rewrites or prunes it, so trim it yourself if it grows. The board doesn't
show the archive, but `/` also greps it and shows matches in a separate
section, and `gh` browses it.
