# Security policy

## Supported versions

Only the latest release receives fixes.

## Reporting a vulnerability

Please report it privately through GitHub:
[Report a vulnerability](https://github.com/lorenzosfienti/ctxwinnow/security/advisories/new).
Do not open a public issue.

You can expect a first answer within 7 days. A confirmed issue is fixed in a patch release and
credited in the advisory unless you prefer otherwise.

## Scope

ctxwinnow is offline and read-only: it reads Claude Code transcripts, never settings files, never
uses the network and never stores or prints transcript contents. Anything that breaks those
guarantees is a vulnerability, for example:

- transcript contents (prompts, tool output, file contents) appearing in a report;
- a name or path surviving `--redact`;
- a write outside `-o FILE`, or any network access;
- a crafted transcript that makes the tool crash, hang or exhaust memory.

Never attach real transcripts or unredacted reports to a report: describe the shape of the input
or build a synthetic one.
