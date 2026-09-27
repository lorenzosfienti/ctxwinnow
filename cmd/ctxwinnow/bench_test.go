package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	benchFiles = 32  // synthetic transcripts in the benchmark corpus
	benchTurns = 120 // tool round trips appended to each (≈2 MB per file, like the owner's average)
)

// benchCorpus writes benchFiles synthetic transcripts under dir: each is the canonical session (its
// message ids made unique per file) followed by benchTurns Read calls whose results carry 16 KiB of
// synthetic text. It returns the total size in bytes.
func benchCorpus(b *testing.B, dir string) int64 {
	b.Helper()
	head, err := os.ReadFile(canonical)
	if err != nil {
		b.Fatal(err)
	}
	payload, err := json.Marshal(strings.Repeat("zz synthetic tool output line for the benchmark\n", 340))
	if err != nil {
		b.Fatal(err)
	}
	var total int64
	for f := range benchFiles {
		var sb strings.Builder
		sb.WriteString(strings.ReplaceAll(string(head), `"msg_zz`, fmt.Sprintf(`"msg_zz%02d_`, f)))
		for i := range benchTurns {
			ts := fmt.Sprintf("2026-09-20T1%d:%02d:%02d.000Z", i/3600, i/60%60, i%60)
			fmt.Fprintf(&sb, `{"type":"assistant","timestamp":%q,"cwd":"/home/zzuser/zzproj","version":"2.1.283","entrypoint":"cli","message":{"id":"msg_bench_%d_%d","type":"message","role":"assistant","model":"zz-model","content":[{"type":"tool_use","id":"toolu_bench_%d_%d","name":"Read","input":{"file_path":"/home/zzuser/zzproj/zz.go"}}],"usage":{"input_tokens":3,"cache_read_input_tokens":%d,"cache_creation_input_tokens":4100,"output_tokens":20}}}`+"\n",
				ts, f, i, f, i, 1200+4100*i)
			fmt.Fprintf(&sb, `{"type":"user","timestamp":%q,"cwd":"/home/zzuser/zzproj","version":"2.1.283","entrypoint":"cli","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bench_%d_%d","content":%s}]}}`+"\n",
				ts, f, i, payload)
		}
		path := filepath.Join(dir, "-home-zzuser-zzproj", fmt.Sprintf("zz-bench-%02d.jsonl", f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			b.Fatal(err)
		}
		total += int64(sb.Len())
	}
	return total
}

// BenchmarkOverhead measures `ctxwinnow overhead` end to end (walk, parse, inventory, aggregation,
// levers, render) on the synthetic corpus. Spec §11 targets < 10 s for 1 GB, i.e. at least
// ≈108 MB/s; the target is not a gate, so the benchmark only reports MB/s.
func BenchmarkOverhead(b *testing.B) {
	root := filepath.Join(b.TempDir(), "projects")
	b.SetBytes(benchCorpus(b, root))
	for b.Loop() {
		if code, _, errOut := runArgs("overhead", "--root", root); code != 0 || errOut != "" {
			b.Fatalf("code=%d stderr=%q", code, errOut)
		}
	}
}
