# herdr

shepherd runs as a [herdr](https://herdr.dev) plugin, in a `split`, `tab`,
`overlay` or `zoomed` pane.

## placement

| action | placement |
|--------|-----------|
| `open` | from config (`split` if unset) |
| `open-split` | split |
| `open-tab` | tab |
| `open-overlay` | overlay |
| `open-zoomed` | zoomed |

`open` resolves placement from (highest first): env
`SHEPHERD_PLACEMENT`/`SHEPHERD_DIRECTION`, then `config.toml`, then
`split`/`right`. `direction` (`right`/`left`/`up`/`down`) applies to `split`
only. The other actions force one placement regardless of config.

Set the defaults in the shared `~/.config/shepherd/config.toml`:

```toml
placement = "split"     # split (default) | tab | overlay | zoomed
direction = "right"     # split only: right (default) | left | up | down
```

For a pane on a named board, see [boards](boards.md#named-boards).

## keybindings

```toml
[[keys.command]]
key = "prefix+shift+o"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open"          # placement from config
description = "shepherd"

[[keys.command]]
key = "prefix+shift+t"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open-tab"
description = "shepherd (tab)"

[[keys.command]]
key = "prefix+shift+y"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open-overlay"
description = "shepherd (overlay)"

[[keys.command]]
key = "prefix+shift+z"
type = "plugin_action"
command = "jwarykowski.herdr-shepherd.open-zoomed"
description = "shepherd (zoomed)"
```
