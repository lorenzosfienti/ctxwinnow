package overhead

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func sumTokens(s *Session) float64 {
	var t float64
	for _, c := range s.Components {
		t += c.Tokens
	}
	return t
}

// comp is a Component without Tokens, for exact comparisons.
type comp struct {
	Kind        CompKind
	Name, Label string
	Bytes       int
}

func comps(s *Session) []comp {
	var out []comp
	for _, c := range s.Components {
		out = append(out, comp{c.Kind, c.Name, c.Label, c.Bytes})
	}
	return out
}

// TestNewSessionCanonical pins the per-file inventory of the canonical synthetic session: B, the
// de-duplicated usages, the components in order with their bytes and labels, and the two-rate
// attribution (tool JSON at 4.5 bytes per token, the remainder R = B − tool tokens spread over the
// other components by bytes).
func TestNewSessionCanonical(t *testing.T) {
	s := loadCanonical(t, "-home-zzuser-zzproj/zz-session-1.jsonl")
	if s.Path != "-home-zzuser-zzproj/zz-session-1.jsonl" || s.Kind != Main || s.CWD != "/home/zzuser/zzproj" {
		t.Errorf("Path=%q Kind=%v CWD=%q", s.Path, s.Kind, s.CWD)
	}
	if !s.Start.Equal(time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)) || !s.End.Equal(time.Date(2026, 9, 20, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("Start=%v End=%v", s.Start, s.End)
	}
	if s.Version != "2.1.283" || s.Entrypoint != "cli" || s.Fork || s.Malformed != 0 || s.CompactBeforeFirstUsage {
		t.Errorf("Version=%q Entrypoint=%q Fork=%v Malformed=%d CompactBeforeFirstUsage=%v",
			s.Version, s.Entrypoint, s.Fork, s.Malformed, s.CompactBeforeFirstUsage)
	}
	if !reflect.DeepEqual(s.UsageIDs, []string{"msg_zz01", "msg_zz02", "msg_zz03", "msg_zz04"}) ||
		!reflect.DeepEqual(s.Contexts, []int{1000, 1100, 1200, 1050}) || s.B != 1000 {
		t.Errorf("UsageIDs=%v Contexts=%v B=%d", s.UsageIDs, s.Contexts, s.B)
	}
	if !s.HasSnapshot || !reflect.DeepEqual(s.ToolNames, []string{"Bash", "Read", "Edit", "Skill", "ToolSearch", "Artifact", "mcp__zzsrv__query"}) {
		t.Errorf("HasSnapshot=%v ToolNames=%v", s.HasSnapshot, s.ToolNames)
	}
	want := []comp{
		{CompTool, "Bash", "", 168},
		{CompTool, "Read", "", 157},
		{CompTool, "Edit", "", 236},
		{CompTool, "Skill", "", 138},
		{CompTool, "ToolSearch", "", 150},
		{CompTool, "Artifact", "", 915},
		{CompMCPTool, "zzsrv", "", 145},
		{CompSystemPrompt, "", "", 157},
		{CompInstruction, "/home/zzuser/.claude/CLAUDE.md", LabelUser, 115},
		{CompInstruction, "/home/zzuser/zzproj/CLAUDE.md", LabelProject, 119},
		{CompInstruction, "/home/zzuser/zzproj/docs/RULES.md", LabelProjectImport, 109},
		{CompInstruction, "/home/zzuser/CLAUDE.md", LabelProject, 61},
		{CompInstruction, "/home/zzuser/.claude/projects/-home-zzuser-zzproj/memory/MEMORY.md", LabelAutoMem, 80},
		{CompSkillListing, "", "", 352},
		{CompMCP, "", "", 293},
		{CompHook, "", "", 75},
		{CompHarness, "", "", 401},
		{CompFirstPrompt, "", "", 68},
	}
	if got := comps(s); !reflect.DeepEqual(got, want) {
		t.Errorf("Components:\n got  %+v\n want %+v", got, want)
	}
	if s.Decomp != DecompOK {
		t.Fatalf("Decomp = %v, want DecompOK", s.Decomp)
	}
	r := 1000 - 1909/toolJSONBytesPerToken // 575.78 tokens for 1830 non-tool bytes
	if got, _ := s.Tokens("tool:Artifact"); !near(got, 915/4.5) {
		t.Errorf("Artifact tokens = %v, want %v", got, 915/4.5)
	}
	if got, _ := s.Tokens("mcp-tool:zzsrv"); !near(got, 145/4.5) {
		t.Errorf("zzsrv MCP tool tokens = %v", got)
	}
	if got, _ := s.Tokens("system-prompt"); !near(got, r*157/1830) {
		t.Errorf("system prompt tokens = %v, want %v", got, r*157/1830)
	}
	if got, _ := s.Tokens("instruction:/home/zzuser/CLAUDE.md"); !near(got, r*61/1830) {
		t.Errorf("instruction tokens = %v, want %v", got, r*61/1830)
	}
	if !near(sumTokens(s), 1000) {
		t.Errorf("components sum to %v, want B = 1000", sumTokens(s))
	}
	if !near(s.RemainderBPT, 1830/r) {
		t.Errorf("RemainderBPT = %v, want %v", s.RemainderBPT, 1830/r)
	}
	wantCalled := map[string]time.Time{
		"Read":  time.Date(2026, 9, 20, 9, 5, 0, 0, time.UTC),
		"Skill": time.Date(2026, 9, 20, 9, 1, 0, 0, time.UTC),
	}
	if len(s.Called) != len(wantCalled) {
		t.Errorf("Called = %v", s.Called)
	}
	for name, ts := range wantCalled {
		if got, ok := s.Called[name]; !ok || !got.Equal(ts) {
			t.Errorf("Called[%s] = %v, %v; want %v", name, got, ok, ts)
		}
	}
	wantListed := map[string]int{"code-review": 75, "zzskill": 55, "zzidle": 65, "zzplug:helper": 59, "zzbare": 8}
	if !reflect.DeepEqual(s.Listed, wantListed) {
		t.Errorf("Listed = %v", s.Listed)
	}
	if !reflect.DeepEqual(s.SkillsUsed, map[string]bool{"zzskill": true, "zzbare": true, "code-review": true}) {
		t.Errorf("SkillsUsed = %v", s.SkillsUsed)
	}
	if !reflect.DeepEqual(s.MCPServers, []string{"zzsrv"}) {
		t.Errorf("MCPServers = %v", s.MCPServers)
	}
}

func TestTokensAndBytes(t *testing.T) {
	s := loadCanonical(t, "p/a.jsonl")
	if b, ok := s.Bytes("mcp"); !ok || b != 293 {
		t.Errorf("Bytes(mcp) = %d, %v", b, ok)
	}
	if b, ok := s.Bytes("instruction:/home/zzuser/zzproj/CLAUDE.md"); !ok || b != 119 {
		t.Errorf("Bytes(instruction) = %d, %v", b, ok)
	}
	if tok, ok := s.Tokens("tool:WebFetch"); ok || tok != 0 {
		t.Errorf("Tokens of an absent key = %v, %v", tok, ok)
	}
	if _, ok := s.Bytes("tool:WebFetch"); ok {
		t.Error("Bytes of an absent key must report ok = false")
	}
}

// TestAttributionGuard pins the sanity guard: Σ component bytes / B outside [2, 6], R = 0 or no
// non-tool bytes leave the session undecomposed (Tokens stay 0); the bounds themselves pass.
func TestAttributionGuard(t *testing.T) {
	tests := []struct {
		name  string
		b     int
		tools []transcript.ToolDef
		other int // one harness attachment of this many bytes (0 = none)
		want  Decomp
	}{
		{"ratio exactly 2", 1000, []transcript.ToolDef{tool("Bash", 900)}, 1100, DecompOK},
		{"ratio exactly 6", 1000, []transcript.ToolDef{tool("Bash", 900)}, 5100, DecompOK},
		{"ratio below 2", 1000, []transcript.ToolDef{tool("Bash", 450)}, 1000, DecompSanity},
		{"ratio above 6", 100, []transcript.ToolDef{tool("Bash", 90)}, 600, DecompSanity},
		{"tools alone reach B (R = 0)", 200, []transcript.ToolDef{tool("Bash", 900)}, 100, DecompSanity},
		{"tools exceed B (R clamped to 0)", 150, []transcript.ToolDef{tool("Bash", 800)}, 100, DecompSanity},
		{"no non-tool bytes", 200, []transcript.ToolDef{tool("Bash", 450)}, 0, DecompSanity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := tsess{Rel: "p/a.jsonl", Contexts: []int{tc.b}, Tools: tc.tools}
			if tc.other > 0 {
				x.Pre = []transcript.Attachment{att(transcript.AttEnvironment, tc.other)}
			}
			s := x.session()
			if s.Decomp != tc.want {
				t.Fatalf("Decomp = %v, want %v", s.Decomp, tc.want)
			}
			if tc.want == DecompOK && !near(sumTokens(s), float64(tc.b)) {
				t.Errorf("components sum to %v, want B = %d", sumTokens(s), tc.b)
			}
			if tc.want == DecompSanity && (sumTokens(s) != 0 || s.RemainderBPT != 0) {
				t.Errorf("undecomposed session carries tokens %v / RemainderBPT %v", sumTokens(s), s.RemainderBPT)
			}
		})
	}
}

// TestDecompReasons: a main session without a tool snapshot is "old version" below 2.1.259 and "no
// snapshot" otherwise (or when the version is unknown); subagents and sessions without usage are
// never decomposed.
func TestDecompReasons(t *testing.T) {
	tools := []transcript.ToolDef{tool("Bash", 900), tool("Artifact", 450)}
	pre := []transcript.Attachment{att(transcript.AttModel, 1500)}
	tests := []struct {
		name string
		x    tsess
		want Decomp
	}{
		{"pre-2.1.259", tsess{Rel: "p/a.jsonl", Version: "2.1.250", Contexts: []int{1000}}, DecompOldVersion},
		{"2.1.259 without snapshot", tsess{Rel: "p/a.jsonl", Version: "2.1.259", Contexts: []int{1000}}, DecompNoSnapshot},
		{"unknown version", tsess{Rel: "p/a.jsonl", Version: "unknown", Contexts: []int{1000}}, DecompNoSnapshot},
		{"subagent with snapshot", tsess{Rel: "p/a/subagents/agent-1.jsonl", Contexts: []int{1000}, Tools: tools, Pre: pre}, DecompNone},
		{"main without usage", tsess{Rel: "p/a.jsonl", Contexts: []int{0}, Tools: tools, Pre: pre}, DecompNone},
		{"main with snapshot", tsess{Rel: "p/a.jsonl", Contexts: []int{1000}, Tools: tools, Pre: pre}, DecompOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if s := tc.x.session(); s.Decomp != tc.want {
				t.Errorf("Decomp = %v, want %v", s.Decomp, tc.want)
			}
		})
	}
	sub := tsess{Rel: "p/a/subagents/agent-1.jsonl", Contexts: []int{1000}, Tools: tools, Pre: pre}.session()
	if sub.Kind != Subagent || sub.Components != nil || !reflect.DeepEqual(sub.ToolNames, []string{"Bash", "Artifact"}) {
		t.Errorf("subagent: Kind=%v Components=%v ToolNames=%v", sub.Kind, sub.Components, sub.ToolNames)
	}
}

func TestKind(t *testing.T) {
	for rel, want := range map[string]Kind{
		"p1/a.jsonl":                   Main,
		"p1/a/subagents/agent-1.jsonl": Subagent,
		"subagents/agent-2.jsonl":      Subagent,
		"p1/subagents-old/a.jsonl":     Main,
	} {
		if got := (tsess{Rel: rel, Contexts: []int{10}}).session().Kind; got != want {
			t.Errorf("Kind(%q) = %v, want %v", rel, got, want)
		}
	}
}

// TestUsagesAndTimes: context-0 usages are dropped, ids are kept as written ("" included), B is the
// first remaining context; Start/End are parsed instants in UTC, zero when unparsable.
func TestUsagesAndTimes(t *testing.T) {
	s := tsess{Rel: "p/a.jsonl", IDs: []string{"z", "", "b", "c"}, Contexts: []int{0, 700, 0, 900},
		Start: "2026-09-01T12:00:00+02:00", End: "yesterday"}.session()
	if !reflect.DeepEqual(s.UsageIDs, []string{"", "c"}) || !reflect.DeepEqual(s.Contexts, []int{700, 900}) || s.B != 700 {
		t.Errorf("UsageIDs=%v Contexts=%v B=%d", s.UsageIDs, s.Contexts, s.B)
	}
	if !s.Start.Equal(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)) || s.Start.Location() != time.UTC || !s.End.IsZero() {
		t.Errorf("Start=%v End=%v", s.Start, s.End)
	}
	if none := (tsess{Rel: "p/b.jsonl"}).session(); none.B != 0 || none.UsageIDs != nil || none.Called == nil || none.Listed == nil || none.SkillsUsed == nil {
		t.Errorf("session without usage: %+v", none)
	}
}

func TestInstructionLabel(t *testing.T) {
	tests := []struct{ typ, path, want string }{
		{"User", "/h/.claude/CLAUDE.md", LabelUser},
		{"Project", "/p/CLAUDE.md", LabelProject},
		{"Project", "/p/CLAUDE.local.md", LabelProject},
		{"Project", `C:\p\CLAUDE.md`, LabelProject},
		{"Project", "/p/.claude/rules/go.md", LabelProjectImport},
		{"Project", "/p/CLAUDE.md.bak", LabelProjectImport},
		{"AutoMem", "/h/.claude/projects/-p/memory/MEMORY.md", LabelAutoMem},
		{"Managed", "/etc/claude/CLAUDE.md", LabelOther},
		{"", "/x", LabelOther},
	}
	for _, tc := range tests {
		if got := instructionLabel(tc.typ, tc.path); got != tc.want {
			t.Errorf("instructionLabel(%q, %q) = %q, want %q", tc.typ, tc.path, got, tc.want)
		}
	}
}

// TestUsageSets: skills used = Skill input ∪ listed commands ∪ invoked skills; MCP servers come from
// snapshot MCP tools, deferred mcp__ names and MCP instruction names.
func TestUsageSets(t *testing.T) {
	s := tsess{Rel: "p/a.jsonl", Contexts: []int{1000},
		Tools: []transcript.ToolDef{tool("Bash", 500), tool("mcp__aa__x", 100), tool("mcp__aa__y", 100), tool("Read", 200), tool("mcp__bb__z", 50)},
		Pre: []transcript.Attachment{
			listing(transcript.SkillLine{Name: "", Bytes: 20}, transcript.SkillLine{Name: "zzbare", Bytes: 30}, transcript.SkillLine{Name: "zzother", Bytes: 40}),
			att(transcript.AttDeferredDelta, 30, "CronCreate", "mcp__cc__q"),
			att(transcript.AttDeferredRecord, 60, "mcp__dd__r"),
			att(transcript.AttMCPInstructions, 40, "ee"),
		},
		Calls: []string{"Bash", "mcp__aa__x"}, SkillCalls: []string{"unlisted"}, Commands: []string{"compact", "zzbare"}, Invoked: []string{"zzinv"},
	}.session()
	if !reflect.DeepEqual(s.SkillsUsed, map[string]bool{"unlisted": true, "zzbare": true, "zzinv": true}) {
		t.Errorf("SkillsUsed = %v", s.SkillsUsed)
	}
	if !reflect.DeepEqual(s.Listed, map[string]int{"zzbare": 30, "zzother": 40}) {
		t.Errorf("Listed = %v", s.Listed)
	}
	if !reflect.DeepEqual(s.MCPServers, []string{"aa", "bb", "cc", "dd", "ee"}) {
		t.Errorf("MCPServers = %v", s.MCPServers)
	}
	want := []comp{
		{CompTool, "Bash", "", 500},
		{CompMCPTool, "aa", "", 200},
		{CompTool, "Read", "", 200},
		{CompMCPTool, "bb", "", 50},
		{CompSkillListing, "", "", 92},
		{CompMCP, "", "", 130},
	}
	if got := comps(s); !reflect.DeepEqual(got, want) {
		t.Errorf("Components:\n got  %+v\n want %+v", got, want)
	}
	if _, ok := s.Called["mcp__aa__x"]; !ok || len(s.Called) != 2 {
		t.Errorf("Called = %v", s.Called)
	}
}

func TestShare(t *testing.T) {
	s := &Session{B: 1000, N: 4, SumCtx: 4350}
	if !near(s.Share(), 4000.0/4350) {
		t.Errorf("Share = %v", s.Share())
	}
	if (&Session{B: 1000}).Share() != 0 {
		t.Error("Share with SumCtx 0 must be 0")
	}
}

// TestBuildDedup: a usage id already seen in an earlier file is dropped from N and SumCtx; the
// session stays eligible while its first id is new.
func TestBuildDedup(t *testing.T) {
	ss := built(
		tsess{Rel: "p/b.jsonl", Start: "2026-09-01T11:00:00Z", IDs: []string{"x1", "a2", "x3"}, Contexts: []int{500, 600, 700}},
		tsess{Rel: "p/a.jsonl", Start: "2026-09-01T10:00:00Z", IDs: []string{"a1", "a2", "a3"}, Contexts: []int{100, 200, 300}},
	)
	a, b := ss[0], ss[1]
	if a.Path != "p/a.jsonl" || a.Status != Eligible || a.N != 3 || a.SumCtx != 600 {
		t.Errorf("a: Path=%q Status=%v N=%d SumCtx=%d", a.Path, a.Status, a.N, a.SumCtx)
	}
	if b.Status != Eligible || b.N != 2 || b.SumCtx != 1200 || b.B != 500 {
		t.Errorf("b: Status=%v N=%d SumCtx=%d B=%d", b.Status, b.N, b.SumCtx, b.B)
	}
}

// TestBuildStatuses: continuation (first id seen earlier), fork and no-usage sessions are excluded
// with one reason each; their ids still enter the global set.
func TestBuildStatuses(t *testing.T) {
	ss := built(
		tsess{Rel: "p/orig.jsonl", Start: "2026-09-01T10:00:00Z", IDs: []string{"o1", "o2"}, Contexts: []int{100, 200}},
		tsess{Rel: "p/cont.jsonl", Start: "2026-09-02T10:00:00Z", IDs: []string{"o1", "o2", "c3"}, Contexts: []int{100, 200, 300}},
		tsess{Rel: "p/x/subagents/fork.jsonl", Start: "2026-09-03T10:00:00Z", IDs: []string{"f1"}, Contexts: []int{900}, Fork: true},
		tsess{Rel: "p/after-fork.jsonl", Start: "2026-09-04T10:00:00Z", IDs: []string{"f1", "n2"}, Contexts: []int{900, 950}},
		tsess{Rel: "p/journal.jsonl", Start: "2026-09-05T10:00:00Z", Contexts: []int{0}},
	)
	want := map[string]Status{"p/orig.jsonl": Eligible, "p/cont.jsonl": Continuation, "p/x/subagents/fork.jsonl": Fork,
		"p/after-fork.jsonl": Continuation, "p/journal.jsonl": NoUsage}
	for _, s := range ss {
		if s.Status != want[s.Path] {
			t.Errorf("%s: Status = %v, want %v", s.Path, s.Status, want[s.Path])
		}
		if s.Status != Eligible && (s.N != 0 || s.SumCtx != 0) {
			t.Errorf("%s: excluded session has N=%d SumCtx=%d", s.Path, s.N, s.SumCtx)
		}
	}
}

// TestBuildContinuationTie (review focus 2): a continuation copies the history with its
// timestamps, so both files share the first timestamp. The file that ended first is the original,
// whatever the path order.
func TestBuildContinuationTie(t *testing.T) {
	start := "2026-09-01T10:00:00Z"
	ss := built(
		tsess{Rel: "p/a-continuation.jsonl", Start: start, End: "2026-09-01T12:00:00Z", IDs: []string{"m1", "m2", "m3"}, Contexts: []int{100, 200, 300}},
		tsess{Rel: "p/b-original.jsonl", Start: start, End: "2026-09-01T11:00:00Z", IDs: []string{"m1", "m2"}, Contexts: []int{100, 200}},
	)
	if ss[0].Path != "p/b-original.jsonl" || ss[0].Status != Eligible || ss[0].N != 2 {
		t.Errorf("first = %q Status=%v N=%d, want the original, eligible, N=2", ss[0].Path, ss[0].Status, ss[0].N)
	}
	if ss[1].Path != "p/a-continuation.jsonl" || ss[1].Status != Continuation {
		t.Errorf("second = %q Status=%v, want the continuation", ss[1].Path, ss[1].Status)
	}
	// Same Start and End: the path decides, deterministically.
	ss = built(
		tsess{Rel: "p/z.jsonl", Start: start, End: start, IDs: []string{"k"}, Contexts: []int{10}},
		tsess{Rel: "p/y.jsonl", Start: start, End: start, IDs: []string{"k"}, Contexts: []int{10}},
	)
	if ss[0].Path != "p/y.jsonl" || ss[0].Status != Eligible || ss[1].Status != Continuation {
		t.Errorf("tie on Start and End: order %q/%q statuses %v/%v", ss[0].Path, ss[1].Path, ss[0].Status, ss[1].Status)
	}
}

// TestBuildEmptyIDs (review focus 3): usages without a message id never enter the global set,
// never mark a continuation and are never dropped across files.
func TestBuildEmptyIDs(t *testing.T) {
	ss := built(
		tsess{Rel: "p/a.jsonl", Start: "2026-09-01T10:00:00Z", IDs: []string{"", "", "k1"}, Contexts: []int{100, 200, 300}},
		tsess{Rel: "p/b.jsonl", Start: "2026-09-01T11:00:00Z", IDs: []string{"", ""}, Contexts: []int{400, 500}},
		tsess{Rel: "p/c.jsonl", Start: "2026-09-01T12:00:00Z", IDs: []string{"", "k1"}, Contexts: []int{600, 700}},
	)
	want := []struct {
		n      int
		sumCtx int64
	}{{3, 600}, {2, 900}, {1, 600}}
	for i, s := range ss {
		if s.Status != Eligible || s.N != want[i].n || s.SumCtx != want[i].sumCtx {
			t.Errorf("%s: Status=%v N=%d SumCtx=%d, want Eligible N=%d SumCtx=%d", s.Path, s.Status, s.N, s.SumCtx, want[i].n, want[i].sumCtx)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.1.259", "2.1.283", -1},
		{"2.1.283", "2.1.259", 1},
		{"2.1.259", "2.1.259", 0},
		{"2.1.30", "2.1.259", -1},
		{"2.1", "2.1.0", 0},
		{"2.2", "2.1.999", 1},
		{"", "2.1.259", -1},
		{"x.y", "0.0", 0},
	}
	for _, tc := range tests {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestServerOf(t *testing.T) {
	tests := []struct {
		tool, server string
		ok           bool
	}{
		{"mcp__zzsrv__query", "zzsrv", true},
		{"mcp__claude_ai_Docs__read", "claude_ai_Docs", true},
		{"mcp__zzsrv", "", false},
		{"mcp____x", "", false},
		{"Read", "", false},
	}
	for _, tc := range tests {
		if server, ok := serverOf(tc.tool); server != tc.server || ok != tc.ok {
			t.Errorf("serverOf(%q) = %q, %v", tc.tool, server, ok)
		}
	}
}
