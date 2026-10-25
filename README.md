<p align="center"><img src="assets/shepherd.svg" alt="Shepherd" width="120"></p>

# 𓋾 Shepherd

An agent-native task queue: a terminal board backed by plain markdown files.
Run it standalone in any terminal, or as a [herdr](https://herdr.dev) plugin in
a split, tab, overlay, or zoomed pane. Greppable, hand-editable, syncable, and
your coding agents can work from it too.

![Shepherd in action](assets/demo.gif)

## install

```sh
brew install jwarykowski/tap/shepherd
```

From a checkout (needs Go), or as a herdr plugin:

```sh
go build -o bin/shepherd .
herdr plugin link .                      # local dev
herdr plugin install jwarykowski/shepherd
```

## usage

Run `shepherd`. No setup: the board lives at `~/.config/shepherd/todo.md`. Press
`a` and type one line; tokens set the fields:

```text
deploy api @work #api !h due:tomorrow
```

| key | action |
|-----|--------|
| `j` / `k` | move (`gg` / `G` top and bottom) |
| `a` / `A` | add an item / a subtask |
| `e` | edit text |
| `space` / `tab` | toggle done / cycle status |
| `enter` | detail view, also an editor |
| `h` `m` `l` | priority |
| `c` `t` `d` `s` | category, tags, due, start |
| `v` | cycle views: category, priority, tag, table, lane |
| `/` | filter |
| `u` / `ctrl+r` | undo / redo |
| `gb` / `ga` | boards / all boards |
| `gs` / `gt` | settings / theme |
| `?` | every key |
| `q` | save and quit |

Keys like `gb` are a sequence: press `g`, and the footer lists what can follow.
Every key can be rebound. See [keys](docs/keys.md).

## command api

Any non-flag argument makes shepherd a one-shot command, for scripts and
agents:

```sh
shepherd list --json                  # items with stable ids
shepherd add "ship v2 @work !h due:3d" --board web
shepherd edit <id> "status:in-progress"
shepherd done <id> <id>               # one atomic write
shepherd watch                        # NDJSON changes until killed
shepherd stats                        # charts in the terminal
```

Agents get the contract from `shepherd help`, the bundled skill or
[`AGENTS.md`](AGENTS.md). No server, no MCP.

## docs

- [keys](docs/keys.md): every key, quick-add tokens, the detail view, rebinding
- [boards](docs/boards.md): named boards, the global view, filters, lanes, subtasks
- [command api](docs/command-api.md): every verb, flags, JSON, `watch`
- [agents](docs/agents.md): setting up agents, and safe writes (`--text`, `--if-rev`, `claim`)
- [configuration](docs/configuration.md): `config.toml`, themes, environment
- [storage](docs/storage.md): file layout, format, archive
- [stats](docs/stats.md): `shepherd stats`
- [herdr](docs/herdr.md): pane placement and keybindings
- [contributing](CONTRIBUTING.md)

## support

shepherd is free and MIT licensed. If it keeps your agents and todos in line,
you can [sponsor its development](https://github.com/sponsors/jwarykowski).
