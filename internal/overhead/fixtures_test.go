package overhead

import (
	"cmp"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

// canonicalFixture is the T2 synthetic session, relative to internal/overhead.
const canonicalFixture = "../transcript/testdata/overhead_main.jsonl"

// loadCanonical parses canonicalFixture and returns NewSession(rel, parsed).
func loadCanonical(t *testing.T, rel string) *Session {
	t.Helper()
	f, err := os.Open(canonicalFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := transcript.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return NewSession(rel, s)
}

// tsess describes a synthetic transcript; build() returns the *transcript.Session, session() runs
// NewSession.
type tsess struct {
	Rel        string   // e.g. "p1/a.jsonl" or "p1/a/subagents/agent-1.jsonl"
	CWD        string   // as written
	Start      string   // RFC 3339; default "2026-09-01T10:00:00Z"
	End        string   // RFC 3339; default Start + len(Contexts) minutes
	Version    string   // default "2.1.283"
	IDs        []string // usage ids; default Rel+"#"+index (also for indexes past len(IDs))
	Contexts   []int
	SysBytes   int
	Tools      []transcript.ToolDef // nil → no snapshot
	Pre        []transcript.Attachment
	Calls      []string // tool names called (LastTS = End)
	SkillCalls []string
	Commands   []string
	Invoked    []string
	Fork       bool
}

func (x tsess) build() *transcript.Session {
	start := x.Start
	if start == "" {
		start = "2026-09-01T10:00:00Z"
	}
	end := x.End
	if end == "" {
		t0, err := time.Parse(time.RFC3339, start)
		if err != nil {
			panic(err)
		}
		end = t0.Add(time.Duration(len(x.Contexts)) * time.Minute).UTC().Format(time.RFC3339)
	}
	version := x.Version
	if version == "" {
		version = "2.1.283"
	}
	s := &transcript.Session{CWD: x.CWD, FirstTS: start, LastTS: end, Version: version, Entrypoint: "cli",
		Fork: x.Fork, Pre: x.Pre, SkillCalls: x.SkillCalls, Commands: x.Commands, InvokedSkills: x.Invoked}
	for i, c := range x.Contexts {
		id := x.Rel + "#" + strconv.Itoa(i)
		if i < len(x.IDs) {
			id = x.IDs[i]
		}
		s.Events = append(s.Events, transcript.Event{Kind: transcript.EvUsage, Usage: transcript.Usage{MessageID: id, Input: c}})
	}
	if x.Tools != nil {
		s.Snapshot = &transcript.Snapshot{SystemPromptBytes: x.SysBytes, Tools: x.Tools}
	}
	counts := map[string]int{}
	for _, name := range x.Calls {
		counts[name]++
	}
	for name, n := range counts {
		s.ToolCalls = append(s.ToolCalls, transcript.ToolCall{Name: name, Count: n, LastTS: end})
	}
	slices.SortFunc(s.ToolCalls, func(a, b transcript.ToolCall) int { return cmp.Compare(a.Name, b.Name) })
	return s
}

func (x tsess) session() *Session { return NewSession(x.Rel, x.build()) }

func tool(name string, bytes int) transcript.ToolDef {
	return transcript.ToolDef{Name: name, Bytes: bytes}
}

func att(typ string, bytes int, names ...string) transcript.Attachment {
	return transcript.Attachment{Type: typ, Bytes: bytes, Names: names}
}

// instr is a one-file AttInstructions attachment.
func instr(path, typ string, bytes int) transcript.Attachment {
	return transcript.Attachment{Type: transcript.AttInstructions, Bytes: bytes,
		Files: []transcript.InstructionFile{{Path: path, Type: typ, Bytes: bytes}}}
}

// listing is an initial skill listing; Bytes = Σ line bytes + len(lines) − 1, Names = the named lines.
func listing(lines ...transcript.SkillLine) transcript.Attachment {
	a := transcript.Attachment{Type: transcript.AttSkillListing, Lines: lines, Bytes: len(lines) - 1}
	for _, l := range lines {
		a.Bytes += l.Bytes
		if l.Name != "" {
			a.Names = append(a.Names, l.Name)
		}
	}
	return a
}

// built runs session() for each description, then Build.
func built(xs ...tsess) []*Session {
	sessions := make([]*Session, 0, len(xs))
	for _, x := range xs {
		sessions = append(sessions, x.session())
	}
	return Build(sessions)
}
