package ceiling

import (
	"math"
	"slices"

	"github.com/lorenzosfienti/ctxwinnow/internal/policy"
	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// contentTokensPerEstimate converts (len+3)/4 estimates of transcript text into tokens: the median
// ratio measured against exact usage (context-composition study). Printed in the report.
const contentTokensPerEstimate = 1.75

// calibrate returns int(math.Round(float64(est) * contentTokensPerEstimate)).
func calibrate(est int) int { return int(math.Round(float64(est) * contentTokensPerEstimate)) }

// Group labels sessions whose working directory is Prefix or lies under it.
type Group struct{ Label, Prefix string }

// Options configures an Analyzer (the flags of `ctxwinnow analyze`).
type Options struct {
	Only, Exclude []string
	Groups        []Group
	MinTurns      int
}

// CatStat counts tool results and their estimated tokens.
type CatStat struct{ Count, Tokens int }

// GroupStats aggregates the sessions of one group.
type GroupStats struct {
	Label            string
	Sessions         int
	SubagentSessions int
	Cats             map[Category]*CatStat // every Category is present
	TotalTokens      int                   // all text tool results
	LosslessSaved    int                   // over compressible results
	SharesC, SharesL []float64             // per eligible session, at its peak context
	Bash, MCP        map[string]*CatStat   // compressible results by head program / MCP tool
}

// CompressibleTokens sums the tokens of the compressible categories.
func (g *GroupStats) CompressibleTokens() int {
	n := 0
	for c, cs := range g.Cats {
		if c.Compressible() {
			n += cs.Tokens
		}
	}
	return n
}

// Result is everything the report needs.
type Result struct {
	Version         string
	MinTurns        int
	Files           int // transcripts scanned, including filtered ones
	Malformed       int // over kept sessions
	Unmatched       int // over kept sessions
	FirstTS, LastTS string
	Overall         *GroupStats
	Groups          []*GroupStats // empty when no group was configured
}

// Analyzer folds sessions into a Result.
type Analyzer struct {
	opt    Options
	res    Result
	byName map[string]*GroupStats
}

const otherGroup = "other"

func newGroupStats(label string) *GroupStats {
	g := &GroupStats{Label: label, Cats: map[Category]*CatStat{}, Bash: map[string]*CatStat{}, MCP: map[string]*CatStat{}}
	for _, c := range Categories {
		g.Cats[c] = &CatStat{}
	}
	return g
}

// NewAnalyzer returns an Analyzer with one group per distinct label, plus "other" when any
// group is configured.
func NewAnalyzer(opt Options) *Analyzer {
	a := &Analyzer{opt: opt, byName: map[string]*GroupStats{}}
	a.res.MinTurns = opt.MinTurns
	a.res.Overall = newGroupStats("all")
	labels := make([]string, 0, len(opt.Groups)+1)
	for _, g := range opt.Groups {
		labels = append(labels, g.Label)
	}
	if len(opt.Groups) > 0 {
		labels = append(labels, otherGroup)
	}
	for _, l := range labels {
		if _, ok := a.byName[l]; !ok {
			g := newGroupStats(l)
			a.byName[l] = g
			a.res.Groups = append(a.res.Groups, g)
		}
	}
	return a
}

// Result returns the statistics gathered so far.
func (a *Analyzer) Result() Result { return a.res }

// AddSession folds one transcript into the statistics unless the filters drop it.
func (a *Analyzer) AddSession(s *transcript.Session, subagent bool) {
	a.res.Files++
	if !a.keep(s.CWD) {
		return
	}
	a.res.Malformed += s.Malformed
	a.res.Unmatched += s.Unmatched
	if s.FirstTS != "" && (a.res.FirstTS == "" || s.FirstTS < a.res.FirstTS) {
		a.res.FirstTS = s.FirstTS
	}
	if s.LastTS > a.res.LastTS {
		a.res.LastTS = s.LastTS
	}
	targets := []*GroupStats{a.res.Overall}
	if g := a.groupOf(s.CWD); g != nil {
		targets = append(targets, g)
	}
	for _, g := range targets {
		g.Sessions++
		if subagent {
			g.SubagentSessions++
		}
	}

	var runC, runL, turns, peak int
	var shareC, shareL float64
	for _, ev := range s.Events {
		switch ev.Kind {
		case transcript.EvCompact:
			runC, runL = 0, 0
		case transcript.EvToolResult:
			r := ev.Result
			cat := Classify(r)
			tok, saved := calibrate(EstTokens(r.Text)), 0
			if cat.Compressible() {
				saved = calibrate(LosslessSaved(cat, r.Text))
				runC += tok
				runL += saved
			}
			for _, g := range targets {
				g.addResult(r, cat, tok, saved)
			}
		case transcript.EvUsage:
			turns++
			if ctx := ev.Usage.Context(); ctx > peak {
				peak = ctx
				shareC = min(1, float64(runC)/float64(ctx))
				shareL = min(1, float64(runL)/float64(ctx))
			}
		}
	}
	if turns >= a.opt.MinTurns && peak > 0 {
		for _, g := range targets {
			g.SharesC = append(g.SharesC, shareC)
			g.SharesL = append(g.SharesL, shareL)
		}
	}
}

func (g *GroupStats) addResult(r transcript.ToolResult, cat Category, tok, saved int) {
	cs := g.Cats[cat]
	cs.Count++
	cs.Tokens += tok
	g.TotalTokens += tok
	if !cat.Compressible() {
		return
	}
	g.LosslessSaved += saved
	if r.ToolName == "Bash" {
		key := policy.BashHead(r.ToolInput)
		if key == "" {
			key = "(unknown)"
		}
		bump(g.Bash, key, tok)
		return
	}
	bump(g.MCP, r.ToolName, tok)
}

func bump(m map[string]*CatStat, key string, tok int) {
	cs := m[key]
	if cs == nil {
		cs = &CatStat{}
		m[key] = cs
	}
	cs.Count++
	cs.Tokens += tok
}

func (a *Analyzer) keep(cwd string) bool {
	if len(a.opt.Only) > 0 && !transcript.UnderAny(cwd, a.opt.Only) {
		return false
	}
	return !transcript.UnderAny(cwd, a.opt.Exclude)
}

func (a *Analyzer) groupOf(cwd string) *GroupStats {
	if len(a.opt.Groups) == 0 {
		return nil
	}
	for _, g := range a.opt.Groups {
		if transcript.UnderPath(cwd, g.Prefix) {
			return a.byName[g.Label]
		}
	}
	return a.byName[otherGroup]
}

// Percentile returns the nearest-rank p-quantile (0 < p ≤ 1) of xs, or 0 for no data.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

// Verdict is the step 0 go/no-go outcome (spec §8).
type Verdict string

const (
	VerdictGo      Verdict = "GO"
	VerdictGrey    Verdict = "GREY"
	VerdictRethink Verdict = "RETHINK"
	VerdictNoData  Verdict = "NO DATA"
)

// VerdictFor applies spec §8 to the eligible sessions' share values.
func VerdictFor(shares []float64) Verdict {
	if len(shares) == 0 {
		return VerdictNoData
	}
	switch m := Percentile(shares, 0.5); {
	case m >= 0.10:
		return VerdictGo
	case m >= 0.05:
		return VerdictGrey
	}
	return VerdictRethink
}
