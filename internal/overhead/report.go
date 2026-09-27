package overhead

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Report is everything Render prints. T8 adds `Levers []LeverResult`; T9 adds `Compare *Comparison`.
type Report struct {
	Version string // internal/version.Version (tests use "test")
	Redact  bool   // --redact
	Summary *Summary
}

// Render writes the markdown report. It builds one Redactor(r.Redact), labels the instruction files
// in table order (so file-N follows the Instruction files table whatever section prints first), then
// calls renderHeader and, when there is at least one eligible main session, renderBaseline,
// renderComponents, renderUnused and renderInstructions; renderLimitations always closes the report.
// Only names, sizes, counts, dates, instruction-file paths and project prefixes are printed.
func Render(w io.Writer, r *Report) error {
	var b strings.Builder
	red := NewRedactor(r.Redact)
	s := r.Summary
	for _, in := range s.Instructions {
		red.Name(FileClass(in.Label), in.Path)
	}
	renderHeader(&b, r, red)
	if s.Counts.Main > 0 {
		renderBaseline(&b, s)
		renderComponents(&b, s, red)
		renderUnused(&b, s, red)
		renderInstructions(&b, s, red)
	}
	renderLimitations(&b, s)
	_, err := io.WriteString(w, b.String())
	return err
}

// printer returns a Fprintf bound to b.
func printer(b *strings.Builder) func(format string, args ...any) {
	return func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
}

// renderHeader writes the title, window, counts, exclusions, entrypoints, versions, attribution
// constants, the undecomposed banner and the not-enough-data line.
func renderHeader(b *strings.Builder, r *Report, red *Redactor) {
	p := printer(b)
	s := r.Summary
	c := s.Counts
	o := s.Options
	p("# ctxwinnow overhead — fixed per-call context report\n\n")
	p("ctxwinnow %s · sessions whose first timestamp is in [%s, %s) · min turns %d\n\n",
		r.Version, fmtInstant(o.Since), fmtInstant(o.Until), o.MinTurns)
	p("- **Sessions:** %s scanned · %s eligible main · %s eligible subagent · %s main decomposed.\n",
		fmtInt(c.Scanned), fmtInt(c.Main), fmtInt(c.Sub), fmtInt(c.Decomposed))
	p("- **Excluded:** %s too few turns · %s no usage · %s filtered · %s fork (inherits parent context) · %s continuation.\n",
		fmtInt(c.TooFewTurns), fmtInt(c.NoUsage), fmtInt(c.Filtered), fmtInt(c.Fork), fmtInt(c.Continuation))
	p("- **Not decomposed** (eligible main): %s no tool snapshot (%s pre-%s, %s other) · %s sanity guard.\n",
		fmtInt(c.OldVersion+c.NoSnapshot), fmtInt(c.OldVersion), snapshotMinVersion, fmtInt(c.NoSnapshot), fmtInt(c.Sanity))
	p("- **Input:** %s files skipped · %s malformed lines · %s sessions compacted before the first call.\n",
		fmtInt(c.FilesSkipped), fmtInt(c.Malformed), fmtInt(c.CompactBeforeFirstUsage))
	p("- **Oldest session:** %s. Claude Code keeps transcripts `cleanupPeriodDays` days (default 30); deleted sessions cannot be measured.\n",
		fmtDay(s.Oldest))
	entry := "none"
	if len(s.Entrypoints) > 0 {
		var parts []string
		for _, e := range s.Entrypoints {
			parts = append(parts, mdEscape(e.Name)+" "+fmtInt(e.N))
		}
		entry = strings.Join(parts, " · ")
	}
	p("- **Entrypoints** (eligible main): %s.\n", entry)
	versions := "unknown"
	switch {
	case s.VersionMin == "":
	case s.VersionMin == s.VersionMax:
		versions = s.VersionMin
	default:
		versions = s.VersionMin + "–" + s.VersionMax
	}
	p("- **Claude Code versions** (eligible): %s.\n", mdEscape(versions))
	bpt := "n/a"
	if len(s.Decomposed) > 0 {
		bpt = strconv.FormatFloat(s.RemainderBPT, 'f', 2, 64) + " bytes per token"
	}
	p("- **Attribution:** tool JSON at %.1f bytes per token (`toolJSONBytesPerToken`, band %.1f–%.1f); the other components share the rest of B by bytes (median remainder rate: %s). B is exact; component figures are estimates (≈).\n",
		toolJSONBytesPerToken, toolJSONBandLow, toolJSONBandHigh, bpt)
	if len(o.Only)+len(o.Exclude) > 0 {
		var parts []string
		for _, pre := range o.Only {
			parts = append(parts, "only "+mdEscape(red.Name(ClassProject, pre)))
		}
		for _, pre := range o.Exclude {
			parts = append(parts, "exclude "+mdEscape(red.Name(ClassProject, pre)))
		}
		p("- **Filters:** %s.\n", strings.Join(parts, " · "))
	}
	p("\n")
	if c.Main > 0 && c.Decomposed == 0 && c.OldVersion+c.NoSnapshot > 0 {
		p("> component breakdown needs Claude Code ≥ %s — %d sessions are older or lack a tool snapshot\n\n",
			snapshotMinVersion, c.OldVersion+c.NoSnapshot)
	}
	if c.Main == 0 {
		p("**Not enough data:** no main session has at least %d calls with token usage inside the window and filters. Lower `--min-turns`, widen `--since`/`--until` or check `--only`/`--exclude`.\n\n",
			o.MinTurns)
	}
}

// renderBaseline writes "## Baseline": the headline, the main row and the subagent row.
func renderBaseline(b *strings.Builder, s *Summary) {
	p := printer(b)
	m := s.Main
	p("## Baseline\n\n")
	p("B is the context of a session's first call (input + cache read + cache creation): exact. Every call re-reads it, so its token-turns share is B × N / Σ context over the session's N calls.\n\n")
	p("**Pooled share (headline): %s** of all input read by the %s eligible main sessions is the fixed baseline. Typical session: %s (median of per-session shares, median N = %s; this mostly reflects session length).\n\n",
		fmtPct(m.Pooled), fmtInt(m.N), fmtPct(m.Typical), fmtInt(m.MedianN))
	p("| Sessions | n | B p50 | B p90 | Pooled share | Typical session | Median N |\n|---|---:|---:|---:|---:|---:|---:|\n")
	baselineLine(b, "main", m)
	baselineLine(b, "subagent (fixed overhead + task prompt)", s.Sub.BaselineRow)
	p("\n")
	if !m.HasP90 || (s.Sub.N > 0 && !s.Sub.HasP90) {
		p("B p90 is printed only from %d sessions.\n\n", MinP90N)
	}
	if s.Sub.Snapshots > 0 {
		p("Artifact is carried in %s of %s subagent tool snapshots; main-session levers also shrink subagent baselines.\n\n",
			fmtInt(s.Sub.ArtifactCarried), fmtInt(s.Sub.Snapshots))
	}
}

func baselineLine(b *strings.Builder, label string, row BaselineRow) {
	if row.N == 0 {
		fmt.Fprintf(b, "| %s | 0 | — | — | — | — | — |\n", label)
		return
	}
	p90 := "—"
	if row.HasP90 {
		p90 = fmtInt(row.BP90)
	}
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", label, fmtInt(row.N), fmtInt(row.BP50), p90,
		fmtPct(row.Pooled), fmtPct(row.Typical), fmtInt(row.MedianN))
}

// renderComponents writes "## What the baseline is made of".
func renderComponents(b *strings.Builder, s *Summary, red *Redactor) {
	p := printer(b)
	p("## What the baseline is made of\n\n")
	if len(s.Decomposed) == 0 {
		p("_No eligible main session could be decomposed; see the counts above._\n\n")
		return
	}
	p("%s of %s eligible main sessions decomposed. Tool JSON is converted at %.1f bytes per token; the rest of B is shared by the other components in proportion to their bytes, so components sum to B. Median ≈tokens is taken where the component is present.\n\n",
		fmtInt(len(s.Decomposed)), fmtInt(s.Counts.Main), toolJSONBytesPerToken)
	p("| Component | Sessions | Median ≈tokens | Share of B |\n|---|---:|---:|---:|\n")
	for _, row := range s.Components {
		p("| %s | %s | %s | %s |\n", mdEscape(componentName(row.Kind, row.Name, red)), fmtInt(row.Present),
			fmtTok(row.MedianTokens), fmtPct(row.ShareOfB))
	}
	p("\n")
}

// renderUnused writes "## Paid for but unused": the tools table, the skill listing row and the MCP row.
func renderUnused(b *strings.Builder, s *Summary, red *Redactor) {
	p := printer(b)
	p("## Paid for but unused\n\n")
	if len(s.Decomposed) == 0 {
		p("_Needs decomposed sessions._\n\n")
		return
	}
	p("Unused cost of a tool = Σ over decomposed main sessions that carry it and never call it of its ≈tokens × N, as a share of all main token-turns (pooled). Calls, projects and last call count every eligible main session. At most %d tools.\n\n",
		MaxUnusedRows)
	if len(s.Unused) == 0 {
		p("_No tool definition in the decomposed sessions._\n\n")
	} else {
		p("| Tool | Carried in | Called in | Projects calling | Last call | Median ≈tokens | Unused token-turns | Share of main input |\n")
		p("|---|---:|---:|---:|---|---:|---:|---:|\n")
		mcp := false
		for _, row := range s.Unused {
			var name string
			if row.MCP {
				name, mcp = componentName(CompMCPTool, row.Name, red)+" †", true
			} else {
				name = red.Name(ClassTool, row.Name)
			}
			p("| %s | %s | %s | %s | %s | %s | %s | %s |\n", mdEscape(name), fmtInt(row.Carrying), fmtInt(row.Calling),
				fmtInt(row.Projects), fmtDay(row.LastCall), fmtTok(row.MedianTokens), fmtTok(row.TokenTurns), fmtPct(row.Share))
		}
		p("\n")
		if mcp {
			p("† MCP tools loaded upfront — tool search may be off (check ENABLE_TOOL_SEARCH, ANTHROPIC_BASE_URL, alwaysLoad); detection unverified.\n\n")
		}
	}
	p("| Listing | Sessions | Median ≈tokens per call | Detail |\n|---|---:|---:|---|\n")
	sk := s.Skills
	if sk.Sessions == 0 {
		p("| skill listing | 0 | — | no skill listing in the decomposed sessions |\n")
	} else {
		p("| skill listing | %s | %s | %s listed, %s used in the window; %s of %s never used (%s tokens per call); per-skill detail: `/skill-doctor` |\n",
			fmtInt(sk.Sessions), fmtTok(sk.MedianTokens), fmtInt(sk.Listed), fmtInt(sk.Used), fmtInt(sk.Listed-sk.Used),
			fmtInt(sk.Listed), fmtTok(sk.NeverUsedTokens))
	}
	m := s.MCP
	tok := "—"
	if m.Sessions > 0 {
		tok = fmtTok(m.MedianTokens)
	}
	p("| %s | %s | %s | servers carried: %s; servers called: %s |\n\n", componentName(CompMCP, "", red), fmtInt(m.Sessions), tok,
		serverList(m.Carried, red), serverList(m.Called, red))
}

func serverList(servers []string, red *Redactor) string {
	if len(servers) == 0 {
		return "none"
	}
	out := make([]string, len(servers))
	for i, srv := range servers {
		out[i] = mdEscape(red.Name(ClassServer, srv))
	}
	return strings.Join(out, ", ")
}

// renderInstructions writes "## Instruction files".
func renderInstructions(b *strings.Builder, s *Summary, red *Redactor) {
	p := printer(b)
	p("## Instruction files\n\n")
	if len(s.Decomposed) == 0 {
		p("_Needs decomposed sessions._\n\n")
		return
	}
	if len(s.Instructions) == 0 {
		p("_No instruction file was loaded in the decomposed sessions._\n\n")
		return
	}
	p("CLAUDE.md, imported and memory files loaded before the first call, ranked by median ≈tokens where loaded.\n\n")
	p("| File | Type | Sessions | Median ≈tokens |\n|---|---|---:|---:|\n")
	for _, in := range s.Instructions {
		p("| %s | %s | %s | %s |\n", mdEscape(red.Name(FileClass(in.Label), in.Path)), mdEscape(in.Label),
			fmtInt(in.Sessions), fmtTok(in.MedianTokens))
	}
	p("\n")
}

// renderLimitations writes "## Limitations".
func renderLimitations(b *strings.Builder, s *Summary) {
	p := printer(b)
	c := s.Counts
	p("## Limitations\n\n")
	p("- B is exact; every component figure is an estimate (≈) from the two-rate attribution: tool JSON at %.1f bytes per token (band %.1f–%.1f), the rest of B shared by bytes.\n",
		toolJSONBytesPerToken, toolJSONBandLow, toolJSONBandHigh)
	p("- A transcript is not the wire request: the auto-mode template and wire-only reminders are invisible and end up spread over the non-tool components.\n")
	p("- %s eligible main sessions could not be decomposed (no tool snapshot or sanity guard); they still count for B and token-turns.\n",
		fmtInt(c.OldVersion+c.NoSnapshot+c.Sanity))
	p("- Token-turns are unweighted: cached and uncached input count the same (owner data: API-weighted pooled share 17.2%% vs 17.7%%; 99.4%% of later calls read ≥ 0.9·B from cache).\n")
	p("- MCP servers still connecting at the first call add their deferred tool names after B (≈0.4k tokens p50 in owner data).\n")
	p("- Subagent baselines are measured but not decomposed.\n")
	p("- Only transcripts Claude Code still keeps are measured (`cleanupPeriodDays`, default 30 days).\n")
	p("- Observed tool presence reflects past launches, not current settings; levers marked UNVERIFIED are documented but not measured on the wire.\n")
}

// componentName is the display name of a component row (tools and servers through red; an
// instruction path through red's file label, "file-N (other)" when it was never labelled).
func componentName(kind CompKind, name string, red *Redactor) string {
	switch kind {
	case CompTool:
		return "tool " + red.Name(ClassTool, name)
	case CompMCPTool:
		return "MCP tools of " + red.Name(ClassServer, name) + " (loaded upfront)"
	case CompSystemPrompt:
		return "system prompt"
	case CompInstruction:
		if name == "" {
			return "instruction files"
		}
		return red.Name(ClassFileOther, name)
	case CompSkillListing:
		return "skill listing"
	case CompMCP:
		return "MCP (deferred tool names, instructions, surfaced schemas)"
	case CompHook:
		return "SessionStart hook context"
	case CompHarness:
		return "harness (environment, model, date, reminders, meta text)"
	case CompFirstPrompt:
		return "first prompt (not configurable)"
	}
	return string(kind)
}

// fmtInt: 52029 → "52,029"; -12864 → "-12,864".
func fmtInt(n int) string {
	s := strconv.Itoa(n)
	sign := ""
	if n < 0 {
		sign, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}

// fmtTok: 12345.6 → "≈12.3k"; 850.4 → "≈850"; 2,345,678 → "≈2.3M"; negative values keep "-".
func fmtTok(x float64) string {
	sign := ""
	if x < 0 {
		sign, x = "-", -x
	}
	r := math.Round(x)
	switch {
	case r == 0:
		return "≈0"
	case x >= 999_950:
		return fmt.Sprintf("≈%s%.1fM", sign, x/1e6)
	case r >= 1000:
		return fmt.Sprintf("≈%s%.1fk", sign, x/1000)
	}
	return fmt.Sprintf("≈%s%.0f", sign, r)
}

// fmtPct: 0.1774 → "17.7%".
func fmtPct(x float64) string { return fmt.Sprintf("%.1f%%", x*100) }

// fmtDay: the UTC day "2026-09-27"; zero → "—".
func fmtDay(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.DateOnly)
}

// fmtInstant: RFC 3339 in UTC; zero → "open".
func fmtInstant(t time.Time) string {
	if t.IsZero() {
		return "open"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// mdEscape keeps a name inside one table cell: "|" → "\|", line breaks → " ".
func mdEscape(s string) string {
	return strings.NewReplacer("|", `\|`, "\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}
