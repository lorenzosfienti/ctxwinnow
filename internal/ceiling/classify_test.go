package ceiling

import (
	"strings"
	"testing"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

const goTest = `{"command":"go test ./..."}`

func repeatLine(line string, n int) string { return strings.Repeat(line+"\n", n) }

func TestClassify(t *testing.T) {
	bigLog := repeatLine("2026-09-01T10:00:00Z INFO request id=42 served in 3ms", 200)
	bigJSON := "[" + strings.Repeat(`{"id": 1, "name": "x"}, `, 300) + `{"id": 2}]`
	ndjson := repeatLine(`{"level":"info","msg":"served request"}`, 200)
	markdown := repeatLine("# Heading", 100) + repeatLine("- bullet item text", 100) + repeatLine("plain line", 200)
	longLines := repeatLine(strings.Repeat("word ", 80), 12) // 12 lines of 400 bytes
	tests := []struct {
		name string
		r    transcript.ToolResult
		want Category
	}{
		{"non text wins", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, NonText: true, Text: bigLog}, CatNonText},
		{"read tool", transcript.ToolResult{ToolName: "Read", Text: bigLog}, CatPassTool},
		{"unmatched tool", transcript.ToolResult{ToolName: "?", Text: bigLog}, CatPassTool},
		{"is_error", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, IsError: true, Text: bigLog}, CatError},
		{"exit code text", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, Text: "Exit code 1\n" + bigLog}, CatError},
		{"bash read", transcript.ToolResult{ToolName: "Bash", ToolInput: `{"command":"cat big.log"}`, Text: bigLog}, CatBashRead},
		{"bash search", transcript.ToolResult{ToolName: "Bash", ToolInput: `{"command":"grep -rn x ."}`, Text: bigLog}, CatBashSearch},
		{"bash unparseable", transcript.ToolResult{ToolName: "Bash", ToolInput: `{"command":"echo \"oops"}`, Text: bigLog}, CatUnparseable},
		{"persisted", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, Text: "<persisted-output>\nOutput too large (40KB). Full output saved to: /tmp/x\n" + bigLog}, CatPersisted},
		{"small", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, Text: "ok\tpkg\t0.1s"}, CatSmall},
		{"json array", transcript.ToolResult{ToolName: "mcp__x__list", ToolInput: `{}`, Text: bigJSON}, CatJSON},
		{"ndjson", transcript.ToolResult{ToolName: "Bash", ToolInput: `{"command":"kubectl logs web"}`, Text: ndjson}, CatJSON},
		{"markdown", transcript.ToolResult{ToolName: "mcp__docs__get", ToolInput: `{}`, Text: markdown}, CatUncertain},
		{"few long lines", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, Text: longLines}, CatUncertain},
		{"log lines", transcript.ToolResult{ToolName: "Bash", ToolInput: goTest, Text: bigLog}, CatLines},
	}
	for _, tc := range tests {
		if got := Classify(tc.r); got != tc.want {
			t.Errorf("%s: Classify = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestEstTokensAndCompressible(t *testing.T) {
	if EstTokens("") != 0 || EstTokens("a") != 1 || EstTokens("abcd") != 1 || EstTokens("abcde") != 2 {
		t.Fatal("EstTokens must be (len+3)/4")
	}
	for _, c := range Categories {
		if c.Compressible() != (c == CatJSON || c == CatLines) {
			t.Errorf("%s.Compressible() = %v", c, c.Compressible())
		}
	}
	if len(Categories) != 11 {
		t.Errorf("len(Categories) = %d, want 11", len(Categories))
	}
}
