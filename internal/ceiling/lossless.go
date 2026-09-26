package ceiling

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Throwaway estimate of the engine's lossless Reformat step (spec §7); the engine will
// re-specify it.

var (
	csiRe = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	oscRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
)

// LosslessSaved estimates the tokens the lossless floor removes from a compressible result.
func LosslessSaved(c Category, text string) int {
	return max(EstTokens(text)-EstTokens(losslessFold(c, text)), 0)
}

func losslessFold(c Category, s string) string {
	s = oscRe.ReplaceAllString(s, "")
	s = csiRe.ReplaceAllString(s, "")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r") // CRLF line ending, not a redraw
		if j := strings.LastIndexByte(l, '\r'); j >= 0 {
			l = l[j+1:]
		}
		lines[i] = l
	}
	switch c {
	case CatJSON:
		return compactJSON(lines)
	case CatLines:
		return strings.Join(foldRuns(lines), "\n")
	}
	return strings.Join(lines, "\n")
}

func compactJSON(lines []string) string {
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(strings.TrimSpace(strings.Join(lines, "\n")))) == nil {
		return buf.String()
	}
	for i, l := range lines {
		buf.Reset()
		if json.Compact(&buf, []byte(strings.TrimSpace(l))) == nil {
			lines[i] = buf.String()
		}
	}
	return strings.Join(lines, "\n")
}

// foldRuns collapses runs of identical lines into "line ×N" and runs of blank lines into one.
func foldRuns(lines []string) []string {
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	same := func(a, b string) bool {
		if blank(a) {
			return blank(b)
		}
		return a == b
	}
	var out []string
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && same(lines[i], lines[j]) {
			j++
		}
		switch {
		case blank(lines[i]):
			out = append(out, "")
		case j-i >= 2:
			out = append(out, fmt.Sprintf("%s ×%d", lines[i], j-i))
		default:
			out = append(out, lines[i])
		}
		i = j
	}
	return out
}
