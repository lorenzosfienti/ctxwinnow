package ceiling

import (
	"reflect"
	"testing"
)

func TestAnalyzer(t *testing.T) {
	r := fixtureResult()
	o := r.Overall
	if r.Files != 3 || o.Sessions != 3 || o.SubagentSessions != 1 {
		t.Fatalf("Files=%d Sessions=%d Subagent=%d", r.Files, o.Sessions, o.SubagentSessions)
	}
	wantCats := map[Category]CatStat{CatLines: {1, 2000}, CatPassTool: {1, 1000}, CatJSON: {1, 1253}}
	for _, c := range Categories {
		if got := *o.Cats[c]; got != wantCats[c] {
			t.Errorf("Cats[%s] = %+v, want %+v", c, got, wantCats[c])
		}
	}
	if o.TotalTokens != 4253 || o.LosslessSaved != 2246 || o.CompressibleTokens() != 3253 {
		t.Errorf("Total=%d Lossless=%d Compressible=%d", o.TotalTokens, o.LosslessSaved, o.CompressibleTokens())
	}
	if !reflect.DeepEqual(o.SharesC, []float64{2000.0 / 20000}) || !reflect.DeepEqual(o.SharesL, []float64{1996.0 / 20000}) {
		t.Errorf("SharesC=%v SharesL=%v", o.SharesC, o.SharesL)
	}
	if *o.Bash["go"] != (CatStat{1, 2000}) || *o.MCP["mcp__x__y"] != (CatStat{1, 1253}) || len(o.Bash) != 1 || len(o.MCP) != 1 {
		t.Errorf("Bash=%v MCP=%v", o.Bash, o.MCP)
	}
	var labels []string
	for _, g := range r.Groups {
		labels = append(labels, g.Label)
	}
	if !reflect.DeepEqual(labels, []string{"work", "personal", "other"}) {
		t.Fatalf("group labels = %v", labels)
	}
	work, personal, other := r.Groups[0], r.Groups[1], r.Groups[2]
	if work.Sessions != 1 || len(work.SharesC) != 1 || personal.SubagentSessions != 1 || len(personal.SharesC) != 0 || other.Sessions != 1 {
		t.Errorf("work=%+v personal=%+v other=%+v", work, personal, other)
	}
	if r.FirstTS != "2026-09-01T10:00:00Z" || r.LastTS != "2026-09-02T09:05:00Z" {
		t.Errorf("FirstTS=%q LastTS=%q", r.FirstTS, r.LastTS)
	}
}

func TestAnalyzerFilters(t *testing.T) {
	count := func(opt Options) (files, sessions int) {
		a := NewAnalyzer(opt)
		for _, s := range fixtureSessions() {
			a.AddSession(s, false)
		}
		r := a.Result()
		return r.Files, r.Overall.Sessions
	}
	if f, s := count(Options{Only: []string{"/w/work"}}); f != 3 || s != 1 {
		t.Errorf("--only: files=%d sessions=%d", f, s)
	}
	if f, s := count(Options{Exclude: []string{"/w/work"}}); f != 3 || s != 2 {
		t.Errorf("--exclude: files=%d sessions=%d", f, s)
	}
	if _, s := count(Options{Only: []string{"/w"}, Exclude: []string{"/w/personal"}}); s != 1 {
		t.Errorf("--only then --exclude: sessions=%d", s)
	}
	if r := NewAnalyzer(Options{}).Result(); len(r.Groups) != 0 || r.Overall.Label != "all" {
		t.Errorf("no --group must give only the overall group")
	}
}

func TestPercentile(t *testing.T) {
	xs := []float64{0.3, 0.1, 0.2}
	if Percentile(xs, 0.5) != 0.2 || Percentile(xs, 0.9) != 0.3 || Percentile(xs, 0.1) != 0.1 || Percentile(nil, 0.5) != 0 {
		t.Fatal("nearest-rank percentile mismatch")
	}
	if !reflect.DeepEqual(xs, []float64{0.3, 0.1, 0.2}) {
		t.Fatal("Percentile must not reorder its input")
	}
}

func TestVerdictFor(t *testing.T) {
	tests := []struct {
		shares []float64
		want   Verdict
	}{
		{nil, VerdictNoData},
		{[]float64{0.10}, VerdictGo},
		{[]float64{0.0999}, VerdictGrey},
		{[]float64{0.05}, VerdictGrey},
		{[]float64{0.049}, VerdictRethink},
		{[]float64{0.01, 0.2, 0.3}, VerdictGo},
	}
	for _, tc := range tests {
		if got := VerdictFor(tc.shares); got != tc.want {
			t.Errorf("VerdictFor(%v) = %s, want %s", tc.shares, got, tc.want)
		}
	}
}
