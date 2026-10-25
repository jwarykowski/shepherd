# agents

The [command api](command-api.md) is the whole integration: any agent that can
run a shell drives shepherd with the same verbs. Point it at the contract:

- Any tool: `shepherd help` prints the contract. Nothing else is required.
- Claude Code: symlink the bundled skill once (from a checkout), and Claude uses
  it in every project when a request is about your todos:
  ```sh
  ln -s "$PWD/skills/shepherd" ~/.claude/skills/shepherd
  ```
- Cursor, Codex, Zed and others: paste [`AGENTS.md`](../AGENTS.md) into the
  tool's global rules or instructions.

All three point at the same `shepherd` CLI: no per-tool server, no MCP.

## safe writes

Three features keep an agent from mangling text or overwriting a change it
hasn't seen.

Literal text: `--text "<s>"` on `add`, `sub` and `edit` sets the item text
verbatim, so `#123`, `@home` or `due:` inside it stay text. The positional
string then holds tokens only; a plain word there alongside `--text` is a usage
error (exit 2) rather than silently dropped.

```sh
shepherd add --text "fix #123 crash due: soon" "@work !h"
```

Revisions: every item in `--json` output carries a `rev`, a short hash of the
item as stored (subtasks included). It's computed on read, never written to the
board. `--if-rev <rev>` on `edit`, `sub`, `claim`, `done`, `undone`, `rm` and
`archive` writes only if the item is unchanged since that rev; otherwise it
exits 1 and writes nothing (`{"error":"conflict"}` under `--json`). Any change
from the board or another agent changes the rev, so on a conflict re-read and
decide again.

```sh
shepherd list --json                       # note the item's id and rev
shepherd done <id> --if-rev 6d6b662e659f   # exit 1 if someone changed it
```

Claims: `claim <ref> --by <name>` marks an item as taken. It records a
`claimed: <name>` line and moves the item to the first configured status that
isn't the default or `done` (`in-progress` by default). It exits 1 if the item
is done or claimed by someone else; claiming your own item again succeeds.
`claim <ref> --release --by <name>` hands it back (status returns to the
default); leaving out `--by` forces the release. Completing an item ends its
claim. The board shows the claimer after the item's text, and the detail view
has a `claimed` row.

```sh
shepherd claim 3 --by agent-1 || echo "taken, pick another"
shepherd claim 3 --release --by agent-1
```

## send tasks to your agent (Shepherd Pro)

With [Shepherd Pro](https://shepherdcli.dev/pro), Shepherd hands tasks to your
coding agent (Claude Code or Codex), keeps its log, and waits for you to say go.
Agents run with drover, installed alongside:

```sh
brew install jwarykowski/tap/drover
shepherd pro activate <key>        # your licence key, checked on your machine
shepherd agents setup              # finds claude or codex, sets up every board, starts in the background
shepherd board dir web ~/src/web   # where the agent works for the web board
```

Then add a task and press `r` (run) on it (or `shepherd go <ref>`). Rows show where
each task stands:

| mark | means |
|---|---|
| waiting | a run is ready and waits for your go |
| queued | you said go; it starts when an agent is free |
| running | your agent is working on it |
| failed | the agent couldn't finish; `r` retries |
| review | a run finished; check its work, then tick the task |

`r` on a waiting task releases it, on a failed one retries it, and on any other
open task starts a run now. A subtask goes with its parent, so the agent knows
what it's part of. Each run works in its own git worktree and branch, so your
checkout is never touched. In a repository with a remote, the agent opens a
pull request.

`gr` opens the agents view: every run on every board and source, one row per
thread (the runs of one job about one thing, which share a branch and a
conversation), grouped by state, with subtasks under their parent. Its keys:
`r` go or retry, `l` the run's log, `o` its pull request or issue, `t` take over
its conversation in your terminal (in the run's worktree), `enter` its task,
`X` archive a run, `C` clear every finished run into the archive, `e` the
archive, `esc` back. The detail view lists a task's runs, and `gl` opens the
latest one's log. If agents aren't running, the board says so.

```sh
shepherd agents status    # running in the background?
shepherd agents stop | start
shepherd pro status
```

Tasks can also come from GitHub issues and from any service that sends
webhooks. Run these in the repository the agent should work in (or pass
`--dir`):

```sh
shepherd agents add github acme/api      # open issues labelled agent
shepherd agents add webhook sentry       # prints a URL to paste into Sentry
shepherd agents sources                  # list them; agents remove <name> to stop one
shepherd agents secret sentry            # take only deliveries Sentry signed
shepherd agents actions                  # what each kind of event does
shepherd agents action "sentry fix" off  # or on, or edit to change its prompt
```

A retry, or a later event about the same thing (a review on the pull request an
agent opened), carries on the same conversation on the same branch, and the
agent is told what earlier runs did. A thread ends when its pull request merges
or closes, or after 14 quiet days.

Webhook services: `sentry`, `linear` (issues labelled agent), `datadog`,
`alertmanager` (Grafana, Prometheus), or `webhook` for anything else. The
service posts to a private URL on shepherdcli.dev that holds each delivery
until your agents collect it, so nothing has to reach your computer. Each one
is encrypted as it arrives to a key only your computer holds, so the relay
can't read what it keeps. Sentry and Linear sign what they send: give
`agents secret` their signing secret and the URL refuses anything else. Tasks
from these sources wait in the agents view, flagged external, until you say go.

drover's config (`~/.config/drover/drover.toml`) is where you change what the
agent is told to do.
