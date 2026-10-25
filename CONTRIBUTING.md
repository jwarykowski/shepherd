# contributing

Go plugin, split into `internal/` packages (`cli`, `store`, `todo`, `tui`). No frameworks.

```sh
go build -o bin/shepherd .   # build the shepherd binary
go test ./...             # logic + model + view tests
go vet ./...              # lint: static checks
golangci-lint run         # lint and format check (config in .golangci.yml)
```

`golangci-lint fmt ./...` fixes formatting (gofumpt and goimports).
`golangci-lint` v2 is the eslint-equivalent meta-linter; install it from
https://golangci-lint.run. CI
(`.github/workflows/pull-request.yml`) runs all of the above on every pull
request; keep them green.

## demo gifs

The README's `assets/demo.gif` (the board) and `docs/stats.md`'s `assets/stats.gif`
(`shepherd stats`) are scripted, not screen-captured — regenerate them after any change to
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

Enable the pre-commit hook (formats staged Go files, once per clone):

```sh
git config core.hooksPath .githooks
```

## Releasing

Bump `version` in `herdr-plugin.toml` in a pull request. When it lands, the
release workflow (Forgejo Actions) tags the commit `v<version>`, waits for the
push mirror to carry the tag to GitHub, then GoReleaser publishes the binaries
as a GitHub release and opens a Homebrew formula pull request, which the
workflow merges: stable versions (`0.21.0`) from `master` update `shepherd`, and
pre-releases (`0.21.0-alpha.2`) from `pro` update `shepherd-alpha`. The workflow
needs the `GH_RELEASE_TOKEN` secret on Forgejo, a GitHub token with write access
to `jwarykowski/shepherd` and `jwarykowski/homebrew-tap`.
