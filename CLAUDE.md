# ctxwinnow

Offline, read-only auditor of what Claude Code sends on every call. `ctxwinnow overhead` measures the fixed
per-call context baseline B from the user's own transcripts, estimates what it is made of, ranks what is paid for
and never used, suggests catalogued config levers, and proves a change with `--compare`. Single Go binary,
standard library only. Portfolio project.

## Status

- v0.2.2 (2026-10-03): open-source hardening (security policy, community files, pinned Actions, govulncheck,
  protected `main` and `v*` tags, release built with stable Go).
- v0.2.1 (2026-10-03): self-releasing workflow on push to `main` (no manual tag).
- v0.2.0 (published 2026-10-03, github.com/lorenzosfienti/ctxwinnow): `ctxwinnow overhead` with levers and
  `--compare`; `analyze` calibration fix; CI matrix; tag-driven release workflow. Acceptance passed: live run on
  `~/.claude/projects` and `--compare` after the Artifact change (2026-10-03: paired delta -9,871 tokens, noise
  floor ±3,790, Artifact removed).
- v0.1.0: step 0 (`ctxwinnow analyze`), verdict RETHINK on 2026-09-26 (median compressible share 0.0%). The
  compression engine and proxy are dropped.

## Commands

`go test ./...` · `gofmt -l .` (must print nothing) · `go vet ./...` · golden files:
`go test ./internal/overhead/ -run Golden -update`, `go test ./internal/ceiling/ -run Golden -update`,
`go test ./cmd/ctxwinnow/ -run TestMissingRoot -update` (review every diff).

## Layout

- `internal/transcript/`: the only transcript parser; streams each JSONL file once, measures content strings and
  drops them; shared path-boundary matcher (`UnderPath`, `UnderAny`).
- `internal/overhead/`: `overhead` — inventory (per-session B and attribution), aggregate, levers, compare,
  redact, report.
- `internal/ceiling/`: `analyze` (step 0) and its report.
- `internal/policy/`: the step-0 compression gate (`Policy`, `BashHead`), kept for `analyze`.
- `internal/version/`: the single version source.
- `cmd/ctxwinnow/`: CLI (`overhead`, `analyze`, `--version`, help), root resolution and the fault-tolerant walker.

## Rules

- Standard library only; nothing newer than Go 1.26 (`go.mod` says `go 1.26.0`; `go vet` checks it).
- The version lives only in `internal/version.Version`. Bump it together with `CHANGELOG.md`, and never leave
  entries under `[Unreleased]` (a test and CI enforce the match).
- `main` is protected: changes land only through a squash-merged pull request with green CI (the six `test` jobs
  and `govulncheck`); no direct push, force push or deletion. Work on a branch, open a PR, merge it.
- Releases are automatic: bump `internal/version.Version` and add the `## [X.Y.Z] - YYYY-MM-DD` heading in the PR;
  when it is merged into `main` the release workflow tags `vX.Y.Z` after vet, tests and build pass, and publishes the archives (no manual
  tag; a released version is a no-op). Never move or delete a pushed tag (the Go proxy and checksum database keep the
  first content): fix forward with a patch release.
- No real transcripts or reports in the repo: `reports/` and `dist/` are gitignored and fixtures are synthetic.
- Docs stay minimal: README, CHANGELOG and this file at the root; community and security files live in `.github/`
  (SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, issue and PR templates). `docs/superpowers/` holds local specs and plans and is
  gitignored on purpose.
- Commits use conventional messages and carry no AI attribution.

## Design decisions that must survive

- B is the context of the first usage (input + cache read + cache creation): exact. Usages are de-duplicated per
  message id (last line wins), context-0 usages dropped, and ids seen in an earlier file dropped (continuations).
- Components are estimates (≈) from a two-rate attribution: tool JSON at `toolJSONBytesPerToken` = 4.5 (band
  4.0–4.8, gate 0), the rest of B shared by bytes so components sum to B; sanity guard [2, 6] bytes per token.
  Tool bytes are the raw JSON length as stored, never re-marshalled.
- Privacy: read transcripts only, no settings files, no network; never store or print contents. `--redact` is an
  allowlist with per-run labels (no hashes); unknown names are redacted.
- Levers: explicit catalog targets only ("no known safe lever" otherwise); snippets in User, Local, Env or Flag
  scope, never a shared Project scope; printed only at ≥ 0.5% of main token-turns; never a total across levers;
  presence reflects past launches and is never reported as "applied".
- `--compare`: the paired per-project delta is primary; the unpaired delta carries a deterministic noise floor
  (fixed-seed `math/rand/v2`); empty or one-session sides print "n/a", never NaN.
- Percentiles are nearest-rank; timestamps are compared as instants; paths are printed as written, never cleaned,
  and golden files are LF (`.gitattributes`) so they match on every OS.
