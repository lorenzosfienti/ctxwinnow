package overhead

import (
	"strings"
	"testing"
)

// TestLeak renders the canonical session, whose every content string carries a LEAKMARK- marker,
// with its levers, and checks that no content and, with --redact, no user name or path reaches the
// report — including the generated skillOverrides snippet.
func TestLeak(t *testing.T) {
	for _, redact := range []bool{false, true} {
		sum := Summarize(Build([]*Session{loadCanonical(t, "-home-zzuser-zzproj/zz-session-1.jsonl")}), Options{MinTurns: 1})
		out := render(t, &Report{Version: "test", Redact: redact, Summary: sum, Levers: Evaluate(sum)})
		if !strings.Contains(out, "## Instruction files") {
			t.Fatalf("redact=%v: the canonical session was not reported:\n%s", redact, out)
		}
		overrides := `"skillOverrides": {"zzidle": "user-invocable-only"}`
		if redact {
			overrides = `"skillOverrides": {"skill-`
		}
		if !strings.Contains(out, overrides) {
			t.Errorf("redact=%v: the skill-visibility snippet %q is missing:\n%s", redact, overrides, out)
		}
		forbidden := []string{"LEAKMARK", "zz@example.invalid"}
		if redact {
			forbidden = append(forbidden, "zzsrv", "zzskill", "zzidle", "zzplug", "zzbare", "/home/zzuser", "zzproj", "MEMORY.md", "zzuser", "~/.claude")
		}
		for _, bad := range forbidden {
			if strings.Contains(out, bad) {
				t.Errorf("redact=%v: report contains %q:\n%s", redact, bad, out)
			}
		}
	}
}
