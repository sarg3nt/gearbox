package updates

import (
	"strings"
	"testing"
)

// 2026-05 audit P2-9: isValidPackageName must reject any name a shell user
// would never plausibly type AND any name that apt-get would interpret as
// a flag. The handler-level boundary validation now relies on this.
func TestIsValidPackageName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Accepted: real Debian-style package names.
		{"nginx", true},
		{"libssl3", true},
		{"python3.11", true},
		{"linux-headers-6.1.0-12-amd64", true},
		{"libstdc++6", true},
		{"foo+bar", true},
		{"a", true},

		// Rejected: empty / length / character set.
		{"", false},
		{strings.Repeat("a", 257), false},
		{"name with space", false},
		{"name;rm /etc/passwd", false},
		{"name$(whoami)", false},
		{"name\nwith-newline", false},
		{"name/with/slash", false},

		// Rejected: would land as a flag if passed to apt-get install.
		// This is the actual P2-9 attack class.
		{"--allow-downgrades", false},
		{"--reinstall", false},
		{"-y", false},
		{"--help", false},

		// Rejected: leading dot would resolve relative paths in some
		// apt internals.
		{".bashrc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidPackageName(tt.name); got != tt.want {
				t.Errorf("isValidPackageName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
