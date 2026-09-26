package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lorenzosfienti/ctxwinnow/internal/version"
)

func runArgs(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runArgs("--version")
	if code != 0 || out != "ctxwinnow "+version.Version+"\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestAnalyze(t *testing.T) {
	code, out, errOut := runArgs("analyze", "--root", "testdata/projects", "--min-turns", "1", "--group", "w=/w")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	for _, want := range []string{"# ctxwinnow analyze", "| 2 | 2 | 1 | 0 | 0 |", "| w | 2 |", "## Verdict"} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
}

func TestOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if code, _, errOut := runArgs("analyze", "--root", "testdata/projects", "-o", path); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "## Verdict") {
		t.Fatalf("report file not written: %v", err)
	}
}

func TestMissingRoot(t *testing.T) {
	code, out, errOut := runArgs("analyze", "--root", filepath.Join(t.TempDir(), "nope"))
	if code != 1 || out != "" || !strings.Contains(errOut, "no such file or directory") {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, errOut)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"frobnicate"},
		{"analyze", "--group", "nolabel"},
		{"analyze", "--group", "=/x"},
		{"analyze", "--min-turns", "many"},
	} {
		if code, _, _ := runArgs(args...); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}

func TestStrayPositionalArgument(t *testing.T) {
	code, _, errOut := runArgs("analyze", "--root", "testdata/projects", "extra")
	if code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
	if !strings.Contains(errOut, `unexpected argument "extra"`) || !strings.Contains(errOut, "usage:") {
		t.Errorf("stderr = %q, want the unexpected-argument message and usage", errOut)
	}
}

func TestAnalyzeHelp(t *testing.T) {
	if code, _, _ := runArgs("analyze", "-h"); code != 0 {
		t.Errorf("analyze -h code=%d, want 0", code)
	}
	if code, _, _ := runArgs("analyze", "--help"); code != 0 {
		t.Errorf("analyze --help code=%d, want 0", code)
	}
}

// TestSubagentDetectionIsRootRelative guards against matching "/subagents/" in the absolute
// path: it puts the scanned root itself under a directory named "subagents", with no such
// directory below root, and expects zero subagent sessions.
func TestSubagentDetectionIsRootRelative(t *testing.T) {
	root := filepath.Join(t.TempDir(), "subagents", "root")
	copyFixture(t, "testdata/projects/proj/a.jsonl", filepath.Join(root, "proj", "a.jsonl"))
	copyFixture(t, "testdata/projects/proj/subagents/b.jsonl", filepath.Join(root, "proj", "x", "b.jsonl"))

	code, out, errOut := runArgs("analyze", "--root", root)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "| 2 | 2 | 0 | 0 | 0 |") {
		t.Errorf("expected the scan row to show 0 subagent sessions:\n%s", out)
	}
}

func copyFixture(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
