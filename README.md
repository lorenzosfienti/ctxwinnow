# ctxwinnow

Separate the grain from the chaff in what your AI coding agent reads.

ctxwinnow is a planned local proxy, written in Go as a single binary, that compresses tool output (build and test
logs, JSON from CLIs and MCP tools) before it reaches the LLM. It is designed around four goals:

- **simple**: one binary, standard library only, no ML model;
- **lossless first**: it folds redundancy before it cuts anything;
- **safe for coding agents**: file reads, searches and failed commands are never touched, so edits keep working;
- **transparent**: every cut is logged, and the model gets exact markers and a path to the original.

## Status

Step 0 is runnable. On the author's sessions it reported RETHINK: the engine is on hold pending a re-scope.

| Step | What | State |
|---|---|---|
| 0 | `ctxwinnow analyze`: measure how much of real Claude Code sessions is compressible (go/no-go) | **v0.1.0 — shipped** |
| 1 | Compression engine and benchmark vs. headroom | on hold (step 0: RETHINK) |
| 2 | Anthropic proxy (append-only replay, byte-faithful) | planned |
| 3 | Retrieval tool for clients without a shared filesystem | planned |

## Try step 0

```bash
go install github.com/lorenzosfienti/ctxwinnow/cmd/ctxwinnow@latest
ctxwinnow analyze --group personal=$HOME/code --min-turns 20 -o ceiling.md
```

The report contains only aggregates (tool names, program names, numbers). Nothing leaves your machine.

## Prior art

Inspired by [headroom](https://github.com/headroomlabs-ai/headroom) (Apache-2.0).
