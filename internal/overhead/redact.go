package overhead

import (
	"fmt"
	"slices"
	"strings"
)

// Class tells Redactor.Name how to treat a string.
type Class int

const (
	ClassTool        Class = iota // BuiltinTools → as is; "mcp__*" → mcp-N; other → tool-N
	ClassServer                   // MCP server name → mcp-N (same counter as MCP tools)
	ClassSkill                    // BundledSkills → as is; contains ":" → "skill-N (plugin)"; other → skill-N
	ClassFileUser                 // instruction path → "file-N (User)"
	ClassFileProject              // → "file-N (Project)"
	ClassFileAutoMem              // → "file-N (AutoMem)"
	ClassFileOther                // → "file-N (other)"
	ClassProject                  // cwd or --only/--exclude prefix → project-A … project-Z, project-AA, …
	ClassPath                     // path inside lever text: an already labelled file → "<path of file-N>"; else "<path>"
)

// BuiltinTools are Claude Code tool names shown as is under --redact (sorted). Anything else is
// redacted, so an outdated list fails safe.
var BuiltinTools = []string{
	"Agent", "Artifact", "ArtifactComments", "ArtifactData", "AskUserQuestion", "Bash", "CronCreate",
	"CronDelete", "CronList", "DesignSync", "Edit", "EndConversation", "EnterPlanMode", "EnterWorktree", "ExitPlanMode",
	"ExitWorktree", "Glob", "Grep", "ListAgents", "ListMcpResourcesTool", "Monitor", "MultiEdit", "NotebookEdit",
	"PushNotification", "Read", "ReadMcpResourceDirTool", "ReadMcpResourceTool", "RemoteTrigger", "ReportFindings",
	"ScheduleWakeup", "SendFeedback", "SendMessage", "SendUserFile", "Skill", "StructuredOutput", "Task", "TaskCreate",
	"TaskGet", "TaskList", "TaskOutput", "TaskStop", "TaskUpdate", "TodoWrite", "ToolSearch", "WebFetch", "WebSearch",
	"Workflow", "Write",
}

// BundledSkills are skills bundled with Claude Code, shown as is under --redact (sorted).
var BundledSkills = []string{
	"claude-api", "code-review", "fewer-permission-prompts", "init", "keybindings-help", "loop", "run",
	"schedule", "security-review", "simplify", "update-config", "workflow-authoring",
}

// fileSuffix is the type shown in a file label.
var fileSuffix = map[Class]string{ClassFileUser: LabelUser, ClassFileProject: LabelProject,
	ClassFileAutoMem: LabelAutoMem, ClassFileOther: LabelOther}

// Redactor implements spec §12. Labels are per run, assigned in call order, and stable: the same
// namespace and string always get the same label. With redaction off, Name returns s unchanged.
type Redactor struct {
	on     bool
	labels map[string]string // namespace + "\x00" + s → label
	next   map[string]int    // counter → last number used ("tool", "mcp", "skill", "file", "project")
}

// NewRedactor returns a Redactor; on is the --redact flag.
func NewRedactor(on bool) *Redactor {
	return &Redactor{on: on, labels: map[string]string{}, next: map[string]int{}}
}

// On reports whether redaction is enabled.
func (r *Redactor) On() bool { return r.on }

// Name is the single redaction entry point: every name or path the report or a snippet prints
// goes through it. File classes share one file-N counter keyed by path, and a path keeps the label
// of its first call. An empty string is returned as is; an unknown class gives "<redacted>".
func (r *Redactor) Name(c Class, s string) string {
	if !r.on || s == "" {
		return s
	}
	switch c {
	case ClassTool:
		if slices.Contains(BuiltinTools, s) {
			return s
		}
		if strings.HasPrefix(s, "mcp__") {
			return r.label("mcp-tool", "mcp", s, func(n int) string { return fmt.Sprintf("mcp-%d", n) })
		}
		return r.label("tool", "tool", s, func(n int) string { return fmt.Sprintf("tool-%d", n) })
	case ClassServer:
		return r.label("server", "mcp", s, func(n int) string { return fmt.Sprintf("mcp-%d", n) })
	case ClassSkill:
		if slices.Contains(BundledSkills, s) {
			return s
		}
		if strings.Contains(s, ":") {
			return r.label("skill", "skill", s, func(n int) string { return fmt.Sprintf("skill-%d (plugin)", n) })
		}
		return r.label("skill", "skill", s, func(n int) string { return fmt.Sprintf("skill-%d", n) })
	case ClassFileUser, ClassFileProject, ClassFileAutoMem, ClassFileOther:
		return r.label("file", "file", s, func(n int) string { return fmt.Sprintf("file-%d (%s)", n, fileSuffix[c]) })
	case ClassProject:
		return r.label("project", "project", s, func(n int) string { return "project-" + letters(n) })
	case ClassPath:
		if l, ok := r.labels["file\x00"+s]; ok {
			file, _, _ := strings.Cut(l, " (")
			return "<path of " + file + ">"
		}
		return "<path>"
	}
	return "<redacted>"
}

// label returns the label of s in namespace ns, assigning the next number of counter on first use.
func (r *Redactor) label(ns, counter, s string, format func(n int) string) string {
	key := ns + "\x00" + s
	if l, ok := r.labels[key]; ok {
		return l
	}
	r.next[counter]++
	l := format(r.next[counter])
	r.labels[key] = l
	return l
}

// letters is the bijective base-26 ordinal: 1 → A, 26 → Z, 27 → AA.
func letters(n int) string {
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

// FileClass maps an instruction Component.Label to its Class (LabelProjectImport → ClassFileProject).
func FileClass(label string) Class {
	switch label {
	case LabelUser:
		return ClassFileUser
	case LabelProject, LabelProjectImport:
		return ClassFileProject
	case LabelAutoMem:
		return ClassFileAutoMem
	}
	return ClassFileOther
}
