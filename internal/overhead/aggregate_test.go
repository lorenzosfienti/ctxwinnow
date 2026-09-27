package overhead

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// flat returns n contexts equal to b: a session whose every call reads exactly B (share 1).
func flat(n, b int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// instant parses an RFC 3339 timestamp in tests.
func instant(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func paths(ss []*Session) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.Path)
	}
	return out
}

func TestPercentile(t *testing.T) {
	xs := []float64{10, 1, 9, 2, 8, 3, 7, 4, 6, 5}
	for _, c := range []struct{ p, want float64 }{{0, 1}, {0.5, 5}, {0.9, 9}, {0.95, 10}, {1, 10}} {
		if got := percentile(xs, c.p); got != c.want {
			t.Errorf("percentile(1..10, %v) = %v, want %v", c.p, got, c.want)
		}
	}
	if xs[0] != 10 {
		t.Errorf("percentile sorted its input: %v", xs)
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("percentile(nil) = %v, want 0", got)
	}
	if got := median([]float64{4, 1, 3, 2}); got != 2 {
		t.Errorf("median(1..4) = %v, want 2 (nearest rank, no interpolation)", got)
	}
	hundred := make([]float64, 100)
	for i := range hundred {
		hundred[i] = float64(i + 1)
	}
	if got := percentile(hundred, 0.07); got != 7 {
		t.Errorf("percentile(1..100, 0.07) = %v, want 7 (0.07·100 is 7.000000000000001 in float64)", got)
	}
}

func TestParseInstant(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"2026-09-27", "2026-09-27T00:00:00Z"},
		{"2026-09-27T14:30:00Z", "2026-09-27T14:30:00Z"},
		{"2026-09-27T14:30:00+02:00", "2026-09-27T12:30:00Z"},
		{"2026-09-27T01:00:00-03:30", "2026-09-27T04:30:00Z"},
		{"2026-09-27T14:30:00.250Z", "2026-09-27T14:30:00.25Z"},
	} {
		got, err := ParseInstant(c.in)
		if err != nil {
			t.Errorf("ParseInstant(%q): %v", c.in, err)
			continue
		}
		if got.Location() != time.UTC || got.Format(time.RFC3339Nano) != c.want {
			t.Errorf("ParseInstant(%q) = %v (%v), want %s UTC", c.in, got, got.Location(), c.want)
		}
	}
	for _, bad := range []string{"", "2026-9-27", "27/09/2026", "2026-09-27T14:30", "2026-09-27 14:30:00Z", "yesterday"} {
		if got, err := ParseInstant(bad); err == nil {
			t.Errorf("ParseInstant(%q) = %v, want an error", bad, got)
		}
	}
}

// TestWindowBoundaries pins review focus 5: a session starting exactly at --since is kept, exactly at
// --until is filtered, and timestamps with or without fractional seconds or offsets compare as instants.
func TestWindowBoundaries(t *testing.T) {
	since, _ := ParseInstant("2026-09-10")
	until, _ := ParseInstant("2026-09-20T12:00:00+02:00") // 10:00Z
	sessions := built(
		tsess{Rel: "a.jsonl", Start: "2026-09-10T00:00:00Z", Contexts: flat(3, 100)},          // exactly at --since: kept
		tsess{Rel: "b.jsonl", Start: "2026-09-10T00:00:00.000Z", Contexts: flat(3, 100)},      // same instant, fractional form: kept
		tsess{Rel: "c.jsonl", Start: "2026-09-09T23:59:59.999Z", Contexts: flat(3, 100)},      // just before --since: filtered
		tsess{Rel: "d.jsonl", Start: "2026-09-20T09:59:59.5Z", Contexts: flat(3, 100)},        // just before --until: kept
		tsess{Rel: "e.jsonl", Start: "2026-09-20T10:00:00Z", Contexts: flat(3, 100)},          // exactly at --until: filtered
		tsess{Rel: "f.jsonl", Start: "2026-09-20T12:00:00+02:00", Contexts: flat(3, 100)},     // same instant with an offset: filtered
		tsess{Rel: "g.jsonl", Start: "not a time", End: "not a time", Contexts: flat(3, 100)}, // no timestamp: filtered once a bound is set
	)
	sum := Summarize(sessions, Options{MinTurns: 1, Since: since, Until: until})
	if got, want := paths(sum.Eligible), []string{"a.jsonl", "b.jsonl", "d.jsonl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
	if sum.Counts.Filtered != 4 {
		t.Errorf("Filtered = %d, want 4", sum.Counts.Filtered)
	}
	for _, s := range sessions {
		if s.Path != "a.jsonl" && s.Path != "b.jsonl" && s.Path != "d.jsonl" && s.Status != Filtered {
			t.Errorf("%s: Status = %v, want Filtered", s.Path, s.Status)
		}
	}

	sum = Summarize(built(tsess{Rel: "g.jsonl", Start: "not a time", End: "not a time", Contexts: flat(3, 100)}), Options{MinTurns: 1})
	if sum.Counts.Main != 1 || sum.Counts.Filtered != 0 {
		t.Errorf("without bounds a session without timestamp is kept: Main=%d Filtered=%d", sum.Counts.Main, sum.Counts.Filtered)
	}
	sum = Summarize(built(
		tsess{Rel: "a.jsonl", Start: "2026-09-09T00:00:00Z", Contexts: flat(3, 100)},
		tsess{Rel: "b.jsonl", Start: "2026-09-11T00:00:00Z", Contexts: flat(3, 100)},
	), Options{MinTurns: 1, Since: since})
	if got := paths(sum.Eligible); !reflect.DeepEqual(got, []string{"b.jsonl"}) {
		t.Errorf("--since only: kept %v", got)
	}
}

// TestFilters checks --only and --exclude at path boundaries (/w/app never matches /w/app-legacy).
func TestFilters(t *testing.T) {
	mk := func() []*Session {
		return built(
			tsess{Rel: "1.jsonl", CWD: "/w/app", Contexts: flat(3, 100)},
			tsess{Rel: "2.jsonl", CWD: "/w/app/sub", Contexts: flat(3, 100)},
			tsess{Rel: "3.jsonl", CWD: "/w/app-legacy", Contexts: flat(3, 100)},
			tsess{Rel: "4.jsonl", CWD: "/w/other", Contexts: flat(3, 100)},
		)
	}
	for _, c := range []struct {
		name          string
		only, exclude []string
		want          []string
	}{
		{"none", nil, nil, []string{"1.jsonl", "2.jsonl", "3.jsonl", "4.jsonl"}},
		{"only", []string{"/w/app"}, nil, []string{"1.jsonl", "2.jsonl"}},
		{"only trailing separator", []string{"/w/app/"}, nil, []string{"1.jsonl", "2.jsonl"}},
		{"only two", []string{"/w/app-legacy", "/w/other"}, nil, []string{"3.jsonl", "4.jsonl"}},
		{"exclude", nil, []string{"/w/app"}, []string{"3.jsonl", "4.jsonl"}},
		{"only and exclude", []string{"/w/app"}, []string{"/w/app/sub"}, []string{"1.jsonl"}},
	} {
		sum := Summarize(mk(), Options{MinTurns: 1, Only: c.only, Exclude: c.exclude})
		if got := paths(sum.Eligible); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: kept %v, want %v", c.name, got, c.want)
		}
		if sum.Counts.Filtered != 4-len(c.want) {
			t.Errorf("%s: Filtered = %d, want %d", c.name, sum.Counts.Filtered, 4-len(c.want))
		}
	}
}

// TestSummarizeCounts gives every session exactly one reason and every eligible main session one
// decomposition outcome.
func TestSummarizeCounts(t *testing.T) {
	// 1,800 tool bytes (400 tokens) + 2,400 other bytes: with B = 1,000 the remainder is 600 tokens
	// (4 bytes per token) and the ratio 4.2 passes the guard; with B = 5,000 the ratio 0.84 fails it.
	tools := []transcript.ToolDef{tool("Bash", 900), tool("Read", 900)}
	pre := []transcript.Attachment{instr("/w/p/CLAUDE.md", "Project", 1800)}
	xs := []tsess{
		{Rel: "p/ok.jsonl", CWD: "/w/p", Start: "2026-09-02T10:00:00Z", Contexts: flat(3, 1000), SysBytes: 600, Tools: tools, Pre: pre},
		{Rel: "p/sanity.jsonl", CWD: "/w/p", Start: "2026-09-03T10:00:00Z", Contexts: flat(3, 5000), SysBytes: 600, Tools: tools, Pre: pre},
		{Rel: "p/old.jsonl", CWD: "/w/p", Start: "2026-09-04T10:00:00Z", Version: "2.1.250", Contexts: flat(3, 900)},
		{Rel: "p/nosnap.jsonl", CWD: "/w/p", Start: "2026-09-05T10:00:00Z", Version: "unknown", Contexts: flat(3, 900)},
		{Rel: "p/ok/subagents/agent-1.jsonl", CWD: "/w/p", Start: "2026-09-02T10:01:00Z", Version: "2.1.9", Contexts: flat(3, 300), Tools: tools},
		{Rel: "p/ok/subagents/agent-2.jsonl", CWD: "/w/p", Start: "2026-09-02T10:02:00Z", Contexts: flat(3, 300), Fork: true},
		{Rel: "p/cont.jsonl", CWD: "/w/p", Start: "2026-09-06T10:00:00Z", IDs: []string{"p/ok.jsonl#0"}, Contexts: flat(3, 1000)},
		{Rel: "workflows/wf_1/journal.jsonl", CWD: "/w/p", Start: "2026-08-30T10:00:00Z"},
		{Rel: "p/short.jsonl", CWD: "/w/p", Start: "2026-09-07T10:00:00Z", Contexts: flat(2, 1000)},
		{Rel: "q/out.jsonl", CWD: "/elsewhere", Start: "2026-09-08T10:00:00Z", Contexts: flat(3, 1000)},
	}
	var sessions []*Session
	for _, x := range xs {
		sessions = append(sessions, x.session())
	}
	sessions[0].Malformed, sessions[7].Malformed = 2, 1
	sessions[1].CompactBeforeFirstUsage = true
	sessions[1].Entrypoint, sessions[2].Entrypoint = "claude-vscode", "claude-vscode"
	sessions[3].Entrypoint = ""
	opt := Options{MinTurns: 3, Only: []string{"/w"}}
	Build(sessions)
	for pass := 1; pass <= 2; pass++ { // Summarize re-evaluates Filtered / TooFewTurns: calling it twice is harmless
		sum := Summarize(sessions, opt)
		want := Counts{Scanned: 10, Main: 4, Sub: 1, Decomposed: 1, NoUsage: 1, Continuation: 1, Fork: 1, Filtered: 1,
			TooFewTurns: 1, OldVersion: 1, NoSnapshot: 1, Sanity: 1, Malformed: 3, CompactBeforeFirstUsage: 1}
		if sum.Counts != want {
			t.Errorf("pass %d: Counts:\n got  %+v\n want %+v", pass, sum.Counts, want)
		}
		if got := paths(sum.Eligible); !reflect.DeepEqual(got, []string{"p/ok.jsonl", "p/sanity.jsonl", "p/old.jsonl", "p/nosnap.jsonl"}) {
			t.Errorf("pass %d: Eligible = %v", pass, got)
		}
		if got := paths(sum.EligibleSub); !reflect.DeepEqual(got, []string{"p/ok/subagents/agent-1.jsonl"}) {
			t.Errorf("pass %d: EligibleSub = %v", pass, got)
		}
		if got := paths(sum.Decomposed); !reflect.DeepEqual(got, []string{"p/ok.jsonl"}) {
			t.Errorf("pass %d: Decomposed = %v", pass, got)
		}
		if !sum.Oldest.Equal(instant("2026-08-30T10:00:00Z")) {
			t.Errorf("pass %d: Oldest = %v, want the journal's start (every scanned session counts)", pass, sum.Oldest)
		}
		if want := []NameCount{{"claude-vscode", 2}, {"cli", 1}, {"unknown", 1}}; !reflect.DeepEqual(sum.Entrypoints, want) {
			t.Errorf("pass %d: Entrypoints = %v, want %v", pass, sum.Entrypoints, want)
		}
		if sum.VersionMin != "2.1.9" || sum.VersionMax != "2.1.283" {
			t.Errorf("pass %d: versions %q … %q, want 2.1.9 … 2.1.283 (numeric order, unknown skipped)", pass, sum.VersionMin, sum.VersionMax)
		}
		if !near(sum.RemainderBPT, 4) {
			t.Errorf("pass %d: RemainderBPT = %v, want 4", pass, sum.RemainderBPT)
		}
		if sum.MainTokenTurns != 3000+15000+2700+2700 {
			t.Errorf("pass %d: MainTokenTurns = %v", pass, sum.MainTokenTurns)
		}
	}
	for _, s := range sessions {
		wantStatus := map[string]Status{"p/cont.jsonl": Continuation, "workflows/wf_1/journal.jsonl": NoUsage,
			"p/short.jsonl": TooFewTurns, "q/out.jsonl": Filtered, "p/ok/subagents/agent-2.jsonl": Fork}[s.Path]
		if s.Status != wantStatus {
			t.Errorf("%s: Status = %v, want %v", s.Path, s.Status, wantStatus)
		}
	}
}

func TestBaselineRows(t *testing.T) {
	var xs []tsess
	for i := 1; i <= 10; i++ {
		xs = append(xs, tsess{Rel: fmt.Sprintf("p/%02d.jsonl", i), Contexts: flat(2, i*100)})
	}
	sum := Summarize(built(xs...), Options{MinTurns: 1})
	if m := sum.Main; m.N != 10 || m.BP50 != 500 || m.BP90 != 900 || !m.HasP90 || m.MedianN != 2 || !near(m.Pooled, 1) || !near(m.Typical, 1) {
		t.Errorf("Main = %+v", m)
	}
	if sum := Summarize(built(xs[:9]...), Options{MinTurns: 1}); sum.Main.HasP90 {
		t.Errorf("HasP90 with n = 9")
	}

	// Pooled weighs long sessions; typical is the median of per-session shares.
	sum = Summarize(built(
		tsess{Rel: "a.jsonl", Contexts: []int{100, 100}},
		tsess{Rel: "b.jsonl", Contexts: []int{1000, 9000, 10000}},
		tsess{Rel: "a/subagents/agent-1.jsonl", Contexts: []int{50, 150}, Tools: []transcript.ToolDef{tool("Read", 100), tool("Artifact", 900)}},
		tsess{Rel: "b/subagents/agent-2.jsonl", Contexts: []int{80, 80}, Tools: []transcript.ToolDef{tool("Read", 100)}},
		tsess{Rel: "b/subagents/agent-3.jsonl", Contexts: []int{70}},
	), Options{MinTurns: 1})
	if m := sum.Main; m.N != 2 || m.BP50 != 100 || m.BP90 != 1000 || m.HasP90 || !near(m.Pooled, 3200.0/20200) || !near(m.Typical, 0.15) || m.MedianN != 2 {
		t.Errorf("Main = %+v", m)
	}
	if sum.MainTokenTurns != 20200 {
		t.Errorf("MainTokenTurns = %v, want 20200", sum.MainTokenTurns)
	}
	if s := sum.Sub; s.N != 3 || s.BP50 != 70 || !near(s.Pooled, 330.0/430) || !near(s.Typical, 1) || s.MedianN != 2 || s.Snapshots != 2 || s.ArtifactCarried != 1 {
		t.Errorf("Sub = %+v", s)
	}
}

// TestComponentRows folds instruction files into one row, ranks rows by total ≈tokens and gives each
// its share of Σ B over decomposed main sessions (the shares sum to 1 because components sum to B).
func TestComponentRows(t *testing.T) {
	canon := loadCanonical(t, "p/zz.jsonl")
	// other: 1,800 tool bytes (400 tokens), 600 system-prompt bytes over R = 600 (ratio 2.4, guard passes).
	other := tsess{Rel: "q/b.jsonl", CWD: "/w/q", Start: "2026-09-21T10:00:00Z", Contexts: flat(3, 1000), SysBytes: 600,
		Tools: []transcript.ToolDef{tool("Bash", 900), tool("Read", 900)}}.session()
	sum := Summarize(Build([]*Session{canon, other}), Options{MinTurns: 1})
	if len(sum.Decomposed) != 2 {
		t.Fatalf("Decomposed = %v", paths(sum.Decomposed))
	}
	var keys []string
	var share float64
	rows := map[string]ComponentRow{}
	for _, r := range sum.Components {
		keys = append(keys, r.Key)
		share += r.ShareOfB
		rows[r.Key] = r
	}
	want := []string{"system-prompt", "tool:Bash", "tool:Read", "tool:Artifact", "instructions", "harness", "skill-listing",
		"mcp", "tool:Edit", "tool:ToolSearch", "mcp-tool:zzsrv", "tool:Skill", "hook", "first-prompt"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("rows:\n got  %v\n want %v", keys, want)
	}
	if !near(share, 1) {
		t.Errorf("Σ ShareOfB = %v, want 1", share)
	}
	r := 1000 - 1909/toolJSONBytesPerToken
	if b := rows["tool:Bash"]; b.Kind != CompTool || b.Name != "Bash" || b.Present != 2 || !near(b.MedianTokens, 168/4.5) || !near(b.ShareOfB, (168/4.5+200)/2000) {
		t.Errorf("Bash row = %+v", b)
	}
	if in := rows["instructions"]; in.Kind != CompInstruction || in.Name != "" || in.Present != 1 || !near(in.MedianTokens, r*484/1830) {
		t.Errorf("instructions row = %+v", in)
	}
	if m := rows["mcp-tool:zzsrv"]; m.Kind != CompMCPTool || m.Name != "zzsrv" || m.Present != 1 {
		t.Errorf("zzsrv row = %+v", m)
	}
}

// TestUnusedCost ranks carried tools by the token-turns they cost in decomposed sessions that never
// call them; MCP tools group per server (a call to any tool of the server counts), calls and projects
// count over every eligible main session, and the share divides by all main token-turns.
func TestUnusedCost(t *testing.T) {
	// 5,400 tool bytes (1,200 tokens) + 5,400 system-prompt bytes; B = 3,000 → R = 1,800 (3 bytes per token).
	tools := []transcript.ToolDef{tool("Artifact", 4500), tool("mcp__db__q", 225), tool("Read", 450), tool("mcp__db__w", 225)}
	sum := Summarize(built(
		tsess{Rel: "a/1.jsonl", CWD: "/w/a", Start: "2026-09-01T10:00:00Z", Contexts: flat(10, 3000), SysBytes: 5400, Tools: tools, Calls: []string{"Read"}},
		tsess{Rel: "b/2.jsonl", CWD: "/w/b", Start: "2026-09-02T10:00:00Z", Contexts: flat(20, 3000), SysBytes: 5400, Tools: tools, Calls: []string{"mcp__db__w", "Read"}},
		tsess{Rel: "b/3.jsonl", CWD: "/w/b", Start: "2026-09-03T10:00:00Z", Contexts: flat(5, 3000), SysBytes: 5400, Tools: tools, Calls: []string{"Artifact"}},
		tsess{Rel: "c/4.jsonl", CWD: "/w/c", Start: "2026-09-04T10:00:00Z", Contexts: flat(5, 2000), Calls: []string{"Artifact"}}, // eligible, no snapshot
	), Options{MinTurns: 1})
	if sum.MainTokenTurns != 115000 {
		t.Fatalf("MainTokenTurns = %v, want 115000", sum.MainTokenTurns)
	}
	want := []UnusedRow{
		{Key: "tool:Artifact", Name: "Artifact", Carrying: 3, Calling: 2, Projects: 2, LastCall: instant("2026-09-04T10:05:00Z"),
			MedianTokens: 1000, TokenTurns: 30000, Share: 30000.0 / 115000},
		{Key: "mcp-tool:db", Name: "db", MCP: true, Carrying: 3, Calling: 1, Projects: 1, LastCall: instant("2026-09-02T10:20:00Z"),
			MedianTokens: 100, TokenTurns: 1500, Share: 1500.0 / 115000},
		{Key: "tool:Read", Name: "Read", Carrying: 3, Calling: 2, Projects: 2, LastCall: instant("2026-09-02T10:20:00Z"),
			MedianTokens: 100, TokenTurns: 500, Share: 500.0 / 115000},
	}
	if len(sum.Unused) != len(want) {
		t.Fatalf("Unused = %+v", sum.Unused)
	}
	for i, w := range want {
		g := sum.Unused[i]
		if g.Key != w.Key || g.Name != w.Name || g.MCP != w.MCP || g.Carrying != w.Carrying || g.Calling != w.Calling ||
			g.Projects != w.Projects || !g.LastCall.Equal(w.LastCall) || !near(g.MedianTokens, w.MedianTokens) ||
			!near(g.TokenTurns, w.TokenTurns) || !near(g.Share, w.Share) {
			t.Errorf("row %d:\n got  %+v\n want %+v", i, g, w)
		}
	}
}

func TestUnusedCap(t *testing.T) {
	var tools []transcript.ToolDef
	for i := 20; i >= 1; i-- {
		tools = append(tools, tool(fmt.Sprintf("T%02d", i), 90)) // 20 tokens each, equal cost
	}
	sum := Summarize(built(tsess{Rel: "a.jsonl", Contexts: flat(3, 1000), SysBytes: 2400, Tools: tools}), Options{MinTurns: 1})
	var names []string
	for _, r := range sum.Unused {
		names = append(names, r.Name)
	}
	want := []string{"T01", "T02", "T03", "T04", "T05", "T06", "T07", "T08", "T09", "T10", "T11", "T12", "T13", "T14", "T15"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Unused = %v, want %v (≤ %d rows, ties by name)", names, want, MaxUnusedRows)
	}
}

// TestSkillMCPInstructionRows pins the canonical skill listing, MCP and instruction-file rows, then
// adds an eligible session without a snapshot: its skill and MCP usage still count.
func TestSkillMCPInstructionRows(t *testing.T) {
	sum := Summarize(Build([]*Session{loadCanonical(t, "p/zz.jsonl")}), Options{MinTurns: 1})
	r := 1000 - 1909/toolJSONBytesPerToken
	sk := sum.Skills
	if sk.Sessions != 1 || !near(sk.MedianTokens, r*352/1830) || sk.Listed != 5 || sk.Used != 3 || !near(sk.NeverUsedTokens, r*(65+59)/1830) {
		t.Errorf("Skills = %+v", sk)
	}
	wantSkills := []SkillStat{
		{Name: "zzidle", Sessions: 1, MedianBytes: 65},
		{Name: "zzplug:helper", Plugin: true, Sessions: 1, MedianBytes: 59},
		{Name: "code-review", Sessions: 1, MedianBytes: 75, Used: true},
		{Name: "zzskill", Sessions: 1, MedianBytes: 55, Used: true},
		{Name: "zzbare", Sessions: 1, MedianBytes: 8, Used: true},
	}
	if !reflect.DeepEqual(sk.Skills, wantSkills) {
		t.Errorf("Skills.Skills:\n got  %+v\n want %+v", sk.Skills, wantSkills)
	}
	if m := sum.MCP; m.Sessions != 1 || !near(m.MedianTokens, r*293/1830) || !reflect.DeepEqual(m.Carried, []string{"zzsrv"}) || m.Called != nil {
		t.Errorf("MCP = %+v", m)
	}
	var files []string
	for _, in := range sum.Instructions {
		files = append(files, in.Label+" "+in.Path)
	}
	wantFiles := []string{
		"Project /home/zzuser/zzproj/CLAUDE.md",
		"User /home/zzuser/.claude/CLAUDE.md",
		"Project (non-CLAUDE.md, likely @import) /home/zzuser/zzproj/docs/RULES.md",
		"AutoMem /home/zzuser/.claude/projects/-home-zzuser-zzproj/memory/MEMORY.md",
		"Project /home/zzuser/CLAUDE.md",
	}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("Instructions:\n got  %v\n want %v", files, wantFiles)
	}
	if in := sum.Instructions[0]; in.Sessions != 1 || !near(in.MedianTokens, r*119/1830) {
		t.Errorf("first instruction row = %+v", in)
	}
	if !reflect.DeepEqual(sum.Entrypoints, []NameCount{{"cli", 1}}) || sum.VersionMin != "2.1.283" || sum.VersionMax != "2.1.283" ||
		!near(sum.RemainderBPT, 1830/r) || !sum.Oldest.Equal(instant("2026-09-20T09:00:00Z")) {
		t.Errorf("Entrypoints=%v versions %s…%s RemainderBPT=%v Oldest=%v", sum.Entrypoints, sum.VersionMin, sum.VersionMax, sum.RemainderBPT, sum.Oldest)
	}

	nosnap := tsess{Rel: "q/b.jsonl", CWD: "/w/q", Start: "2026-09-21T10:00:00Z", Contexts: flat(3, 1000),
		SkillCalls: []string{"zzidle"}, Calls: []string{"mcp__zzsrv__query"}}.session()
	sum = Summarize(Build([]*Session{loadCanonical(t, "p/zz.jsonl"), nosnap}), Options{MinTurns: 1})
	if sk := sum.Skills; sk.Used != 4 || !near(sk.NeverUsedTokens, r*59/1830) || sk.Skills[0].Name != "zzplug:helper" {
		t.Errorf("with a second session: Skills = %+v", sk)
	}
	if !reflect.DeepEqual(sum.MCP.Called, []string{"zzsrv"}) {
		t.Errorf("MCP.Called = %v, want [zzsrv]", sum.MCP.Called)
	}
}
