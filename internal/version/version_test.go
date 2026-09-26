package version

import (
	"os"
	"regexp"
	"testing"
)

// TestVersionMatchesChangelog keeps Version and the top released CHANGELOG entry in sync.
func TestVersionMatchesChangelog(t *testing.T) {
	data, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\] - \d{4}-\d{2}-\d{2}$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("no released version heading in CHANGELOG.md")
	}
	if got := string(m[1]); got != Version {
		t.Fatalf("CHANGELOG top release %s != version.Version %s", got, Version)
	}
}
