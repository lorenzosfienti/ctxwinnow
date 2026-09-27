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

// TestParseOverheadFixture pins every size-only record of the canonical synthetic session
// (testdata/overhead_main.jsonl, real record order). Byte counts are len() of the decoded strings
// (raw JSON length for tool elements, environment snapshots and deferred-record entries).
func TestParseOverheadFixture(t *testing.T) {
	f, err := os.Open("testdata/overhead_main.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if s.CWD != "/home/zzuser/zzproj" || s.Version != "2.1.283" || s.Entrypoint != "cli" || s.Fork {
		t.Errorf("CWD=%q Version=%q Entrypoint=%q Fork=%v", s.CWD, s.Version, s.Entrypoint, s.Fork)
	}
	if s.FirstTS != "2026-09-20T09:00:00.000Z" || s.LastTS != "2026-09-20T09:30:00.000Z" {
		t.Errorf("FirstTS=%q LastTS=%q", s.FirstTS, s.LastTS)
	}
	if s.Malformed != 0 || s.CompactBeforeUsage != 0 {
		t.Errorf("Malformed=%d CompactBeforeUsage=%d", s.Malformed, s.CompactBeforeUsage)
	}
	var ctx []int
	for _, ev := range s.Events {
		if ev.Kind == EvUsage {
			ctx = append(ctx, ev.Usage.Context())
		}
	}
	if !reflect.DeepEqual(ctx, []int{1000, 1100, 1200, 0, 1050}) {
		t.Errorf("usage contexts = %v (split messages: last line wins)", ctx)
	}
	wantSnap := &Snapshot{SystemPromptBytes: 157, Tools: []ToolDef{
		{"Bash", 168}, {"Read", 157}, {"Edit", 236}, {"Skill", 138}, {"ToolSearch", 150}, {"Artifact", 915}, {"mcp__zzsrv__query", 145},
	}}
	if !reflect.DeepEqual(s.Snapshot, wantSnap) {
		t.Errorf("Snapshot = %+v, want %+v", s.Snapshot, wantSnap)
	}
	wantPre := []Attachment{
		{Type: AttHookContext, Bytes: 75},
		{Type: AttFirstPrompt, Bytes: 68},
		{Type: AttEnvironment, Bytes: 133},
		{Type: AttModel, Bytes: 47},
		{Type: AttDeferredDelta, Bytes: 44, Names: []string{"CronCreate", "mcp__zzsrv__query", "mcp__zzsrv__write"}},
		{Type: AttMCPInstructions, Bytes: 57, Names: []string{"zzsrv"}},
		{Type: AttSkillListing, Bytes: 352, Names: []string{"code-review", "zzskill", "zzidle", "zzplug:helper", "zzbare"},
			Lines: []SkillLine{{"", 35}, {"code-review", 75}, {"zzskill", 55}, {"", 49}, {"zzidle", 65}, {"zzplug:helper", 59}, {"zzbare", 8}}},
		{Type: AttTokensReminder, Bytes: 49},
		{Type: AttUserMeta, Bytes: 97},
		{Type: AttInstructions, Bytes: 484, Files: []InstructionFile{
			{"/home/zzuser/.claude/CLAUDE.md", "User", 115},
			{"/home/zzuser/zzproj/CLAUDE.md", "Project", 119},
			{"/home/zzuser/zzproj/docs/RULES.md", "Project", 109},
			{"/home/zzuser/CLAUDE.md", "Project", 61},
			{"/home/zzuser/.claude/projects/-home-zzuser-zzproj/memory/MEMORY.md", "AutoMem", 80},
		}},
		{Type: AttSessionContext, Bytes: 65},
		{Type: AttDate, Bytes: 10},
		{Type: AttDeferredRecord, Bytes: 192, Names: []string{"mcp__zzsrv__query"}},
	}
	if !reflect.DeepEqual(s.Pre, wantPre) {
		t.Errorf("Pre:\n got  %+v\n want %+v", s.Pre, wantPre)
	}
	wantCalls := []ToolCall{{"Read", 1, "2026-09-20T09:05:00.000Z"}, {"Skill", 1, "2026-09-20T09:01:00.000Z"}}
	if !reflect.DeepEqual(s.ToolCalls, wantCalls) {
		t.Errorf("ToolCalls = %+v, want %+v", s.ToolCalls, wantCalls)
	}
	if !reflect.DeepEqual(s.SkillCalls, []string{"zzskill"}) || !reflect.DeepEqual(s.Commands, []string{"zzbare"}) ||
		!reflect.DeepEqual(s.InvokedSkills, []string{"code-review"}) {
		t.Errorf("SkillCalls=%v Commands=%v InvokedSkills=%v", s.SkillCalls, s.Commands, s.InvokedSkills)
	}
}

// TestParseUsageRecords covers the whole-file usage records: tool calls (counted once per
// tool_use id, latest timestamp as an instant), Skill input.skill, <command-name> values with and
// without a leading "/", invoked_skills, fork-context-ref and the first non-empty version and
// entrypoint.
func TestParseUsageRecords(t *testing.T) {
	in := `{"type":"user","version":"","entrypoint":"","message":{"content":"<command-name>/zzb</command-name> and <command-name>zza</command-name>"}}
{"type":"assistant","timestamp":"2026-09-20T10:00:00.5Z","version":"2.1.270","entrypoint":"sdk-cli","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Skill","input":{"skill":"zzs","args":"x"}},{"type":"tool_use","id":"t2","name":"Bash","input":{}}],"usage":{"input_tokens":5}}}
{"type":"assistant","timestamp":"2026-09-20T10:00:00Z","version":"2.1.283","entrypoint":"cli","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Skill","input":{"skill":"zzs"}}],"usage":{"input_tokens":6}}}
{"type":"assistant","timestamp":"2026-09-20T09:59:00Z","message":{"id":"m2","content":[{"type":"tool_use","id":"t3","name":"Skill","input":{"skill":"zzr"}},{"type":"tool_use","id":"t4","name":"Skill","input":{"other":1}}],"usage":{"input_tokens":7}}}
{"type":"user","isMeta":true,"message":{"content":[{"type":"text","text":"<command-name>/zzb</command-name>"}]}}
{"type":"attachment","attachment":{"type":"invoked_skills","skills":[{"name":"zzk","path":"p","content":"c"},{"name":"zzj"},{"name":"zzk"}]}}
{"type":"fork-context-ref","agentId":"a","parentSessionId":"p","parentLastUuid":"u","contextLength":10}
`
	s, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []ToolCall{{"Bash", 1, "2026-09-20T10:00:00.5Z"}, {"Skill", 3, "2026-09-20T10:00:00.5Z"}}
	if !reflect.DeepEqual(s.ToolCalls, wantCalls) {
		t.Errorf("ToolCalls = %+v, want %+v", s.ToolCalls, wantCalls)
	}
	if !reflect.DeepEqual(s.SkillCalls, []string{"zzr", "zzs"}) {
		t.Errorf("SkillCalls = %v", s.SkillCalls)
	}
	if !reflect.DeepEqual(s.Commands, []string{"zza", "zzb"}) {
		t.Errorf("Commands = %v", s.Commands)
	}
	if !reflect.DeepEqual(s.InvokedSkills, []string{"zzj", "zzk"}) {
		t.Errorf("InvokedSkills = %v", s.InvokedSkills)
	}
	if !s.Fork || s.Version != "2.1.270" || s.Entrypoint != "sdk-cli" {
		t.Errorf("Fork=%v Version=%q Entrypoint=%q", s.Fork, s.Version, s.Entrypoint)
	}
}

// TestParseTimestampOrder: timestamps order as instants, so a fractional second never sorts before
// the whole second (string order would put "…00.5Z" before "…00Z"), and offsets resolve to UTC.
func TestParseTimestampOrder(t *testing.T) {
	in := `{"type":"system","timestamp":"2026-09-20T09:00:00.5Z"}
{"type":"system","timestamp":"2026-09-20T09:00:00Z"}
{"type":"system","timestamp":"2026-09-20T11:30:00+02:00"}
{"type":"system","timestamp":"2026-09-20T09:29:59.999Z"}
`
	s, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.FirstTS != "2026-09-20T09:00:00Z" || s.LastTS != "2026-09-20T11:30:00+02:00" {
		t.Errorf("FirstTS=%q LastTS=%q", s.FirstTS, s.LastTS)
	}
}
