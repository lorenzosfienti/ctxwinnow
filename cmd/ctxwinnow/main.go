// Command ctxwinnow measures (and, later, compresses) what AI coding agents read.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lorenzosfienti/ctxwinnow/internal/ceiling"
	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
	"github.com/lorenzosfienti/ctxwinnow/internal/version"
)

const usage = `usage:
  ctxwinnow analyze [--root DIR] [--only PREFIX]... [--exclude PREFIX]... [--group LABEL=PREFIX]...
                    [--min-turns N] [-o FILE]
  ctxwinnow --version
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
	home, _ := os.UserHomeDir()
	root := flags.String("root", filepath.Join(home, ".claude", "projects"), "directory scanned recursively for *.jsonl")
	var only, exclude, groups multiFlag
	flags.Var(&only, "only", "keep only sessions whose cwd starts with PREFIX (repeatable)")
	flags.Var(&exclude, "exclude", "drop sessions whose cwd starts with PREFIX (repeatable)")
	flags.Var(&groups, "group", "report group LABEL=PREFIX (repeatable, first match wins)")
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

	res, err := scan(*root, opt)
	if err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	if *out == "" {
		err = ceiling.Render(stdout, res)
	} else {
		err = writeFile(*out, res)
	}
	if err != nil {
		fmt.Fprintln(stderr, "ctxwinnow:", err)
		return 1
	}
	return 0
}

func writeFile(path string, res ceiling.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = ceiling.Render(f, res)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// scan parses every *.jsonl under root; files under a "subagents" directory are subagent sessions.
func scan(root string, opt ceiling.Options) (ceiling.Result, error) {
	if _, err := os.Stat(root); err != nil {
		return ceiling.Result{}, err
	}
	a := ceiling.NewAnalyzer(opt)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".jsonl" {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		s, err := transcript.Parse(f)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		a.AddSession(s, strings.Contains("/"+filepath.ToSlash(rel), "/subagents/"))
		return nil
	})
	res := a.Result()
	res.Version = version.Version
	return res, err
}
