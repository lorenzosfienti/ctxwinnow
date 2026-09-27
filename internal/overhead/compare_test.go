package overhead

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// csess is a decomposable main session for compare tests: the given tools, a 3,000-byte system
// prompt plus pre, and B = Σ tool bytes / 4.5 + non-tool bytes / 3 (rounded), so with tool bytes
// divisible by 4.5 and non-tool bytes divisible by 3 the remainder rate is exactly 3 bytes per
// token. Every call reads exactly B.
func csess(rel, cwd, start string, n int, pre []transcript.Attachment, tools ...transcript.ToolDef) tsess {
	other := 3000
	for _, a := range pre {
		other += a.Bytes
	}
	b := float64(other) / 3
	for _, t := range tools {
		b += float64(t.Bytes) / toolJSONBytesPerToken
	}
	return tsess{Rel: rel, CWD: cwd, Start: start, Contexts: flat(n, int(math.Round(b))), SysBytes: 3000, Tools: tools, Pre: pre}
}

// bsess is a session without a tool snapshot whose three calls read exactly b.
func bsess(rel, cwd, start string, b int) tsess {
	return tsess{Rel: rel, CWD: cwd, Start: start, Contexts: flat(3, b)}
}

// compareSessions is the input of report_compare.golden, split at 2026-09-10: Artifact is carried
// before and gone after, Monitor appears after, Read grows from 450 to 675 bytes (drift), /w/c has
// only before-sessions and /w/d only after-sessions; a4 starts exactly at the split. B: a1, a2
// 2,200; a3 2,300; b1, b2 2,400; c1 2,100 | a4, a5 1,350; b3 1,550; d1 1,450.
func compareSessions() []*Session {
	art := []transcript.ToolDef{tool("Bash", 450), tool("Read", 450), tool("Artifact", 4500)}
	after := []transcript.ToolDef{tool("Bash", 450), tool("Read", 675), tool("Monitor", 450)}
	a1 := csess("w-a/a1.jsonl", "/w/a", "2026-09-01T10:00:00Z", 4, nil, art...)
	a1.Version = "2.1.270"
	return built(
		a1,
		csess("w-a/a2.jsonl", "/w/a", "2026-09-02T10:00:00Z", 6, nil, art...),
		csess("w-a/a3.jsonl", "/w/a", "2026-09-03T10:00:00Z", 5, nil, append(slices.Clone(art), tool("Grep", 450))...),
		csess("w-b/b1.jsonl", "/w/b", "2026-09-04T10:00:00Z", 8, nil, append(slices.Clone(art), tool("Workflow", 900))...),
		csess("w-b/b2.jsonl", "/w/b", "2026-09-05T10:00:00Z", 3, nil, append(slices.Clone(art), tool("Workflow", 900))...),
		csess("w-c/c1.jsonl", "/w/c", "2026-09-06T10:00:00Z", 5, nil, tool("Bash", 450), tool("Artifact", 4500)),
		csess("w-a/a4.jsonl", "/w/a", "2026-09-10T00:00:00Z", 5, nil, after...),
		csess("w-a/a5.jsonl", "/w/a", "2026-09-11T10:00:00Z", 7, nil, after...),
		csess("w-b/b3.jsonl", "/w/b", "2026-09-12T10:00:00Z", 4, nil, append(slices.Clone(after), tool("Workflow", 900))...),
		csess("w-d/d1.jsonl", "/w/d", "2026-09-13T10:00:00Z", 6, nil, append(slices.Clone(after), tool("Grep", 450))...),
	)
}

// TestCompareSplitAtT: before = Start < T, after = Start ≥ T, comparing instants (fractional
// seconds and offsets included), never strings.
func TestCompareSplitAtT(t *testing.T) {
	at := instant("2026-09-10T00:00:00Z")
	sum := Summarize(built(
		bsess("p/x1.jsonl", "/w/x", "2026-09-09T23:59:59.999Z", 1000),
		bsess("p/x2.jsonl", "/w/x", "2026-09-10T00:00:00Z", 1000),
		bsess("p/x3.jsonl", "/w/x", "2026-09-10T00:00:00.000Z", 1000),
		bsess("p/x4.jsonl", "/w/x", "2026-09-10T02:00:00+02:00", 1000),
		bsess("p/x5.jsonl", "/w/x", "2026-09-10T00:00:01Z", 1000),
	), Options{MinTurns: 1})
	c := Compare(sum, at)
	if c.Before.N != 1 || c.After.N != 4 {
		t.Fatalf("before %d, after %d; want 1 and 4 (a session exactly at T is on the after side)", c.Before.N, c.After.N)
	}
	if !c.At.Equal(at) || !c.Before.Last.Equal(instant("2026-09-09T23:59:59.999Z")) || !c.After.First.Equal(at) ||
		!c.After.Last.Equal(instant("2026-09-10T00:00:01Z")) {
		t.Errorf("side dates: before %v–%v, after %v–%v", c.Before.First, c.Before.Last, c.After.First, c.After.Last)
	}
}

// TestComparePaired: the paired delta is the median over projects with sessions on both sides of
// (median B after − median B before); the unpaired delta ignores projects.
func TestComparePaired(t *testing.T) {
	sum := Summarize(built(
		bsess("p1/b1.jsonl", "/w/p1", "2026-09-01T10:00:00Z", 1000),
		bsess("p1/b2.jsonl", "/w/p1", "2026-09-02T10:00:00Z", 1200),
		bsess("p1/b3.jsonl", "/w/p1", "2026-09-03T10:00:00Z", 1100),
		bsess("p2/b1.jsonl", "/w/p2", "2026-09-04T10:00:00Z", 2000),
		bsess("p3/b1.jsonl", "/w/p3", "2026-09-05T10:00:00Z", 5000),
		bsess("p1/a1.jsonl", "/w/p1", "2026-09-11T10:00:00Z", 800),
		bsess("p1/a2.jsonl", "/w/p1", "2026-09-12T10:00:00Z", 900),
		bsess("p2/a1.jsonl", "/w/p2", "2026-09-13T10:00:00Z", 1500),
		bsess("p2/a2.jsonl", "/w/p2", "2026-09-14T10:00:00Z", 1700),
		bsess("p2/a3.jsonl", "/w/p2", "2026-09-15T10:00:00Z", 1600),
		bsess("p4/a1.jsonl", "/w/p4", "2026-09-16T10:00:00Z", 300),
	), Options{MinTurns: 1})
	c := Compare(sum, instant("2026-09-10T00:00:00Z"))
	// p1: 800 − 1100 = −300; p2: 1600 − 2000 = −400; median (lower middle) −400.
	if !c.Paired || c.PairedDelta != -400 || c.PairedProjects != 2 || c.PairedAfter != 5 || c.After.N != 6 {
		t.Errorf("paired = %v %v over %d projects, %d of %d after", c.Paired, c.PairedDelta, c.PairedProjects, c.PairedAfter, c.After.N)
	}
	// before {1000, 1100, 1200, 2000, 5000} → 1200; after {300, 800, 900, 1500, 1600, 1700} → 900.
	if !c.HasUnpaired || c.UnpairedDelta != -300 {
		t.Errorf("unpaired = %v %v, want -300", c.HasUnpaired, c.UnpairedDelta)
	}
	if c.Before.BP50 != 1200 || c.After.BP50 != 900 || c.Before.HasP90 || c.Before.Decomposed != 0 {
		t.Errorf("sides: %+v / %+v", c.Before, c.After)
	}
	if c.Removed != nil || c.Appeared != nil || c.Drift != nil {
		t.Errorf("components without decomposed sessions: %v %v %v", c.Removed, c.Appeared, c.Drift)
	}
}

// TestCompareNoiseFloor pins the documented procedure: NoiseSplits permutations of the before side
// from the fixed seed, groups of NoiseK = min(n_after, ⌊n_before/2⌋), nearest-rank 95th percentile of
// |median difference|; the result is deterministic.
func TestCompareNoiseFloor(t *testing.T) {
	var xs []tsess
	var bs []float64
	for i := range 30 {
		b := 1000 + 100*((i*7)%30)
		bs = append(bs, float64(b))
		xs = append(xs, bsess(fmt.Sprintf("p/b%02d.jsonl", i), "/w/p", fmt.Sprintf("2026-08-%02dT10:00:00Z", i+1), b))
	}
	for i := range 10 {
		xs = append(xs, bsess(fmt.Sprintf("p/a%02d.jsonl", i), "/w/q", fmt.Sprintf("2026-09-%02dT10:00:00Z", i+11), 500))
	}
	sum := Summarize(built(xs...), Options{MinTurns: 1})
	at := instant("2026-09-10T00:00:00Z")
	c := Compare(sum, at)
	if c.NoiseK != 10 {
		t.Fatalf("NoiseK = %d, want min(10, 30/2) = 10", c.NoiseK)
	}
	rng := rand.New(rand.NewPCG(noiseSeed[0], noiseSeed[1]))
	var diffs []float64
	for range NoiseSplits {
		perm := rng.Perm(len(bs))
		var g1, g2 []float64
		for j := range c.NoiseK {
			g1 = append(g1, bs[perm[j]])
			g2 = append(g2, bs[perm[c.NoiseK+j]])
		}
		diffs = append(diffs, math.Abs(median(g1)-median(g2)))
	}
	want := percentile(diffs, NoiseQuantile)
	if c.NoiseFloor != want || want <= 0 || want > 2900 {
		t.Errorf("NoiseFloor = %v, want %v (0 < floor ≤ max − min)", c.NoiseFloor, want)
	}
	if again := Compare(sum, at); again.NoiseFloor != c.NoiseFloor {
		t.Errorf("noise floor not deterministic: %v then %v", c.NoiseFloor, again.NoiseFloor)
	}

	flatBefore := Summarize(built(
		bsess("p/b1.jsonl", "/w/p", "2026-09-01T10:00:00Z", 1000), bsess("p/b2.jsonl", "/w/p", "2026-09-02T10:00:00Z", 1000),
		bsess("p/b3.jsonl", "/w/p", "2026-09-03T10:00:00Z", 1000), bsess("p/b4.jsonl", "/w/p", "2026-09-04T10:00:00Z", 1000),
		bsess("p/b5.jsonl", "/w/p", "2026-09-05T10:00:00Z", 1000), bsess("p/b6.jsonl", "/w/p", "2026-09-06T10:00:00Z", 1000),
		bsess("p/b7.jsonl", "/w/p", "2026-09-07T10:00:00Z", 1000),
		bsess("p/a1.jsonl", "/w/p", "2026-09-11T10:00:00Z", 900), bsess("p/a2.jsonl", "/w/p", "2026-09-12T10:00:00Z", 900),
		bsess("p/a3.jsonl", "/w/p", "2026-09-13T10:00:00Z", 900), bsess("p/a4.jsonl", "/w/p", "2026-09-14T10:00:00Z", 900),
		bsess("p/a5.jsonl", "/w/p", "2026-09-15T10:00:00Z", 900),
	), Options{MinTurns: 1})
	if c := Compare(flatBefore, at); c.NoiseK != 3 || c.NoiseFloor != 0 {
		t.Errorf("constant before side: NoiseK = %d (want min(5, 7/2) = 3), floor = %v (want 0)", c.NoiseK, c.NoiseFloor)
	}
}

// TestCompareEmptyAfterSide: T later than every session leaves the after side empty; nothing is
// indexed, nothing is split and every delta is unavailable.
func TestCompareEmptyAfterSide(t *testing.T) {
	sum := Summarize(compareSessions(), Options{MinTurns: 3})
	c := Compare(sum, instant("2026-09-27T00:00:00Z"))
	if c.Before.N != 10 || c.After.N != 0 || c.After.Versions != "" || !c.After.First.IsZero() || c.After.Decomposed != 0 {
		t.Fatalf("sides: %+v / %+v", c.Before, c.After)
	}
	if c.Paired || c.HasUnpaired || c.NoiseK != 0 || c.NoiseFloor != 0 || c.Saving != 0 || !c.FewSessions {
		t.Errorf("empty after side: %+v", c)
	}
	if c.Removed != nil || c.Appeared != nil || c.Drift != nil || c.EverythingElse != 0 {
		t.Errorf("components with an empty side: %v %v %v %v", c.Removed, c.Appeared, c.Drift, c.EverythingElse)
	}
	c = Compare(sum, instant("2026-08-01T00:00:00Z"))
	if c.Before.N != 0 || c.After.N != 10 || c.Paired || c.HasUnpaired || c.NoiseK != 0 || c.Before.Versions != "" {
		t.Errorf("empty before side: %+v", c)
	}
}

// TestCompareSingleBeforeSession: n_before = 1 makes the noise group size 0 (no split, no floor)
// while both deltas still exist.
func TestCompareSingleBeforeSession(t *testing.T) {
	sum := Summarize(built(
		bsess("a/b1.jsonl", "/w/a", "2026-09-01T10:00:00Z", 2200),
		bsess("a/a1.jsonl", "/w/a", "2026-09-11T10:00:00Z", 1350),
		bsess("a/a2.jsonl", "/w/a", "2026-09-12T10:00:00Z", 1350),
		bsess("b/a1.jsonl", "/w/b", "2026-09-13T10:00:00Z", 1450),
	), Options{MinTurns: 1})
	c := Compare(sum, instant("2026-09-10T00:00:00Z"))
	if c.NoiseK != 0 || c.NoiseFloor != 0 {
		t.Errorf("NoiseK = %d, floor = %v; want 0 and 0", c.NoiseK, c.NoiseFloor)
	}
	if !c.Paired || c.PairedDelta != -850 || !c.HasUnpaired || c.UnpairedDelta != -850 {
		t.Errorf("deltas: paired %v %v, unpaired %v %v", c.Paired, c.PairedDelta, c.HasUnpaired, c.UnpairedDelta)
	}
	// |−850| × 9 calls / (3·1,350 + 3·1,350 + 3·1,450) after-side input.
	if want := 850.0 * 9 / (3*1350 + 3*1350 + 3*1450); !near(c.Saving, want) {
		t.Errorf("Saving = %v, want %v", c.Saving, want)
	}
}

// TestCompareComponents pins the removed / appeared thresholds (≥ 50% on one side and ≤ 5% on the
// other, both inclusive), the predicted tokens (4.5 bytes per token for tool JSON, the side's median
// remainder rate otherwise), everything else, and drift (≥ 20% median byte change, inclusive).
func TestCompareComponents(t *testing.T) {
	md := instr("/w/p/CLAUDE.md", "Project", 900)
	xs := []tsess{
		csess("p/b1.jsonl", "/w/p", "2026-09-01T10:00:00Z", 3, []transcript.Attachment{md},
			tool("Bash", 450), tool("Read", 450), tool("zzgone", 450), tool("zzhalf", 450)),
		csess("p/b2.jsonl", "/w/p", "2026-09-02T10:00:00Z", 3, []transcript.Attachment{md}, tool("Bash", 450), tool("Read", 450)),
	}
	for i := range 20 {
		tools := []transcript.ToolDef{tool("Bash", 530), tool("Read", 540)}
		if i == 0 {
			tools = append(tools, tool("zzgone", 450)) // 1 of 20 after: 5%, still removed
		}
		if i < 2 {
			tools = append(tools, tool("zzhalf", 450)) // 2 of 20 after: 10%, not removed
		}
		if i%2 == 0 {
			tools = append(tools, tool("zzlate", 450)) // 10 of 20 after, none before: appeared
		}
		xs = append(xs, csess(fmt.Sprintf("p/a%02d.jsonl", i), "/w/p", fmt.Sprintf("2026-09-%02dT10:00:00Z", 10+i), 3, nil, tools...))
	}
	sum := Summarize(built(xs...), Options{MinTurns: 1})
	c := Compare(sum, instant("2026-09-10T00:00:00Z"))
	if c.Before.Decomposed != 2 || c.After.Decomposed != 20 {
		t.Fatalf("decomposed: %d / %d", c.Before.Decomposed, c.After.Decomposed)
	}
	want := []CompDelta{
		{Key: "instruction:/w/p/CLAUDE.md", Kind: CompInstruction, Name: "/w/p/CLAUDE.md", BeforePresence: 1, AfterPresence: 0, Bytes: 900, Predicted: -300},
		{Key: "tool:zzgone", Kind: CompTool, Name: "zzgone", BeforePresence: 0.5, AfterPresence: 0.05, Bytes: 450, Predicted: -100},
	}
	if !reflect.DeepEqual(c.Removed, want) {
		t.Errorf("Removed = %+v\nwant      %+v", c.Removed, want)
	}
	wantApp := []CompDelta{{Key: "tool:zzlate", Kind: CompTool, Name: "zzlate", BeforePresence: 0, AfterPresence: 0.5, Bytes: 450, Predicted: 100}}
	if !reflect.DeepEqual(c.Appeared, wantApp) {
		t.Errorf("Appeared = %+v, want %+v", c.Appeared, wantApp)
	}
	if !near(c.EverythingElse, c.PairedDelta-(-300-100+100)) {
		t.Errorf("EverythingElse = %v, want paired %v − predicted −300", c.EverythingElse, c.PairedDelta)
	}
	if wantDrift := []Drift{{Tool: "Read", BeforeBytes: 450, AfterBytes: 540, Change: 0.2}}; !reflect.DeepEqual(c.Drift, wantDrift) {
		t.Errorf("Drift = %+v, want %+v (Bash +17.8%% is below the threshold)", c.Drift, wantDrift)
	}
}

// TestCompareGoldenInput pins the numbers report_compare.golden prints.
func TestCompareGoldenInput(t *testing.T) {
	sum := Summarize(compareSessions(), Options{MinTurns: 3})
	c := Compare(sum, instant("2026-09-10T00:00:00Z"))
	if c.Before.N != 6 || c.After.N != 4 || c.Before.Versions != "2.1.270–2.1.283 (2 versions)" || c.After.Versions != "2.1.283" {
		t.Fatalf("sides: %+v / %+v", c.Before, c.After)
	}
	if c.PairedDelta != -850 || c.PairedProjects != 2 || c.PairedAfter != 3 || c.UnpairedDelta != -850 || c.NoiseK != 3 {
		t.Errorf("deltas: %+v", c)
	}
	if len(c.Removed) != 1 || c.Removed[0].Key != "tool:Artifact" || c.Removed[0].Predicted != -1000 ||
		len(c.Appeared) != 1 || c.Appeared[0].Key != "tool:Monitor" || c.Appeared[0].Predicted != 100 {
		t.Errorf("removed %+v, appeared %+v", c.Removed, c.Appeared)
	}
	if !near(c.EverythingElse, 50) || len(c.Drift) != 1 || c.Drift[0].Tool != "Read" || c.Drift[0].Change != 0.5 {
		t.Errorf("everything else %v, drift %+v", c.EverythingElse, c.Drift)
	}
	// |−850| × (5 + 7 + 4 + 6) calls / (5·1,350 + 7·1,350 + 4·1,550 + 6·1,450) after-side input.
	if want := 850.0 * 22 / 31100; !near(c.Saving, want) || !c.FewSessions {
		t.Errorf("Saving = %v, want %v; FewSessions = %v", c.Saving, want, c.FewSessions)
	}
}

func TestSideVersions(t *testing.T) {
	mk := func(vs ...string) []*Session {
		var out []*Session
		for i, v := range vs {
			out = append(out, &Session{Version: v, B: 1000 + i, Start: instant("2026-09-01T10:00:00Z")})
		}
		return out
	}
	for _, c := range []struct {
		ss   []*Session
		want string
	}{
		{nil, ""},
		{mk("", "unknown"), "unknown"},
		{mk("2.1.283", "2.1.283"), "2.1.283"},
		{mk("2.1.283", "2.1.259", "2.1.270", "", "2.1.259"), "2.1.259–2.1.283 (3 versions)"},
	} {
		if got := side(c.ss).Versions; got != c.want {
			t.Errorf("side(%d sessions).Versions = %q, want %q", len(c.ss), got, c.want)
		}
	}
}

func TestPredictionRate(t *testing.T) {
	ss := []*Session{{RemainderBPT: 3}, {RemainderBPT: 5}, {RemainderBPT: 4}, {}}
	if got := rateFor(CompTool, ss, 2.5); got != toolJSONBytesPerToken {
		t.Errorf("tool rate = %v", got)
	}
	if got := rateFor(CompMCPTool, nil, 2.5); got != toolJSONBytesPerToken {
		t.Errorf("mcp-tool rate = %v", got)
	}
	if got := rateFor(CompInstruction, ss, 2.5); got != 4 {
		t.Errorf("instruction rate = %v, want the median remainder rate 4", got)
	}
	if got := rateFor(CompHook, nil, 2.5); got != 2.5 {
		t.Errorf("fallback rate = %v, want 2.5", got)
	}
	if got := predict(900, 0); got != 0 {
		t.Errorf("predict with no rate = %v, want 0 (never Inf)", got)
	}
}
