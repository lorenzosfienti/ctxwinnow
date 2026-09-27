package transcript

import "testing"

func TestUnderPath(t *testing.T) {
	tests := []struct {
		path, prefix string
		want         bool
	}{
		{"/Users/a/app", "/Users/a/app", true},
		{"/Users/a/app/x", "/Users/a/app", true},
		{"/Users/a/app/x/y", "/Users/a/app", true},
		{"/Users/a/app", "/Users/a/app/", true},
		{"/Users/a/app/", "/Users/a/app", true},
		{"/Users/a/app/x", "/Users/a/app/", true},
		{"/Users/a/app-legacy", "/Users/a/app", false},
		{"/Users/a/application", "/Users/a/app", false},
		{"/Users/a", "/Users/a/app", false},
		{"/x", "/", true},
		{"/", "/", true},
		{"", "/", false},
		{"/x", "", false},
		{"", "", false},
		{"/Users/a/./app/x", "/Users/a/app", true},
	}
	for _, tc := range tests {
		if got := UnderPath(tc.path, tc.prefix); got != tc.want {
			t.Errorf("UnderPath(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.want)
		}
	}
}

func TestUnderAny(t *testing.T) {
	prefixes := []string{"/w/app", "/w/lib/"}
	tests := []struct {
		path string
		want bool
	}{
		{"/w/app/x", true},
		{"/w/lib", true},
		{"/w/app-legacy", false},
		{"/w/other", false},
	}
	for _, tc := range tests {
		if got := UnderAny(tc.path, prefixes); got != tc.want {
			t.Errorf("UnderAny(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
	if UnderAny("/w/app", nil) {
		t.Error("UnderAny with no prefixes must be false")
	}
}
