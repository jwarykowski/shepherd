# Contributing

Single-package Go plugin. No frameworks.

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

## Git hooks

Enable the pre-commit hook (gofmt's staged Go files, once per clone):

```sh
git config core.hooksPath .githooks
```
