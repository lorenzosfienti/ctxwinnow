package transcript

import (
	"reflect"
	"strings"
	"testing"
)

func parseString(t *testing.T, in string) *Session {
	t.Helper()
	s, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestAttachmentByteRules pins the byte rule of every kept attachment type (lengths of decoded
// strings; raw JSON length where the rule says raw) and the types that are ignored.
func TestAttachmentByteRules(t *testing.T) {
	tests := []struct {
		name string
		att  string // the "attachment" object
		want []Attachment
	}{
		{"instructions", `{"type":"instructions","files":[{"path":"/h/.claude/CLAUDE.md","type":"User","content":"é\n"},{"path":"/p/CLAUDE.md","type":"Project","content":"abcd"}]}`,
			[]Attachment{{Type: AttInstructions, Bytes: 7, Files: []InstructionFile{{"/h/.claude/CLAUDE.md", "User", 3}, {"/p/CLAUDE.md", "Project", 4}}}}},
		{"initial skill listing", `{"type":"skill_listing","isInitial":true,"names":["a"],"content":"hdr\n- a: x"}`,
			[]Attachment{{Type: AttSkillListing, Bytes: 10, Names: []string{"a"}, Lines: []SkillLine{{"", 3}, {"a", 6}}}}},
		{"non-initial skill listing ignored", `{"type":"skill_listing","isInitial":false,"names":["a"],"content":"- a: x"}`, nil},
		{"deferred tools delta", `{"type":"deferred_tools_delta","addedNames":["Cron","mcp__s__t"],"addedLines":["Cron","mcp__s__t"]}`,
			[]Attachment{{Type: AttDeferredDelta, Bytes: 13, Names: []string{"Cron", "mcp__s__t"}}}},
		{"deferred tools record (raw entries)", `{"type":"deferred_tools_record","entries":[{"name":"mcp__s__t", "d":1},{"name":"mcp__s__u"}]}`,
			[]Attachment{{Type: AttDeferredRecord, Bytes: 47, Names: []string{"mcp__s__t", "mcp__s__u"}}}},
		{"mcp instructions", `{"type":"mcp_instructions_delta","addedNames":["s"],"addedBlocks":["ab","cde"]}`,
			[]Attachment{{Type: AttMCPInstructions, Bytes: 5, Names: []string{"s"}}}},
		{"hook context", `{"type":"hook_additional_context","content":["hook text"],"hookEvent":"SessionStart"}`,
			[]Attachment{{Type: AttHookContext, Bytes: 9}}},
		{"empty hook context", `{"type":"hook_additional_context","content":[]}`, []Attachment{{Type: AttHookContext}}},
		{"hook success ignored", `{"type":"hook_success","content":"hook text","stdout":"hook text"}`, nil},
		{"environment (raw snapshot)", `{"type":"environment","snapshot":{"a": 1}}`, []Attachment{{Type: AttEnvironment, Bytes: 8}}},
		{"model", `{"type":"model","text":"a\"b"}`, []Attachment{{Type: AttModel, Bytes: 3}}},
		{"date", `{"type":"date","date":"2026-09-20"}`, []Attachment{{Type: AttDate, Bytes: 10}}},
		{"session context", `{"type":"session_context","context":{"userEmail":"a@b","n":12,"o":{"k":1}}}`,
			[]Attachment{{Type: AttSessionContext, Bytes: 12}}},
		{"tokens reminder", `{"type":"total_tokens_reminder","text":"12345"}`, []Attachment{{Type: AttTokensReminder, Bytes: 5}}},
		{"agent listing", `{"type":"agent_listing_delta","addedLines":["ab","c"],"isInitial":true}`, []Attachment{{Type: AttAgentListing, Bytes: 3}}},
		{"credential org ignored", `{"type":"credential_org","organizationUuid":"x"}`, nil},
		{"auto mode ignored", `{"type":"auto_mode","bypass":false}`, nil},
		{"unknown type ignored", `{"type":"zz_future","text":"x"}`, nil},
		{"undecodable ignored", `{"type":"model","text":42}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := parseString(t, `{"type":"attachment","attachment":`+tc.att+"}\n")
			if !reflect.DeepEqual(s.Pre, tc.want) {
				t.Errorf("Pre:\n got  %+v\n want %+v", s.Pre, tc.want)
			}
		})
	}
}

// TestUserPseudoAttachments: the first non-meta user record without tool results is the first
// prompt (at most one); isMeta user text before the first usage is user_meta.
func TestUserPseudoAttachments(t *testing.T) {
	in := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"x","content":"not a prompt"}]}}
{"type":"user","isMeta":true,"message":{"content":"meta"}}
{"type":"user","message":{"content":[{"type":"text","text":"ab"},{"type":"image","source":{}},{"type":"text","text":"cd"}]}}
{"type":"user","message":{"content":"second prompt"}}
{"type":"user","isMeta":true,"message":{"content":[{"type":"text","text":"xyz"}]}}
`
	want := []Attachment{{Type: AttUserMeta, Bytes: 4}, {Type: AttFirstPrompt, Bytes: 4}, {Type: AttUserMeta, Bytes: 3}}
	if s := parseString(t, in); !reflect.DeepEqual(s.Pre, want) {
		t.Errorf("Pre:\n got  %+v\n want %+v", s.Pre, want)
	}
}

// TestPreStopsAtFirstUsage: only records before the first usage with context > 0 are kept; a
// context-0 usage does not close the window; compact_boundary records before it are counted.
func TestPreStopsAtFirstUsage(t *testing.T) {
	in := `{"type":"attachment","attachment":{"type":"date","date":"d1"}}
{"type":"system","subtype":"compact_boundary"}
{"type":"assistant","message":{"id":"m0","content":[],"usage":{"input_tokens":0}}}
{"type":"attachment","attachment":{"type":"date","date":"d22"}}
{"type":"assistant","message":{"id":"m1","content":[],"usage":{"input_tokens":1}}}
{"type":"attachment","attachment":{"type":"date","date":"d333"}}
{"type":"user","message":{"content":"late prompt"}}
{"type":"system","subtype":"compact_boundary"}
`
	s := parseString(t, in)
	want := []Attachment{{Type: AttDate, Bytes: 2}, {Type: AttDate, Bytes: 3}}
	if !reflect.DeepEqual(s.Pre, want) || s.CompactBeforeUsage != 1 {
		t.Errorf("Pre=%+v CompactBeforeUsage=%d", s.Pre, s.CompactBeforeUsage)
	}
}

// TestSkillLineWalk pins the forward walk over the listed names (review focus 4): a listed name
// without a line is skipped without desynchronising later lines, a name-only line matches, a name
// that prefixes another never steals its line, and a "- " overhead line stays overhead.
func TestSkillLineWalk(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		content string
		want    []SkillLine
	}{
		{"missing name, prefixes, name-only, dash overhead",
			[]string{"code", "code-review", "zzplug", "zzplug:helper", "missing", "last"},
			"- note: skills load lazily\n- code: short\n- code-review: longer one\n- zzplug\n- zzplug:helper: plugin helper\nMid-listing text\n- last:",
			[]SkillLine{{"", 26}, {"code", 13}, {"code-review", 25}, {"zzplug", 8}, {"zzplug:helper", 30}, {"", 16}, {"last", 7}}},
		{"prefix name without its own line",
			[]string{"zzplug", "zzplug:helper"},
			"- zzplug:helper: x",
			[]SkillLine{{"zzplug:helper", 18}}},
		{"no match after the last name",
			[]string{"a"},
			"- a: x\n- b: y\n",
			[]SkillLine{{"a", 6}, {"", 6}, {"", 0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := skillLines(tc.content, tc.names); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("skillLines:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}

// TestToolSnapshotRawBytes: tool bytes are the raw element as stored (Go's encoder would escape
// <, > and & and change the size); system prompt bytes are decoded string lengths.
func TestToolSnapshotRawBytes(t *testing.T) {
	elems := []string{
		`{"name":"Edit","description":"<a> & b \u0026 é"}`,
		`{ "name" : "Read" }`,
	}
	in := `{"type":"attachment","attachment":{"type":"prompt_snapshot","systemPrompt":["ab\n","é"],"tools":[` +
		elems[0] + `,` + elems[1] + `]}}` + "\n"
	s := parseString(t, in)
	want := &Snapshot{SystemPromptBytes: 5, Tools: []ToolDef{{"Edit", len(elems[0])}, {"Read", len(elems[1])}}}
	if !reflect.DeepEqual(s.Snapshot, want) {
		t.Errorf("Snapshot = %+v, want %+v", s.Snapshot, want)
	}
}

// TestSnapshotSelection: the first snapshot with a non-empty, decodable tools array wins, anywhere in
// the file; tool-less and undecodable snapshots count as absent.
func TestSnapshotSelection(t *testing.T) {
	snap := func(tools string) string {
		return `{"type":"attachment","attachment":{"type":"prompt_snapshot","systemPrompt":["s"],"tools":` + tools + `}}` + "\n"
	}
	usage := `{"type":"assistant","message":{"id":"m1","content":[],"usage":{"input_tokens":1}}}` + "\n"
	in := snap(`[]`) + usage + snap(`"not an array"`) + snap(`[{"description":"no name"}]`) + snap(`[42]`) +
		snap(`[{"name":"A"}]`) + snap(`[{"name":"B"}]`)
	s := parseString(t, in)
	want := &Snapshot{SystemPromptBytes: 1, Tools: []ToolDef{{"A", 12}}}
	if !reflect.DeepEqual(s.Snapshot, want) {
		t.Errorf("Snapshot = %+v, want %+v", s.Snapshot, want)
	}
	if s := parseString(t, snap(`[]`)+usage); s.Snapshot != nil {
		t.Errorf("tool-less snapshot must count as absent, got %+v", s.Snapshot)
	}
}
