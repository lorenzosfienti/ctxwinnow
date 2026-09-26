package ceiling

import "testing"

func TestLosslessFold(t *testing.T) {
	tests := []struct {
		name string
		cat  Category
		in   string
		want string
	}{
		{"ansi csi", CatLines, "\x1b[31mERROR\x1b[0m boom", "ERROR boom"},
		{"ansi osc", CatLines, "\x1b]0;title\x07text", "text"},
		{"progress carriage returns", CatLines, "10%\r50%\r100%", "100%"},
		{"crlf keeps lines", CatLines, "a\r\nb\r\n", "a\nb\n"},
		{"duplicate run", CatLines, "x\nx\nx\ny", "x ×3\ny"},
		{"blank run", CatLines, "a\n\n \n\t\nb", "a\n\nb"},
		{"single lines untouched", CatLines, "a\nb", "a\nb"},
		{"json pretty", CatJSON, "{\n  \"a\": 1\n}", `{"a":1}`},
		{"ndjson", CatJSON, "{ \"a\": 1 }\n{ \"b\": 2 }", "{\"a\":1}\n{\"b\":2}"},
		{"invalid json unchanged", CatJSON, "{ nope", "{ nope"},
	}
	for _, tc := range tests {
		if got := losslessFold(tc.cat, tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLosslessSaved(t *testing.T) {
	if got := LosslessSaved(CatLines, "a\na"); got != 0 {
		t.Errorf("growth must clamp to 0, got %d", got)
	}
	in := repeatLine("ok line", 1000) // 8000 bytes -> 2000 tokens; folds to "ok line ×1000\n" (15 bytes -> 4)
	if got := LosslessSaved(CatLines, in); got != 1996 {
		t.Errorf("LosslessSaved = %d, want 1996", got)
	}
}
