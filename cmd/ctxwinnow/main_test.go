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
