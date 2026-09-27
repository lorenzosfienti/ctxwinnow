package transcript

import (
	"os"
	"path/filepath"
	"strings"
)

// UnderPath reports whether path equals prefix or lies under it at a separator boundary. Both are
// passed through filepath.Clean first (so a trailing separator on prefix is ignored and "/" matches
// every absolute path); comparison is byte-exact. An empty path or prefix never matches.
//
//	UnderPath("/Users/a/app/x", "/Users/a/app")      == true
//	UnderPath("/Users/a/app", "/Users/a/app/")       == true
//	UnderPath("/Users/a/app-legacy", "/Users/a/app") == false
//	UnderPath("/x", "/")                             == true
//	UnderPath("", "/")                               == false
func UnderPath(path, prefix string) bool {
	if path == "" || prefix == "" {
		return false
	}
	path, prefix = filepath.Clean(path), filepath.Clean(prefix)
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// prefix is a root ("/", `C:\`) when Clean leaves a trailing separator on it.
	return os.IsPathSeparator(prefix[len(prefix)-1]) || os.IsPathSeparator(path[len(prefix)])
}

// UnderAny reports whether UnderPath(path, p) holds for some p in prefixes.
func UnderAny(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if UnderPath(path, p) {
			return true
		}
	}
	return false
}
