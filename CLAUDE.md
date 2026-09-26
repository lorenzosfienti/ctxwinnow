# ctxwinnow

Go tool that compresses tool output before it reaches the LLM. Like headroom, but lossless-first, no ML, a single
binary, and every cut is explained. Portfolio project.

## Status

- v0.1.0: step 0 (`ctxwinnow analyze`) shipped. Real-data verdict on 2026-09-26: RETHINK (median 0.0%).
- Next: the engine, only if step 0 reports GO; then the Anthropic proxy.

## Commands

`go test ./...` · `gofmt -l .` (must print nothing) · `go vet ./...`

## Layout

- `compress/`: the engine. Step 0 ships only `Policy`, which is permanent code.
- `internal/transcript/`: Claude Code transcript reader.
- `internal/ceiling/`: step 0 analysis and markdown report.
- `cmd/ctxwinnow/`: the CLI.

## Rules

- Standard library only.
- The version lives only in `internal/version.Version`. Bump it together with `CHANGELOG.md`, and never leave
  entries under `[Unreleased]`.
- No real transcripts or reports in the repo: `reports/` is gitignored and fixtures are synthetic.
- Docs stay minimal: README, CHANGELOG and this file. `docs/superpowers/` holds local specs and plans and is
  gitignored on purpose.
- Commits use conventional messages and carry no AI attribution.

## Design decisions that must survive

- Compress only `Bash` and `mcp__*` output. Read, Grep, Edit and every other tool pass through by name. Bash
  reads, searches and failed commands pass through too, because Edit anchors must stay byte-exact.
- Lossless first: strip ANSI and `\r` redraws, compact JSON, fold duplicate lines. Cut content only if the output
  is still over budget. The only kinds are `json` and `lines`; prose and code are never cut.
- Proxy (later): compress only the tool_results in the newest message, and replay every earlier one byte for byte
  by `tool_use_id`. Never touch `system`, `tools` or `cache_control`: an edited prefix is an HTTP 400 on Opus 5.5
  and on accounts created on or after 2026-08-31.
- Every cut is logged. Markers give exact counts and the path of the spilled original.
- headroom is already query-aware (BM25), so query-awareness is not a differentiator. BM25 stays optional unless
  the benchmark justifies it.
