# ctxwinnow

Separate the grain from the chaff in what your AI coding agent reads.

ctxwinnow is a planned local proxy, written in Go as a single binary, that compresses tool output (build and test
logs, JSON from CLIs and MCP tools) before it reaches the LLM. It is designed around four goals:

- **simple**: one binary, standard library only, no ML model;
- **lossless first**: it folds redundancy before it cuts anything;
- **safe for coding agents**: file reads, searches and failed commands are never touched, so edits keep working;
- **transparent**: every cut is logged, and the model gets exact markers and a path to the original.

## Status

Design phase. Nothing is runnable yet.

| Step | What | State |
|---|---|---|
| 0 | `ctxwinnow analyze`: measure how much of real Claude Code sessions is compressible (go/no-go) | spec written |
| 1 | Compression engine and benchmark vs. headroom | direction agreed |
| 2 | Anthropic proxy (append-only replay, byte-faithful) | planned |
| 3 | Retrieval tool for clients without a shared filesystem | planned |

## Prior art

Inspired by [headroom](https://github.com/headroomlabs-ai/headroom) (Apache-2.0).
