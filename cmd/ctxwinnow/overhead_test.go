package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
	"github.com/lorenzosfienti/ctxwinnow/internal/version"
)

// canonical is the synthetic T2 session (every content string carries a LEAKMARK- marker).
const canonical = "../../internal/transcript/testdata/overhead_main.jsonl"

// overheadRoot builds a transcripts root holding the canonical session as a main session, a copy
// of it as a subagent and a non-transcript file, and returns its path.
func overheadRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "projects")
	copyFixture(t, canonical, filepath.Join(root, "-home-zzuser-zzproj", "zz-session-1.jsonl"))
	copyFixture(t, canonical, filepath.Join(root, "-home-zzuser-zzproj", "zz-session-1", "subagents", "agent-zz.jsonl"))
	copyFixture(t, canonical, filepath.Join(root, "-home-zzuser-zzproj", "notes.txt"))
	return root
}

func TestOverheadEndToEnd(t *testing.T) {
	code, out, errOut := runArgs("overhead", "--root", overheadRoot(t), "--min-turns", "1")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"# ctxwinnow overhead — fixed per-call context report\n",
		"ctxwinnow " + version.Version + " · sessions whose first timestamp is in [open, open) · min turns 1\n",
		"- **Sessions:** 2 scanned · 1 eligible main · 0 eligible subagent · 1 main decomposed.\n",
		"- **Excluded:** 0 too few turns · 0 no usage · 0 filtered · 0 fork (inherits parent context) · 1 continuation.\n",
		"| main | 1 | 1,000 | — | 92.0% | 92.0% | 4 |\n",
		"| Artifact | 1 | 0 | 0 | — | ≈203 | ≈813 | 18.7% | disable-artifact |\n",
		"### 1. disable-artifact: tool Artifact\n",
		"## Limitations\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "LEAKMARK") || strings.Contains(out, "## Before and after") {
		t.Errorf("report leaks content or prints a comparison without --compare:\n%s", out)
	}
}

func TestOverheadRedactToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	code, out, errOut := runArgs("overhead", "--root", overheadRoot(t), "--min-turns", "1", "--redact", "-o", path)
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	if !strings.Contains(report, "## Suggested levers") || !strings.Contains(report, "User (`<path>`)") {
		t.Errorf("redacted report incomplete:\n%s", report)
	}
	for _, bad := range []string{"LEAKMARK", "zz", "/home/", "MEMORY.md"} {
		if strings.Contains(report, bad) {
			t.Errorf("redacted report contains %q:\n%s", bad, report)
		}
	}
	code, _, errOut = runArgs("overhead", "--root", overheadRoot(t), "-o", filepath.Join(t.TempDir(), "missing-dir", "r.md"))
	if code != 1 || !strings.HasPrefix(errOut, "ctxwinnow: ") {
		t.Errorf("unwritable -o: code=%d stderr=%q, want 1", code, errOut)
	}
}

// TestOverheadCompare: the canonical session starts at 2026-09-20T09:00:00.000Z, so a split at
// that instant puts it on the after side; YYYY-MM-DD resolves to UTC midnight.
func TestOverheadCompare(t *testing.T) {
	root := overheadRoot(t)
	for at, want := range map[string][]string{
		"2026-09-20T11:00:00+02:00": {"Split at 2026-09-20T09:00:00Z:", "| before | 0 | — | — | — | — | — | 0 |\n",
			"| after | 1 | 2026-09-20 | 2026-09-20 | 2.1.283 | 1,000 | — | 1 |\n"},
		"2026-09-27": {"Split at 2026-09-27T00:00:00Z:", "| after | 0 | — | — | — | — | — | 0 |\n",
			"> **Fewer than 20 sessions on a side** (before 1, after 0)"},
	} {
		code, out, errOut := runArgs("overhead", "--root", root, "--min-turns", "1", "--compare", at)
		if code != 0 {
			t.Fatalf("--compare %s: code=%d stderr=%q", at, code, errOut)
		}
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("--compare %s: report lacks %q:\n%s", at, w, out)
			}
		}
	}
}

// TestOverheadWindow: a window that excludes every session still exits 0 with the not-enough-data
// line; the resolved instants are printed.
func TestOverheadWindow(t *testing.T) {
	root := overheadRoot(t)
	code, out, _ := runArgs("overhead", "--root", root, "--min-turns", "1", "--since", "2026-09-21")
	if code != 0 || !strings.Contains(out, "**Not enough data:**") || !strings.Contains(out, "[2026-09-21T00:00:00Z, open)") ||
		!strings.Contains(out, "1 filtered") {
		t.Errorf("--since after the session: code=%d\n%s", code, out)
	}
	code, out, _ = runArgs("overhead", "--root", root, "--min-turns", "1", "--until", "2026-09-20T09:00:00.001Z")
	if code != 0 || !strings.Contains(out, "| main | 1 |") || !strings.Contains(out, "[open, 2026-09-20T09:00:00.001Z)") {
		t.Errorf("--until just after the session start: code=%d\n%s", code, out)
	}
	code, out, _ = runArgs("overhead", "--root", root, "--exclude", "/home/zzuser/zzproj/", "--min-turns", "1")
	if code != 0 || !strings.Contains(out, "**Not enough data:**") || !strings.Contains(out, "- **Filters:** exclude /home/zzuser/zzproj/.\n") {
		t.Errorf("--exclude: code=%d\n%s", code, out)
	}
}

func TestOverheadUsageErrors(t *testing.T) {
	root := overheadRoot(t)
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"--since", "yesterday"}, `invalid --since: invalid time "yesterday"`},
		{[]string{"--until", "2026-13-01"}, `invalid --until: invalid time "2026-13-01"`},
		{[]string{"--compare", "soon"}, `invalid --compare: invalid time "soon"`},
		{[]string{"--since", "2026-09-27", "--until", "2026-09-27"}, "--since must be earlier than --until"},
		{[]string{"--since", "2026-09-28", "--until", "2026-09-27T23:00:00Z"}, "--since must be earlier than --until"},
		{[]string{"--min-turns", "0"}, "--min-turns must be at least 1"},
		{[]string{"--min-turns", "x"}, "invalid value"},
		{[]string{"stray"}, `unexpected argument "stray"`},
		{[]string{"--root", root, "extra"}, `unexpected argument "extra"`},
		{[]string{"--nope"}, "flag provided but not defined: -nope"},
	} {
		code, out, errOut := runArgs(append([]string{"overhead"}, c.args...)...)
		if code != 2 || out != "" || !strings.Contains(errOut, c.msg) {
			t.Errorf("overhead %q: code=%d stdout=%q stderr=%q; want 2 and %q", c.args, code, out, errOut, c.msg)
		}
	}
}

// TestOverheadSkippedFile: an unreadable file is skipped, warned on stderr and counted in the
// report header; the run still succeeds. Permissions cannot make a file unreadable on Windows or for
// root, so the test is skipped there (TestWalkTranscriptsVanished covers the same path everywhere).
func TestOverheadSkippedFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions do not deny reading here")
	}
	root := overheadRoot(t)
	locked := filepath.Join(root, "-home-zzuser-zzproj", "zz-locked.jsonl")
	copyFixture(t, canonical, locked)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })
	code, out, errOut := runArgs("overhead", "--root", root, "--min-turns", "1")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "ctxwinnow: skipping "+locked+": ") || !strings.HasSuffix(errOut, "ctxwinnow: 1 files skipped\n") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(out, "- **Input:** 1 files skipped") {
		t.Errorf("header does not count the skipped file:\n%s", out)
	}
}

// TestWalkTranscriptsVanished deletes a file and a subdirectory while the walk is running (as
// Claude Code's cleanup can): both are skipped, warned and counted; the walk goes on in lexical
// order and ignores files that are not *.jsonl.
func TestWalkTranscriptsVanished(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"a.jsonl", "b.jsonl", "c/x.jsonl", "d/y.jsonl", "z.jsonl", "notes.txt"} {
		copyFixture(t, canonical, filepath.Join(root, filepath.FromSlash(rel)))
	}
	var visited []string
	var stderr bytes.Buffer
	skipped, err := walkTranscripts(root, &stderr, func(rel string, s *transcript.Session) {
		visited = append(visited, filepath.ToSlash(rel))
		if s.CWD != "/home/zzuser/zzproj" {
			t.Errorf("%s parsed with cwd %q", rel, s.CWD)
		}
		if rel == "a.jsonl" {
			os.Remove(filepath.Join(root, "b.jsonl"))
			os.RemoveAll(filepath.Join(root, "c"))
		}
	})
	if err != nil || skipped != 2 {
		t.Fatalf("skipped=%d err=%v", skipped, err)
	}
	if want := []string{"a.jsonl", "d/y.jsonl", "z.jsonl"}; !reflect.DeepEqual(visited, want) {
		t.Errorf("visited %v, want %v", visited, want)
	}
	for _, p := range []string{filepath.Join(root, "b.jsonl"), filepath.Join(root, "c")} {
		if !strings.Contains(stderr.String(), "ctxwinnow: skipping "+p+": ") {
			t.Errorf("no warning for %s in %q", p, stderr.String())
		}
	}
}

func TestWalkTranscriptsMissingRoot(t *testing.T) {
	_, err := walkTranscripts(filepath.Join(t.TempDir(), "nope"), &bytes.Buffer{}, func(string, *transcript.Session) {
		t.Error("visit called for a missing root")
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestDefaultRoots(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "CLAUDE_CONFIG_DIR" {
				return v
			}
			return ""
		}
	}
	home := func(h string, err error) func() (string, error) { return func() (string, error) { return h, err } }
	cfg, usr := filepath.Join("cfg", "dir"), filepath.Join("home", "u")
	for _, c := range []struct {
		getenv func(string) string
		home   func() (string, error)
		want   []string
	}{
		{env(cfg), home(usr, nil), []string{filepath.Join(cfg, "projects"), filepath.Join(usr, ".claude", "projects")}},
		{env(""), home(usr, nil), []string{filepath.Join(usr, ".claude", "projects")}},
		{env(cfg), home("", errors.New("no home")), []string{filepath.Join(cfg, "projects")}},
		{env(""), home("", errors.New("no home")), nil},
	} {
		if got := defaultRoots(c.getenv, c.home); !reflect.DeepEqual(got, c.want) {
			t.Errorf("defaultRoots = %q, want %q", got, c.want)
		}
	}
}

func TestResolveRoot(t *testing.T) {
	tmp := t.TempDir()
	dir, missing, file := filepath.Join(tmp, "projects"), filepath.Join(tmp, "missing"), filepath.Join(tmp, "file.jsonl")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveRoot(dir, []string{missing}); got != dir || err != nil {
		t.Errorf("explicit dir: %q, %v", got, err)
	}
	if got, err := resolveRoot("", []string{missing, dir}); got != dir || err != nil {
		t.Errorf("second candidate: %q, %v", got, err)
	}
	var re *rootError
	_, err := resolveRoot(missing, []string{dir})
	if !errors.As(err, &re) || !reflect.DeepEqual(re.Tried, []string{missing}) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("explicit missing root: %v (an explicit --root never falls back)", err)
	}
	_, err = resolveRoot("", []string{missing, filepath.Join(tmp, "also-missing")})
	if !errors.As(err, &re) || len(re.Tried) != 2 || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no candidate exists: %v", err)
	}
	_, err = resolveRoot(file, nil)
	if err == nil || errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "  "+file+"\n") {
		t.Errorf("root is a file: %v", err)
	}
	_, err = resolveRoot("", nil)
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "pass --root DIR") {
		t.Errorf("no candidate at all: %v", err)
	}
}

// TestRootErrorCause: a root that exists but cannot be used prints why (the error names the path),
// not just "no transcripts directory found"; a missing root prints no cause line.
func TestRootErrorCause(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file.jsonl")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveRoot(file, nil)
	if !errors.Is(err, errNotDir) || !strings.Contains(err.Error(), "\ncause: "+file+": not a directory\n") {
		t.Errorf("root is a file: %v", err)
	}
	_, err = resolveRoot(filepath.Join(tmp, "missing"), nil)
	if !errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "cause:") {
		t.Errorf("missing root: %v", err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return // permissions do not deny reading a directory here
	}
	locked := filepath.Join(tmp, "locked")
	if err := os.Mkdir(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	_, err = resolveRoot(locked, nil)
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "\ncause: open "+locked+": ") {
		t.Errorf("unreadable root: %v", err)
	}
}

// TestSymlinkedRoot: a transcripts root that is itself a symlink (a ~/.claude/projects moved to
// another disk, a dotfile manager) is scanned like its target, whether it is given with --root or
// found as a default candidate. filepath.WalkDir alone does not descend into a symlinked root, so
// the run used to report 0 sessions and exit 0.
func TestSymlinkedRoot(t *testing.T) {
	root := overheadRoot(t)
	link := filepath.Join(t.TempDir(), "projects-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	for _, c := range []struct {
		explicit   string
		candidates []string
	}{{link, nil}, {"", []string{link}}} {
		dir, err := resolveRoot(c.explicit, c.candidates)
		if err != nil {
			t.Fatalf("resolveRoot(%q, %q): %v", c.explicit, c.candidates, err)
		}
		n := 0
		if _, err := walkTranscripts(dir, &bytes.Buffer{}, func(string, *transcript.Session) { n++ }); err != nil || n != 2 {
			t.Errorf("walk of %q (resolved from %q, %q): %d transcripts, err %v; want 2", dir, c.explicit, c.candidates, n, err)
		}
	}
	for _, cmd := range []string{"overhead", "analyze"} {
		codeReal, outReal, _ := runArgs(cmd, "--root", root, "--min-turns", "1")
		code, out, errOut := runArgs(cmd, "--root", link, "--min-turns", "1")
		if code != 0 || codeReal != 0 || out != outReal {
			t.Errorf("%s via symlink: code=%d stderr=%q; report differs from the real root:\n%s", cmd, code, errOut, out)
		}
	}
	if _, out, _ := runArgs("overhead", "--root", link, "--min-turns", "1"); !strings.Contains(out, "- **Sessions:** 2 scanned ·") {
		t.Errorf("overhead via symlink does not scan the target:\n%s", out)
	}
}

// writeLines writes a synthetic transcript, one JSON record per line.
func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestOverheadDegradedFixtures runs the real parser and the CLI on the degraded shapes of spec §14
// next to the canonical session and its continuation copy: a pre-2.1.259 main session (no tool
// snapshot), a workflow journal (no usage) and a fork subagent (fork-context-ref). Each lands under
// its own reason and no content leaks; restricted to the old session's project, the report prints
// the component-breakdown banner.
func TestOverheadDegradedFixtures(t *testing.T) {
	root := overheadRoot(t)
	sub := filepath.Join(root, "-home-zzuser-zzproj", "zz-session-1", "subagents")
	writeLines(t, filepath.Join(root, "-home-zzuser-zzold", "zz-old-1.jsonl"),
		`{"type":"permission-mode","permissionMode":"default","sessionId":"zz-old-1"}`,
		`{"type":"user","timestamp":"2026-09-05T08:00:00.000Z","cwd":"/home/zzuser/zzold","version":"2.1.250","entrypoint":"cli","message":{"role":"user","content":"LEAKMARK-old-prompt fix the zz build"}}`,
		`{"type":"attachment","timestamp":"2026-09-05T08:00:00.000Z","cwd":"/home/zzuser/zzold","version":"2.1.250","entrypoint":"cli","attachment":{"type":"date","date":"2026-09-05"}}`,
		`{"type":"assistant","timestamp":"2026-09-05T08:01:00.000Z","cwd":"/home/zzuser/zzold","version":"2.1.250","entrypoint":"cli","message":{"id":"msg_zzold1","type":"message","role":"assistant","model":"zz-model","content":[{"type":"text","text":"LEAKMARK-old-answer 1"}],"usage":{"input_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":1997,"output_tokens":10}}}`,
		`{"type":"user","timestamp":"2026-09-05T08:02:00.000Z","cwd":"/home/zzuser/zzold","version":"2.1.250","entrypoint":"cli","message":{"role":"user","content":"LEAKMARK-old-prompt and the tests"}}`,
		`{"type":"assistant","timestamp":"2026-09-05T08:03:00.000Z","cwd":"/home/zzuser/zzold","version":"2.1.250","entrypoint":"cli","message":{"id":"msg_zzold2","type":"message","role":"assistant","model":"zz-model","content":[{"type":"text","text":"LEAKMARK-old-answer 2"}],"usage":{"input_tokens":3,"cache_read_input_tokens":2000,"cache_creation_input_tokens":97,"output_tokens":10}}}`,
		`{"type":"assistant","timestamp":"2026-09-05T08:04:00.000Z","cwd":"/home/zzuser/zzold","version":"2.1.250","entrypoint":"cli","message":{"id":"msg_zzold3","type":"message","role":"assistant","model":"zz-model","content":[{"type":"text","text":"LEAKMARK-old-answer 3"}],"usage":{"input_tokens":3,"cache_read_input_tokens":2100,"cache_creation_input_tokens":97,"output_tokens":10}}}`,
	)
	writeLines(t, filepath.Join(sub, "workflows", "wf_zz01", "journal.jsonl"),
		`{"type":"launched"}`,
		`{"type":"started","key":"zz-k1","agentId":"zz-a1","label":"LEAKMARK-label","phase":"zz-phase"}`,
		`{"type":"result","key":"zz-k1","agentId":"zz-a1","result":{"status":"LEAKMARK-status"}}`,
	)
	writeLines(t, filepath.Join(sub, "agent-zzfork.jsonl"),
		`{"type":"fork-context-ref","agentId":"zzfork","contextLength":24,"parentLastUuid":"zz-uuid-24","parentSessionId":"zz-session-1"}`,
		`{"type":"assistant","timestamp":"2026-09-20T09:10:00.000Z","cwd":"/home/zzuser/zzproj","version":"2.1.283","entrypoint":"cli","message":{"id":"msg_zzfork1","type":"message","role":"assistant","model":"zz-model","content":[{"type":"text","text":"LEAKMARK-fork-answer"}],"usage":{"input_tokens":3,"cache_read_input_tokens":1200,"cache_creation_input_tokens":40,"output_tokens":10}}}`,
		`{"type":"assistant","timestamp":"2026-09-20T09:11:00.000Z","cwd":"/home/zzuser/zzproj","version":"2.1.283","entrypoint":"cli","message":{"id":"msg_zzfork2","type":"message","role":"assistant","model":"zz-model","content":[{"type":"text","text":"LEAKMARK-fork-answer"}],"usage":{"input_tokens":3,"cache_read_input_tokens":1240,"cache_creation_input_tokens":40,"output_tokens":10}}}`,
	)
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"--min-turns", "1"}, []string{
			"- **Sessions:** 5 scanned · 2 eligible main · 0 eligible subagent · 1 main decomposed.\n",
			"- **Excluded:** 0 too few turns · 1 no usage · 0 filtered · 1 fork (inherits parent context) · 1 continuation.\n",
			"- **Not decomposed** (eligible main): 1 no tool snapshot (1 pre-2.1.259, 0 other) · 0 sanity guard.\n",
			"- **Claude Code versions** (eligible): 2.1.250–2.1.283.\n",
			"| main | 2 | 1,000 | — | 93.9% | 92.0% | 3 |\n",
			"1 of 2 eligible main sessions decomposed.",
			"- 1 eligible main sessions could not be decomposed (no tool snapshot or sanity guard); they still count for B and token-turns.\n",
		}},
		{[]string{"--min-turns", "1", "--only", "/home/zzuser/zzold"}, []string{
			"- **Sessions:** 5 scanned · 1 eligible main · 0 eligible subagent · 0 main decomposed.\n",
			"- **Excluded:** 0 too few turns · 1 no usage · 1 filtered · 1 fork (inherits parent context) · 1 continuation.\n",
			"> component breakdown needs Claude Code ≥ 2.1.259 — 1 sessions are older or lack a tool snapshot\n",
			"| main | 1 | 2,000 | — | 95.2% | 95.2% | 3 |\n",
			"_Needs decomposed sessions._",
		}},
	} {
		code, out, errOut := runArgs(append([]string{"overhead", "--root", root}, c.args...)...)
		if code != 0 || errOut != "" {
			t.Fatalf("%q: code=%d stderr=%q", c.args, code, errOut)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%q: report lacks %q:\n%s", c.args, want, out)
			}
		}
		if strings.Contains(out, "LEAKMARK") {
			t.Errorf("%q: report leaks content:\n%s", c.args, out)
		}
	}
}
