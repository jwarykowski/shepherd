#!/usr/bin/env bash
# Open the Shepherd board. Placement/direction resolved from (highest first):
#   $1 / $2 args  >  SHEPHERD_PLACEMENT / SHEPHERD_DIRECTION env  >  config.toml  >  defaults.
set -euo pipefail

herdr_bin="${HERDR_BIN_PATH:-herdr}"

# Same config path the board binary uses.
cfg="${SHEPHERD_CONFIG:-${HERDR_PLUGIN_STATE_DIR:-$HOME/.config/shepherd}/config.toml}"

# ponytail: grep one key out of the tiny TOML instead of a parser. Strips quotes.
# Must return 0 even when the file/key is missing, else `set -e` kills the
# assignment `placement=$(read_cfg ...)` and no pane opens.
read_cfg() {
  [ -f "$cfg" ] || return 0
  sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*[\"']\{0,1\}\([^\"']*\)[\"']\{0,1\}.*/\1/p" "$cfg" | head -1
}

placement="${1:-${SHEPHERD_PLACEMENT:-$(read_cfg placement)}}"
direction="${2:-${SHEPHERD_DIRECTION:-$(read_cfg direction)}}"
placement="${placement:-split}"
direction="${direction:-right}"

args=(--plugin jwarykowski.herdr-shepherd --entrypoint shepherd --placement "$placement")
# direction only applies to split panes.
[ "$placement" = "split" ] && args+=(--direction "$direction")

exec "$herdr_bin" plugin pane open "${args[@]}"
