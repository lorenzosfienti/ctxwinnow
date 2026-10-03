# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.1] - 2026-10-03

### Changed
- Release workflow releases itself: a push to `main` whose `internal/version.Version` has no
  GitHub release yet creates the `vX.Y.Z` tag after vet, tests and the build pass, then publishes
  the archives and notes. A pushed tag or a manual run does the same, an already released version
  is a no-op, and an existing tag is built as is, never moved. The `v0.2.0` tag push did not start
  the old tag-only workflow, so that release was built by hand with the same steps.

## [0.2.0] - 2026-09-27

### Added
- `ctxwinnow overhead`: offline, read-only audit of the fixed per-call context baseline B of Claude
  Code sessions. B is exact (first-call usage); the report gives the pooled and typical-session
  token-turns share, a subagent row, an estimated component breakdown (tool JSON at 4.5 bytes per
  token, the rest of B shared by bytes), the cost of tools carried but never called, and skill
  listing, MCP and instruction-file tables.
- Suggested levers from a static catalog (Artifact, SendFeedback, Workflow, skill visibility,
  instruction files, MCP), each marked VERIFIED or UNVERIFIED, with user- and project-scope savings
  from the user's own sessions, usage, observed presence and copy-paste snippets for User, Local,
  Env or Flag scope (never a shared Project scope). Only levers worth at least 0.5% of main
  token-turns are printed, and savings are never added up.
- `--compare T`: before/after report with a paired per-project delta, an unpaired delta with a
  deterministic noise floor, removed and appeared components with predicted tokens, tool drift
  warnings and the counterfactual saving.
- `--since` / `--until` (`YYYY-MM-DD` or RFC 3339), `--min-turns`, `--only`, `--exclude`,
  `--redact` (allowlist redaction with per-run labels, no hashes) and `-o`.
- `-h`, `--help` and `help` print the usage and exit 0.
- Release workflow: a `vX.Y.Z` tag builds archives for macOS, Linux and Windows (amd64, arm64)
  with `SHA256SUMS` and publishes the CHANGELOG section as release notes.
- CI matrix: Ubuntu, macOS and Windows × the `go.mod` Go version and the latest stable; on `main`
  CI also checks that CHANGELOG.md has a heading for the current version.

### Changed
- `compress/` moved to `internal/policy/` (it was never a public API).
- The transcript reader also extracts size-only records (tool snapshot bytes, component attachments
  before the first call, tool and skill usage); contents are measured and dropped, never stored.
- Both commands find transcripts in `$CLAUDE_CONFIG_DIR/projects`, then `~/.claude/projects`; a
  missing root lists every path tried and exits 1; unreadable or vanished files are skipped,
  warned on stderr and counted instead of failing the run.
- `go.mod` targets Go 1.26 (`go 1.26.0`); `.gitattributes` keeps LF line endings so golden files
  match on every OS.

### Fixed
- `ctxwinnow analyze` token estimates are calibrated ×1.75 (`contentTokensPerEstimate`, the median
  ratio against exact usage); the report states it.
- `--only`, `--exclude` and `--group` match at path boundaries: `/Users/a/app` no longer matches
  `/Users/a/app-legacy`.
- A transcripts root that is itself a symlink (for example a `~/.claude/projects` moved to another
  disk) is scanned like its target; both commands used to find 0 sessions there.
- A transcripts root that exists but cannot be used (not a directory, permission denied) prints the
  cause after the paths tried instead of only "no transcripts directory found".
- The `overhead` Limitations section labels its fixed figures (API-weighted share, cache reads, pending
  MCP names) as measured on the author's sessions; "owner data" read as the reader's own data.
- Local snippets say "keep it gitignored": Claude Code gitignores `.claude/settings.local.json` only
  when it creates the file, and a committed copy would apply to every collaborator.

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
