// Package ceiling measures how much of real sessions ctxwinnow could compress (step 0).
package ceiling

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lorenzosfienti/ctxwinnow/compress"
	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// Category is the step 0 bucket of one tool result (spec §5).
type Category string

const (
	CatNonText     Category = "non_text"
	CatPassTool    Category = "pass_tool"
	CatError       Category = "error"
	CatBashRead    Category = "bash_read"
	CatBashSearch  Category = "bash_search"
	CatUnparseable Category = "unparseable"
	CatPersisted   Category = "persisted"
	CatSmall       Category = "small"
	CatJSON        Category = "json"
	CatUncertain   Category = "uncertain"
	CatLines       Category = "lines"
)

// Categories lists every category in report order.
var Categories = []Category{
	CatNonText, CatPassTool, CatError, CatBashRead, CatBashSearch, CatUnparseable,
	CatPersisted, CatSmall, CatJSON, CatUncertain, CatLines,
}

// Compressible reports whether the engine would be allowed to compress this category.
func (c Category) Compressible() bool { return c == CatJSON || c == CatLines }

const smallTokens = 1000

// EstTokens estimates tokens as ceil(bytes/4).
func EstTokens(s string) int { return (len(s) + 3) / 4 }

var exitCodeRe = regexp.MustCompile(`^Exit code \d+`)

// Classify assigns the first matching category, in the order of spec §5.
func Classify(r transcript.ToolResult) Category {
	if r.NonText {
		return CatNonText
	}
	v, _ := compress.Policy(r.ToolName, r.ToolInput, r.IsError)
	switch v {
	case compress.PassTool:
		return CatPassTool
	case compress.PassError:
		return CatError
	}
	if exitCodeRe.MatchString(r.Text) {
		return CatError
	}
	switch v {
	case compress.PassBashRead:
		return CatBashRead
	case compress.PassBashSearch:
		return CatBashSearch
	case compress.PassUnparseable:
		return CatUnparseable
	}
	switch {
	case strings.HasPrefix(strings.TrimSpace(r.Text), "<persisted-output>"):
		return CatPersisted
	case EstTokens(r.Text) < smallTokens:
		return CatSmall
	case isJSON(r.Text):
		return CatJSON
	case isProse(r.Text):
		return CatUncertain
	}
	return CatLines
}

func nonBlankLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func isJSONValue(s string) bool {
	return s != "" && (s[0] == '{' || s[0] == '[') && json.Valid([]byte(s))
}

// isJSON: the whole text is one JSON object/array, or ≥80% of non-blank lines are (NDJSON).
func isJSON(s string) bool {
	t := strings.TrimSpace(s)
	if isJSONValue(t) {
		return true
	}
	lines := nonBlankLines(t)
	if len(lines) < 2 {
		return false
	}
	n := 0
	for _, l := range lines {
		if isJSONValue(strings.TrimSpace(l)) {
			n++
		}
	}
	return n*5 >= len(lines)*4
}

var mdPrefixes = []string{"#", "- ", "* ", "|", ">", "```"}

// isProse: ≥30% markdown-looking lines, or fewer than 20 lines averaging more than 160 runes.
func isProse(s string) bool {
	lines := nonBlankLines(s)
	if len(lines) == 0 {
		return false
	}
	md, runes := 0, 0
	for _, l := range lines {
		t := strings.TrimLeft(l, " \t")
		for _, p := range mdPrefixes {
			if strings.HasPrefix(t, p) {
				md++
				break
			}
		}
		runes += utf8.RuneCountInString(l)
	}
	if md*10 >= len(lines)*3 {
		return true
	}
	return len(lines) < 20 && runes > 160*len(lines)
}
