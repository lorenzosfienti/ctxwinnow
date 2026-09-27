package policy

import "strings"

// parseBash lexes a shell command into pipelines of simple commands, each a list of words.
// Separators ; && || & and newlines end a pipeline; | and |& end a simple command inside one.
// Quotes and backslash escapes are honoured, $(...) and backticks are kept as opaque word text,
// comments are dropped and heredoc bodies are skipped. ok is false when quotes or substitutions
// are unbalanced.
func parseBash(src string) (pipelines [][][]string, ok bool) {
	var (
		word     strings.Builder
		inWord   bool
		cmd      []string
		pipeline [][]string
		heredocs []string // delimiters whose bodies start after the next newline
	)
	endWord := func() {
		if inWord {
			cmd = append(cmd, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCmd := func() {
		endWord()
		if len(cmd) > 0 {
			pipeline = append(pipeline, cmd)
			cmd = nil
		}
	}
	endPipeline := func() {
		endCmd()
		if len(pipeline) > 0 {
			pipelines = append(pipelines, pipeline)
			pipeline = nil
		}
	}

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t':
			endWord()
			i++
		case c == '\n':
			endPipeline()
			i++
			if len(heredocs) > 0 {
				i = skipHeredocs(src, i, heredocs)
				heredocs = nil
			}
		case c == '#' && !inWord:
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '\'':
			end := strings.IndexByte(src[i+1:], '\'')
			if end < 0 {
				return nil, false
			}
			word.WriteString(src[i+1 : i+1+end])
			inWord = true
			i += end + 2
		case c == '"':
			j, closed := i+1, false
			for j < len(src) {
				if src[j] == '"' {
					closed = true
					break
				}
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				word.WriteByte(src[j])
				j++
			}
			if !closed {
				return nil, false
			}
			inWord = true
			i = j + 1
		case c == '\\':
			switch {
			case i+1 >= len(src):
				i++
			case src[i+1] == '\n': // line continuation
				i += 2
			default:
				word.WriteByte(src[i+1])
				inWord = true
				i += 2
			}
		case c == '$' && i+1 < len(src) && src[i+1] == '(':
			end, found := matchParen(src, i+1)
			if !found {
				return nil, false
			}
			word.WriteString(src[i : end+1])
			inWord = true
			i = end + 1
		case (c == '<' || c == '>') && i+1 < len(src) && src[i+1] == '(':
			// process substitution: <(...) / >(...) is kept opaque, like $(...).
			end, found := matchParen(src, i+1)
			if !found {
				return nil, false
			}
			word.WriteString(src[i : end+1])
			inWord = true
			i = end + 1
		case c == '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return nil, false
			}
			word.WriteString(src[i : i+end+2])
			inWord = true
			i += end + 2
		case c == ';':
			endPipeline()
			i++
		case c == '(' || c == ')':
			// unquoted parens are subshell/grouping boundaries: end the current pipeline.
			// $(...), `...`, <(...) and >(...) are already consumed opaquely above.
			endPipeline()
			i++
		case c == '|':
			if i+1 < len(src) && src[i+1] == '|' {
				endPipeline()
				i += 2
				continue
			}
			endCmd()
			i++
			if i < len(src) && src[i] == '&' {
				i++
			}
		case c == '&':
			switch {
			case i+1 < len(src) && src[i+1] == '&':
				endPipeline()
				i += 2
			case i+1 < len(src) && src[i+1] == '>', inWord && endsWithRedirect(word.String()):
				word.WriteByte(c) // &> or 2>&1: part of a redirection word
				inWord = true
				i++
			default:
				endPipeline()
				i++
			}
		case strings.HasPrefix(src[i:], "<<<"):
			endWord()
			cmd = append(cmd, "<<<")
			i += 3
		case strings.HasPrefix(src[i:], "<<"):
			endWord()
			j := i + 2
			if j < len(src) && src[j] == '-' {
				j++
			}
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			k := j
			for k < len(src) && !strings.ContainsRune(" \t\n;&|<>()", rune(src[k])) {
				k++
			}
			if delim := strings.Trim(src[j:k], `'"\`); delim != "" {
				heredocs = append(heredocs, delim)
			}
			i = k
		default:
			word.WriteByte(c)
			inWord = true
			i++
		}
	}
	endPipeline()
	return pipelines, true
}

func endsWithRedirect(w string) bool {
	return strings.HasSuffix(w, ">") || strings.HasSuffix(w, "<")
}

// matchParen returns the index of the ')' matching the '(' at open, skipping quoted text.
func matchParen(src string, open int) (int, bool) {
	depth := 0
	for j := open; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '\'':
			k := strings.IndexByte(src[j+1:], '\'')
			if k < 0 {
				return 0, false
			}
			j += k + 1
		case '"':
			k := j + 1
			for k < len(src) && src[k] != '"' {
				if src[k] == '\\' {
					k++
				}
				k++
			}
			if k >= len(src) {
				return 0, false
			}
			j = k
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j, true
			}
		}
	}
	return 0, false
}

// skipHeredocs skips, in order, the body of each heredoc starting at i and returns the index
// after the last delimiter line (or len(src) if a delimiter never appears).
func skipHeredocs(src string, i int, delims []string) int {
	for _, d := range delims {
		for i < len(src) {
			line, next := src[i:], len(src)
			if nl := strings.IndexByte(src[i:], '\n'); nl >= 0 {
				line, next = src[i:i+nl], i+nl+1
			}
			i = next
			if strings.TrimSpace(line) == d {
				break
			}
		}
	}
	return i
}
