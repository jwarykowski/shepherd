#!/bin/sh
# Seeds a throwaway board plus its archive for the vhs recordings
# (assets/demo.tape, assets/stats.tape). $SHEPHERD_TODO_FILE must already point
# at a scratch todo.md — the tapes set it under `mktemp -d`.
#
# Dates are written relative to today, so `shepherd stats` still has a populated
# sparkline and backlog trend whenever the gifs get re-recorded.
set -e

board=$SHEPHERD_TODO_FILE
archive=$(dirname "$board")/archive.md

# day offsets: dmy for created/completed timestamps, iso for due dates
ago() { date -v-"$1"d +%d-%m-%Y 2>/dev/null || date -d "$1 days ago" +%d-%m-%Y; }
in_days() { date -v+"$1"d +%Y-%m-%d 2>/dev/null || date -d "$1 days" +%Y-%m-%d; }

cat >"$board" <<EOF
- [ ] (H) cut the 1.0 release tag
  created: $(ago 2) 09:14
  due: $(in_days 1)
  category: release
  - [ ] tag the commit
    created: $(ago 2) 09:15
  - [ ] push the tap bump
    created: $(ago 2) 09:15
- [ ] verify the signed artefacts
  created: $(ago 3) 16:02
  category: release
  status: in-progress
- [ ] document the config file
  created: $(ago 6) 11:30
  due: $(in_days 7)
  category: docs
- [ ] (L) port the key table to a grid
  created: $(ago 11) 14:47
  category: docs
- [ ] (H) fix the flaky lock test
  created: $(ago 38) 08:21
  category: bugs
- [ ] reply on the mailing list thread
  created: $(ago 1) 20:05
EOF

# Completed work lives in the archive: stats counts it (so the done/day
# sparkline and the backlog trend have something to draw) while the board itself
# stays short enough to fit the recording.
cat >"$archive" <<EOF
- [x] (H) ship the flock sidecar
  created: $(ago 26) 09:00
  completed: $(ago 24) 17:40
  category: release
- [x] round-trip the markdown byte for byte
  created: $(ago 25) 10:15
  completed: $(ago 22) 11:05
  category: release
- [x] (H) atomic save via temp file rename
  created: $(ago 22) 09:30
  completed: $(ago 21) 15:20
  category: release
- [x] document the quick-add tokens
  created: $(ago 20) 13:00
  completed: $(ago 18) 09:45
  category: docs
- [x] (L) downcase the headings
  created: $(ago 19) 08:10
  completed: $(ago 18) 08:55
  category: docs
- [x] group the footer keys into columns
  created: $(ago 17) 14:20
  completed: $(ago 12) 16:30
  category: docs
- [x] (H) subtask status cycling
  created: $(ago 15) 09:05
  completed: $(ago 9) 18:10
  category: release
- [x] board rename and archive
  created: $(ago 12) 11:40
  completed: $(ago 7) 10:25
  category: release
- [x] tag view grouping
  created: $(ago 9) 15:15
  completed: $(ago 4) 12:00
  category: release
- [x] (M) inherit the cursor row's category
  created: $(ago 5) 09:50
  completed: $(ago 2) 14:35
  category: release
- [x] correct the README key table
  created: $(ago 3) 10:05
  completed: $(ago 1) 09:30
  category: docs
EOF
