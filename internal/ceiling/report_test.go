package ceiling

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

var update = flag.Bool("update", false, "rewrite testdata/report.golden")

func render(t *testing.T, r Result) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestRender(t *testing.T) {
	out := render(t, fixtureResult())
	for _, want := range []string{
		"# ctxwinnow analyze — compression ceiling report",
		"ctxwinnow test · transcripts 2026-09-01 … 2026-09-02",
		"**GO** — median share of peak context taken by compressible tool output: **10.0%** over 1 sessions with ≥ 2 turns",
		"| 3 | 3 | 1 | 0 | 0 |",
		"| all | 1 | 10.0% | 10.0% | 10.0% | 10.0% | GO |",
		"| work | 1 | 10.0% | 10.0% | 10.0% | 10.0% | GO |",
		"| personal | 0 | 0.0% | 0.0% | 0.0% | 0.0% | NO DATA |",
		"| other | 0 | 0.0% | 0.0% | 0.0% | 0.0% | NO DATA |",
		"| Category | Compressible | Results | Est. tokens | Share | work | personal | other |",
		"| lines | yes | 1 | 2000 | 47.0% | 66.7% | 0.0% | 0.0% |",
		"| json | yes | 1 | 1253 | 29.5% | 0.0% | 100.0% | 0.0% |",
		"| pass_tool | no | 1 | 1000 | 23.5% | 33.3% | 0.0% | 0.0% |",
		"| **total** | | 3 | 4253 | 100.0% | 100.0% | 100.0% | 0.0% |",
		"Group columns show each group's share of its own tool-output tokens.",
		"Lossless floor on compressible output: 2246 est. tokens (69.0% of compressible).",
		"| go | 1 | 2000 |",
		"| mcp__x__y | 1 | 1253 |",
		"## Limitations",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report misses %q", want)
		}
	}
}

func TestRenderGolden(t *testing.T) {
	out := render(t, fixtureResult())
	if *update {
		if err := os.WriteFile("testdata/report.golden", []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/report.golden")
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Errorf("report differs from testdata/report.golden (run go test -run Golden -update and review the diff)")
	}
}

func TestRenderNoData(t *testing.T) {
	r := NewAnalyzer(Options{MinTurns: 20}).Result()
	out := render(t, r)
	if !strings.Contains(out, "**NO DATA** — no session has at least 20 turns") || !strings.Contains(out, "_none_") {
		t.Errorf("empty input must render NO DATA:\n%s", out)
	}
	if strings.Contains(out, "NaN") {
		t.Error("report contains NaN")
	}
}

func TestRenderLeaksNoContent(t *testing.T) {
	s := &transcript.Session{CWD: "/home/u/secret-project", Events: []transcript.Event{
		tr("Bash", `{"command":"go test ./internal/secretpkg -run TestTOPSECRET"}`, repeatLine("TOPSECRET token=abc123", 400)),
		usage("u1", 50000),
		// A dynamic head (a command substitution) must not leak the substituted command or its
		// argument into the Bash top-sources table: BashHead must report it as "(dynamic)".
		tr("Bash", `{"command":"$(which python3) TOPSECRETSCRIPT.py"}`, repeatLine("ok line", 600)),
		usage("u2", 60000),
	}}
	a := NewAnalyzer(Options{MinTurns: 1})
	a.AddSession(s, false)
	out := render(t, a.Result())
	for _, leak := range []string{"secret", "TOPSECRET", "abc123", "/home/u", "which", "TOPSECRETSCRIPT"} {
		if strings.Contains(out, leak) {
			t.Errorf("report leaks %q", leak)
		}
	}
}
