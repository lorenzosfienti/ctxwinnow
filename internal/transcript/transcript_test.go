package transcript

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseFixture(t *testing.T) {
	f, err := os.Open("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Kind: EvUsage, Usage: Usage{MessageID: "m1", Input: 10, CacheRead: 100, CacheCreation: 5}},
		{Kind: EvToolResult, Result: ToolResult{ToolUseID: "t1", ToolName: "Bash", ToolInput: `{"command":"go test ./..."}`, Text: "ok  \tpkg\t0.1s"}},
		{Kind: EvUsage, Usage: Usage{MessageID: "m2", Input: 20, CacheRead: 200}},
		{Kind: EvToolResult, Result: ToolResult{ToolUseID: "t2", ToolName: "mcp__gh__list", ToolInput: `{}`, Text: "a\nb"}},
		{Kind: EvToolResult, Result: ToolResult{ToolUseID: "t3", ToolName: "Read", ToolInput: `{"file_path":"/a"}`, NonText: true, IsError: true}},
		{Kind: EvToolResult, Result: ToolResult{ToolUseID: "tX", ToolName: "?"}},
		{Kind: EvCompact},
	}
	if !reflect.DeepEqual(s.Events, want) {
		t.Errorf("events:\n got  %+v\n want %+v", s.Events, want)
	}
	if s.CWD != "/home/u/proj" || s.Malformed != 1 || s.Unmatched != 1 {
		t.Errorf("CWD=%q Malformed=%d Unmatched=%d", s.CWD, s.Malformed, s.Unmatched)
	}
	if s.FirstTS != "2026-09-01T10:00:00Z" || s.LastTS != "2026-09-01T11:00:00Z" {
		t.Errorf("FirstTS=%q LastTS=%q", s.FirstTS, s.LastTS)
	}
	if got := s.Events[0].Usage.Context(); got != 115 {
		t.Errorf("Context() = %d, want 115", got)
	}
}

func TestParseEmptyContent(t *testing.T) {
	in := `{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"true"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":[]}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a"}]}}
`
	s, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(s.Events))
	}
	for _, ev := range s.Events {
		if ev.Result.Text != "" || ev.Result.NonText || ev.Result.ToolName != "Bash" {
			t.Errorf("unexpected result %+v", ev.Result)
		}
	}
}

func TestParseHugeLine(t *testing.T) {
	big := strings.Repeat("x", 2<<20) // 2 MiB of text in one line
	in := `{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"a","name":"Bash","input":{}}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"` + big + `"}]}}`
	s, err := Parse(strings.NewReader(in)) // no trailing newline on purpose
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || len(s.Events[0].Result.Text) != len(big) {
		t.Fatalf("huge line not parsed: %d events", len(s.Events))
	}
}

func TestParseUsageWithoutID(t *testing.T) {
	in := `{"type":"assistant","message":{"content":[],"usage":{"input_tokens":1}}}
{"type":"assistant","message":{"content":[],"usage":{"input_tokens":2}}}
`
	s, _ := Parse(strings.NewReader(in))
	if len(s.Events) != 2 {
		t.Fatalf("usages without id must not be merged: got %d events", len(s.Events))
	}
}
