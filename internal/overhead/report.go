package overhead

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Report is everything Render prints.
type Report struct {
	Version string // internal/version.Version (tests use "test")
	Redact  bool   // --redact
	Summary *Summary
	Levers  []LeverResult // Evaluate(Summary); only Printable results are rendered
	Compare *Comparison   // Compare(Summary, --compare); nil without --compare
}

// Render writes the markdown report. It builds one Redactor(r.Redact), labels the instruction files
// in table order (so file-N follows the Instruction files table whatever section prints first), then
// calls renderHeader, renderCompare (with --compare) and, when there is at least one eligible main
// session, renderBaseline, renderComponents, renderUnused, renderInstructions and renderLevers;
// renderLimitations always closes the report.
// Only names, sizes, counts, dates, instruction-file paths and project prefixes are printed.
func Render(w io.Writer, r *Report) error {
	var b strings.Builder
	red := NewRedactor(r.Redact)
	s := r.Summary
	for _, in := range s.Instructions {
		red.Name(FileClass(in.Label), in.Path)
	}
	renderHeader(&b, r, red)
	if r.Compare != nil {
		renderCompare(&b, r.Compare, red)
	}
	if s.Counts.Main > 0 {
		renderBaseline(&b, s)
		renderComponents(&b, s, red)
		renderUnused(&b, s, red)
		renderInstructions(&b, s, red)
		renderLevers(&b, r, red)
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

// renderUnused writes "## Paid for but unused": the tools table (with the catalogued lever of each
// tool), the skill listing row and the MCP row.
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
		p("| Tool | Carried in | Called in | Projects calling | Last call | Median ≈tokens | Unused token-turns | Share of main input | Lever |\n")
		p("|---|---:|---:|---:|---|---:|---:|---:|---|\n")
		mcp := false
		for _, row := range s.Unused {
			var name string
			lever := "no known safe lever"
			if lv, ok := LeverFor("tool:" + row.Name); ok {
				lever = lv.ID
			}
			if row.MCP {
				name, lever, mcp = componentName(CompMCPTool, row.Name, red)+" †", "mcp", true
			} else {
				name = red.Name(ClassTool, row.Name)
			}
			p("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", mdEscape(name), fmtInt(row.Carrying), fmtInt(row.Calling),
				fmtInt(row.Projects), fmtDay(row.LastCall), fmtTok(row.MedianTokens), fmtTok(row.TokenTurns), fmtPct(row.Share), lever)
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

// renderCompare writes "## Before and after": both sides, the paired delta with its coverage, the
// unpaired delta with its noise floor, the counterfactual saving, removed and appeared components
// with predicted ≈tokens, everything else and drift. What cannot be computed prints "n/a".
func renderCompare(b *strings.Builder, c *Comparison, red *Redactor) {
	p := printer(b)
	p("## Before and after\n\n")
	p("Split at %s: **before** = eligible main sessions whose first timestamp is earlier, **after** = sessions starting on or after it (a settings change applies to sessions started after it). B deltas are exact tokens; component predictions are estimates (≈).\n\n",
		fmtInstant(c.At))
	if c.FewSessions {
		p("> **Fewer than %d sessions on a side** (before %s, after %s): the report still prints; weigh every delta against the noise floor.\n\n",
			FewSessions, fmtInt(c.Before.N), fmtInt(c.After.N))
	}
	p("| Side | Sessions | First | Last | Claude Code | B p50 | B p90 | Decomposed |\n|---|---:|---|---|---|---:|---:|---:|\n")
	sideLine(b, "before", c.Before)
	sideLine(b, "after", c.After)
	p("\n")
	if (c.Before.N > 0 && !c.Before.HasP90) || (c.After.N > 0 && !c.After.HasP90) {
		p("B p90 is printed only from %d sessions.\n\n", MinP90N)
	}
	if c.Paired {
		p("- **Paired delta (primary):** %s tokens: median over %s with sessions on both sides of (median B after − median B before); covers %s of %s after-sessions.\n",
			fmtSigned(c.PairedDelta), plural(c.PairedProjects, "project"), fmtInt(c.PairedAfter), fmtInt(c.After.N))
	} else {
		p("- **Paired delta (primary):** n/a: no project has sessions on both sides.\n")
	}
	switch {
	case !c.HasUnpaired:
		p("- **Unpaired delta:** n/a: a side has no session.\n")
	case c.NoiseK == 0:
		p("- **Unpaired delta:** %s tokens (median B after − median B before). Noise floor: n/a (needs at least 2 sessions before the split).\n",
			fmtSigned(c.UnpairedDelta))
	default:
		p("- **Unpaired delta:** %s tokens (median B after − median B before). Differences smaller than ±%s are indistinguishable from session mix at this n (%.0fth percentile of |median difference| over %s random splits of the before side into two groups of %s).\n",
			fmtSigned(c.UnpairedDelta), fmtInt(int(math.Round(c.NoiseFloor))), NoiseQuantile*100, fmtInt(NoiseSplits), fmtInt(c.NoiseK))
	}
	switch {
	case !c.Paired:
		p("- **Saving:** n/a (needs a paired delta).\n")
	case c.PairedDelta < 0:
		p("- **Saving:** %s of after-side input: |paired delta| × after-side calls / after-side input, the input the after-side sessions would have added at the before-side baseline.\n",
			fmtPct(c.Saving))
	case c.PairedDelta > 0:
		p("- **Extra cost:** %s of after-side input: |paired delta| × after-side calls / after-side input, the input the after-side sessions read beyond the before-side baseline.\n",
			fmtPct(c.Saving))
	default:
		p("- **Saving:** none (the paired delta is 0).\n")
	}
	p("\n")
	if c.Before.Decomposed == 0 || c.After.Decomposed == 0 {
		p("_Components and drift: n/a (needs decomposed sessions on both sides)._\n\n")
		return
	}
	if len(c.Removed)+len(c.Appeared) == 0 {
		p("No component was removed or appeared (removed = present in ≥ %.0f%% of before decomposed sessions and ≤ %.0f%% after; appeared = the reverse).\n\n",
			RemovedBefore*100, RemovedAfter*100)
	} else {
		p("Removed = present in ≥ %.0f%% of before decomposed sessions and ≤ %.0f%% after; appeared = the reverse. Predicted ≈tokens come from bytes: tool JSON at %.1f bytes per token, other components at that side's median remainder rate.\n\n",
			RemovedBefore*100, RemovedAfter*100, toolJSONBytesPerToken)
		p("| Change | Component | Before | After | Median bytes | Predicted ≈tokens |\n|---|---|---:|---:|---:|---:|\n")
		for _, d := range c.Removed {
			compDeltaLine(b, "removed", d, red)
		}
		for _, d := range c.Appeared {
			compDeltaLine(b, "appeared", d, red)
		}
		p("\n")
	}
	if c.Paired {
		p("Everything else: %s (paired delta − Σ predicted).\n\n", fmtDelta(c.EverythingElse))
	} else {
		p("Everything else: n/a (needs a paired delta).\n\n")
	}
	if len(c.Drift) == 0 {
		p("No tool definition present on both sides changed its median size by %.0f%% or more.\n\n", DriftMin*100)
		return
	}
	p("**Drift:** tool definitions present on both sides whose median size changed by ≥ %.0f%% (bytes, not tokens); a Claude Code upgrade can move B without any settings change.\n\n",
		DriftMin*100)
	p("| Tool | Before bytes | After bytes | Change |\n|---|---:|---:|---:|\n")
	for _, d := range c.Drift {
		sign := ""
		if d.Change > 0 {
			sign = "+"
		}
		p("| %s | %s | %s | %s%s |\n", mdEscape(red.Name(ClassTool, d.Tool)), fmtInt(d.BeforeBytes), fmtInt(d.AfterBytes), sign, fmtPct(d.Change))
	}
	p("\n")
}

func sideLine(b *strings.Builder, label string, sd Side) {
	if sd.N == 0 {
		fmt.Fprintf(b, "| %s | 0 | — | — | — | — | — | 0 |\n", label)
		return
	}
	p90 := "—"
	if sd.HasP90 {
		p90 = fmtInt(sd.BP90)
	}
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", label, fmtInt(sd.N), fmtDay(sd.First), fmtDay(sd.Last),
		mdEscape(sd.Versions), fmtInt(sd.BP50), p90, fmtInt(sd.Decomposed))
}

func compDeltaLine(b *strings.Builder, change string, d CompDelta, red *Redactor) {
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n", change, mdEscape(componentName(d.Kind, d.Name, red)),
		fmtPct(d.BeforePresence), fmtPct(d.AfterPresence), fmtInt(d.Bytes), fmtDelta(d.Predicted))
}

// renderLevers writes "## Suggested levers": every Printable result of r.Levers in Evaluate order
// (ranked by user-scope saving) with its status, action, user- and project-scope savings, usage,
// observed presence, snippets, trade-off and doc URL. Savings are never added up: levers overlap.
func renderLevers(b *strings.Builder, r *Report, red *Redactor) {
	p := printer(b)
	s := r.Summary
	p("## Suggested levers\n\n")
	if len(s.Decomposed) == 0 {
		p("_Needs decomposed sessions._\n\n")
		return
	}
	p("Catalogued levers whose user-scope saving reaches %s of main token-turns (`MinBenefit`), ranked by that saving. Savings are ≈tokens from your own sessions; levers overlap, so they are never added up. Snippets use User, Local, Env or Flag scope only — never a committed project settings file, which would change the setting for every collaborator.\n\n",
		fmtPct(MinBenefit))
	n := 0
	for _, lr := range r.Levers {
		if !lr.Printable {
			continue
		}
		n++
		tool := strings.HasPrefix(lr.Target, "tool:")
		p("### %d. %s: %s\n\n", n, lr.ID, mdEscape(leverTitle(lr, red)))
		p("- **Status:** %s\n", lr.Status)
		if act, ok := leverActions[lr.Action]; ok {
			p("- **Action:** %s: %s\n", lr.Action, act)
		}
		if len(lr.Skills) > 0 {
			p("- **Skills:** %s never-used skills, largest listing lines first (at most %d; plugin skills are left out).\n",
				fmtInt(len(lr.Skills)), MaxSkillOverrides)
		}
		p("- **Saving, user scope** (%s): %s\n", userScopeText(lr), savingText(lr.User))
		if lr.HasProject && hasScope(lr.Snippets, ScopeLocal) {
			if lr.Project.Sessions == 0 {
				p("- **Saving, project scope** (Local snippet, projects that never called it): none — every project carrying it also called it.\n")
			} else {
				p("- **Saving, project scope** (Local snippet, one per project; %s): %s\n", projectScopeText(lr), savingText(lr.Project))
			}
		}
		if tool {
			u := lr.Usage
			p("- **Usage:** called in %s of %s, %s, last used %s.", fmtInt(u.Called), plural(s.Counts.Main, "session"),
				plural(u.Projects, "project"), fmtDay(u.Last))
			if u.Called > 0 {
				p(" You do use this tool: disabling it takes it away from those sessions too.")
			}
			p("\n")
		}
		if tool && lr.Presence != nil {
			p("- **Observed presence:** %s Presence reflects past launches, not current settings.\n",
				presenceText(red.Name(ClassTool, lr.Subject), lr.Presence))
		}
		if len(lr.Snippets) == 0 {
			if lr.Action == "trim" {
				p("- **Snippets:** none (trim the file by hand).\n")
			} else {
				p("- **Snippets:** none (informational).\n")
			}
		} else {
			p("- **Snippets:**\n")
			for _, sn := range lr.Snippets {
				p("  - %s: `%s`", scopeLabel(sn.Scope, red), expandSnippet(sn, lr, red))
				if sn.OffWins {
					p(" — no other settings file can re-enable it; delete this line to undo.")
				}
				p("\n")
			}
		}
		p("- **Trade-off:** %s\n", lr.Tradeoff)
		p("- **Docs:** %s\n\n", lr.DocURL)
	}
	if n == 0 {
		p("_None reaches the %s minimum benefit; smaller items stay in the tables above._\n\n", fmtPct(MinBenefit))
	}
}

// leverActions explains each instruction action.
var leverActions = map[string]string{
	"trim":    "a User file or a Project file inside the session's project; no settings lever, shorten it by hand.",
	"exclude": "a Project file above the project directory of every session that loaded it; exclude it there with the Local snippet.",
	"prune":   "an auto-memory file; prune it by hand, or turn auto memory off locally (heavier).",
}

// leverTitle names the subject of a lever result (tools and files through red).
func leverTitle(lr LeverResult, red *Redactor) string {
	switch {
	case strings.HasPrefix(lr.Target, "tool:"):
		return componentName(CompTool, lr.Subject, red)
	case lr.Target == "instructions":
		if red.On() {
			return red.Name(FileClass(lr.FileLabel), lr.Subject)
		}
		return lr.Subject + " (" + lr.FileLabel + ")"
	case lr.Target == "skill-listing":
		return componentName(CompSkillListing, "", red)
	case lr.Target == "mcp":
		return componentName(CompMCP, "", red)
	}
	return lr.Target
}

// userScopeText says which sessions the user-scope saving counts.
func userScopeText(lr LeverResult) string {
	switch lr.Target {
	case "instructions":
		return "every decomposed session loading it"
	case "skill-listing":
		return "every decomposed session listing these skills"
	}
	return "every decomposed session carrying it"
}

// projectScopeText says how many projects need their own Local snippet to reach the project-scope
// saving: "2 projects never called it" (tools) or "1 project loaded it" (instruction files).
func projectScopeText(lr LeverResult) string {
	if lr.Target == "instructions" {
		return plural(lr.Project.Projects, "project") + " loaded it"
	}
	return plural(lr.Project.Projects, "project") + " never called it"
}

// savingText: "≈1.0k tokens per call over 3 sessions (13 calls) · 11.7% of main token-turns."
func savingText(sv Saving) string {
	return fmt.Sprintf("%s tokens per call over %s (%s) · %s of main token-turns.", fmtTok(sv.PerCall),
		plural(sv.Sessions, "session"), plural(sv.Calls, "call"), fmtPct(sv.Share))
}

// presenceText is the observed-presence sentence of a tool; it never claims a setting is applied.
func presenceText(tool string, pr *Presence) string {
	if pr.LastSent.IsZero() {
		return tool + " was not sent in any eligible main session with a tool snapshot."
	}
	version := pr.LastVersion
	if !validVersion(version) {
		version = "unknown version"
	}
	sent := fmt.Sprintf("%s last sent on %s (Claude Code %s)", tool, fmtDay(pr.LastSent), mdEscape(version))
	if pr.AbsentRecent == 0 {
		return sent + ", in the most recent session with a tool snapshot."
	}
	word := "sessions"
	if pr.AbsentRecent == 1 {
		word = "session"
	}
	return fmt.Sprintf("%s; absent from the %s most recent %s with a tool snapshot (%s).", sent, fmtInt(pr.AbsentRecent), word,
		plural(pr.AbsentProjects, "project"))
}

func hasScope(sns []Snippet, sc Scope) bool {
	for _, sn := range sns {
		if sn.Scope == sc {
			return true
		}
	}
	return false
}

// expandSnippet fills {{skills}} (a JSON object of skill → "user-invocable-only", names through
// red as ClassSkill) and {{path}} (a JSON string of red.Name(ClassPath, lr.Subject)).
func expandSnippet(sn Snippet, lr LeverResult, red *Redactor) string {
	text := sn.Text
	if strings.Contains(text, "{{skills}}") {
		parts := make([]string, len(lr.Skills))
		for i, name := range lr.Skills {
			parts[i] = jsonString(red.Name(ClassSkill, name)) + `: "user-invocable-only"`
		}
		text = strings.ReplaceAll(text, "{{skills}}", "{"+strings.Join(parts, ", ")+"}")
	}
	if strings.Contains(text, "{{path}}") {
		text = strings.ReplaceAll(text, "{{path}}", jsonString(red.Name(ClassPath, lr.Subject)))
	}
	return text
}

// jsonString is s as a JSON string literal without HTML escaping, so "<path>" stays readable and a
// Windows path keeps valid escaped backslashes.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // encoding a string cannot fail
	return strings.TrimSuffix(b.String(), "\n")
}

// scopeLabel: "User (`~/.claude/settings.json`)", "Local (`.claude/settings.local.json`, keep it gitignored)",
// "Env", "Flag"; the settings paths go through red.Name(ClassPath, …).
func scopeLabel(sc Scope, red *Redactor) string {
	switch sc {
	case ScopeUser:
		return "User (`" + red.Name(ClassPath, "~/.claude/settings.json") + "`)"
	case ScopeLocal:
		return "Local (`" + red.Name(ClassPath, ".claude/settings.local.json") + "`, keep it gitignored)"
	}
	return string(sc)
}

// plural: plural(1, "session") = "1 session"; plural(3, "session") = "3 sessions".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmtInt(n) + " " + word + "s"
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
	p("- Token-turns are unweighted: cached and uncached input count the same (measured on the author's sessions, not yours: API-weighted pooled share 17.2%% vs 17.7%%; 99.4%% of later calls read ≥ 0.9·B from cache).\n")
	p("- MCP servers still connecting at the first call add their deferred tool names after B (≈0.4k tokens p50, measured on the author's sessions).\n")
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

// fmtSigned: an exact token delta with its sign: -850.0 → "-850"; 1234 → "+1,234"; 0 → "0".
func fmtSigned(x float64) string {
	n := int(math.Round(x))
	if n > 0 {
		return "+" + fmtInt(n)
	}
	return fmtInt(n)
}

// fmtDelta: fmtTok with an explicit "+" on positive values: 100 → "≈+100"; -1000 → "≈-1.0k"; 0.2 → "≈0".
func fmtDelta(x float64) string {
	s := fmtTok(x)
	if x > 0 && s != "≈0" {
		return "≈+" + strings.TrimPrefix(s, "≈")
	}
	return s
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
