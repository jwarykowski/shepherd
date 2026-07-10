#!/usr/bin/env bash
# Open the Shepherd board in a split pane.
set -euo pipefail

herdr_bin="${HERDR_BIN_PATH:-herdr}"
exec "$herdr_bin" plugin pane open \
  --plugin jwarykowski.herdr-shepherd \
  --entrypoint board \
  --placement split \
  --direction right
