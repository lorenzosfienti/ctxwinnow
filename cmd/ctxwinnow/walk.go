package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// defaultRoots returns $CLAUDE_CONFIG_DIR/projects (when the variable is non-empty) then
// <home>/.claude/projects (home from os.UserHomeDir; omitted when it fails).
func defaultRoots(getenv func(string) string, home func() (string, error)) []string {
	var roots []string
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		roots = append(roots, filepath.Join(dir, "projects"))
	}
	if h, err := home(); err == nil && h != "" {
		roots = append(roots, filepath.Join(h, ".claude", "projects"))
	}
	return roots
}

// rootError lists every root tried and, unless nothing exists, the first problem ("cause: …");
// Unwrap returns the underlying error (fs.ErrNotExist when nothing exists).
type rootError struct {
	Tried []string
	Err   error
}

func (e *rootError) Error() string {
	var b strings.Builder
	b.WriteString("no transcripts directory found; tried:\n")
	if len(e.Tried) == 0 {
		b.WriteString("  (nothing: $CLAUDE_CONFIG_DIR is unset and the home directory is unknown)\n")
	}
	for _, p := range e.Tried {
		b.WriteString("  " + p + "\n")
	}
	if e.Err != nil && !errors.Is(e.Err, fs.ErrNotExist) {
		b.WriteString("cause: " + e.Err.Error() + "\n")
	}
	b.WriteString("pass --root DIR to point at your Claude Code projects directory")
	return b.String()
}

func (e *rootError) Unwrap() error { return e.Err }

// errNotDir reports a root that exists but is not a directory.
var errNotDir = errors.New("not a directory")

// resolveRoot returns explicit when it is non-empty and a readable directory (an explicit root never
// falls back), else the first candidate that is; otherwise a *rootError whose Err is the first
// problem other than a missing path, or fs.ErrNotExist when nothing exists. A root that is itself a
// symlink is returned resolved (see walkRoot).
func resolveRoot(explicit string, candidates []string) (string, error) {
	tried := candidates
	if explicit != "" {
		tried = []string{explicit}
	}
	var problem error
	for _, p := range tried {
		err := readableDir(p)
		if err == nil {
			var dir string
			if dir, err = walkRoot(p); err == nil {
				return dir, nil
			}
		}
		if problem == nil && !errors.Is(err, fs.ErrNotExist) {
			problem = err
		}
	}
	if problem == nil {
		problem = fs.ErrNotExist
	}
	return "", &rootError{Tried: tried, Err: problem}
}

// readableDir returns nil when p is a directory that can be opened.
func readableDir(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s: %w", p, errNotDir)
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}

// walkRoot returns the readable directory p in a form filepath.WalkDir descends into. WalkDir
// Lstats its root and does not follow a symlink there, so a symlinked ~/.claude/projects would scan
// nothing: when the last element of p is not itself a directory, p is resolved with
// filepath.EvalSymlinks. Any other root is returned unchanged, so warnings keep the path as given.
func walkRoot(p string) (string, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return p, nil
	}
	return filepath.EvalSymlinks(p)
}

// walkTranscripts calls visit for every *.jsonl under root in lexical order (rel = path relative to
// root). An unreadable, vanished or unparsable file or subdirectory is skipped, counted and warned on
// stderr as "ctxwinnow: skipping <path>: <err>"; only an unreadable root returns an error.
func walkTranscripts(root string, stderr io.Writer, visit func(rel string, s *transcript.Session)) (skipped int, err error) {
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			fmt.Fprintf(stderr, "ctxwinnow: skipping %s: %v\n", p, err)
			skipped++
			return nil
		}
		if d.IsDir() || filepath.Ext(p) != ".jsonl" {
			return nil
		}
		s, err := parseFile(p)
		if err != nil {
			fmt.Fprintf(stderr, "ctxwinnow: skipping %s: %v\n", p, err)
			skipped++
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		visit(rel, s)
		return nil
	})
	return skipped, err
}

// parseFile opens and parses one transcript.
func parseFile(p string) (*transcript.Session, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return transcript.Parse(f)
}

// writeReport renders to stdout, or to the file at path when path is set (created or truncated).
func writeReport(path string, stdout io.Writer, render func(io.Writer) error) error {
	if path == "" {
		return render(stdout)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = render(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
