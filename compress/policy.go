// Package compress holds ctxwinnow's compression engine. Step 0 ships only Policy, the gate
// that decides from the tool call alone whether an output may be compressed.
package compress

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Verdict is the Policy decision for one tool call.
type Verdict int

const (
	Compress        Verdict = iota // output may be compressed
	PassTool                       // tool not in the compressible set
	PassError                      // the tool call failed
	PassBashRead                   // Bash command reads file content (future Edit anchors)
	PassBashSearch                 // Bash command searches or lists (paths, Edit anchors)
	PassUnparseable                // Bash command could not be parsed; pass through to be safe
)

var verdictNames = [...]string{"Compress", "PassTool", "PassError", "PassBashRead", "PassBashSearch", "PassUnparseable"}

func (v Verdict) String() string {
	if v >= 0 && int(v) < len(verdictNames) {
		return verdictNames[v]
	}
	return "Verdict(?)"
}

const maxShellDepth = 5

// Policy decides from the tool call alone whether its output may be compressed.
// toolInput is the raw JSON of tool_use.input; note explains the verdict.
func Policy(toolName, toolInput string, isError bool) (v Verdict, note string) {
	isMCP := strings.HasPrefix(toolName, "mcp__")
	if toolName != "Bash" && !isMCP {
		return PassTool, "tool not compressible: " + toolName
	}
	if isError {
		return PassError, "tool call failed"
	}
	if isMCP {
		return Compress, "mcp tool"
	}
	cmd, ok := bashCommand(toolInput)
	if !ok {
		return PassUnparseable, "bash input has no command string"
	}
	return classifyScript(cmd, 0)
}

// BashHead returns the program used to rank Bash output in reports: the first program, after
// peeling wrappers and recursing into bash -c, that is not a no-output builtin such as cd.
// It returns "" when the input cannot be parsed.
func BashHead(toolInput string) string {
	cmd, ok := bashCommand(toolInput)
	if !ok {
		return ""
	}
	return scriptHead(cmd, 0)
}

func bashCommand(toolInput string) (string, bool) {
	var in struct {
		Command *string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolInput), &in); err != nil || in.Command == nil {
		return "", false
	}
	return *in.Command, true
}

type verbKind int

const (
	verbOther verbKind = iota
	verbRead
	verbSearch
)

// classifyScript applies the Bash rules: a read verb heading any pipeline wins, then a search
// verb, then an unparseable nested script; otherwise the output may be compressed.
func classifyScript(script string, depth int) (Verdict, string) {
	if depth > maxShellDepth {
		return PassUnparseable, "shell -c nested too deep"
	}
	pipelines, ok := parseBash(script)
	if !ok {
		return PassUnparseable, "unbalanced quotes or substitution"
	}
	var search, unparseable string
	for _, p := range pipelines {
		prog, args, inner, isShell := peel(p[0])
		if isShell {
			v, note := classifyScript(inner, depth+1)
			switch v {
			case PassBashRead:
				return v, note
			case PassBashSearch:
				if search == "" {
					search = note
				}
			case PassUnparseable:
				if unparseable == "" {
					unparseable = note
				}
			}
			continue
		}
		switch verbOf(prog, args) {
		case verbRead:
			return PassBashRead, "bash read verb: " + prog
		case verbSearch:
			if search == "" {
				search = "bash search verb: " + prog
			}
		}
	}
	if search != "" {
		return PassBashSearch, search
	}
	if unparseable != "" {
		return PassUnparseable, unparseable
	}
	return Compress, "bash command"
}

var noOutputBuiltins = map[string]bool{
	"cd": true, "pushd": true, "popd": true, "export": true, "set": true, "unset": true, "source": true, ".": true,
}

func scriptHead(script string, depth int) string {
	if depth > maxShellDepth {
		return ""
	}
	pipelines, ok := parseBash(script)
	if !ok {
		return ""
	}
	for _, p := range pipelines {
		prog, _, inner, isShell := peel(p[0])
		if isShell {
			if h := scriptHead(inner, depth+1); h != "" {
				return h
			}
			continue
		}
		if prog != "" && !noOutputBuiltins[prog] {
			return prog
		}
	}
	return ""
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// shellReserved lists shell reserved words that can head a simple command without producing any
// output of their own; peel skips them to reach the real command, if any.
var shellReserved = map[string]bool{
	"do": true, "then": true, "else": true, "elif": true, "if": true, "while": true, "until": true,
	"!": true, "{": true, "}": true, "done": true, "fi": true, "esac": true,
}

// peel strips variable assignments, leading shell reserved words and wrapper commands (sudo,
// env, timeout, ...) from a simple command and returns the program that actually runs. For
// bash/sh/zsh -c it returns the script. A loop or case header (for/select/case) produces no
// output of its own, so it returns no program.
func peel(words []string) (prog string, args []string, script string, isShell bool) {
	for {
		for len(words) > 0 && (assignRe.MatchString(words[0]) || shellReserved[words[0]]) {
			words = words[1:]
		}
		if len(words) == 0 {
			return "", nil, "", false
		}
		name, rest := path.Base(words[0]), words[1:]
		switch name {
		case "for", "select", "case":
			return "", nil, "", false // loop/case header: no output of its own
		case "sudo":
			words = skipFlags(rest, "u", "g", "C", "h", "p", "U")
		case "env":
			rest = skipFlags(rest, "u", "C", "S")
			for len(rest) > 0 && assignRe.MatchString(rest[0]) {
				rest = rest[1:]
			}
			words = rest
		case "timeout":
			rest = skipFlags(rest, "s", "k")
			if len(rest) > 0 {
				rest = rest[1:] // the duration
			}
			words = rest
		case "time", "nohup", "command", "exec":
			words = skipFlags(rest)
		case "nice":
			words = skipFlags(rest, "n")
		case "xargs":
			words = skipFlags(rest, "I", "n", "P", "L", "d", "s", "E", "a")
		case "bash", "sh", "zsh":
			if s, ok := shellScript(rest); ok {
				return name, rest, s, true
			}
			return name, rest, "", false
		default:
			return name, rest, "", false
		}
	}
}

// skipFlags drops leading flags; a bare "-x" listed in withArg also consumes the next word.
// "--" ends the flags.
func skipFlags(words []string, withArg ...string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
		f := words[0]
		words = words[1:]
		if f == "--" {
			break
		}
		if len(f) == 2 && slices.Contains(withArg, f[1:]) && len(words) > 0 {
			words = words[1:]
		}
	}
	return words
}

// shellScript returns the script of `bash -c SCRIPT` (flag clusters like -lc included).
func shellScript(args []string) (string, bool) {
	hasC := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "+o" || a == "-O" || a == "+O":
			i++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+"):
			if strings.Contains(a[1:], "c") {
				hasC = true
			}
		default:
			return a, hasC
		}
	}
	return "", false
}

var readVerbs = map[string]bool{
	"cat": true, "nl": true, "bat": true, "batcat": true, "head": true, "tail": true, "less": true,
	"more": true, "tac": true, "strings": true, "awk": true, "gawk": true, "mawk": true,
}

var searchVerbs = map[string]bool{
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true, "ack": true, "ugrep": true,
	"find": true, "fd": true, "fdfind": true, "ls": true, "tree": true, "locate": true,
}

func verbOf(prog string, args []string) verbKind {
	switch {
	case readVerbs[prog]:
		return verbRead
	case searchVerbs[prog]:
		return verbSearch
	case prog == "sed":
		if sedInPlace(args) {
			return verbOther
		}
		return verbRead
	case prog == "jq" || prog == "yq":
		if nonFlagCount(args) >= 2 {
			return verbRead
		}
	case prog == "git":
		return gitVerb(args)
	}
	return verbOther
}

func sedInPlace(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--in-place") {
			return true
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a[1:], "i") {
			return true
		}
	}
	return false
}

func nonFlagCount(args []string) int {
	n := 0
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			n++
		}
	}
	return n
}

func gitVerb(args []string) verbKind {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-C" || args[0] == "-c") && len(args) > 1 {
			args = args[2:]
			continue
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return verbOther
	}
	switch args[0] {
	case "show", "diff", "blame", "cat-file":
		return verbRead
	case "grep", "ls-files", "ls-tree":
		return verbSearch
	}
	return verbOther
}
