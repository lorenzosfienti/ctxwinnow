// Package overhead measures the fixed per-call context baseline of Claude Code sessions (spec 0.2).
package overhead

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

const (
	toolJSONBytesPerToken  = 4.5 // gate-0 anchor: 54,260 removed bytes matched an 11.3–13.4k delta
	toolJSONBandLow        = 4.0
	toolJSONBandHigh       = 4.8
	sanityMinBytesPerToken = 2.0
	sanityMaxBytesPerToken = 6.0
	snapshotMinVersion     = "2.1.259" // first Claude Code version that writes tool snapshots
)

// Kind tells main sessions from subagent sessions.
type Kind int

const (
	Main Kind = iota
	Subagent
)

// Status is the single reason a session is or is not eligible.
type Status int

const (
	Eligible     Status = iota // set by Build; Summarize may downgrade to Filtered or TooFewTurns
	NoUsage                    // no usage with context > 0 (e.g. workflows/wf_*/journal.jsonl)
	Continuation               // first usage id already seen in an earlier file
	Fork                       // fork-context-ref present ("fork (inherits parent context)")
	Filtered                   // outside --since/--until or rejected by --only/--exclude (Summarize)
	TooFewTurns                // N < --min-turns (Summarize)
)

// Decomp says whether a main session was decomposed; subagents keep DecompNone.
type Decomp int

const (
	DecompNone       Decomp = iota // subagent, or main session without usage
	DecompOK                       // components carry Tokens summing to B
	DecompOldVersion               // no tool snapshot, Version < 2.1.259
	DecompNoSnapshot               // no tool snapshot, Version ≥ 2.1.259 or unknown
	DecompSanity                   // sanity guard: bytes/B outside [2,6], R = 0 or no non-tool bytes
)

// CompKind is the kind of one component of the baseline.
type CompKind string

const (
	CompTool         CompKind = "tool"     // one per non-MCP tool in the snapshot; Name = tool
	CompMCPTool      CompKind = "mcp-tool" // one per server with mcp__<server>__* tools in the snapshot; Name = server
	CompSystemPrompt CompKind = "system-prompt"
	CompInstruction  CompKind = "instruction"   // one per instruction file; Name = path, Label set
	CompSkillListing CompKind = "skill-listing" // skill lines + overhead lines
	CompMCP          CompKind = "mcp"           // deferred names + MCP instructions + surfaced MCP schemas
	CompHook         CompKind = "hook"          // SessionStart hook context
	CompHarness      CompKind = "harness"       // environment, model, date, session_context, tokens reminder, agent listing, isMeta text
	CompFirstPrompt  CompKind = "first-prompt"  // part of B, not configurable
)

// Instruction file labels.
const (
	LabelUser          = "User"
	LabelProject       = "Project" // Project type, basename CLAUDE.md or CLAUDE.local.md (split on / and \)
	LabelProjectImport = "Project (non-CLAUDE.md, likely @import)"
	LabelAutoMem       = "AutoMem"
	LabelOther         = "other"
)

// Component is one part of a main session's baseline.
type Component struct {
	Kind   CompKind
	Name   string // tool name, MCP server name or instruction path; "" otherwise
	Label  string // instruction label; "" otherwise
	Bytes  int
	Tokens float64 // ≈tokens, set only when the session is DecompOK
}

// Key is "tool:<name>", "mcp-tool:<server>", "instruction:<path>", or string(Kind) for the others.
func (c Component) Key() string {
	switch c.Kind {
	case CompTool, CompMCPTool, CompInstruction:
		return string(c.Kind) + ":" + c.Name
	}
	return string(c.Kind)
}

// Session is the size-only record of one transcript file; contents are never kept.
type Session struct {
	// Per file (NewSession).
	Path                    string    // root-relative, slash-separated
	Kind                    Kind      // Subagent when "/"+Path contains "/subagents/"
	CWD                     string    // as written; never cleaned
	Start, End              time.Time // parsed FirstTS / LastTS (RFC 3339, UTC); zero when absent or unparsable
	Version                 string
	Entrypoint              string
	Fork                    bool
	Malformed               int
	CompactBeforeFirstUsage bool
	UsageIDs                []string // file-de-duplicated usages with Context() > 0, file order ("" = no id)
	Contexts                []int    // parallel to UsageIDs
	B                       int      // Contexts[0]; 0 without usage
	HasSnapshot             bool
	ToolNames               []string    // snapshot tool names in order (main and subagent)
	Components              []Component // main sessions with a snapshot, in order (see NewSession); zero-byte components omitted
	Decomp                  Decomp
	RemainderBPT            float64              // Σ non-tool bytes / R when DecompOK
	Called                  map[string]time.Time // tool name → latest call (zero when unknown); never nil
	Listed                  map[string]int       // listed skill → line bytes (initial listing); never nil
	SkillsUsed              map[string]bool      // Skill input.skill ∪ Commands that are listed ∪ InvokedSkills; never nil
	MCPServers              []string             // servers carried (snapshot MCP tools, deferred mcp__ names, MCP instruction names), sorted unique

	// Cross-file (Build); Status also refined by Summarize.
	Status Status
	N      int   // usages left after cross-file de-duplication
	SumCtx int64 // Σ their contexts
}

// Share is float64(B*N)/float64(SumCtx), 0 when SumCtx == 0.
func (s *Session) Share() float64 {
	if s.SumCtx == 0 {
		return 0
	}
	return float64(s.B) * float64(s.N) / float64(s.SumCtx)
}

// Tokens sums Tokens of the components with this Key; ok is false when none is present.
func (s *Session) Tokens(key string) (tokens float64, ok bool) {
	for _, c := range s.Components {
		if c.Key() == key {
			tokens, ok = tokens+c.Tokens, true
		}
	}
	return tokens, ok
}

// Bytes sums Bytes of the components with this Key; ok is false when none is present.
func (s *Session) Bytes(key string) (bytes int, ok bool) {
	for _, c := range s.Components {
		if c.Key() == key {
			bytes, ok = bytes+c.Bytes, true
		}
	}
	return bytes, ok
}

// NewSession summarises one parsed transcript (rel = path relative to --root, any separator).
// Main sessions with a snapshot get Components in this order: tools (snapshot order, MCP tools
// merged per server at first appearance), system prompt, instruction files, skill listing, MCP,
// hook, harness, first prompt. With B > 0 they are attributed: tool tokens = Bytes/4.5; R = B − Σ
// tool tokens clamped at 0; other components get R·Bytes/Σ other bytes; guard → DecompSanity.
func NewSession(rel string, s *transcript.Session) *Session {
	out := &Session{
		Path:                    filepath.ToSlash(rel),
		CWD:                     s.CWD,
		Start:                   parseTS(s.FirstTS),
		End:                     parseTS(s.LastTS),
		Version:                 s.Version,
		Entrypoint:              s.Entrypoint,
		Fork:                    s.Fork,
		Malformed:               s.Malformed,
		CompactBeforeFirstUsage: s.CompactBeforeUsage > 0,
		HasSnapshot:             s.Snapshot != nil,
		Called:                  map[string]time.Time{},
		Listed:                  map[string]int{},
		SkillsUsed:              map[string]bool{},
	}
	if strings.Contains("/"+out.Path, "/subagents/") {
		out.Kind = Subagent
	}
	for _, ev := range s.Events {
		if ev.Kind == transcript.EvUsage && ev.Usage.Context() > 0 {
			out.UsageIDs = append(out.UsageIDs, ev.Usage.MessageID)
			out.Contexts = append(out.Contexts, ev.Usage.Context())
		}
	}
	if len(out.Contexts) > 0 {
		out.B = out.Contexts[0]
	}
	for _, c := range s.ToolCalls {
		out.Called[c.Name] = parseTS(c.LastTS)
	}
	servers := map[string]bool{}
	if s.Snapshot != nil {
		for _, t := range s.Snapshot.Tools {
			out.ToolNames = append(out.ToolNames, t.Name)
			if srv, ok := serverOf(t.Name); ok {
				servers[srv] = true
			}
		}
	}
	for _, a := range s.Pre {
		switch a.Type {
		case transcript.AttSkillListing:
			for _, name := range a.Names {
				out.Listed[name] = 0
			}
			for _, l := range a.Lines {
				if l.Name != "" {
					out.Listed[l.Name] = l.Bytes
				}
			}
		case transcript.AttDeferredDelta, transcript.AttDeferredRecord:
			for _, name := range a.Names {
				if srv, ok := serverOf(name); ok {
					servers[srv] = true
				}
			}
		case transcript.AttMCPInstructions:
			for _, name := range a.Names {
				servers[name] = true
			}
		}
	}
	out.MCPServers = sortedSet(servers)
	for _, name := range s.SkillCalls {
		out.SkillsUsed[name] = true
	}
	for _, name := range s.Commands {
		if _, listed := out.Listed[name]; listed {
			out.SkillsUsed[name] = true
		}
	}
	for _, name := range s.InvokedSkills {
		out.SkillsUsed[name] = true
	}

	if out.Kind == Subagent {
		return out
	}
	if s.Snapshot == nil {
		if out.B > 0 {
			out.Decomp = DecompNoSnapshot
			if validVersion(out.Version) && compareVersions(out.Version, snapshotMinVersion) < 0 {
				out.Decomp = DecompOldVersion
			}
		}
		return out
	}
	out.Components = components(s)
	if out.B > 0 {
		out.attribute()
	}
	return out
}

// components lists a main session's components in report order, zero-byte ones omitted.
func components(s *transcript.Session) []Component {
	var cs []Component
	add := func(c Component) {
		if c.Bytes > 0 {
			cs = append(cs, c)
		}
	}
	mcpAt := map[string]int{} // server → index in tools
	var tools []Component
	for _, t := range s.Snapshot.Tools {
		if srv, ok := serverOf(t.Name); ok {
			if i, seen := mcpAt[srv]; seen {
				tools[i].Bytes += t.Bytes
				continue
			}
			mcpAt[srv] = len(tools)
			tools = append(tools, Component{Kind: CompMCPTool, Name: srv, Bytes: t.Bytes})
			continue
		}
		tools = append(tools, Component{Kind: CompTool, Name: t.Name, Bytes: t.Bytes})
	}
	for _, c := range tools {
		add(c)
	}
	add(Component{Kind: CompSystemPrompt, Bytes: s.Snapshot.SystemPromptBytes})
	sums := map[CompKind]int{}
	for _, a := range s.Pre {
		switch a.Type {
		case transcript.AttInstructions:
			for _, f := range a.Files {
				add(Component{Kind: CompInstruction, Name: f.Path, Label: instructionLabel(f.Type, f.Path), Bytes: f.Bytes})
			}
		case transcript.AttSkillListing:
			sums[CompSkillListing] += a.Bytes
		case transcript.AttDeferredDelta, transcript.AttDeferredRecord, transcript.AttMCPInstructions:
			sums[CompMCP] += a.Bytes
		case transcript.AttHookContext:
			sums[CompHook] += a.Bytes
		case transcript.AttEnvironment, transcript.AttModel, transcript.AttDate, transcript.AttSessionContext,
			transcript.AttTokensReminder, transcript.AttAgentListing, transcript.AttUserMeta:
			sums[CompHarness] += a.Bytes
		case transcript.AttFirstPrompt:
			sums[CompFirstPrompt] += a.Bytes
		}
	}
	for _, k := range []CompKind{CompSkillListing, CompMCP, CompHook, CompHarness, CompFirstPrompt} {
		add(Component{Kind: k, Bytes: sums[k]})
	}
	return cs
}

// attribute applies the two-rate attribution and the sanity guard to a main session with B > 0.
func (s *Session) attribute() {
	var toolBytes, otherBytes int
	for _, c := range s.Components {
		if c.Kind == CompTool || c.Kind == CompMCPTool {
			toolBytes += c.Bytes
		} else {
			otherBytes += c.Bytes
		}
	}
	b := float64(s.B)
	r := max(0, b-float64(toolBytes)/toolJSONBytesPerToken)
	ratio := float64(toolBytes+otherBytes) / b
	if ratio < sanityMinBytesPerToken || ratio > sanityMaxBytesPerToken || r == 0 || otherBytes == 0 {
		s.Decomp = DecompSanity
		return
	}
	for i := range s.Components {
		c := &s.Components[i]
		if c.Kind == CompTool || c.Kind == CompMCPTool {
			c.Tokens = float64(c.Bytes) / toolJSONBytesPerToken
		} else {
			c.Tokens = r * float64(c.Bytes) / float64(otherBytes)
		}
	}
	s.Decomp = DecompOK
	s.RemainderBPT = float64(otherBytes) / r
}

// instructionLabel maps an instruction file type and path to its report label.
func instructionLabel(typ, path string) string {
	switch typ {
	case "User":
		return LabelUser
	case "AutoMem":
		return LabelAutoMem
	case "Project":
		base := path[strings.LastIndexAny(path, `/\`)+1:]
		if base == "CLAUDE.md" || base == "CLAUDE.local.md" {
			return LabelProject
		}
		return LabelProjectImport
	}
	return LabelOther
}

// Build sorts sessions by (Start, End, Path) and walks them with one global set of non-empty usage
// ids: no usage → NoUsage; first id non-empty and seen → Continuation; Fork → Fork; otherwise
// Eligible with N / SumCtx counting only usages whose id is "" or unseen. Every non-empty id of every
// file is added to the set after the file is processed. Returns the same slice, sorted.
func Build(sessions []*Session) []*Session {
	slices.SortStableFunc(sessions, func(a, b *Session) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		if c := a.End.Compare(b.End); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})
	seen := map[string]bool{}
	for _, s := range sessions {
		s.N, s.SumCtx = 0, 0
		switch {
		case len(s.UsageIDs) == 0:
			s.Status = NoUsage
		case s.UsageIDs[0] != "" && seen[s.UsageIDs[0]]:
			s.Status = Continuation
		case s.Fork:
			s.Status = Fork
		default:
			s.Status = Eligible
			for i, id := range s.UsageIDs {
				if id == "" || !seen[id] {
					s.N++
					s.SumCtx += int64(s.Contexts[i])
				}
			}
		}
		for _, id := range s.UsageIDs {
			if id != "" {
				seen[id] = true
			}
		}
	}
	return sessions
}

// compareVersions compares dotted versions numerically part by part ("2.1.259" < "2.1.283");
// a missing or non-numeric part counts as 0. Returns -1, 0 or 1.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		if c := cmp.Compare(versionPart(pa, i), versionPart(pb, i)); c != 0 {
			return c
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

// validVersion reports whether v is a dotted list of decimal numbers ("2.1.283"); anything else
// counts as an unknown version.
func validVersion(v string) bool {
	for _, p := range strings.Split(v, ".") {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// serverOf returns the server of "mcp__<server>__<tool>" (strings.Cut after "mcp__" on "__").
func serverOf(tool string) (server string, ok bool) {
	rest, isMCP := strings.CutPrefix(tool, "mcp__")
	if !isMCP {
		return "", false
	}
	server, _, ok = strings.Cut(rest, "__")
	if !ok || server == "" {
		return "", false
	}
	return server, true
}

// parseTS parses an RFC 3339 timestamp into UTC; zero when absent or unparsable.
func parseTS(ts string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func sortedSet(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
