// Command ctxwinnow audits what Claude Code sends on every call: `overhead` measures the fixed
// per-call context baseline, `analyze` is the step-0 tool-output ceiling.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lorenzosfienti/ctxwinnow/internal/ceiling"
	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
	"github.com/lorenzosfienti/ctxwinnow/internal/version"
)

const usage = `usage:
  ctxwinnow overhead [--root DIR] [--since T] [--until T] [--min-turns N] [--compare T]
                     [--only PREFIX]... [--exclude PREFIX]... [--redact] [-o FILE]
  ctxwinnow analyze  [--root DIR] [--only PREFIX]... [--exclude PREFIX]... [--group LABEL=PREFIX]...
                     [--min-turns N] [-o FILE]
  ctxwinnow --version | -h | --help | help
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "--version", "version":
		fmt.Fprintln(stdout, "ctxwinnow", version.Version)
		return 0
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "overhead":
		return overheadCmd(args[1:], stdout, stderr)
	case "analyze":
		return analyze(args[1:], stdout, stderr)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func analyze(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("analyze", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "transcripts directory, scanned recursively for *.jsonl (default $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects)")
	var only, exclude, groups multiFlag
	flags.Var(&only, "only", "keep only sessions whose cwd is PREFIX or lies under it (repeatable)")
	flags.Var(&exclude, "exclude", "drop sessions whose cwd is PREFIX or lies under it (repeatable)")
	flags.Var(&groups, "group", "report group LABEL=PREFIX: sessions whose cwd is PREFIX or lies under it (repeatable, first match wins)")
	minTurns := flags.Int("min-turns", 20, "minimum assistant messages for a session to count in the median")
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
	opt := ceiling.Options{Only: only, Exclude: exclude, MinTurns: *minTurns}
	for _, g := range groups {
		label, prefix, ok := strings.Cut(g, "=")
		if !ok || label == "" || prefix == "" {
			fmt.Fprintf(stderr, "invalid --group %q: want LABEL=PREFIX\n", g)
			return 2
		}
		opt.Groups = append(opt.Groups, ceiling.Group{Label: label, Prefix: prefix})
	}

	dir, err := resolveRoot(*root, defaultRoots(os.Getenv, os.UserHomeDir))
	if err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	a := ceiling.NewAnalyzer(opt)
	skipped, err := walkTranscripts(dir, stderr, func(rel string, s *transcript.Session) {
		a.AddSession(s, strings.Contains("/"+filepath.ToSlash(rel), "/subagents/"))
	})
	if err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "ctxwinnow: %d files skipped\n", skipped)
	}
	res := a.Result()
	res.Version = version.Version
	if err := writeReport(*out, stdout, func(w io.Writer) error { return ceiling.Render(w, res) }); err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	return 0
}
