# stats

`shepherd stats` summarises a board as terminal charts: completion, due and
urgency, priority load, aging, status mix, throughput and backlog trend (drawn
with [ntcharts](https://github.com/NimbleMarkets/ntcharts)).

![shepherd stats](../assets/stats.gif)

Done-based counts include the archive, so throughput survives a sweep. Only
top-level items are counted: subtasks don't appear in any chart.

| flag | what |
|------|------|
| `--all` | aggregate every board, and add a by-board breakdown |
| `--json` | the raw numbers instead of charts, for scripts |
| `--legend` | explain every chart and the aging numbers |
| `--no-color` | charts without ANSI colour |

Colour drops itself when stdout isn't a terminal, `$NO_COLOR` is set, or
`$TERM=dumb`; `--no-color` forces it off.

The backlog health line under the charts is the part worth watching: `oldest
open`, `avg open`, `stale >30d` (open items created over 30 days ago) and `time
to done` (mean time from create to complete). A rising `net +N/mo` on the
backlog chart means the board is growing faster than it's being cleared.
