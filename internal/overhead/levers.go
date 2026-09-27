package overhead

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lorenzosfienti/ctxwinnow/internal/transcript"
)

const (
	MinBenefit           = 0.005 // user-scope saving ≥ 0.5% of main token-turns
	InstructionMinTokens = 2000  // instruction lever threshold, median ≈tokens when present
	MaxSkillOverrides    = 10    // generated skillOverrides entries
)

// Scope is where a snippet goes. There is deliberately no shared Project scope: a committed
// project file would disable the feature for every collaborator.
type Scope string

const (
	ScopeUser  Scope = "User"  // ~/.claude/settings.json
	ScopeLocal Scope = "Local" // .claude/settings.local.json (gitignored)
	ScopeEnv   Scope = "Env"
	ScopeFlag  Scope = "Flag"
)

// Snippet is one copy-paste configuration line.
type Snippet struct {
	Scope Scope
	// Text is literal except two placeholders the renderer expands through the Redactor:
	// {{skills}} → a JSON object of skill → "user-invocable-only"; {{path}} → a JSON string of the path.
	Text    string
	OffWins bool // renderer appends "no other settings file can re-enable it; delete this line to undo"
}

// Lever is one catalogued configuration change.
type Lever struct {
	ID       string // "disable-artifact"
	Target   string // "tool:Artifact" | "tool:SendFeedback" | "tool:Workflow" | "skill-listing" | "instructions" | "mcp"
	Snippets []Snippet
	Status   string // "VERIFIED 2.1.283 (gate 0): …" | "UNVERIFIED: documented at <url>"
	Tradeoff string
	DocURL   string
}

const (
	docSettings = "https://code.claude.com/docs/en/settings-reference"
	docSkills   = "https://code.claude.com/docs/en/skills"
	docMemory   = "https://code.claude.com/docs/en/memory"
	docMCP      = "https://code.claude.com/docs/en/mcp"
)

// Catalog is the 0.2 catalog of spec §7, in this order: disable-artifact, disable-sendfeedback,
// disable-workflows, skill-visibility, instructions, mcp. No Scope is ever a shared Project scope.
var Catalog = []Lever{
	{
		ID:     "disable-artifact",
		Target: "tool:Artifact",
		Snippets: []Snippet{
			{Scope: ScopeUser, Text: `"enableArtifact": false`, OffWins: true},
			{Scope: ScopeLocal, Text: `"enableArtifact": false`, OffWins: true},
			{Scope: ScopeEnv, Text: `CLAUDE_CODE_DISABLE_ARTIFACT=1 claude`},
			{Scope: ScopeFlag, Text: `claude --disallowedTools Artifact`},
		},
		Status: "VERIFIED 2.1.283 (gate 0): n=1 per mechanism; Local enableArtifact −13,382; env −11,740; " +
			"flag −12,864; Local deny −12,866; ≈54.3k bytes removed",
		Tradeoff: "Claude can no longer publish pages; off wins everywhere (Local/Project honoured from 2.1.242)",
		DocURL:   docSettings,
	},
	{
		ID:     "disable-sendfeedback",
		Target: "tool:SendFeedback",
		Snippets: []Snippet{
			{Scope: ScopeUser, Text: `"feedbackDrafts": "off"`},
			{Scope: ScopeEnv, Text: `CLAUDE_CODE_SEND_FEEDBACK=0 claude`},
		},
		Status:   "UNVERIFIED: documented at " + docSettings + ` ("removes the SendFeedback tool"; User or managed scope only)`,
		Tradeoff: "No feedback drafts",
		DocURL:   docSettings,
	},
	{
		ID:     "disable-workflows",
		Target: "tool:Workflow",
		Snippets: []Snippet{
			{Scope: ScopeUser, Text: `"enableWorkflows": false`},
			{Scope: ScopeLocal, Text: `"enableWorkflows": false`},
		},
		Status:   "UNVERIFIED: documented at " + docSettings,
		Tradeoff: "Also removes bundled workflow commands, `/workflow-authoring` and the ultracode keyword",
		DocURL:   docSettings,
	},
	{
		ID:     "skill-visibility",
		Target: "skill-listing",
		Snippets: []Snippet{
			{Scope: ScopeUser, Text: `"skillOverrides": {{skills}}`},
			{Scope: ScopeLocal, Text: `"skillOverrides": {{skills}}`},
		},
		Status: "UNVERIFIED: documented at " + docSkills,
		Tradeoff: "Claude no longer picks the skill itself; `/name` still works. `\"name-only\"` suits rarely used " +
			"skills; `\"off\"` also hides `/name`. Plugin skills (names with `:`) are unaffected — manage them via " +
			"`/plugin`. Broader knobs: `skillListingMaxDescChars`, `skillListingBudgetFraction`. Detail: `/skill-doctor`",
		DocURL: docSkills,
	},
	{
		ID:     "instructions",
		Target: "instructions",
		Snippets: []Snippet{
			{Scope: ScopeLocal, Text: `"claudeMdExcludes": [{{path}}]`},
			{Scope: ScopeLocal, Text: `"autoMemoryEnabled": false`},
		},
		Status:   "UNVERIFIED: documented at " + docMemory,
		Tradeoff: "Excluded files' instructions are lost; turning auto memory off also stops Claude from saving new memories",
		DocURL:   docMemory,
	},
	{
		ID:     "mcp",
		Target: "mcp",
		Status: "UNVERIFIED: documented at " + docMCP,
		Tradeoff: "Informational: per-server mechanisms are `permissions.deny [\"mcp__<server>\"]` and " +
			"`deniedMcpServers [{\"serverName\": \"…\"}]`; both block that server's tools",
		DocURL: docMCP,
	},
}

// LeverFor returns the catalog entry with this Target (e.g. "tool:Read" → false: "no known safe lever").
func LeverFor(target string) (Lever, bool) {
	for _, lv := range Catalog {
		if lv.Target == target {
			return lv, true
		}
	}
	return Lever{}, false
}

// Saving is the expected saving of a lever over one scope, from the user's own data.
type Saving struct {
	Sessions   int     // decomposed main sessions in the scope
	Calls      int     // Σ N over the scope
	Projects   int     // distinct CWDs in the scope; a Local snippet is needed once per project
	TokenTurns float64 // Σ tokens_s × N_s
	PerCall    float64 // TokenTurns / Calls (0 when Calls == 0)
	Share      float64 // TokenTurns / Summary.MainTokenTurns

	cwds map[string]bool // CWDs counted in Projects
}

func (sv *Saving) add(cwd string, tokens float64, n int) {
	sv.Sessions++
	sv.Calls += n
	sv.TokenTurns += tokens * float64(n)
	if sv.cwds == nil {
		sv.cwds = map[string]bool{}
	}
	if !sv.cwds[cwd] {
		sv.cwds[cwd] = true
		sv.Projects++
	}
}

func (sv *Saving) finish(mainTokenTurns float64) {
	if sv.Calls > 0 {
		sv.PerCall = sv.TokenTurns / float64(sv.Calls)
	}
	if mainTokenTurns > 0 {
		sv.Share = sv.TokenTurns / mainTokenTurns
	}
}

// LeverUsage is the usage printed next to a tool lever's saving.
type LeverUsage struct {
	Called   int       // eligible main sessions calling the target
	Carrying int       // eligible main sessions whose snapshot carries it
	Projects int       // distinct CWDs among calling sessions
	Last     time.Time // latest call; zero when never
}

// Presence replaces reading settings files: when the tool was last sent and how many recent
// snapshot sessions lacked it. It reflects past launches, not current settings.
type Presence struct {
	LastSent       time.Time // Start of the latest eligible main snapshot session carrying the tool; zero if never
	LastVersion    string
	AbsentRecent   int // eligible main snapshot sessions after LastSent (the M most recent)
	AbsentProjects int // distinct CWDs among them (P)
}

// LeverResult is one catalog entry evaluated on the user's data.
type LeverResult struct {
	Lever                 // catalog entry; Snippets narrowed per Action for instructions
	Subject    string     // tool name or instruction path; "" for skill-listing and mcp
	FileLabel  string     // instructions: Component.Label of Subject
	Action     string     // instructions: "trim" | "exclude" | "prune" | "none"; "" otherwise
	Skills     []string   // skill-visibility: ≤ MaxSkillOverrides never-used non-plugin skills, largest MedianBytes first
	User       Saving     // all carrying decomposed main sessions
	Project    Saving     // tools: carrying sessions in CWDs with no eligible main session calling the target; instructions with a Local snippet: = User
	HasProject bool       // tool targets, and instruction results whose action has a Local snippet (exclude, prune)
	Usage      LeverUsage // tool targets only
	Presence   *Presence  // tool targets only
	Printable  bool       // User.Share ≥ MinBenefit (and, for instructions, Action != "none")
}

// Evaluate computes every catalogued lever on the user's data, ranked by User.TokenTurns desc, then
// ID, then Subject. Tool levers always return one result (Printable false when nothing is saved).
// Instructions: one result per file with median ≈tokens ≥ InstructionMinTokens; User/Project(in
// cwd) → "trim" (no snippet); Project whose directory is a strict ancestor of every carrying
// session's CWD → "exclude" (Local claudeMdExcludes {{path}}); AutoMem → "prune" (Local
// "autoMemoryEnabled": false); other → "none" (never printable). exclude and prune carry a project
// scope equal to the user scope (Local snippet, once per carrying project).
func Evaluate(sum *Summary) []LeverResult {
	var out []LeverResult
	for _, lv := range Catalog {
		switch {
		case strings.HasPrefix(lv.Target, "tool:"):
			out = append(out, evalTool(sum, lv))
		case lv.Target == "skill-listing":
			out = append(out, evalSkills(sum, lv))
		case lv.Target == "instructions":
			out = append(out, evalInstructions(sum, lv)...)
		case lv.Target == "mcp":
			lr := LeverResult{Lever: lv}
			for _, s := range sum.Decomposed {
				if t, ok := s.Tokens(string(CompMCP)); ok {
					lr.User.add(s.CWD, t, s.N)
				}
			}
			lr.User.finish(sum.MainTokenTurns)
			lr.Printable = lr.User.Share >= MinBenefit
			out = append(out, lr)
		}
	}
	slices.SortStableFunc(out, func(a, b LeverResult) int {
		if c := cmp.Compare(b.User.TokenTurns, a.User.TokenTurns); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return cmp.Compare(a.Subject, b.Subject)
	})
	return out
}

func evalTool(sum *Summary, lv Lever) LeverResult {
	name := strings.TrimPrefix(lv.Target, "tool:")
	lr := LeverResult{Lever: lv, Subject: name, HasProject: true}
	calledIn := map[string]bool{}
	for _, s := range sum.Eligible {
		if slices.Contains(s.ToolNames, name) {
			lr.Usage.Carrying++
		}
		if ts, ok := s.Called[name]; ok {
			lr.Usage.Called++
			calledIn[s.CWD] = true
			if ts.After(lr.Usage.Last) {
				lr.Usage.Last = ts
			}
		}
	}
	lr.Usage.Projects = len(calledIn)
	for _, s := range sum.Decomposed {
		t, ok := s.Tokens(lv.Target)
		if !ok {
			continue
		}
		lr.User.add(s.CWD, t, s.N)
		if !calledIn[s.CWD] {
			lr.Project.add(s.CWD, t, s.N)
		}
	}
	lr.User.finish(sum.MainTokenTurns)
	lr.Project.finish(sum.MainTokenTurns)
	lr.Presence = presence(sum.Eligible, name)
	lr.Printable = lr.User.Share >= MinBenefit
	return lr
}

// presence finds the latest eligible main snapshot session carrying tool and counts the snapshot
// sessions that started after it.
func presence(eligible []*Session, tool string) *Presence {
	p := &Presence{}
	for _, s := range eligible {
		if s.HasSnapshot && slices.Contains(s.ToolNames, tool) && s.Start.After(p.LastSent) {
			p.LastSent, p.LastVersion = s.Start, s.Version
		}
	}
	projects := map[string]bool{}
	for _, s := range eligible {
		if s.HasSnapshot && s.Start.After(p.LastSent) {
			p.AbsentRecent++
			projects[s.CWD] = true
		}
	}
	p.AbsentProjects = len(projects)
	return p
}

func evalSkills(sum *Summary, lv Lever) LeverResult {
	lr := LeverResult{Lever: lv}
	chosen := map[string]bool{}
	for _, st := range sum.Skills.Skills { // never-used first, largest MedianBytes first
		if len(lr.Skills) == MaxSkillOverrides {
			break
		}
		if !st.Used && !st.Plugin && st.MedianBytes > 0 {
			lr.Skills = append(lr.Skills, st.Name)
			chosen[st.Name] = true
		}
	}
	key := string(CompSkillListing)
	for _, s := range sum.Decomposed {
		t, ok := s.Tokens(key)
		if !ok {
			continue
		}
		b, _ := s.Bytes(key)
		hidden := 0
		for name, lb := range s.Listed {
			if chosen[name] {
				hidden += lb
			}
		}
		if hidden > 0 {
			lr.User.add(s.CWD, t*float64(hidden)/float64(b), s.N)
		}
	}
	lr.User.finish(sum.MainTokenTurns)
	lr.Printable = len(lr.Skills) > 0 && lr.User.Share >= MinBenefit
	return lr
}

func evalInstructions(sum *Summary, lv Lever) []LeverResult {
	var out []LeverResult
	for _, in := range sum.Instructions {
		if in.MedianTokens < InstructionMinTokens {
			continue
		}
		key := string(CompInstruction) + ":" + in.Path
		lr := LeverResult{Lever: lv, Subject: in.Path, FileLabel: in.Label}
		var cwds []string
		for _, s := range sum.Decomposed {
			if t, ok := s.Tokens(key); ok {
				lr.User.add(s.CWD, t, s.N)
				cwds = append(cwds, s.CWD)
			}
		}
		lr.User.finish(sum.MainTokenTurns)
		lr.Action = instructionAction(in.Label, in.Path, cwds)
		if lr.Action == "exclude" || lr.Action == "prune" {
			// An instruction file is never "called", so no carrying project drops out: the project
			// scope of the Local snippet (one per project) is every carrying session.
			lr.HasProject, lr.Project = true, lr.User
		}
		lr.Snippets = nil
		for _, sn := range lv.Snippets {
			if lr.Action == "exclude" && strings.Contains(sn.Text, "claudeMdExcludes") ||
				lr.Action == "prune" && strings.Contains(sn.Text, "autoMemoryEnabled") {
				lr.Snippets = append(lr.Snippets, sn)
			}
		}
		lr.Printable = lr.Action != "none" && lr.User.Share >= MinBenefit
		out = append(out, lr)
	}
	return out
}

// instructionAction: User → "trim"; AutoMem → "prune"; Project (either label) → "exclude" when the
// file's directory is a strict ancestor of every carrying cwd, else "trim"; anything else → "none".
func instructionAction(label, path string, cwds []string) string {
	switch label {
	case LabelUser:
		return "trim"
	case LabelAutoMem:
		return "prune"
	case LabelProject, LabelProjectImport:
		dir := path[:max(strings.LastIndexAny(path, `/\`), 0)]
		if len(cwds) == 0 {
			return "trim"
		}
		for _, cwd := range cwds {
			if !strictAncestor(dir, cwd) {
				return "trim"
			}
		}
		return "exclude"
	}
	return "none"
}

// strictAncestor reports whether cwd lies strictly under dir (path-boundary match via
// transcript.UnderPath; dir itself does not count).
func strictAncestor(dir, cwd string) bool {
	return transcript.UnderPath(cwd, dir) && filepath.Clean(cwd) != filepath.Clean(dir)
}
