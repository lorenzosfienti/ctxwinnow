package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/overhead"
	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
	"github.com/lorenzosfienti/ctxwinnow/internal/version"
)

// overheadCmd runs `ctxwinnow overhead`; it returns the exit code: 0 success (including "not enough
// data" and -h), 1 I/O error on the root or the output file, 2 usage error.
func overheadCmd(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("overhead", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, usage)
		flags.PrintDefaults()
	}
	root := flags.String("root", "", "transcripts directory, scanned recursively for *.jsonl (default $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects)")
	since := flags.String("since", "", "keep sessions whose first timestamp is at or after T (YYYY-MM-DD = UTC midnight, or RFC 3339)")
	until := flags.String("until", "", "keep sessions whose first timestamp is before T (YYYY-MM-DD = UTC midnight, or RFC 3339)")
	minTurns := flags.Int("min-turns", 20, "minimum calls with token usage for a session to be eligible")
	compare := flags.String("compare", "", "add a before/after comparison split at T (sessions starting before T vs on or after T)")
	var only, exclude multiFlag
	flags.Var(&only, "only", "keep only sessions whose cwd is PREFIX or lies under it (repeatable)")
	flags.Var(&exclude, "exclude", "drop sessions whose cwd is PREFIX or lies under it (repeatable)")
	redact := flags.Bool("redact", false, "replace every non-built-in name and every path with a per-run label")
	out := flags.String("o", "", "write the report to FILE instead of stdout")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		fmt.Fprint(stderr, usage)
		return 2
	}
	if *minTurns < 1 {
		fmt.Fprintln(stderr, "ctxwinnow: --min-turns must be at least 1")
		return 2
	}
	opt := overhead.Options{MinTurns: *minTurns, Only: only, Exclude: exclude}
	var at time.Time
	for _, f := range []struct {
		name, value string
		dst         *time.Time
	}{{"since", *since, &opt.Since}, {"until", *until, &opt.Until}, {"compare", *compare, &at}} {
		if f.value == "" {
			continue
		}
		t, err := overhead.ParseInstant(f.value)
		if err != nil {
			fmt.Fprintf(stderr, "ctxwinnow: invalid --%s: %v\n", f.name, err)
			return 2
		}
		*f.dst = t
	}
	if !opt.Since.IsZero() && !opt.Until.IsZero() && !opt.Since.Before(opt.Until) {
		fmt.Fprintln(stderr, "ctxwinnow: --since must be earlier than --until")
		return 2
	}

	dir, err := resolveRoot(*root, defaultRoots(os.Getenv, os.UserHomeDir))
	if err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	var sessions []*overhead.Session
	skipped, err := walkTranscripts(dir, stderr, func(rel string, s *transcript.Session) {
		sessions = append(sessions, overhead.NewSession(rel, s))
	})
	if err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "ctxwinnow: %d files skipped\n", skipped)
	}
	sum := overhead.Summarize(overhead.Build(sessions), opt)
	sum.Counts.FilesSkipped = skipped
	rep := &overhead.Report{Version: version.Version, Redact: *redact, Summary: sum, Levers: overhead.Evaluate(sum)}
	if *compare != "" {
		rep.Compare = overhead.Compare(sum, at)
	}
	if err := writeReport(*out, stdout, func(w io.Writer) error { return overhead.Render(w, rep) }); err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	return 0
}
