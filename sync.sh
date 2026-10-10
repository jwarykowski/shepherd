#!/usr/bin/env bash
# Keep `shepherd sync --all` running in the background for the herdr session,
# so items handed to agents (shepherd agent) complete once their branch's pull
# request merges. Started by the plugin's startup hook; a pidfile + mkdir lock
# keep it to one loop across herdr restarts and overlapping starts. Needs gh.
set -euo pipefail

state_dir="${HERDR_PLUGIN_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/herdr-shepherd}"
pidfile="$state_dir/sync.pid"

command -v gh >/dev/null 2>&1 || exit 0
mkdir -p "$state_dir"
mkdir "$state_dir/sync.lock" 2>/dev/null || exit 0
trap 'rmdir "$state_dir/sync.lock"' EXIT

if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
  exit 0
fi
nohup bin/shepherd sync --all --every 2m >"$state_dir/sync.log" 2>&1 &
echo $! >"$pidfile"
