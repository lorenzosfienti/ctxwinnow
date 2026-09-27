package overhead

import (
	"slices"
	"strings"
	"testing"
)

func TestRedactOff(t *testing.T) {
	r := NewRedactor(false)
	if r.On() {
		t.Fatal("On() = true for NewRedactor(false)")
	}
	for c := ClassTool; c <= ClassPath; c++ {
		for _, s := range []string{"zzsecret", "mcp__zzsrv__query", "/home/zzuser/CLAUDE.md", "zzplug:helper"} {
			if got := r.Name(c, s); got != s {
				t.Errorf("Name(%d, %q) = %q with redaction off, want it unchanged", c, s, got)
			}
		}
	}
}

// TestRedactLabels pins spec §12: allowlisted built-ins as is, per-run ordinal labels in call order,
// one label per namespace and string, shared mcp-N and file-N counters.
func TestRedactLabels(t *testing.T) {
	r := NewRedactor(true)
	if !r.On() {
		t.Fatal("On() = false for NewRedactor(true)")
	}
	for i, c := range []struct {
		class   Class
		in, out string
	}{
		{ClassTool, "Artifact", "Artifact"},
		{ClassTool, "zzcustom", "tool-1"},
		{ClassTool, "mcp__zzsrv__query", "mcp-1"},
		{ClassServer, "zzsrv", "mcp-2"},
		{ClassTool, "zzother", "tool-2"},
		{ClassTool, "zzcustom", "tool-1"},
		{ClassServer, "zzsrv", "mcp-2"},
		{ClassTool, "mcp__zzsrv__write", "mcp-3"},
		{ClassSkill, "code-review", "code-review"},
		{ClassSkill, "zzskill", "skill-1"},
		{ClassSkill, "zzplug:helper", "skill-2 (plugin)"},
		{ClassSkill, "code-review:extra", "skill-3 (plugin)"}, // namespaced: not the bundled skill
		{ClassSkill, "zzskill", "skill-1"},
		{ClassSkill, "zzcustom", "skill-4"}, // namespaces are separate: the tool zzcustom is tool-1
		{ClassFileUser, "/home/zzuser/.claude/CLAUDE.md", "file-1 (User)"},
		{ClassFileProject, "/home/zzuser/zzproj/CLAUDE.md", "file-2 (Project)"},
		{ClassFileAutoMem, "/home/zzuser/.claude/projects/-home-zzuser-zzproj/memory/MEMORY.md", "file-3 (AutoMem)"},
		{ClassFileOther, "/etc/claude-code/CLAUDE.md", "file-4 (other)"},
		{ClassFileOther, "/home/zzuser/zzproj/CLAUDE.md", "file-2 (Project)"}, // a path keeps its first label
		{ClassPath, "/home/zzuser/zzproj/CLAUDE.md", "<path of file-2>"},
		{ClassPath, "~/.claude/settings.json", "<path>"},
		{ClassPath, "/home/zzuser/unlabelled.md", "<path>"},
		{ClassProject, "/home/zzuser/zzproj", "project-A"},
		{ClassProject, "/home/zzuser/other", "project-B"},
		{ClassProject, "/home/zzuser/zzproj", "project-A"},
		{ClassTool, "", ""},
	} {
		if got := r.Name(c.class, c.in); got != c.out {
			t.Errorf("#%d Name(%d, %q) = %q, want %q", i, c.class, c.in, got, c.out)
		}
	}
}

// TestRedactFailSafe: anything not on the allowlists is redacted, and an unknown class never leaks.
func TestRedactFailSafe(t *testing.T) {
	r := NewRedactor(true)
	for i, c := range []struct {
		class   Class
		in, out string
	}{
		{ClassTool, "bash", "tool-1"},      // case matters
		{ClassTool, "Artifact ", "tool-2"}, // so does whitespace
		{ClassServer, "Bash", "mcp-1"},     // servers are never allowlisted
		{ClassSkill, "Read", "skill-1"},    // tool names are not skill names
		{Class(99), "zzsecret", "<redacted>"},
	} {
		if got := r.Name(c.class, c.in); got != c.out {
			t.Errorf("#%d Name(%d, %q) = %q, want %q", i, c.class, c.in, got, c.out)
		}
	}
}

func TestProjectLabels(t *testing.T) {
	for n, want := range map[int]string{1: "A", 2: "B", 26: "Z", 27: "AA", 28: "AB", 52: "AZ", 53: "BA", 702: "ZZ", 703: "AAA"} {
		if got := letters(n); got != want {
			t.Errorf("letters(%d) = %q, want %q", n, got, want)
		}
	}
	r := NewRedactor(true)
	var last string
	for i := range 28 {
		last = r.Name(ClassProject, "/w/p"+strings.Repeat("x", i))
	}
	if last != "project-AB" {
		t.Errorf("28th project = %q, want project-AB", last)
	}
}

func TestFileClass(t *testing.T) {
	for label, want := range map[string]Class{
		LabelUser: ClassFileUser, LabelProject: ClassFileProject, LabelProjectImport: ClassFileProject,
		LabelAutoMem: ClassFileAutoMem, LabelOther: ClassFileOther, "Managed": ClassFileOther,
	} {
		if got := FileClass(label); got != want {
			t.Errorf("FileClass(%q) = %d, want %d", label, got, want)
		}
	}
}

func TestAllowlists(t *testing.T) {
	for name, list := range map[string][]string{"BuiltinTools": BuiltinTools, "BundledSkills": BundledSkills} {
		if !slices.IsSorted(list) || len(slices.Compact(slices.Clone(list))) != len(list) {
			t.Errorf("%s must be sorted and unique: %v", name, list)
		}
	}
	for _, tool := range BuiltinTools {
		if strings.HasPrefix(tool, "mcp__") {
			t.Errorf("BuiltinTools contains the MCP tool %q", tool)
		}
	}
	for _, skill := range BundledSkills {
		if strings.Contains(skill, ":") {
			t.Errorf("BundledSkills contains the plugin skill %q", skill)
		}
	}
	// Every non-MCP tool seen in 2.1.283 snapshots and every catalog target is built in.
	for _, tool := range []string{"Artifact", "AskUserQuestion", "Bash", "Edit", "ListAgents", "Read", "ReportFindings",
		"ScheduleWakeup", "SendFeedback", "Skill", "StructuredOutput", "ToolSearch", "Workflow", "Write", "CronCreate"} {
		if !slices.Contains(BuiltinTools, tool) {
			t.Errorf("BuiltinTools lacks %q", tool)
		}
	}
	if len(BuiltinTools) != 48 || len(BundledSkills) != 12 {
		t.Errorf("len(BuiltinTools) = %d, len(BundledSkills) = %d; want 48 and 12", len(BuiltinTools), len(BundledSkills))
	}
}
