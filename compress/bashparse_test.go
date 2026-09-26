package compress

import (
	"reflect"
	"testing"
)

func TestParseBash(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want [][][]string
	}{
		{"and then pipe", `cd /x && cat "a b.txt" | head -5`,
			[][][]string{{{"cd", "/x"}}, {{"cat", "a b.txt"}, {"head", "-5"}}}},
		{"single quotes keep separators", `echo 'x;y' ; ls`,
			[][][]string{{{"echo", "x;y"}}, {{"ls"}}}},
		{"redirect stays a word", `FOO=1 go test ./... 2>&1 | tail -20`,
			[][][]string{{{"FOO=1", "go", "test", "./...", "2>&1"}, {"tail", "-20"}}}},
		{"heredoc body skipped", "cat <<'EOF'\nrm -rf /\nEOF\nls",
			[][][]string{{{"cat"}}, {{"ls"}}}},
		{"heredoc then pipe on same line", "cat <<EOF | grep x\nbody\nEOF",
			[][][]string{{{"cat"}, {"grep", "x"}}}},
		{"here-string is a word", `grep x <<< "$v"`,
			[][][]string{{{"grep", "x", "<<<", "$v"}}}},
		{"substitution opaque and comment dropped", `echo $(cat a | wc -l) # note`,
			[][][]string{{{"echo", "$(cat a | wc -l)"}}}},
		{"backticks opaque", "echo `cat a`",
			[][][]string{{{"echo", "`cat a`"}}}},
		{"or and background", `a || b & c`,
			[][][]string{{{"a"}}, {{"b"}}, {{"c"}}}},
		{"shell -c script is one word", `bash -c "rg x | head"`,
			[][][]string{{{"bash", "-c", "rg x | head"}}}},
		{"line continuation", "ls \\\n -la",
			[][][]string{{{"ls", "-la"}}}},
		{"escaped space", `cat my\ file`,
			[][][]string{{{"cat", "my file"}}}},
		{"empty quoted word kept", `sed -i '' s/a/b/ f`,
			[][][]string{{{"sed", "-i", "", "s/a/b/", "f"}}}},
		{"hash inside word is literal", `echo a#b`,
			[][][]string{{{"echo", "a#b"}}}},
		{"pipe-ampersand", `make |& tee log`,
			[][][]string{{{"make"}, {"tee", "log"}}}},
		{"empty input", ``, nil},
		{"subshell splits into pipelines", `(a; b)`,
			[][][]string{{{"a"}}, {{"b"}}}},
		{"braces are not split", `echo {a,b}`,
			[][][]string{{{"echo", "{a,b}"}}}},
		{"process substitution opaque", `diff <(sort a) b`,
			[][][]string{{{"diff", "<(sort a)", "b"}}}},
		{"leading semicolon produces no empty pipeline", `;ls`,
			[][][]string{{{"ls"}}}},
		{"leading double semicolon produces no empty pipeline", `;;ls`,
			[][][]string{{{"ls"}}}},
		{"leading and-then produces no empty pipeline", `&& ls`,
			[][][]string{{{"ls"}}}},
	}
	for _, tc := range tests {
		got, ok := parseBash(tc.src)
		if !ok {
			t.Errorf("%s: ok = false", tc.name)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseBashUnbalanced(t *testing.T) {
	for _, src := range []string{`echo "oops`, `echo 'oops`, "echo `oops", `echo $(oops`} {
		if _, ok := parseBash(src); ok {
			t.Errorf("parseBash(%q) ok = true, want false", src)
		}
	}
}
