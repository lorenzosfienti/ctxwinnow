# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-09-26

### Added
- `ctxwinnow analyze`: offline compression-ceiling report on Claude Code transcripts, with
  GO / GREY / RETHINK / NO DATA verdict, per-group breakdown (`--group`), filters
  (`--only`, `--exclude`), `--min-turns` and `-o`.
- `compress.Policy`: compression gate by tool name and Bash command (reads, searches and
  failures pass through), with a dependency-free Bash lexer.
- Streaming transcript reader, lossless-floor estimate and markdown report.
- CI: gofmt, go vet and go test on every push.

### Fixed
- `compress.Policy` now also detects read and search verbs inside compound Bash commands
  (`for`/`while`/`if`/subshells/brace groups), not only at the top level of the command.
- `ctxwinnow analyze` now rejects a stray positional argument with exit code 2 instead of
  silently ignoring flags that follow it; `-h`/`--help` now exits 0.
- Subagent-session detection now matches the transcript path relative to `--root`, instead of
  the absolute filesystem path, which could misclassify sessions under a root that itself sits
  inside a directory named `subagents`.

## [0.0.1] - 2026-09-26

### Added
- README, CLAUDE.md (agent guide) and changelog.
