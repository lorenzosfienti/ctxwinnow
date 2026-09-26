package ceiling

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

const topN = 15

// Render writes the markdown report. It contains only aggregates: category names, tool names,
// head program names, group labels and numbers (spec §9).
func Render(w io.Writer, r Result) error {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	o := r.Overall

	p("# ctxwinnow analyze — compression ceiling report\n\n")
	p("ctxwinnow %s · transcripts %s … %s\n\n", r.Version, day(r.FirstTS), day(r.LastTS))

	p("## Verdict\n\n")
	if v := VerdictFor(o.SharesC); v == VerdictNoData {
		p("**%s** — no session has at least %d turns with token usage; lower `--min-turns` or widen `--only`/`--exclude`.\n\n", v, r.MinTurns)
	} else {
		p("**%s** — median share of peak context taken by compressible tool output: **%s** over %d sessions with ≥ %d turns (GO ≥ 10%%, GREY 5–10%%, RETHINK < 5%%).\n\n",
			v, pct(Percentile(o.SharesC, 0.5)), len(o.SharesC), r.MinTurns)
	}

	p("## Scan\n\n| Files | Sessions | Subagent sessions | Malformed lines | Unmatched tool results |\n|---:|---:|---:|---:|---:|\n")
	p("| %d | %d | %d | %d | %d |\n\n", r.Files, o.Sessions, o.SubagentSessions, r.Malformed, r.Unmatched)

	p("## Context impact\n\nShare of each session's peak context occupied by compressible tool output (sessions with ≥ %d turns).\n\n", r.MinTurns)
	p("| Group | Sessions | Median | p75 | p90 | Lossless floor (median) | Verdict |\n|---|---:|---:|---:|---:|---:|---|\n")
	for _, g := range append([]*GroupStats{o}, r.Groups...) {
		p("| %s | %d | %s | %s | %s | %s | %s |\n", mdEscape(g.Label), len(g.SharesC),
			pct(Percentile(g.SharesC, 0.5)), pct(Percentile(g.SharesC, 0.75)), pct(Percentile(g.SharesC, 0.9)),
			pct(Percentile(g.SharesL, 0.5)), VerdictFor(g.SharesC))
	}

	p("\n## Tool output by category\n\nAll text tool results, estimated tokens.\n\n")
	p("| Category | Compressible | Results | Est. tokens | Share |\n|---|---|---:|---:|---:|\n")
	results := 0
	for _, c := range Categories {
		cs := o.Cats[c]
		results += cs.Count
		p("| %s | %s | %d | %d | %s |\n", c, yesNo(c.Compressible()), cs.Count, cs.Tokens, pct(ratio(cs.Tokens, o.TotalTokens)))
	}
	p("| **total** | | %d | %d | %s |\n\n", results, o.TotalTokens, pct(ratio(o.TotalTokens, o.TotalTokens)))
	p("Lossless floor on compressible output: %d est. tokens (%s of compressible).\n\n",
		o.LosslessSaved, pct(ratio(o.LosslessSaved, o.CompressibleTokens())))

	p("## Top compressible sources\n\n### Bash (head program)\n\n")
	topTable(&b, o.Bash)
	p("\n### MCP tools\n\n")
	topTable(&b, o.MCP)

	p("\n## Limitations\n\n")
	p("- Tokens are estimated as bytes/4; shares are robust to the constant, absolute numbers are indicative.\n")
	p("- Transcripts are what Claude Code stores, not the exact requests sent to the API.\n")
	p("- Only the history Claude Code keeps (30 days by default) is analysed.\n")
	p("- The policy is conservative: a Bash command that reads or searches anywhere passes through entirely.\n")

	_, err := io.WriteString(w, b.String())
	return err
}

func topTable(b *strings.Builder, m map[string]*CatStat) {
	if len(m) == 0 {
		b.WriteString("_none_\n")
		return
	}
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(x, y string) int {
		if c := cmp.Compare(m[y].Tokens, m[x].Tokens); c != 0 {
			return c
		}
		return cmp.Compare(x, y)
	})
	if len(keys) > topN {
		keys = keys[:topN]
	}
	b.WriteString("| Name | Results | Est. tokens |\n|---|---:|---:|\n")
	for _, k := range keys {
		fmt.Fprintf(b, "| %s | %d | %d |\n", mdEscape(k), m[k].Count, m[k].Tokens)
	}
}

func pct(x float64) string { return fmt.Sprintf("%.1f%%", x*100) }

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func day(ts string) string {
	if len(ts) < 10 {
		return "?"
	}
	return ts[:10]
}

func mdEscape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
