package overhead

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// lsess is a decomposable main session for lever tests: Artifact is 4,500 bytes (1,000 tokens),
// every other tool 450 bytes (100 tokens), the system prompt 3,000 bytes, and B = tool tokens +
// 1,000, so the remainder is 1,000 tokens at 3 bytes per token and every call reads exactly B.
func lsess(rel, cwd, start string, n int, tools []string, calls ...string) tsess {
	var defs []transcript.ToolDef
	tok := 1000
	for _, name := range tools {
		b := 450
		if name == "Artifact" {
			b = 4500
		}
		defs = append(defs, tool(name, b))
		tok += b * 2 / 9
	}
	return tsess{Rel: rel, CWD: cwd, Start: start, Contexts: flat(n, tok), SysBytes: 3000, Tools: defs, Calls: calls}
}

func ids(rs []LeverResult) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func find(t *testing.T, rs []LeverResult, id, subject string) LeverResult {
	t.Helper()
	for _, r := range rs {
		if r.ID == id && r.Subject == subject {
			return r
		}
	}
	t.Fatalf("no result %s %q in %v", id, subject, ids(rs))
	return LeverResult{}
}

func sameSaving(a, b Saving) bool {
	return a.Sessions == b.Sessions && a.Calls == b.Calls && a.Projects == b.Projects && near(a.TokenTurns, b.TokenTurns) &&
		near(a.PerCall, b.PerCall) && near(a.Share, b.Share)
}

// TestCatalog pins spec §7: explicit targets, never a shared Project scope, off-wins only where the
// setting is known to win, and a status that says VERIFIED or UNVERIFIED with its source.
func TestCatalog(t *testing.T) {
	want := []string{"disable-artifact", "disable-sendfeedback", "disable-workflows", "skill-visibility", "instructions", "mcp"}
	var got []string
	for _, lv := range Catalog {
		got = append(got, lv.ID)
		if strings.Contains(lv.Target, "*") || lv.Target == "" {
			t.Errorf("%s: target %q is not explicit", lv.ID, lv.Target)
		}
		if !strings.HasPrefix(lv.Status, "VERIFIED 2.1.283 (gate 0): ") && !strings.HasPrefix(lv.Status, "UNVERIFIED: documented at https://code.claude.com/docs/en/") {
			t.Errorf("%s: status %q", lv.ID, lv.Status)
		}
		if !strings.HasPrefix(lv.DocURL, "https://code.claude.com/docs/en/") || lv.Tradeoff == "" {
			t.Errorf("%s: DocURL %q, Tradeoff %q", lv.ID, lv.DocURL, lv.Tradeoff)
		}
		for _, sn := range lv.Snippets {
			switch sn.Scope {
			case ScopeUser, ScopeLocal, ScopeEnv, ScopeFlag:
			default:
				t.Errorf("%s: scope %q (a shared Project scope would disable the feature for every collaborator)", lv.ID, sn.Scope)
			}
			if sn.OffWins != (strings.Contains(sn.Text, `"enableArtifact": false`) && (sn.Scope == ScopeUser || sn.Scope == ScopeLocal)) {
				t.Errorf("%s: OffWins = %v for %s %s", lv.ID, sn.OffWins, sn.Scope, sn.Text)
			}
			rest := strings.NewReplacer("{{skills}}", "", "{{path}}", "").Replace(sn.Text)
			if sn.Text == "" || strings.Contains(rest, "{{") {
				t.Errorf("%s: snippet %q", lv.ID, sn.Text)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("catalog order %v, want %v", got, want)
	}
	if lv, _ := LeverFor("mcp"); len(lv.Snippets) != 0 {
		t.Errorf("the mcp lever is informational, got snippets %v", lv.Snippets)
	}
}

// TestLeverFor: only explicitly catalogued targets have a lever; core and feature-backing tools
// print "no known safe lever" (denying ToolSearch, for example, would switch tool search off).
func TestLeverFor(t *testing.T) {
	for target, id := range map[string]string{"tool:Artifact": "disable-artifact", "tool:SendFeedback": "disable-sendfeedback",
		"tool:Workflow": "disable-workflows", "skill-listing": "skill-visibility", "instructions": "instructions", "mcp": "mcp"} {
		if lv, ok := LeverFor(target); !ok || lv.ID != id {
			t.Errorf("LeverFor(%q) = %q, %v; want %q", target, lv.ID, ok, id)
		}
	}
	for _, target := range []string{"tool:Read", "tool:Edit", "tool:ToolSearch", "tool:Bash", "tool:Skill", "tool:Write",
		"tool:AskUserQuestion", "tool:ReportFindings", "tool:ScheduleWakeup", "tool:ListAgents", "tool:*", "tool:mcp__zzsrv__query",
		"tool:artifact", ""} {
		if lv, ok := LeverFor(target); ok {
			t.Errorf("LeverFor(%q) = %q, want no lever", target, lv.ID)
		}
	}
}

// TestEvaluateToolScopes: the user scope is every carrying decomposed session; the project scope keeps
// sessions in CWDs where no eligible main session called the tool; usage and presence count every
// eligible main session.
func TestEvaluateToolScopes(t *testing.T) {
	sum := Summarize(built(
		lsess("a/1.jsonl", "/w/a", "2026-09-01T10:00:00Z", 10, []string{"Artifact", "Read"}, "Read"),
		lsess("a/2.jsonl", "/w/a", "2026-09-02T10:00:00Z", 20, []string{"Artifact", "Read"}),
		lsess("b/1.jsonl", "/w/b", "2026-09-03T10:00:00Z", 5, []string{"Artifact", "Read"}, "Artifact"),
		tsess{Rel: "c/1.jsonl", CWD: "/w/c", Start: "2026-09-04T10:00:00Z", Contexts: flat(5, 2000), Calls: []string{"Artifact"}},
		lsess("b/2.jsonl", "/w/b", "2026-09-05T10:00:00Z", 4, []string{"Read"}),
		lsess("d/1.jsonl", "/w/d", "2026-09-06T10:00:00Z", 2, []string{"Read"}),
	), Options{MinTurns: 1})
	if sum.MainTokenTurns != 90100 {
		t.Fatalf("MainTokenTurns = %v, want 90100", sum.MainTokenTurns)
	}
	rs := Evaluate(sum)
	if got, want := ids(rs), []string{"disable-artifact", "disable-sendfeedback", "disable-workflows", "mcp", "skill-visibility"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results %v, want %v", got, want)
	}
	a := find(t, rs, "disable-artifact", "Artifact")
	if !sameSaving(a.User, Saving{Sessions: 3, Calls: 35, Projects: 2, TokenTurns: 35000, PerCall: 1000, Share: 35000.0 / 90100}) {
		t.Errorf("User = %+v", a.User)
	}
	if !a.HasProject || !sameSaving(a.Project, Saving{Sessions: 2, Calls: 30, Projects: 1, TokenTurns: 30000, PerCall: 1000, Share: 30000.0 / 90100}) {
		t.Errorf("HasProject = %v, Project = %+v", a.HasProject, a.Project)
	}
	if u := a.Usage; u.Called != 2 || u.Carrying != 3 || u.Projects != 2 || !u.Last.Equal(instant("2026-09-04T10:05:00Z")) {
		t.Errorf("Usage = %+v", u)
	}
	want := Presence{LastSent: instant("2026-09-03T10:00:00Z"), LastVersion: "2.1.283", AbsentRecent: 2, AbsentProjects: 2}
	if a.Presence == nil || *a.Presence != want {
		t.Errorf("Presence = %+v, want %+v", a.Presence, want)
	}
	if !a.Printable || len(a.Snippets) != 4 || a.Action != "" {
		t.Errorf("Printable = %v, Snippets = %v, Action = %q", a.Printable, a.Snippets, a.Action)
	}
	w := find(t, rs, "disable-workflows", "Workflow")
	if w.Printable || w.User.TokenTurns != 0 || w.Presence == nil || !w.Presence.LastSent.IsZero() || w.Presence.AbsentRecent != 5 {
		t.Errorf("never-carried tool: Printable = %v, User = %+v, Presence = %+v", w.Printable, w.User, w.Presence)
	}
}

// TestCoreToolsNoLever: Read, Edit, ToolSearch and Bash unused in every session produce no result
// and no snippet, and no result ever carries a Project scope.
func TestCoreToolsNoLever(t *testing.T) {
	sum := Summarize(built(
		lsess("a/1.jsonl", "/w/a", "2026-09-01T10:00:00Z", 10, []string{"Read", "Edit", "ToolSearch", "Bash"}),
		lsess("a/2.jsonl", "/w/a", "2026-09-02T10:00:00Z", 10, []string{"Read", "Edit", "ToolSearch", "Bash"}),
	), Options{MinTurns: 1})
	if len(sum.Unused) != 4 || sum.Unused[0].Share < MinBenefit {
		t.Fatalf("the core tools must be costly and unused: %+v", sum.Unused)
	}
	for _, r := range Evaluate(sum) {
		if _, ok := LeverFor(r.Target); !ok {
			t.Errorf("result for the uncatalogued target %q", r.Target)
		}
		switch r.Subject {
		case "Read", "Edit", "ToolSearch", "Bash":
			t.Errorf("core tool %s got the lever %s", r.Subject, r.ID)
		}
		if r.Printable {
			t.Errorf("%s printable without anything to save: %+v", r.ID, r.User)
		}
		for _, sn := range r.Snippets {
			if sn.Scope != ScopeUser && sn.Scope != ScopeLocal && sn.Scope != ScopeEnv && sn.Scope != ScopeFlag {
				t.Errorf("%s: scope %q", r.ID, sn.Scope)
			}
		}
	}
}

// TestMinimumBenefit: a lever prints only when its user-scope saving is ≥ 0.5% of main token-turns.
func TestMinimumBenefit(t *testing.T) {
	mk := func(tokens float64) *Summary {
		s := &Session{Path: "a.jsonl", CWD: "/w/a", B: 1000, N: 10, SumCtx: 100000, Status: Eligible, Decomp: DecompOK,
			HasSnapshot: true, ToolNames: []string{"Artifact"},
			Components: []Component{{Kind: CompTool, Name: "Artifact", Bytes: 1, Tokens: tokens}},
			Called:     map[string]time.Time{}, Listed: map[string]int{}, SkillsUsed: map[string]bool{}}
		return &Summary{Eligible: []*Session{s}, Decomposed: []*Session{s}, MainTokenTurns: 100000}
	}
	if a := find(t, Evaluate(mk(50)), "disable-artifact", "Artifact"); !a.Printable || a.User.Share != MinBenefit {
		t.Errorf("share %v: Printable = %v, want true at exactly %v", a.User.Share, a.Printable, MinBenefit)
	}
	if a := find(t, Evaluate(mk(49.99)), "disable-artifact", "Artifact"); a.Printable {
		t.Errorf("share %v: Printable = true below %v", a.User.Share, MinBenefit)
	}
}

// TestSkillVisibility generates skillOverrides for at most 10 never-used, non-plugin skills, largest
// line first, and saves their share of the listing tokens.
func TestSkillVisibility(t *testing.T) {
	lines := []transcript.SkillLine{{Bytes: 100}}
	for i := 1; i <= 12; i++ {
		lines = append(lines, transcript.SkillLine{Name: fmt.Sprintf("s%02d", i), Bytes: 10 * i})
	}
	lines = append(lines, transcript.SkillLine{Name: "zzplug:big", Bytes: 500}, transcript.SkillLine{Name: "zzused", Bytes: 300},
		transcript.SkillLine{Name: "zzempty", Bytes: 0})
	lst := listing(lines...) // 100 + 780 + 500 + 300 + 0 bytes + 15 newlines = 1,695
	// Read: 450 bytes (100 tokens); B = 2,100 → R = 2,000 over 3,000 + 1,695 other bytes.
	sum := Summarize(built(tsess{Rel: "a.jsonl", CWD: "/w/a", Contexts: flat(10, 2100), SysBytes: 3000,
		Tools: []transcript.ToolDef{tool("Read", 450)}, Pre: []transcript.Attachment{lst}, SkillCalls: []string{"zzused"}}), Options{MinTurns: 1})
	if len(sum.Decomposed) != 1 || lst.Bytes != 1695 {
		t.Fatalf("Decomposed = %d, listing bytes = %d", len(sum.Decomposed), lst.Bytes)
	}
	r := find(t, Evaluate(sum), "skill-visibility", "")
	want := []string{"s12", "s11", "s10", "s09", "s08", "s07", "s06", "s05", "s04", "s03"}
	if !reflect.DeepEqual(r.Skills, want) {
		t.Errorf("Skills = %v, want %v (≤ %d, never used, no plugin, no zero-byte line, largest first)", r.Skills, want, MaxSkillOverrides)
	}
	perCall := 2000.0 * 750 / 4695 // listing tokens × hidden bytes / listing bytes
	if !sameSaving(r.User, Saving{Sessions: 1, Calls: 10, Projects: 1, TokenTurns: perCall * 10, PerCall: perCall, Share: perCall * 10 / 21000}) || !r.Printable {
		t.Errorf("User = %+v, Printable = %v; want %v per call", r.User, r.Printable, perCall)
	}
	if r.HasProject || r.Usage != (LeverUsage{}) || r.Presence != nil || len(r.Snippets) != 2 {
		t.Errorf("skill-visibility is user/local only: %+v", r)
	}
}

// TestInstructionActions: one result per file of ≥ 2k median ≈tokens; User and in-cwd Project files
// are trimmed, a Project file in a strict ancestor of every carrying cwd is excluded, AutoMem is
// pruned, anything else has no lever. Results with a Local snippet (exclude, prune) carry a project
// scope equal to the user scope, with the number of projects that need the snippet.
func TestInstructionActions(t *testing.T) {
	// Read: 100 tokens; B = 16,400 → R = 16,300 over 48,900 other bytes (3 bytes per token): every
	// 9,000-byte file is 3,000 ≈tokens, the 900-byte file 300.
	x := tsess{Rel: "p1/a.jsonl", CWD: "/w/p1", Contexts: flat(5, 16400), SysBytes: 3000,
		Tools: []transcript.ToolDef{tool("Read", 450)},
		Pre: []transcript.Attachment{
			instr("/home/u/.claude/CLAUDE.md", "User", 9000),
			instr("/w/p1/CLAUDE.md", "Project", 9000),
			instr("/w/CLAUDE.md", "Project", 9000),
			instr("/home/u/.claude/projects/-w-p1/memory/MEMORY.md", "AutoMem", 9000),
			instr("/etc/claude-code/CLAUDE.md", "Managed", 9000),
			instr("/w/p1/docs/SMALL.md", "Project", 900),
		}}
	sum := Summarize(built(x), Options{MinTurns: 1})
	var got []string
	for _, r := range Evaluate(sum) {
		if r.ID != "instructions" {
			continue
		}
		var snippets []string
		for _, sn := range r.Snippets {
			snippets = append(snippets, string(sn.Scope)+" "+sn.Text)
		}
		got = append(got, fmt.Sprintf("%s|%s|%s|%v|%v", r.Subject, r.FileLabel, r.Action, r.Printable, snippets))
		if !sameSaving(r.User, Saving{Sessions: 1, Calls: 5, Projects: 1, TokenTurns: 15000, PerCall: 3000, Share: 15000.0 / 82000}) {
			t.Errorf("%s: User = %+v", r.Subject, r.User)
		}
		local := r.Action == "exclude" || r.Action == "prune"
		if r.HasProject != local || local && !sameSaving(r.Project, r.User) {
			t.Errorf("%s (%s): HasProject = %v, Project = %+v, User = %+v", r.Subject, r.Action, r.HasProject, r.Project, r.User)
		}
	}
	want := []string{
		"/etc/claude-code/CLAUDE.md|other|none|false|[]",
		"/home/u/.claude/CLAUDE.md|User|trim|true|[]",
		`/home/u/.claude/projects/-w-p1/memory/MEMORY.md|AutoMem|prune|true|[Local "autoMemoryEnabled": false]`,
		`/w/CLAUDE.md|Project|exclude|true|[Local "claudeMdExcludes": [{{path}}]]`,
		"/w/p1/CLAUDE.md|Project|trim|true|[]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("instruction results:\n got  %q\n want %q", got, want)
	}

	// A second session whose cwd is the file's own directory: /w is no longer a strict ancestor of
	// every carrying cwd, so /w/CLAUDE.md is trimmed instead.
	y := x
	y.Rel, y.CWD, y.Start = "w/b.jsonl", "/w", "2026-09-02T10:00:00Z"
	for _, r := range Evaluate(Summarize(built(x, y), Options{MinTurns: 1})) {
		if r.Subject == "/w/CLAUDE.md" && (r.Action != "trim" || r.Snippets != nil || r.HasProject) {
			t.Errorf("/w/CLAUDE.md with a session in /w: Action = %q, Snippets = %v, HasProject = %v", r.Action, r.Snippets, r.HasProject)
		}
	}

	// A second project under /w: /w/CLAUDE.md stays excluded, and its Local snippet is needed in both
	// projects, so the project scope counts 2 projects.
	z := x
	z.Rel, z.CWD, z.Start = "p2/c.jsonl", "/w/p2", "2026-09-03T10:00:00Z"
	for _, r := range Evaluate(Summarize(built(x, z), Options{MinTurns: 1})) {
		if r.Subject != "/w/CLAUDE.md" {
			continue
		}
		want := Saving{Sessions: 2, Calls: 10, Projects: 2, TokenTurns: 30000, PerCall: 3000, Share: 30000.0 / 164000}
		if r.Action != "exclude" || !r.HasProject || !sameSaving(r.User, want) || !sameSaving(r.Project, want) {
			t.Errorf("/w/CLAUDE.md in two projects: Action = %q, HasProject = %v, User = %+v, Project = %+v", r.Action, r.HasProject, r.User, r.Project)
		}
	}
}

func TestStrictAncestor(t *testing.T) {
	for _, c := range []struct {
		dir, cwd string
		want     bool
	}{
		{"/w", "/w/p1", true},
		{"/w/", "/w/p1/x", true},
		{"/w/p1", "/w/p1", false},
		{"/w/p1", "/w/p1-legacy", false},
		{"/w/p1/sub", "/w/p1", false},
		{"", "/w", false},
	} {
		if got := strictAncestor(c.dir, c.cwd); got != c.want {
			t.Errorf("strictAncestor(%q, %q) = %v, want %v", c.dir, c.cwd, got, c.want)
		}
	}
}

// TestEvaluateCanonical ranks levers by user-scope token-turns on the canonical session.
func TestEvaluateCanonical(t *testing.T) {
	sum := Summarize(Build([]*Session{loadCanonical(t, "p/zz.jsonl")}), Options{MinTurns: 1})
	rs := Evaluate(sum)
	if got, want := ids(rs), []string{"disable-artifact", "mcp", "skill-visibility", "disable-sendfeedback", "disable-workflows"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results %v, want %v", got, want)
	}
	r := 1000 - 1909/toolJSONBytesPerToken
	if a := find(t, rs, "disable-artifact", "Artifact"); !near(a.User.TokenTurns, 915/4.5*4) || !a.Printable || a.Project.Sessions != 1 {
		t.Errorf("Artifact: User = %+v, Project = %+v", a.User, a.Project)
	}
	if m := find(t, rs, "mcp", ""); !near(m.User.TokenTurns, r*293/1830*4) || !m.Printable || m.Snippets != nil {
		t.Errorf("mcp: %+v", m)
	}
	if s := find(t, rs, "skill-visibility", ""); !reflect.DeepEqual(s.Skills, []string{"zzidle"}) || !near(s.User.PerCall, r*65/1830) || !s.Printable {
		t.Errorf("skill-visibility: Skills = %v, User = %+v", s.Skills, s.User)
	}
}

// TestEvaluateGolden pins the levers of the T6 golden input (rendered by T8).
func TestEvaluateGolden(t *testing.T) {
	var got []string
	for _, r := range Evaluate(goldenSummary(t)) {
		got = append(got, fmt.Sprintf("%s %s %s %v %.4f", r.ID, r.Subject, r.Action, r.Printable, r.User.Share))
	}
	want := []string{
		"instructions /home/zzuser/work/CLAUDE.md exclude true 0.1860",
		"disable-artifact Artifact  true 0.1169",
		"disable-workflows Workflow  true 0.0119",
		"mcp   true 0.0052",
		"disable-sendfeedback SendFeedback  false 0.0040",
		"skill-visibility   false 0.0036",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("golden levers:\n got  %q\n want %q", got, want)
	}
}
