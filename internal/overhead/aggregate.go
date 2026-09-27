package overhead

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

const (
	MaxUnusedRows = 15 // rows of the unused-tool table
	MinP90N       = 10 // p90 is printed only from this many sessions
)

// Options are the aggregation inputs of `ctxwinnow overhead`.
type Options struct {
	MinTurns      int       // eligibility threshold on N (CLI default 20)
	Since, Until  time.Time // keep Since ≤ Start < Until; zero = open bound; with any bound, a zero Start is Filtered
	Only, Exclude []string  // transcript.UnderAny on CWD
}

// Counts are the header counters; every scanned session has exactly one Status.
type Counts struct {
	Scanned                                            int // sessions passed to Summarize
	Main, Sub                                          int // eligible main / subagent sessions
	Decomposed                                         int // eligible main sessions with DecompOK
	NoUsage, Continuation, Fork, Filtered, TooFewTurns int // one reason per session (Status)
	OldVersion, NoSnapshot, Sanity                     int // eligible main sessions by Decomp reason
	FilesSkipped                                       int // set by the CLI after Summarize
	Malformed                                          int // Σ Malformed over scanned sessions
	CompactBeforeFirstUsage                            int // sessions with a compact_boundary before the first usage
}

// NameCount is one entry of a ranked count.
type NameCount struct {
	Name string
	N    int
}

// BaselineRow summarises B and the token-turns share over a set of sessions.
type BaselineRow struct {
	N          int
	BP50, BP90 int
	HasP90     bool    // N ≥ MinP90N
	Pooled     float64 // Σ B×N / Σ SumCtx
	Typical    float64 // median Share()
	MedianN    int
}

// SubagentRow is the subagent baseline ("fixed overhead + task prompt").
type SubagentRow struct {
	BaselineRow
	Snapshots       int // eligible subagent sessions with a tool snapshot
	ArtifactCarried int // of which ToolNames contains "Artifact"
}

// ComponentRow is one component over decomposed main sessions.
type ComponentRow struct {
	Key          string // Component.Key(), except every instruction file folds into "instructions"
	Kind         CompKind
	Name         string
	Present      int     // decomposed main sessions where present
	MedianTokens float64 // median ≈tokens when present
	ShareOfB     float64 // Σ tokens / Σ B over decomposed main sessions
}

// UnusedRow is the unused cost of one carried tool (or one MCP server's upfront tools).
type UnusedRow struct {
	Key          string    // "tool:<name>" or "mcp-tool:<server>"
	Name         string    // tool or server name
	MCP          bool      // mcp__<server>__* tools found in the snapshot, grouped per server
	Carrying     int       // decomposed main sessions carrying it
	Calling      int       // eligible main sessions calling it (any tool of the server when MCP)
	Projects     int       // distinct CWDs among calling sessions
	LastCall     time.Time // zero when never called in the window
	MedianTokens float64   // median ≈tokens when carried
	TokenTurns   float64   // Σ tokens_s × N_s over carrying decomposed sessions that did not call it
	Share        float64   // TokenTurns / Summary.MainTokenTurns
}

// SkillStat is one listed skill.
type SkillStat struct {
	Name        string
	Plugin      bool // name contains ":"
	Sessions    int  // decomposed main sessions listing it
	MedianBytes int  // median line bytes when listed
	Used        bool // in SkillsUsed of any eligible main session
}

// SkillRow is the skill-listing row.
type SkillRow struct {
	Sessions        int // decomposed main sessions with a skill-listing component
	MedianTokens    float64
	Listed, Used    int         // distinct listed names / of which used
	NeverUsedTokens float64     // median over listing sessions of listing tokens × never-used line bytes / listing bytes
	Skills          []SkillStat // never-used first by MedianBytes desc, then used; ties by Name
}

// MCPRow is the MCP row (deferred names, MCP instructions, surfaced schemas).
type MCPRow struct {
	Sessions     int // decomposed main sessions with an MCP component
	MedianTokens float64
	Carried      []string // distinct servers carried by eligible main sessions, sorted
	Called       []string // distinct servers whose tools were called, sorted
}

// InstructionRow is one instruction file.
type InstructionRow struct {
	Path         string
	Label        string
	Sessions     int // decomposed main sessions carrying it
	MedianTokens float64
}

// Summary is the aggregate of spec §6.
type Summary struct {
	Options        Options
	Counts         Counts
	Oldest         time.Time   // earliest non-zero Start over all scanned sessions
	Entrypoints    []NameCount // eligible main, count desc then name ("unknown" when empty)
	VersionMin     string      // over eligible main and subagent sessions (compareVersions)
	VersionMax     string
	RemainderBPT   float64 // median RemainderBPT over decomposed main sessions
	Main           BaselineRow
	Sub            SubagentRow
	MainTokenTurns float64          // Σ SumCtx over eligible main sessions (denominator of every % of token-turns)
	Components     []ComponentRow   // Σ tokens desc, then Key
	Unused         []UnusedRow      // TokenTurns desc, then Name; every carried tool, ≤ MaxUnusedRows
	Skills         SkillRow         // skill listing
	MCP            MCPRow           // MCP row
	Instructions   []InstructionRow // MedianTokens desc, then Path
	Eligible       []*Session       // eligible main sessions, Build order
	EligibleSub    []*Session       // eligible subagent sessions, Build order
	Decomposed     []*Session       // Eligible with Decomp == DecompOK
}

// Summarize applies the window and prefix filters and --min-turns to Build's output (setting
// Filtered / TooFewTurns), then aggregates spec §6. Call once per Build result (a second call
// re-evaluates Filtered and TooFewTurns, so it is harmless).
func Summarize(sessions []*Session, opt Options) *Summary {
	sum := &Summary{Options: opt}
	c := &sum.Counts
	for _, s := range sessions {
		c.Scanned++
		c.Malformed += s.Malformed
		if s.CompactBeforeFirstUsage {
			c.CompactBeforeFirstUsage++
		}
		if !s.Start.IsZero() && (sum.Oldest.IsZero() || s.Start.Before(sum.Oldest)) {
			sum.Oldest = s.Start
		}
		if s.Status == Filtered || s.Status == TooFewTurns {
			s.Status = Eligible
		}
		if s.Status == Eligible {
			switch {
			case !opt.keep(s):
				s.Status = Filtered
			case s.N < opt.MinTurns:
				s.Status = TooFewTurns
			}
		}
		switch s.Status {
		case NoUsage:
			c.NoUsage++
		case Continuation:
			c.Continuation++
		case Fork:
			c.Fork++
		case Filtered:
			c.Filtered++
		case TooFewTurns:
			c.TooFewTurns++
		case Eligible:
			if s.Kind == Subagent {
				sum.EligibleSub = append(sum.EligibleSub, s)
				continue
			}
			sum.Eligible = append(sum.Eligible, s)
			switch s.Decomp {
			case DecompOK:
				sum.Decomposed = append(sum.Decomposed, s)
			case DecompOldVersion:
				c.OldVersion++
			case DecompNoSnapshot:
				c.NoSnapshot++
			case DecompSanity:
				c.Sanity++
			}
		}
	}
	c.Main, c.Sub, c.Decomposed = len(sum.Eligible), len(sum.EligibleSub), len(sum.Decomposed)

	for _, s := range sum.Eligible {
		sum.MainTokenTurns += float64(s.SumCtx)
	}
	sum.Entrypoints = entrypoints(sum.Eligible)
	sum.VersionMin, sum.VersionMax = versionRange(append(slices.Clone(sum.Eligible), sum.EligibleSub...))
	var bpt []float64
	for _, s := range sum.Decomposed {
		bpt = append(bpt, s.RemainderBPT)
	}
	sum.RemainderBPT = median(bpt)
	sum.Main = baselineRow(sum.Eligible)
	sum.Sub = SubagentRow{BaselineRow: baselineRow(sum.EligibleSub)}
	for _, s := range sum.EligibleSub {
		if s.HasSnapshot {
			sum.Sub.Snapshots++
			if slices.Contains(s.ToolNames, "Artifact") {
				sum.Sub.ArtifactCarried++
			}
		}
	}
	sum.Components = componentRows(sum.Decomposed)
	sum.Unused = unusedRows(sum)
	sum.Skills = skillRow(sum)
	sum.MCP = mcpRow(sum)
	sum.Instructions = instructionRows(sum.Decomposed)
	return sum
}

// keep applies the window (Since ≤ Start < Until) and the --only / --exclude prefixes.
func (o Options) keep(s *Session) bool {
	if !o.Since.IsZero() || !o.Until.IsZero() {
		if s.Start.IsZero() {
			return false
		}
		if !o.Since.IsZero() && s.Start.Before(o.Since) {
			return false
		}
		if !o.Until.IsZero() && !s.Start.Before(o.Until) {
			return false
		}
	}
	if len(o.Only) > 0 && !transcript.UnderAny(s.CWD, o.Only) {
		return false
	}
	return !transcript.UnderAny(s.CWD, o.Exclude)
}

func entrypoints(ss []*Session) []NameCount {
	counts := map[string]int{}
	for _, s := range ss {
		name := s.Entrypoint
		if name == "" {
			name = "unknown"
		}
		counts[name]++
	}
	var out []NameCount
	for name, n := range counts {
		out = append(out, NameCount{name, n})
	}
	slices.SortFunc(out, func(a, b NameCount) int {
		if c := cmp.Compare(b.N, a.N); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// versionRange returns the lowest and highest valid version (numeric order); "" when none.
func versionRange(ss []*Session) (lo, hi string) {
	for _, s := range ss {
		v := s.Version
		if !validVersion(v) {
			continue
		}
		if lo == "" || compareVersions(v, lo) < 0 {
			lo = v
		}
		if hi == "" || compareVersions(v, hi) > 0 {
			hi = v
		}
	}
	return lo, hi
}

func baselineRow(ss []*Session) BaselineRow {
	row := BaselineRow{N: len(ss), HasP90: len(ss) >= MinP90N}
	if len(ss) == 0 {
		return row
	}
	var bs, shares, ns []float64
	var bn, ctx float64
	for _, s := range ss {
		bs = append(bs, float64(s.B))
		shares = append(shares, s.Share())
		ns = append(ns, float64(s.N))
		bn += float64(s.B) * float64(s.N)
		ctx += float64(s.SumCtx)
	}
	row.BP50, row.BP90 = int(percentile(bs, 0.5)), int(percentile(bs, 0.9))
	if ctx > 0 {
		row.Pooled = bn / ctx
	}
	row.Typical = median(shares)
	row.MedianN = int(median(ns))
	return row
}

func componentRows(dec []*Session) []ComponentRow {
	rows := map[string]*ComponentRow{}
	tokens := map[string][]float64{}
	var sumB float64
	for _, s := range dec {
		sumB += float64(s.B)
		per := map[string]float64{}
		for _, c := range s.Components {
			key, name := c.Key(), c.Name
			if c.Kind == CompInstruction {
				key, name = "instructions", ""
			}
			if rows[key] == nil {
				rows[key] = &ComponentRow{Key: key, Kind: c.Kind, Name: name}
			}
			per[key] += c.Tokens
		}
		for key, t := range per {
			rows[key].Present++
			rows[key].ShareOfB += t // Σ tokens for now
			tokens[key] = append(tokens[key], t)
		}
	}
	out := make([]ComponentRow, 0, len(rows))
	for key, r := range rows {
		r.MedianTokens = median(tokens[key])
		r.ShareOfB /= sumB
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b ComponentRow) int {
		if c := cmp.Compare(b.ShareOfB, a.ShareOfB); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// lastCall reports whether s called the tool (any tool of the server when mcp) and the latest call.
func lastCall(s *Session, name string, mcp bool) (last time.Time, called bool) {
	for tool, ts := range s.Called {
		match := tool == name
		if mcp {
			srv, ok := serverOf(tool)
			match = ok && srv == name
		}
		if match {
			called = true
			if ts.After(last) {
				last = ts
			}
		}
	}
	return last, called
}

func unusedRows(sum *Summary) []UnusedRow {
	rows := map[string]*UnusedRow{}
	tokens := map[string][]float64{}
	for _, s := range sum.Decomposed {
		seen := map[string]bool{}
		for _, c := range s.Components {
			key := c.Key()
			if (c.Kind != CompTool && c.Kind != CompMCPTool) || seen[key] {
				continue
			}
			seen[key] = true
			r := rows[key]
			if r == nil {
				r = &UnusedRow{Key: key, Name: c.Name, MCP: c.Kind == CompMCPTool}
				rows[key] = r
			}
			t, _ := s.Tokens(key)
			r.Carrying++
			tokens[key] = append(tokens[key], t)
			if _, called := lastCall(s, r.Name, r.MCP); !called {
				r.TokenTurns += t * float64(s.N)
			}
		}
	}
	out := make([]UnusedRow, 0, len(rows))
	for key, r := range rows {
		projects := map[string]bool{}
		for _, s := range sum.Eligible {
			last, called := lastCall(s, r.Name, r.MCP)
			if !called {
				continue
			}
			r.Calling++
			projects[s.CWD] = true
			if last.After(r.LastCall) {
				r.LastCall = last
			}
		}
		r.Projects = len(projects)
		r.MedianTokens = median(tokens[key])
		if sum.MainTokenTurns > 0 {
			r.Share = r.TokenTurns / sum.MainTokenTurns
		}
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b UnusedRow) int {
		if c := cmp.Compare(b.TokenTurns, a.TokenTurns); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	if len(out) > MaxUnusedRows {
		out = out[:MaxUnusedRows]
	}
	return out
}

func skillRow(sum *Summary) SkillRow {
	var row SkillRow
	used := map[string]bool{}
	for _, s := range sum.Eligible {
		for name := range s.SkillsUsed {
			used[name] = true
		}
	}
	lineBytes := map[string][]float64{}
	var toks, never []float64
	for _, s := range sum.Decomposed {
		for name, b := range s.Listed {
			lineBytes[name] = append(lineBytes[name], float64(b))
		}
		t, ok := s.Tokens(string(CompSkillListing))
		if !ok {
			continue
		}
		b, _ := s.Bytes(string(CompSkillListing))
		unused := 0
		for name, lb := range s.Listed {
			if !used[name] {
				unused += lb
			}
		}
		row.Sessions++
		toks = append(toks, t)
		never = append(never, t*float64(unused)/float64(b))
	}
	row.MedianTokens, row.NeverUsedTokens = median(toks), median(never)
	for name, bs := range lineBytes {
		st := SkillStat{Name: name, Plugin: strings.Contains(name, ":"), Sessions: len(bs), MedianBytes: int(median(bs)), Used: used[name]}
		row.Skills = append(row.Skills, st)
		if st.Used {
			row.Used++
		}
	}
	row.Listed = len(row.Skills)
	slices.SortFunc(row.Skills, func(a, b SkillStat) int {
		if a.Used != b.Used {
			if !a.Used {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(b.MedianBytes, a.MedianBytes); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return row
}

func mcpRow(sum *Summary) MCPRow {
	var row MCPRow
	var toks []float64
	for _, s := range sum.Decomposed {
		if t, ok := s.Tokens(string(CompMCP)); ok {
			row.Sessions++
			toks = append(toks, t)
		}
	}
	row.MedianTokens = median(toks)
	carried, called := map[string]bool{}, map[string]bool{}
	for _, s := range sum.Eligible {
		for _, srv := range s.MCPServers {
			carried[srv] = true
		}
		for tool := range s.Called {
			if srv, ok := serverOf(tool); ok {
				called[srv] = true
			}
		}
	}
	row.Carried, row.Called = sortedSet(carried), sortedSet(called)
	return row
}

func instructionRows(dec []*Session) []InstructionRow {
	rows := map[string]*InstructionRow{}
	tokens := map[string][]float64{}
	for _, s := range dec {
		per := map[string]float64{}
		for _, c := range s.Components {
			if c.Kind != CompInstruction {
				continue
			}
			if rows[c.Name] == nil {
				rows[c.Name] = &InstructionRow{Path: c.Name, Label: c.Label}
			}
			per[c.Name] += c.Tokens
		}
		for path, t := range per {
			rows[path].Sessions++
			tokens[path] = append(tokens[path], t)
		}
	}
	out := make([]InstructionRow, 0, len(rows))
	for path, r := range rows {
		r.MedianTokens = median(tokens[path])
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b InstructionRow) int {
		if c := cmp.Compare(b.MedianTokens, a.MedianTokens); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})
	return out
}

// ParseInstant parses "YYYY-MM-DD" (UTC midnight) or an RFC 3339 instant (returned in UTC).
func ParseInstant(s string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time %q: want YYYY-MM-DD (UTC midnight) or an RFC 3339 instant such as 2026-09-27T14:00:00Z", s)
	}
	return t.UTC(), nil
}

// percentile is nearest-rank (i = ceil(p·n) − 1, clamped) on a copy; 0 for no data. The tiny
// epsilon keeps float noise such as 0.07·100 = 7.000000000000001 on the intended rank.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(math.Ceil(p*float64(len(s))-1e-9)) - 1
	return s[min(max(i, 0), len(s)-1)]
}

// median is percentile(xs, 0.5): the lower middle value for an even count.
func median(xs []float64) float64 { return percentile(xs, 0.5) }
