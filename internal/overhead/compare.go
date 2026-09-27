package overhead

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

const (
	NoiseSplits   = 1000 // random splits of the before side
	NoiseQuantile = 0.95 // the noise floor is this nearest-rank percentile of |median difference|
	RemovedBefore = 0.50 // removed: present in ≥ 50% of before decomposed sessions …
	RemovedAfter  = 0.05 // … and ≤ 5% after (appeared: the reverse)
	DriftMin      = 0.20 // drift: median tool bytes change of at least 20%
	FewSessions   = 20   // warning below this many sessions on a side
)

// noiseSeed seeds rand.New(rand.NewPCG(noiseSeed[0], noiseSeed[1])): the noise floor is deterministic.
var noiseSeed = [2]uint64{0x6374786e, 0x77696e6e}

// Side describes the eligible main sessions on one side of the split.
type Side struct {
	N           int
	First, Last time.Time // earliest / latest session Start (First = oldest session date); zero when N == 0
	Versions    string    // "2.1.259" or "2.1.259–2.1.270 (8 versions)"; "unknown" without a valid version; "" when N == 0
	BP50, BP90  int
	HasP90      bool // N ≥ MinP90N
	Decomposed  int
}

// CompDelta is a component removed or appeared across the split.
type CompDelta struct {
	Key            string // Component.Key()
	Kind           CompKind
	Name           string
	BeforePresence float64 // share of decomposed sessions on each side
	AfterPresence  float64
	Bytes          int     // median bytes where present (before side if removed, after side if appeared)
	Predicted      float64 // ≈tokens: −Bytes/rate if removed, +Bytes/rate if appeared (see rateFor)
}

// Drift is a tool definition whose median size changed across the split.
type Drift struct {
	Tool                    string
	BeforeBytes, AfterBytes int     // median bytes where present
	Change                  float64 // (after − before) / before
}

// Comparison is the before/after report of spec §8.
type Comparison struct {
	At             time.Time
	Before, After  Side
	Paired         bool        // at least one CWD has sessions on both sides
	PairedDelta    float64     // median over such CWDs of (median B after − median B before)
	PairedProjects int         // k
	PairedAfter    int         // m after-sessions in paired CWDs (of After.N)
	HasUnpaired    bool        // both sides non-empty
	UnpairedDelta  float64     // median(after B) − median(before B)
	NoiseK         int         // min(After.N, Before.N/2); 0 → no noise floor ("n/a")
	NoiseFloor     float64     // NoiseQuantile nearest-rank of |Δmedian| over NoiseSplits splits
	Removed        []CompDelta // sorted by Predicted asc, then Key
	Appeared       []CompDelta // sorted by Predicted desc, then Key
	EverythingElse float64     // PairedDelta − Σ Predicted (Removed and Appeared); 0 unless Paired
	Drift          []Drift     // |Change| ≥ DriftMin, sorted by |Change| desc, then Tool
	Saving         float64     // |PairedDelta| × Σ N_after / Σ SumCtx_after; 0 unless Paired
	FewSessions    bool        // a side has < FewSessions sessions
}

// Compare splits sum.Eligible at `at` (before: Start < at; after: Start ≥ at) and computes the
// paired and unpaired deltas, the noise floor, removed and appeared components, drift and the
// counterfactual saving. Each noise split takes rng.Perm(Before.N): group 1 = the first NoiseK,
// group 2 = the next NoiseK. Empty or one-session sides never index or split an empty slice.
func Compare(sum *Summary, at time.Time) *Comparison {
	c := &Comparison{At: at}
	var before, after []*Session
	for _, s := range sum.Eligible {
		if s.Start.Before(at) {
			before = append(before, s)
		} else {
			after = append(after, s)
		}
	}
	c.Before, c.After = side(before), side(after)
	c.FewSessions = c.Before.N < FewSessions || c.After.N < FewSessions
	c.pair(before, after)
	if len(before) > 0 && len(after) > 0 {
		c.HasUnpaired = true
		c.UnpairedDelta = median(baselines(after)) - median(baselines(before))
	}
	c.NoiseK = min(len(after), len(before)/2)
	if c.NoiseK > 0 {
		c.NoiseFloor = noiseFloor(baselines(before), c.NoiseK)
	}
	c.components(sum, before, after)
	if c.Paired {
		c.EverythingElse = c.PairedDelta
		for _, d := range append(slices.Clone(c.Removed), c.Appeared...) {
			c.EverythingElse -= d.Predicted
		}
		var calls, input float64
		for _, s := range after {
			calls += float64(s.N)
			input += float64(s.SumCtx)
		}
		if input > 0 {
			c.Saving = math.Abs(c.PairedDelta) * calls / input
		}
	}
	return c
}

func baselines(ss []*Session) []float64 {
	out := make([]float64, len(ss))
	for i, s := range ss {
		out[i] = float64(s.B)
	}
	return out
}

// side summarises one side; sessions without a Start do not move First/Last.
func side(ss []*Session) Side {
	sd := Side{N: len(ss), HasP90: len(ss) >= MinP90N}
	if len(ss) == 0 {
		return sd
	}
	bs := baselines(ss)
	sd.BP50, sd.BP90 = int(percentile(bs, 0.5)), int(percentile(bs, 0.9))
	versions := map[string]bool{}
	for _, s := range ss {
		if !s.Start.IsZero() {
			if sd.First.IsZero() || s.Start.Before(sd.First) {
				sd.First = s.Start
			}
			if s.Start.After(sd.Last) {
				sd.Last = s.Start
			}
		}
		if s.Decomp == DecompOK {
			sd.Decomposed++
		}
		if validVersion(s.Version) {
			versions[s.Version] = true
		}
	}
	list := sortedSet(versions)
	slices.SortFunc(list, compareVersions)
	switch len(list) {
	case 0:
		sd.Versions = "unknown"
	case 1:
		sd.Versions = list[0]
	default:
		sd.Versions = fmt.Sprintf("%s–%s (%d versions)", list[0], list[len(list)-1], len(list))
	}
	return sd
}

// pair computes the paired delta over CWDs with sessions on both sides.
func (c *Comparison) pair(before, after []*Session) {
	bb, ab := map[string][]float64{}, map[string][]float64{}
	for _, s := range before {
		bb[s.CWD] = append(bb[s.CWD], float64(s.B))
	}
	for _, s := range after {
		ab[s.CWD] = append(ab[s.CWD], float64(s.B))
	}
	var deltas []float64
	for cwd, a := range ab {
		b, ok := bb[cwd]
		if !ok {
			continue
		}
		deltas = append(deltas, median(a)-median(b))
		c.PairedAfter += len(a)
	}
	c.PairedProjects = len(deltas)
	if len(deltas) > 0 {
		c.Paired = true
		c.PairedDelta = median(deltas)
	}
}

// noiseFloor is the NoiseQuantile nearest-rank of |median(group 1) − median(group 2)| over
// NoiseSplits random splits of before into two groups of k (2k ≤ len(before)).
func noiseFloor(before []float64, k int) float64 {
	rng := rand.New(rand.NewPCG(noiseSeed[0], noiseSeed[1]))
	diffs := make([]float64, NoiseSplits)
	g1, g2 := make([]float64, k), make([]float64, k)
	for i := range diffs {
		perm := rng.Perm(len(before))
		for j := range k {
			g1[j], g2[j] = before[perm[j]], before[perm[k+j]]
		}
		diffs[i] = math.Abs(median(g1) - median(g2))
	}
	return percentile(diffs, NoiseQuantile)
}

// compStat is one component key on one side: its kind, name and Σ bytes per carrying session.
type compStat struct {
	kind  CompKind
	name  string
	bytes []float64
}

func compStats(ss []*Session) map[string]*compStat {
	out := map[string]*compStat{}
	for _, s := range ss {
		per := map[string]int{}
		for _, comp := range s.Components {
			k := comp.Key()
			if out[k] == nil {
				out[k] = &compStat{kind: comp.Kind, name: comp.Name}
			}
			per[k] += comp.Bytes
		}
		for k, b := range per {
			out[k].bytes = append(out[k].bytes, float64(b))
		}
	}
	return out
}

func decomposedOf(ss []*Session) []*Session {
	var out []*Session
	for _, s := range ss {
		if s.Decomp == DecompOK {
			out = append(out, s)
		}
	}
	return out
}

// components finds removed and appeared components and tool drift over the decomposed sessions of
// each side; it does nothing unless both sides have one.
func (c *Comparison) components(sum *Summary, before, after []*Session) {
	bd, ad := decomposedOf(before), decomposedOf(after)
	if len(bd) == 0 || len(ad) == 0 {
		return
	}
	bs, as := compStats(bd), compStats(ad)
	keys := map[string]bool{}
	for k := range bs {
		keys[k] = true
	}
	for k := range as {
		keys[k] = true
	}
	for _, k := range sortedSet(keys) {
		b, a := bs[k], as[k]
		d := CompDelta{Key: k}
		if b != nil {
			d.Kind, d.Name, d.BeforePresence = b.kind, b.name, float64(len(b.bytes))/float64(len(bd))
		}
		if a != nil {
			d.Kind, d.Name, d.AfterPresence = a.kind, a.name, float64(len(a.bytes))/float64(len(ad))
		}
		switch {
		case d.BeforePresence >= RemovedBefore && d.AfterPresence <= RemovedAfter:
			d.Bytes = int(median(b.bytes))
			d.Predicted = -predict(d.Bytes, rateFor(d.Kind, bd, sum.RemainderBPT))
			c.Removed = append(c.Removed, d)
		case d.AfterPresence >= RemovedBefore && d.BeforePresence <= RemovedAfter:
			d.Bytes = int(median(a.bytes))
			d.Predicted = predict(d.Bytes, rateFor(d.Kind, ad, sum.RemainderBPT))
			c.Appeared = append(c.Appeared, d)
		case b != nil && a != nil && d.Kind == CompTool:
			bb, ab := int(median(b.bytes)), int(median(a.bytes))
			if change := float64(ab-bb) / float64(bb); bb > 0 && math.Abs(change) >= DriftMin {
				c.Drift = append(c.Drift, Drift{Tool: d.Name, BeforeBytes: bb, AfterBytes: ab, Change: change})
			}
		}
	}
	slices.SortFunc(c.Removed, func(x, y CompDelta) int {
		return cmp.Or(cmp.Compare(x.Predicted, y.Predicted), cmp.Compare(x.Key, y.Key))
	})
	slices.SortFunc(c.Appeared, func(x, y CompDelta) int {
		return cmp.Or(cmp.Compare(y.Predicted, x.Predicted), cmp.Compare(x.Key, y.Key))
	})
	slices.SortFunc(c.Drift, func(x, y Drift) int {
		return cmp.Or(cmp.Compare(math.Abs(y.Change), math.Abs(x.Change)), cmp.Compare(x.Tool, y.Tool))
	})
}

// rateFor is the bytes-per-token rate used to predict a component's tokens: toolJSONBytesPerToken
// for tool JSON, else the median RemainderBPT of the side's sessions that have one, else fallback
// (Summary.RemainderBPT); 0 when none is known.
func rateFor(kind CompKind, side []*Session, fallback float64) float64 {
	if kind == CompTool || kind == CompMCPTool {
		return toolJSONBytesPerToken
	}
	var rates []float64
	for _, s := range side {
		if s.RemainderBPT > 0 {
			rates = append(rates, s.RemainderBPT)
		}
	}
	if len(rates) > 0 {
		return median(rates)
	}
	return fallback
}

// predict is bytes / rate, 0 when the rate is unknown (never Inf).
func predict(bytes int, rate float64) float64 {
	if rate <= 0 {
		return 0
	}
	return float64(bytes) / rate
}
