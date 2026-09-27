# ctxwinnow

Separate the grain from the chaff in what your AI coding agent reads.

ctxwinnow is an offline, read-only auditor for Claude Code, written in Go as a single binary with no dependencies.
`ctxwinnow overhead` answers three questions from your own transcripts:

- **What does my fixed per-call context cost?** Every call re-reads a baseline B: system prompt, tool definitions,
  CLAUDE.md files, the skill listing, MCP notes, harness reminders and the first prompt. B is exact (the first
  call's token usage); the report shows it per session and as a share of all input read ("token-turns").
- **What do I pay for and never use?** B is split into components (estimated, marked ≈), and tools carried in every
  call but never called are ranked by the input they cost.
- **Did my change help?** `--compare` splits your sessions at the moment you changed a setting and measures the
  difference, with a noise floor.

It suggests configuration levers only from an explicit catalog, as copy-paste snippets for your user or local
settings, each marked VERIFIED (measured on a Claude Code version) or UNVERIFIED (documented, not measured).

## Install

With Go 1.26 or newer (the binary lands in `$(go env GOPATH)/bin`, which must be on your `PATH`):

```bash
go install github.com/lorenzosfienti/ctxwinnow/cmd/ctxwinnow@latest
```

Or download an archive for macOS, Linux or Windows (amd64, arm64) from the GitHub release, verify it and extract it:

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing
tar -xzf ctxwinnow_0.2.0_darwin_arm64.tar.gz ctxwinnow
xattr -d com.apple.quarantine ctxwinnow   # macOS, only for files downloaded with a browser
```

## Usage

```
ctxwinnow overhead [--root DIR] [--since T] [--until T] [--min-turns N] [--compare T]
                   [--only PREFIX]... [--exclude PREFIX]... [--redact] [-o FILE]
ctxwinnow analyze  [--root DIR] [--only PREFIX]... [--exclude PREFIX]... [--group LABEL=PREFIX]...
                   [--min-turns N] [-o FILE]
ctxwinnow --version | -h | --help | help
```

```bash
ctxwinnow overhead -o overhead.md                          # every session Claude Code still keeps
ctxwinnow overhead --since 2026-09-01 --only ~/code/app    # one project, one window
ctxwinnow overhead --compare 2026-09-27T14:00:00Z          # before/after a settings change
```

- `--root` defaults to `$CLAUDE_CONFIG_DIR/projects`, else `~/.claude/projects`; subagent transcripts are included.
- `--since` / `--until` keep sessions whose first timestamp is in [since, until). T is `YYYY-MM-DD` (UTC midnight)
  or an RFC 3339 instant; the report prints the resolved instants.
- `--min-turns` (default 20): a session counts only with at least N calls that carry token usage.
- `--only` / `--exclude` match the session's working directory at path boundaries (`/a/app` never matches
  `/a/app-legacy`).
- Exit codes: 0 success (also "not enough data" and help), 1 missing or unreadable transcripts directory or output
  file, 2 usage error. Unreadable files are skipped, warned and counted on stderr (and in the `overhead` header).

### Measuring a change

1. Apply the change, for example `"enableArtifact": false` in `~/.claude/settings.json`, and note the time.
2. Keep working until at least 20 new sessions exist (the report still prints with fewer, with a warning).
3. Run `ctxwinnow overhead --compare <that time>`. The primary figure is the paired delta: per project, median B
   after minus median B before, then the median over projects. The unpaired delta comes with a noise floor:
   differences smaller than it are indistinguishable from session mix.

Claude Code deletes transcripts after `cleanupPeriodDays` (default 30): run `--compare` inside that window, or raise
`cleanupPeriodDays` before a change you want to measure. ctxwinnow cannot see deleted sessions.

## Privacy

ctxwinnow reads transcripts read-only, reads no settings file, makes no network call and writes only the report.
File contents, prompts, hook text, system prompts, skill and tool descriptions and tool output are measured and
dropped, never stored or printed. The report contains names, sizes, counts, dates, instruction-file paths and
project paths.

`--redact` makes the report shareable: built-in tool names and a short list of bundled skills stay readable;
every other tool becomes `tool-N`, MCP tools and servers `mcp-N`, skills `skill-N`, instruction files
`file-N (User|Project|AutoMem)`, projects `project-A`, and settings paths in snippets a placeholder. Labels are
per run (no hashes), and an unknown name is always redacted. `ctxwinnow analyze` has no redaction: its report prints
script basenames.

## Step 0: why this is an auditor

ctxwinnow started as a planned proxy that would compress tool output before it reaches the model. Step 0,
`ctxwinnow analyze` (v0.1.0), measured how much of real Claude Code sessions such compression could remove: the
median compressible share was 0.0%, verdict RETHINK. The cost was elsewhere, in the fixed context re-read on every
call, so 0.2 measures that instead. `analyze` is kept, with token estimates calibrated ×1.75 against exact usage.

## Prior art

Inspired by [headroom](https://github.com/headroomlabs-ai/headroom) (Apache-2.0).
