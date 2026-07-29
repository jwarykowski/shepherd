# contributing

Go plugin, split into `internal/` packages (`cli`, `store`, `todo`, `tui`). No frameworks.

```sh
go build -o bin/shepherd .   # build the shepherd binary
go test ./...             # logic + model + view tests
gofmt -l .                # lint: lists unformatted files (should print nothing)
go vet ./...              # lint: static checks
golangci-lint run         # lint: meta-linter (config in .golangci.yml)
```

`gofmt -w .` fixes formatting. `golangci-lint` is the eslint-equivalent
meta-linter; install it from https://golangci-lint.run. CI
(`.github/workflows/pull-request.yml`) runs all of the above on every pull
request; keep them green.

## demo gifs

The README's `assets/demo.gif` (the board) and `assets/stats.gif` (`shepherd
stats`) are scripted, not screen-captured — regenerate them after any change to
the board layout, the footer, or the charts:

```sh
brew install vhs            # pulls in ttyd + ffmpeg
go install .                # so the recording runs your build, not the released one
vhs assets/demo.tape
vhs assets/stats.tape
```

Both tapes seed the same throwaway board under `mktemp -d` via
`assets/demo-seed.sh`, so recording never touches a real board or config. The
seed writes its dates relative to today — including an archive of completed work
— so the sparkline and backlog trend stay populated whenever the gifs are
re-recorded.

Two things to know before editing a tape: interactive zsh eats the `!h`/`!l`
priority tokens as history expansion (the seed script sidesteps it by writing the
markdown directly), and each recording's height has to clear its tallest screen —
the priority view for the board, 56 rows for `stats`.

## git hooks

Enable the pre-commit hook (gofmt's staged Go files, once per clone):

```sh
git config core.hooksPath .githooks
```
