package ceiling

import (
	"strings"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

func tr(name, input, text string) transcript.Event {
	return transcript.Event{Kind: transcript.EvToolResult,
		Result: transcript.ToolResult{ToolName: name, ToolInput: input, Text: text}}
}

func usage(id string, ctx int) transcript.Event {
	return transcript.Event{Kind: transcript.EvUsage, Usage: transcript.Usage{MessageID: id, Input: 1000, CacheRead: ctx - 1000}}
}

// fixtureJSON is 5010 bytes (1253 tokens); compacted 4009 bytes (1003 tokens).
var fixtureJSON = "[" + strings.Repeat(`{"a": 1}, `, 500) + `{"a": 1}]`

// fixtureSessions returns three sessions with hand-computed statistics:
//
//	A /w/work/x      Bash go test 2000 tok (lines, lossless 1996), Read 1000 tok; peak ctx 20000
//	                 -> share_c 0.1, share_l 0.0998; 3 turns
//	B /w/personal/y  subagent; mcp JSON 1253 tok (lossless 250); only a zero-usage turn -> not eligible
//	C /other/z       one turn of ctx 100, no results -> not eligible with MinTurns 2
func fixtureSessions() []*transcript.Session {
	a := &transcript.Session{CWD: "/w/work/x", FirstTS: "2026-09-01T10:00:00Z", LastTS: "2026-09-01T12:00:00Z",
		Events: []transcript.Event{
			tr("Bash", `{"command":"go test ./..."}`, repeatLine("ok line", 1000)),
			usage("a1", 20000),
			tr("Read", `{"file_path":"/secret/path"}`, strings.Repeat("x", 4000)),
			usage("a2", 10000),
			{Kind: transcript.EvCompact},
			usage("a3", 5000),
		}}
	b := &transcript.Session{CWD: "/w/personal/y", FirstTS: "2026-09-02T09:00:00Z", LastTS: "2026-09-02T09:05:00Z",
		Events: []transcript.Event{
			tr("mcp__x__y", `{}`, fixtureJSON),
			{Kind: transcript.EvUsage, Usage: transcript.Usage{MessageID: "b1"}},
		}}
	c := &transcript.Session{CWD: "/other/z", Events: []transcript.Event{
		{Kind: transcript.EvUsage, Usage: transcript.Usage{MessageID: "c1", Input: 100}},
	}}
	return []*transcript.Session{a, b, c}
}

func fixtureOptions() Options {
	return Options{MinTurns: 2, Groups: []Group{{"work", "/w/work"}, {"personal", "/w/personal"}}}
}

func fixtureResult() Result {
	a := NewAnalyzer(fixtureOptions())
	for i, s := range fixtureSessions() {
		a.AddSession(s, i == 1)
	}
	r := a.Result()
	r.Version = "test"
	return r
}
