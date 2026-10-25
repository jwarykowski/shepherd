# configuration

Optional `config.toml` at `$XDG_CONFIG_HOME/shepherd/config.toml` (defaults to
`~/.config/shepherd/config.toml`), shared across every board (override with
`SHEPHERD_CONFIG`):

```toml
view = "category"                          # category (default) | priority | tag | table | lane
density = "compact"                        # compact (default) | comfort
autosave = 60                              # seconds idle before writing; 0 disables
categories = ["work", "home", "personal"]  # tab-cycles in the category prompt
statuses = ["open", "in-progress", "done"] # tab cycles item status in the list
hidefooter = false                         # true starts with the footer help grid hidden (gf toggles at runtime)
mouse = false                              # true: wheel scrolls, click selects, double-click opens
theme = "ansi"                             # ansi (default) | catppuccin | dracula | gruvbox | nord | mono
# theme.accent = "#88c0d0"                 # optional per-colour overrides (see below)
```

- `view`: default grouping or layout on launch (`v` still cycles at runtime).
- `density`: `comfort` adds outer padding and blank lines between rows.
- `autosave`: idle seconds before an unsaved board is written (default 60); `0`
  disables it, so only `w` and quit save.
- `categories`: `tab` in the category prompt (`c`) cycles through them.
- `statuses`: the ordered list `tab` cycles through; `done` is always kept and
  forced last. Defaults to `["open", "done"]`. Intermediate statuses persist as
  a `status:` line and show a `◐` glyph; [`shepherd stats`](stats.md) breaks
  items down by status in this order.
- `hidefooter`: start with the footer help grid hidden (the
  `jwarykowski/shepherd` · version line stays); `gf` toggles it.
- `mouse`: wheel scrolling, click to select a row or card, double-click for its
  detail. Off by default because it takes over the terminal's own text
  selection (hold `shift` to select while it's on).
- `key.<action>`: rebind a key; see [key bindings](keys.md#key-bindings).

Edit these in the running board with `gs` or `,` (settings): `enter` cycles
`view`/`density`/`footer`/`mouse`/`theme` (previewed live) and edits
`autosave`/`categories`/`statuses`, and each change is written straight back to
`config.toml`. shepherd owns the file: a settings save rewrites the managed
keys.

herdr pane placement (`placement` / `direction`) lives in the same file; see
[herdr](herdr.md).

## themes

`theme` sets the colour palette for the board and `shepherd stats`. `ansi` uses
your terminal's own 16 colours, so it follows your terminal theme. The others
are fixed palettes with light and dark variants, picked from the terminal's
background (and downsampled on 256- or 16-colour terminals). `mono` drops colour
for bold and faint only. `gt` previews each one live.

Override single colours with `theme.<token> = "<colour>"`, on top of whichever
theme is active. Tokens: `accent` (group headers), `ok`, `info`, `warn`,
`rule`, `muted`, `cursor` (row highlight), `prio_high`, `prio_med`,
`prio_low`. A colour is a palette index (`0`-`255`), `#rrggbb`, an ANSI name
(`red`, `bright-blue`, `grey`), or `none`.

## environment

| variable | does |
|----------|------|
| `SHEPHERD_BOARD` | the board to open or act on |
| `SHEPHERD_FILTER` | start the board filtered |
| `SHEPHERD_CONFIG` | a different `config.toml` |
| `SHEPHERD_TODO_FILE` | a different board file altogether (see [storage](storage.md)) |
| `SHEPHERD_PLACEMENT`, `SHEPHERD_DIRECTION` | herdr pane placement (see [herdr](herdr.md)) |
