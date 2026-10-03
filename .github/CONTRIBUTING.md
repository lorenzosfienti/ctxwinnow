# Contributing

Thanks for your interest. ctxwinnow is small on purpose; please open an issue before a large change.

## Ground rules

- Go standard library only, nothing newer than the `go` version in `go.mod`.
- Privacy first: read transcripts only, no settings files, no network, never store or print
  transcript contents.
- No real transcripts or reports in issues, pull requests or test fixtures: fixtures are synthetic.

## Before you open a pull request

```bash
gofmt -l .      # must print nothing
go vet ./...
go test ./...
```

If a golden file changes, regenerate it and review every line of the diff:

```bash
go test ./internal/overhead/ -run Golden -update
go test ./internal/ceiling/ -run Golden -update
go test ./cmd/ctxwinnow/ -run TestMissingRoot -update
```

Add a line to the `[Unreleased]` section of `CHANGELOG.md`. The maintainer assigns the version:
a push to `main` with a new `internal/version.Version` tags and publishes the release automatically.

Use conventional commit messages (`feat:`, `fix:`, `docs:`, `ci:` …).

By contributing you agree that your contribution is licensed under the Apache License 2.0.
