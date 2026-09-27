package overhead

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

// goldenFile compares got with testdata/name, or rewrites it with -update.
func goldenFile(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("report differs from %s (run go test ./internal/overhead/ -run Golden -update and review the diff)\n%s", path, got)
	}
}

// render runs Render and fails the test on error.
func render(t *testing.T, r *Report) string {
	t.Helper()
	var b strings.Builder
	if err := Render(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// goldenSessions is the synthetic input of report.golden (also used by the leak test and by T8): the
// canonical session plus sessions that exercise every row and every exclusion reason. Build order:
// zz-session-4 (old version), 1 (canonical), 2, its subagents, the workflow journal, 3, 5 (continuation
// of 1), 6 (too few turns), 7 (sanity guard), 8 (after --until).
func goldenSessions(t *testing.T) []*Session {
	t.Helper()
	sessions := []*Session{loadCanonical(t, "-home-zzuser-zzproj/zz-session-1.jsonl")}
	for _, x := range []tsess{
		// 8,010 tool bytes (1,780 tokens) + 3,313 other bytes over R = 1,220: 2.72 bytes per token.
		{Rel: "-home-zzuser-zzproj/zz-session-2.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-21T10:00:00Z",
			Contexts: []int{3000, 3200, 3400, 3600, 3800}, SysBytes: 1200,
			Tools: []transcript.ToolDef{tool("Bash", 900), tool("Read", 900), tool("Edit", 900), tool("Artifact", 4500),
				tool("SendFeedback", 360), tool("mcp__zzdb__query", 450)},
			Pre: []transcript.Attachment{
				att(transcript.AttFirstPrompt, 72), att(transcript.AttEnvironment, 130),
				att(transcript.AttDeferredDelta, 27, "CronCreate", "mcp__zzsrv__query"), att(transcript.AttMCPInstructions, 60, "zzsrv"),
				listing(transcript.SkillLine{Bytes: 35}, transcript.SkillLine{Name: "code-review", Bytes: 75},
					transcript.SkillLine{Name: "zzskill", Bytes: 55}, transcript.SkillLine{Name: "zzidle", Bytes: 65},
					transcript.SkillLine{Name: "zzextra", Bytes: 90}),
				instr("/home/zzuser/zzproj/CLAUDE.md", "Project", 1500)},
			Calls: []string{"Read", "Edit", "mcp__zzdb__query"}, SkillCalls: []string{"zzskill"}},
		// 7,200 tool bytes (1,600 tokens) + 12,640 other bytes over R = 4,400: 2.87 bytes per token.
		{Rel: "-home-zzuser-work-zzother/zz-session-3.jsonl", CWD: "/home/zzuser/work/zzother", Start: "2026-09-22T11:00:00Z",
			Contexts: []int{6000, 6500, 7000, 7500, 8000, 8500}, SysBytes: 3000,
			Tools: []transcript.ToolDef{tool("Bash", 900), tool("Read", 900), tool("Artifact", 4500), tool("Workflow", 900)},
			Pre: []transcript.Attachment{att(transcript.AttFirstPrompt, 40),
				instr("/home/zzuser/work/CLAUDE.md", "Project", 9000),
				instr("/home/zzuser/.claude/projects/-home-zzuser-work-zzother/memory/MEMORY.md", "AutoMem", 600)},
			Calls: []string{"Bash", "Workflow"}},
		{Rel: "-home-zzuser-zzold/zz-session-4.jsonl", CWD: "/home/zzuser/zzold", Start: "2026-09-05T08:00:00Z", Version: "2.1.250",
			Contexts: []int{5000, 5200, 5400, 5600}},
		{Rel: "-home-zzuser-zzproj/zz-session-2/subagents/agent-zz1.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-21T10:05:00Z",
			Contexts: []int{800, 900, 1000}, Tools: []transcript.ToolDef{tool("Bash", 900), tool("Read", 900), tool("Artifact", 4500)}},
		{Rel: "-home-zzuser-zzproj/zz-session-2/subagents/agent-zz2.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-21T10:06:00Z",
			Contexts: []int{2500, 2600, 2700}, Fork: true},
		{Rel: "-home-zzuser-zzproj/zz-session-5.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-23T10:00:00Z",
			IDs: []string{"msg_zz01", "msg_zz02"}, Contexts: []int{1000, 1100, 1300}},
		{Rel: "-home-zzuser-zzproj/workflows/wf_zz/journal.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-21T12:00:00Z"},
		{Rel: "-home-zzuser-zzproj/zz-session-6.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-23T15:00:00Z", Contexts: []int{1000, 1100}},
		// 1,000 bytes for B = 5,000: 0.2 bytes per token, the sanity guard fires.
		{Rel: "-home-zzuser-zzproj/zz-session-7.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-24T09:00:00Z",
			Contexts: flat(3, 5000), SysBytes: 100, Tools: []transcript.ToolDef{tool("Bash", 900)}},
		{Rel: "-home-zzuser-zzproj/zz-session-8.jsonl", CWD: "/home/zzuser/zzproj", Start: "2026-09-27T08:00:00Z", Contexts: flat(3, 1000)},
	} {
		sessions = append(sessions, x.session())
	}
	sessions[3].Malformed = 1 // zz-session-4
	return Build(sessions)
}

// goldenSummary summarises goldenSessions with --min-turns 3 --until 2026-09-27 and one skipped file.
func goldenSummary(t *testing.T) *Summary {
	t.Helper()
	sum := Summarize(goldenSessions(t), Options{MinTurns: 3, Until: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)})
	sum.Counts.FilesSkipped = 1
	return sum
}

func TestRenderGolden(t *testing.T) {
	sum := goldenSummary(t)
	if sum.Counts.Main != 5 || sum.Counts.Decomposed != 3 || sum.Counts.Sub != 1 {
		t.Fatalf("golden input drifted: %+v", sum.Counts)
	}
	goldenFile(t, "report.golden", render(t, &Report{Version: "test", Summary: sum}))
}

func TestRenderEmptyGolden(t *testing.T) {
	out := render(t, &Report{Version: "test", Summary: Summarize(nil, Options{MinTurns: 20})})
	if !strings.Contains(out, "**Not enough data:**") || strings.Contains(out, "## Baseline") {
		t.Errorf("empty report must print the not-enough-data line and no data section:\n%s", out)
	}
	goldenFile(t, "report_empty.golden", out)
}

func TestRenderUndecomposedGolden(t *testing.T) {
	sum := Summarize(built(
		tsess{Rel: "-home-zzuser-zzold/zz-a.jsonl", CWD: "/home/zzuser/zzold", Version: "2.1.250", Contexts: []int{4000, 4100, 4200}},
		tsess{Rel: "-home-zzuser-zzold/zz-b.jsonl", CWD: "/home/zzuser/zzold", Start: "2026-09-02T10:00:00Z", Contexts: []int{4200, 4300, 4400}},
	), Options{MinTurns: 3})
	out := render(t, &Report{Version: "test", Summary: sum})
	banner := "> component breakdown needs Claude Code ≥ 2.1.259 — 2 sessions are older or lack a tool snapshot\n"
	if !strings.Contains(out, banner) {
		t.Errorf("banner missing:\n%s", out)
	}
	goldenFile(t, "report_undecomposed.golden", out)
}

// TestRenderSections checks the section order the later tasks anchor on and that the banner only
// appears when nothing could be decomposed.
func TestRenderSections(t *testing.T) {
	out := render(t, &Report{Version: "test", Summary: goldenSummary(t)})
	last := -1
	for _, h := range []string{"# ctxwinnow overhead — fixed per-call context report\n", "\n## Baseline\n",
		"\n## What the baseline is made of\n", "\n## Paid for but unused\n", "\n## Instruction files\n", "\n## Limitations\n"} {
		i := strings.Index(out, h)
		if i <= last {
			t.Errorf("heading %q at %d, want after %d", strings.TrimSpace(h), i, last)
		}
		last = i
	}
	if strings.Contains(out, "component breakdown needs") {
		t.Errorf("banner printed although sessions were decomposed")
	}
}

// TestRenderP90 prints B p90 only from MinP90N sessions.
func TestRenderP90(t *testing.T) {
	var xs []tsess
	for i := 1; i <= 10; i++ {
		xs = append(xs, tsess{Rel: "p/" + string(rune('a'+i)) + ".jsonl", Contexts: flat(2, i*1000)})
	}
	out := render(t, &Report{Version: "test", Summary: Summarize(built(xs...), Options{MinTurns: 1})})
	if !strings.Contains(out, "| main | 10 | 5,000 | 9,000 |") {
		t.Errorf("p90 missing with n = 10:\n%s", out)
	}
	out = render(t, &Report{Version: "test", Summary: Summarize(built(xs[:9]...), Options{MinTurns: 1})})
	if !strings.Contains(out, "| main | 9 | 5,000 | — |") || !strings.Contains(out, "B p90 is printed only from 10 sessions.") {
		t.Errorf("p90 printed with n = 9:\n%s", out)
	}
}

// TestRenderRedacted: with --redact the golden report keeps built-in names and labels everything else.
func TestRenderRedacted(t *testing.T) {
	out := render(t, &Report{Version: "test", Redact: true, Summary: goldenSummary(t)})
	if strings.Contains(out, "zz") || strings.Contains(out, "/home/") {
		t.Errorf("redacted report leaks a synthetic name or path:\n%s", out)
	}
	for _, want := range []string{"| Artifact |", "| file-1 (Project) | Project |", "MCP tools of mcp-", "servers carried: mcp-"} {
		if !strings.Contains(out, want) {
			t.Errorf("redacted report lacks %q:\n%s", want, out)
		}
	}
}

// TestRenderUnusedMCPOrdinal: an MCP row before a custom (non-built-in) tool row in the unused table
// must not consume a tool-N ordinal, so the custom tool keeps tool-1 (regression for a discarded
// red.Name(ClassTool, ...) call that ran for MCP rows too).
func TestRenderUnusedMCPOrdinal(t *testing.T) {
	s := &Summary{
		Decomposed:     []*Session{{}},
		MainTokenTurns: 1000,
		Unused: []UnusedRow{
			{Name: "zzsrv", MCP: true, TokenTurns: 500},
			{Name: "zzcustom", TokenTurns: 100},
		},
	}
	var b strings.Builder
	renderUnused(&b, s, NewRedactor(true))
	out := b.String()
	if !strings.Contains(out, "| tool-1 |") {
		t.Errorf("custom tool after an MCP row must be tool-1:\n%s", out)
	}
	if strings.Contains(out, "tool-2") {
		t.Errorf("MCP row must not consume a tool-N ordinal:\n%s", out)
	}
}

func TestComponentName(t *testing.T) {
	off, on := NewRedactor(false), NewRedactor(true)
	for _, c := range []struct {
		kind          CompKind
		name, off, on string
	}{
		{CompTool, "Artifact", "tool Artifact", "tool Artifact"},
		{CompTool, "zzcustom", "tool zzcustom", "tool tool-1"},
		{CompMCPTool, "zzsrv", "MCP tools of zzsrv (loaded upfront)", "MCP tools of mcp-1 (loaded upfront)"},
		{CompSystemPrompt, "", "system prompt", "system prompt"},
		{CompInstruction, "", "instruction files", "instruction files"},
		{CompInstruction, "/home/zzuser/CLAUDE.md", "/home/zzuser/CLAUDE.md", "file-1 (other)"},
		{CompSkillListing, "", "skill listing", "skill listing"},
		{CompMCP, "", "MCP (deferred tool names, instructions, surfaced schemas)", "MCP (deferred tool names, instructions, surfaced schemas)"},
		{CompHook, "", "SessionStart hook context", "SessionStart hook context"},
		{CompHarness, "", "harness (environment, model, date, reminders, meta text)", "harness (environment, model, date, reminders, meta text)"},
		{CompFirstPrompt, "", "first prompt (not configurable)", "first prompt (not configurable)"},
	} {
		if got := componentName(c.kind, c.name, off); got != c.off {
			t.Errorf("componentName(%s, %q) = %q, want %q", c.kind, c.name, got, c.off)
		}
		if got := componentName(c.kind, c.name, on); got != c.on {
			t.Errorf("redacted componentName(%s, %q) = %q, want %q", c.kind, c.name, got, c.on)
		}
	}
}

func TestFormatHelpers(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 52029: "52,029", 1234567: "1,234,567", -12864: "-12,864"} {
		if got := fmtInt(n); got != want {
			t.Errorf("fmtInt(%d) = %q, want %q", n, got, want)
		}
	}
	for x, want := range map[float64]string{0: "≈0", -0.3: "≈0", 850.4: "≈850", 999.6: "≈1.0k", 12345.6: "≈12.3k",
		-11740: "≈-11.7k", 999_949: "≈999.9k", 999_950: "≈1.0M", 2_345_678: "≈2.3M"} {
		if got := fmtTok(x); got != want {
			t.Errorf("fmtTok(%v) = %q, want %q", x, got, want)
		}
	}
	if got := fmtPct(0.1774); got != "17.7%" {
		t.Errorf("fmtPct(0.1774) = %q", got)
	}
	if got := fmtDay(time.Time{}); got != "—" {
		t.Errorf("fmtDay(zero) = %q", got)
	}
	if got := fmtDay(time.Date(2026, 9, 27, 23, 30, 0, 0, time.FixedZone("x", -3600))); got != "2026-09-28" {
		t.Errorf("fmtDay = %q, want the UTC day 2026-09-28", got)
	}
	if got := fmtInstant(time.Time{}); got != "open" {
		t.Errorf("fmtInstant(zero) = %q", got)
	}
	if got := fmtInstant(time.Date(2026, 9, 27, 14, 0, 0, 0, time.FixedZone("x", 7200))); got != "2026-09-27T12:00:00Z" {
		t.Errorf("fmtInstant = %q", got)
	}
	if got := mdEscape("a|b\nc"); got != `a\|b c` {
		t.Errorf("mdEscape = %q", got)
	}
}
