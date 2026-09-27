package main

import (
	"bytes"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lorenzosfienti/ctxwinnow/internal/version"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

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

// TestMissingRoot: with neither $CLAUDE_CONFIG_DIR/projects nor ~/.claude/projects present, both
// commands exit 1 and print every path tried and the --root hint (testdata/missing_root.golden, the
// temp dir replaced by <TMP> and separators normalised to "/"); the error is matched with
// errors.Is(err, fs.ErrNotExist), never on OS message text.
func TestMissingRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	_, err := resolveRoot("", defaultRoots(os.Getenv, os.UserHomeDir))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("resolveRoot error %v is not fs.ErrNotExist", err)
	}
	for _, cmd := range []string{"overhead", "analyze"} {
		code, out, errOut := runArgs(cmd)
		if code != 1 || out != "" {
			t.Fatalf("%s: code=%d stdout=%q, want 1 and no report", cmd, code, out)
		}
		got := strings.ReplaceAll(filepath.ToSlash(errOut), filepath.ToSlash(tmp), "<TMP>")
		goldenFile(t, "missing_root.golden", got)
	}
	code, _, errOut := runArgs("overhead", "--root", filepath.Join(tmp, "nope"))
	if code != 1 || !strings.Contains(errOut, "tried:\n  "+filepath.Join(tmp, "nope")+"\n") {
		t.Errorf("explicit --root: code=%d stderr=%q", code, errOut)
	}
}

// goldenFile compares got with testdata/name, or rewrites it with -update.
func goldenFile(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run go test ./cmd/ctxwinnow/ -run TestMissingRoot -update and review the diff)\n%s", path, got)
	}
}

// TestHelp: -h, --help and help print the usage on stdout and exit 0.
func TestHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		code, out, errOut := runArgs(arg)
		if code != 0 || errOut != "" || out != usage {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", arg, code, out, errOut)
		}
	}
	for _, want := range []string{"ctxwinnow overhead [--root DIR]", "ctxwinnow analyze  [--root DIR]", "ctxwinnow --version | -h | --help | help"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage lacks %q:\n%s", want, usage)
		}
	}
	if code, _, _ := runArgs("overhead", "-h"); code != 0 {
		t.Errorf("overhead -h: code=%d, want 0", code)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"frobnicate"},
		{"analyze", "--group", "nolabel"},
		{"analyze", "--group", "=/x"},
		{"analyze", "--min-turns", "many"},
		{"--frobnicate"},
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
