package overhead

import (
	"strings"
	"testing"
)

// TestLeak renders the canonical session, whose every content string carries a LEAKMARK- marker,
// and checks that no content and, with --redact, no user name or path reaches the report.
func TestLeak(t *testing.T) {
	for _, redact := range []bool{false, true} {
		sum := Summarize(Build([]*Session{loadCanonical(t, "-home-zzuser-zzproj/zz-session-1.jsonl")}), Options{MinTurns: 1})
		out := render(t, &Report{Version: "test", Redact: redact, Summary: sum})
		if !strings.Contains(out, "## Instruction files") {
			t.Fatalf("redact=%v: the canonical session was not reported:\n%s", redact, out)
		}
		forbidden := []string{"LEAKMARK", "zz@example.invalid"}
		if redact {
			forbidden = append(forbidden, "zzsrv", "zzskill", "zzidle", "zzplug", "zzbare", "/home/zzuser", "zzproj", "MEMORY.md", "zzuser")
		}
		for _, bad := range forbidden {
			if strings.Contains(out, bad) {
				t.Errorf("redact=%v: report contains %q:\n%s", redact, bad, out)
			}
		}
	}
}
