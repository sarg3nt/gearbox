package handler

import "testing"

// 2026-05 audit P0-4. The login and logout handlers both consult
// isSafeReturnURL to decide whether a ?return=... query value can be
// used as a redirect target. These tests pin down the matrix so future
// edits don't accidentally widen what the helper accepts.
func TestIsSafeReturnURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Safe: same-origin relative paths.
		{"plain-root", "/", true},
		{"plain-path", "/dashboard", true},
		{"nested-path", "/settings/profile", true},
		{"with-query", "/search?q=hello", true},
		{"with-fragment", "/page#section", true},

		// Unsafe: anything that could escape origin.
		{"empty", "", false},
		{"absolute-https", "https://evil.example.com/x", false},
		{"absolute-http", "http://evil.example.com/x", false},
		{"protocol-relative", "//evil.example.com/x", false},
		{"backslash-relative", "/\\evil.example.com", false},
		{"backslash-prefix", "\\evil.example.com", false},
		{"javascript-uri", "javascript:alert(1)", false},
		{"data-uri", "data:text/html,evil", false},
		{"no-leading-slash", "dashboard", false},
		{"backslash-anywhere", "/legit\\path", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafeReturnURL(tt.input); got != tt.want {
				t.Errorf("isSafeReturnURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
