package transcript

import (
	"bytes"
	"os"
	"testing"
)

// FuzzParse checks that Parse never panics and keeps its size-only records consistent on arbitrary
// input. `go test` runs the seeds; `go test -run '^$' -fuzz FuzzParse -fuzztime 30s` explores.
func FuzzParse(f *testing.F) {
	for _, name := range []string{"testdata/session.jsonl", "testdata/overhead_main.jsonl"} {
		data, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, seed := range []string{
		`{"type":"attachment","attachment":{"type":"skill_listing","isInitial":true,"names":["a","a:b"],"content":"- a:b: x\n- a"}}`,
		`{"type":"attachment","attachment":{"type":"prompt_snapshot","systemPrompt":[1],"tools":[{"name":"X"}]}}`,
		`{"type":"attachment","attachment":{"type":"instructions","files":[{"path":1}]}}`,
		`{"type":"attachment","attachment":null}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":"x"}],"usage":null}}`,
		`{"type":"user","message":{"content":"<command-name>"}}`,
		`{"type":"user","message":{"content":"<command-name></command-name><command-name>/"}}`,
		"\n\n{}\n[]\nnull\n\"x\"\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Parse(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Parse returned an error on in-memory input: %v", err)
		}
		for _, a := range s.Pre {
			if a.Bytes < 0 {
				t.Fatalf("negative bytes in %+v", a)
			}
			if a.Type == AttSkillListing {
				sum := 0
				for _, l := range a.Lines {
					sum += l.Bytes
				}
				if sum+len(a.Lines)-1 != a.Bytes {
					t.Fatalf("skill lines %+v do not add up to %d bytes", a.Lines, a.Bytes)
				}
			}
		}
		for i := 1; i < len(s.ToolCalls); i++ {
			if s.ToolCalls[i-1].Name >= s.ToolCalls[i].Name {
				t.Fatalf("ToolCalls not sorted unique: %+v", s.ToolCalls)
			}
		}
	})
}
