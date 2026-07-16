# claude.md

Guidance for agents working in shepherd. Read `CONTRIBUTING.md` (workflow) and
`AGENTS.md` (CLI contract) too — this file is the short must-follow list, not a
replacement.

shepherd is a Bubble Tea TUI todo board (also a herdr plugin), backed by a
markdown file. Go, no frameworks.

## commands

- build: `go build -o bin/shepherd .`
- test: `go test ./...`
- format: `gofmt -w .` (CI fails if `gofmt -l .` is non-empty)
- vet: `go vet ./...`
- lint: `golangci-lint run`

Run format + vet + test + lint before proposing changes done — CI
(`.github/workflows/pull-request.yml`) runs all of them on every PR and push to
`master`. Keep them green. Optional local hook: `git config core.hooksPath
.githooks`.

## architecture

`internal/` packages, dependencies pointing strictly inward:
`todo` (pure domain, no I/O) → `store` (markdown persistence) → `cli` +
`tui` (frontends) → `main`. Each package doc comment states its job and
dependency direction; keep that true.

TUI is split by file: `model.go` (the `model` struct + all state, config
load/save), `update.go` (`Update` and per-mode `updateXxx` handlers),
`view.go` (`View` and `xxxView` renderers). `Update` type-switches the msg,
then switches on `m.mode`; text-entry modes share `updateInput`.

Receivers on `model`: **value** for read-only helpers and Bubble Tea methods,
**pointer** for in-place mutators (`clamp`, `beforeMutate`, `resort`, …).

## adding a mode or key binding

A new mode touches ~6 spots: the `modeXxx` iota (`model.go`), a dispatch case
in `Update` (`update.go`), `updateXxx` + `enterXxx`, a `View` case + `xxxView`
(`view.go`), the footer/hint (`listFooter`), the help grid/body
(`helpGrid`/`helpBody`), and a `drive()` test. A new key = a case in the right
`updateXxx` + a `helpGrid` entry + a test. Keep bindings consistent: `esc`
backs out, `q`/`ctrl+c` quit, `space` toggles, `enter` activates — one key per
action.

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
the filesystem with `t.TempDir()` + `t.Setenv`. Capture CLI output through an
injected `io.Writer`. Table-driven for pure logic; scenario-style for stateful
flows. Every non-trivial change ships a test.

## versioning & commits

Version lives once in `herdr-plugin.toml` (`version = "x.y.z"`), embedded into
the binary at build. Bump it with a `chore: bump version to x.y.z` commit; tag
`vX.Y.Z`.

Conventional Commits; PR-merge commits carry the trailing `(#NN)`. Branch
`feat/…`, `fix/…`, `refactor/…`, `chore/…`, `docs/…` off `master`. (Global
rules already apply: `--no-gpg-sign`, no `Co-Authored-By`, never commit on the
default branch, commit only when asked.) Remote pushes to both GitHub
(primary, CI) and Codeberg (mirror).
