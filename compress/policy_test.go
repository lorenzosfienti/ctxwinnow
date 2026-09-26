package compress

import (
	"encoding/json"
	"testing"
)

func bashInput(cmd string) string {
	b, _ := json.Marshal(map[string]string{"command": cmd, "description": "d"})
	return string(b)
}

func TestPolicy(t *testing.T) {
	tests := []struct {
		name, tool, input string
		isErr             bool
		want              Verdict
	}{
		{"read tool", "Read", `{"file_path":"/a"}`, false, PassTool},
		{"grep tool", "Grep", `{}`, false, PassTool},
		{"glob tool", "Glob", `{}`, false, PassTool},
		{"edit tool", "Edit", `{}`, false, PassTool},
		{"webfetch", "WebFetch", `{}`, false, PassTool},
		{"task", "Task", `{}`, false, PassTool},
		{"unmatched tool", "?", ``, false, PassTool},
		{"mcp", "mcp__github__list_issues", `{}`, false, Compress},
		{"mcp error", "mcp__github__list_issues", `{}`, true, PassError},
		{"bash error", "Bash", bashInput("go test ./..."), true, PassError},
		{"bash without command", "Bash", `{"description":"x"}`, false, PassUnparseable},
		{"bash command not a string", "Bash", `{"command":["ls"]}`, false, PassUnparseable},
		{"bash invalid json", "Bash", `{`, false, PassUnparseable},
		{"go test", "Bash", bashInput("go test ./..."), false, Compress},
		{"build piped to tail", "Bash", bashInput("npm run build 2>&1 | tail -50"), false, Compress},
		{"git status", "Bash", bashInput("git status"), false, Compress},
		{"git log", "Bash", bashInput("git log --oneline -20"), false, Compress},
		{"docker logs", "Bash", bashInput("docker compose logs --tail 200 web"), false, Compress},
		{"jq on stdin", "Bash", bashInput("curl -s https://x | jq ."), false, Compress},
		{"sed in place", "Bash", bashInput("sed -i '' 's/a/b/' main.go"), false, Compress},
		{"sed --in-place", "Bash", bashInput("sed --in-place=.bak 's/a/b/' f"), false, Compress},
		{"echo quoted pipe", "Bash", bashInput(`echo "a | cat"`), false, Compress},
		{"substitution is opaque", "Bash", bashInput("echo $(cat f)"), false, Compress},
		{"heredoc body not parsed", "Bash", bashInput("python3 - <<'EOF'\nprint(open('f').read())\ncat x\nEOF"), false, Compress},
		{"comment only", "Bash", bashInput("# nothing"), false, Compress},
		{"cat", "Bash", bashInput("cat main.go"), false, PassBashRead},
		{"cd then cat", "Bash", bashInput("cd /repo && cat main.go"), false, PassBashRead},
		{"sed -n", "Bash", bashInput("sed -n '10,40p' main.go"), false, PassBashRead},
		{"head", "Bash", bashInput("head -100 log.txt"), false, PassBashRead},
		{"env and sudo wrappers", "Bash", bashInput("FOO=1 sudo -u app tail -n 50 /var/log/app.log"), false, PassBashRead},
		{"nl", "Bash", bashInput("nl -ba f.go"), false, PassBashRead},
		{"awk", "Bash", bashInput("awk 'NR>5' f"), false, PassBashRead},
		{"jq on a file", "Bash", bashInput("jq '.dependencies' package.json"), false, PassBashRead},
		{"git diff", "Bash", bashInput("git diff HEAD~1"), false, PassBashRead},
		{"git -C show", "Bash", bashInput("git -C /repo show abc:main.go"), false, PassBashRead},
		{"timeout wrapper", "Bash", bashInput("timeout 30 cat big.log"), false, PassBashRead},
		{"cat inside bash -lc", "Bash", bashInput(`bash -lc 'cat f'`), false, PassBashRead},
		{"heredoc cat", "Bash", bashInput("cat <<EOF\nhello\nEOF"), false, PassBashRead},
		{"absolute program path", "Bash", bashInput("/usr/bin/cat f"), false, PassBashRead},
		{"read anywhere wins", "Bash", bashInput("go build ./... ; cat out.txt"), false, PassBashRead},
		{"read beats search", "Bash", bashInput("ls; cat f"), false, PassBashRead},
		{"grep", "Bash", bashInput("grep -rn TODO src/"), false, PassBashSearch},
		{"git grep", "Bash", bashInput("git grep -n Foo"), false, PassBashSearch},
		{"rg inside bash -c", "Bash", bashInput(`bash -c "rg Foo | head"`), false, PassBashSearch},
		{"find", "Bash", bashInput("find . -name '*.go'"), false, PassBashSearch},
		{"ls", "Bash", bashInput("ls -la"), false, PassBashSearch},
		{"xargs grep", "Bash", bashInput("xargs -0 grep -l foo"), false, PassBashSearch},
		{"search beats compress", "Bash", bashInput("make test && ls dist"), false, PassBashSearch},
		{"unbalanced quote", "Bash", bashInput(`echo "oops`), false, PassUnparseable},
		{"for-loop cat", "Bash", bashInput(`for f in a b; do cat "$f"; done`), false, PassBashRead},
		{"while-read cat", "Bash", bashInput(`while read f; do cat "$f"; done`), false, PassBashRead},
		{"if-then cat", "Bash", bashInput(`if grep -q x f; then cat y; fi`), false, PassBashRead},
		{"subshell cd and read", "Bash", bashInput(`(cd /repo && git diff)`), false, PassBashRead},
		{"subshell cd and search", "Bash", bashInput(`(cd x && ls)`), false, PassBashSearch},
		{"brace group cat", "Bash", bashInput(`{ cat a; }`), false, PassBashRead},
		{"for-loop wc is not a read verb", "Bash", bashInput(`for f in *.log; do wc -l "$f"; done`), false, Compress},
		{"process substitution opaque", "Bash", bashInput(`diff <(sort a) <(sort b)`), false, Compress},
		{"bash -c subshell cat", "Bash", bashInput(`bash -c '(cat f)'`), false, PassBashRead},
		{"brace word not split", "Bash", bashInput(`echo {a,b}`), false, Compress},
	}
	for _, tc := range tests {
		got, note := Policy(tc.tool, tc.input, tc.isErr)
		if got != tc.want {
			t.Errorf("%s: Policy = %v (%q), want %v", tc.name, got, note, tc.want)
		}
		if note == "" {
			t.Errorf("%s: empty note", tc.name)
		}
	}
}

func TestBashHead(t *testing.T) {
	tests := map[string]string{
		bashInput("cd /x && go test ./..."):                "go",
		bashInput("FOO=1 timeout 60 npm test"):             "npm",
		bashInput(`bash -c "cd a && pytest -q"`):           "pytest",
		bashInput("export A=1; source env.sh; make build"): "make",
		bashInput(`echo "oops`):                            "",
		`{`:                                                "",
		bashInput("for f in a; do go test; done"):          "go",
	}
	for in, want := range tests {
		if got := BashHead(in); got != want {
			t.Errorf("BashHead(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestVerdictString(t *testing.T) {
	if PassBashRead.String() != "PassBashRead" || Verdict(99).String() != "Verdict(?)" {
		t.Fatal("unexpected Verdict names")
	}
}
