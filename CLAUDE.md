# claude.md

Guidance for agents working in shepherd. Read `CONTRIBUTING.md` (workflow) and
`AGENTS.md` (CLI contract) too — this file is the short must-follow list, not a
replacement.

shepherd is a Bubble Tea TUI todo board (also a herdr plugin), backed by a
markdown file. Go, no frameworks.

## commands

- build: `go build -o bin/shepherd .`
- test: `go test ./...`
- format: `golangci-lint fmt ./...` (gofumpt and goimports; CI fails on unformatted files)
- vet: `go vet ./...`
- lint: `golangci-lint run` (v2, `.golangci.yml`)

Run format + vet + test + lint before proposing changes done — CI
(`.github/workflows/pull-request.yml`) runs all of them on every PR and push to
`master`. Keep them green. Optional local hook: `git config core.hooksPath
.githooks`.

## architecture

`internal/` packages, dependencies pointing strictly inward:
`todo` (pure domain, no I/O) → `store` (markdown persistence) → `cli` +
`tui` (frontends) → `main`. `theme` (colour palettes, pure) sits beside
`todo` and is used by both frontends; `testenv` is imported only by tests.
Each package doc comment states its job and dependency direction; keep that
true.

TUI is split by file: `model.go` (the `model` struct + all state, config
load/save), `update.go` (`Update`, the list/global/input handlers),
`update_boards.go` / `update_settings.go` / `update_detail.go` (those modes'
handlers), `keymap.go` (the `keys` bindings), `view.go` (`View` and `xxxView`
renderers). Bubble Tea v2: `View` wraps `render() string`, which tests assert
on. `Update` type-switches the msg, then switches on `m.mode`; text-entry modes
share `updateInput`. Disk polling runs in a `Cmd` (`checkDisk` -> `diskMsg`).

Receivers on `model`: **value** for read-only helpers and Bubble Tea methods,
**pointer** for in-place mutators (`clamp`, `beforeMutate`, `resort`, …).

## adding a mode or key binding

A new mode touches ~6 spots: the `modeXxx` iota (`model.go`), a dispatch case
in `Update` (`update.go`), `updateXxx` + `enterXxx`, a `View` case + `xxxView`
(`view.go`), the footer/hint (`listFooter`), the help grid/body
(`helpGrid`/`helpBody`), and a `drive()` test. A new key on the list, global
or detail view = a field on `keyMap` with its default in `defaultKeys` (its
config name, `key.<snake_case>`, comes from the field name) + a
`binding.Matches` case in the right `updateXxx` + an entry in its grid
(`helpGrid`/`detailGrid`) and `helpBody` (read the label from the binding,
never hardcode it) + the key table and action list in `docs/keys.md` + a test. Users
rebind keys, so handlers must match bindings, not literal strings. Keys are
mnemonic (`d` due, `t` tags, `c` category); rare screen-level actions go under
the `g` sequence (`gb` boards, `gs` settings). Keep bindings consistent: `esc`
backs out, `q`/`ctrl+c` quit, `space` toggles, `enter` activates — one key per
action, and `TestKeymapNoClashes` holds the defaults to that.

## invariants — do not break

- The global aggregate view is read-only; never write it back to disk.
- Call `m.beforeMutate()` before any list mutation (undo snapshot).
- `cursor` indexes visible rows; call `clamp()` after anything changes the row
  count. Use `todo.Clone` for snapshots and `sameItem` for identity — `Item` is
  non-comparable.
- Subtask cascade and status normalization live only in `todo`; don't
  reimplement them in `cli`/`tui`.
- The binary owns the todo-file format. Never hand-edit board markdown; go
  through the CLI/store.

## errors

TUI drops save errors on purpose (`_ = store.Save(...)`) — no actionable
mid-render recovery. CLI surfaces errors to stderr and returns exit 1
(operational) or 2 (usage). Visible board ops (rename/create/archive) put the
error in `m.projNotice`.

## testing

Drive the TUI headless with `key()`/`drive()` then assert on model fields or
`m.View()`. Pin time via `todo.Now`/`todo.Today` (`pinToday` helper). Isolate
the filesystem with `t.TempDir()` + `t.Setenv`. Every test package's
`TestMain` runs through `testenv.Run`, which points `HOME` at a temp dir and
clears `SHEPHERD_*`/`XDG_CONFIG_HOME`, so a test can never write the user's
real config or boards; a new test package needs the same `TestMain`. Capture CLI output through an
injected `io.Writer`. Table-driven for pure logic; scenario-style for stateful
flows. Every non-trivial change ships a test.

## versioning & commits

Version lives once in `herdr-plugin.toml` (`version = "x.y.z"`), embedded into
the binary at build. Bump it with a `chore: bump version to x.y.z` commit in a
PR. Never tag by hand and never make a GitHub Release: when the bump lands, the
release workflow (`.github/workflows/release.yml`) has
`.github/scripts/release.sh` tag the commit `vX.Y.Z` and wait for the push
mirror to carry it to GitHub, then GoReleaser (`.goreleaser.yaml`) publishes
the binaries as a GitHub release and opens a `jwarykowski/homebrew-tap` PR with
the formula, which the workflow merges (needs the `GH_RELEASE_TOKEN` secret on
Forgejo: a GitHub token with write access to shepherd and the tap).

- `master`: stable versions (`0.21.0`) → `Formula/shepherd.rb`
- `pro`: pre-releases (`0.21.0-alpha.2`) → `Formula/shepherd-alpha.rb`

A pre-release version on `master`, or a stable one on `pro`, releases nothing.
`pro` is the integration branch for the agents/Pro work under test: branch
those features off `pro` and PR into it; it merges into `master` when stable.

Conventional Commits; PR-merge commits carry the trailing `(#NN)`. Branch
`feat/…`, `fix/…`, `refactor/…`, `chore/…`, `docs/…` off `master` (or off `pro`
for agents/Pro work). (Global
rules already apply: `--no-gpg-sign`, no `Co-Authored-By`, never commit on the
default branch, commit only when asked.) `origin` is the Forgejo repo
(`code.warykowski.io/jwarykowski/shepherd`) and runs CI and releases; GitHub
(release host) and Codeberg mirror it.
