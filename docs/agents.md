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
