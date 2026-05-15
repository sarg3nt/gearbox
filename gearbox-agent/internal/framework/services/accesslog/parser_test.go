package accesslog

import (
	"strings"
	"testing"
)

func TestAllProfilesReturnDistinctNames(t *testing.T) {
	// Guards against accidental copy-paste duplicates when adding a
	// new profile: every Profile() must be unique because the source-
	// dispatch lookup is string-keyed.
	seen := map[string]bool{}
	for _, p := range AllProfiles() {
		name := p.Profile()
		if name == "" {
			t.Errorf("%T returned empty Profile()", p)
		}
		if seen[name] {
			t.Errorf("duplicate Profile() %q across parsers", name)
		}
		seen[name] = true
	}
}

func TestProfileByName(t *testing.T) {
	// Confirm dispatch works for every profile, is case-insensitive
	// (operators may pass "NGINX" via URL), and rejects unknowns.
	for _, p := range AllProfiles() {
		if got := ProfileByName(p.Profile()); got == nil {
			t.Errorf("ProfileByName(%q) returned nil", p.Profile())
		}
		if got := ProfileByName(strings.ToUpper(p.Profile())); got == nil {
			t.Errorf("ProfileByName(%q) (upper) returned nil — should be case-insensitive", p.Profile())
		}
	}
	if ProfileByName("not-a-real-profile") != nil {
		t.Error("ProfileByName should return nil for unknown profile names")
	}
}

func TestTrimRawCapsLongLines(t *testing.T) {
	short := strings.Repeat("x", 100)
	if got := trimRaw(short); got != short {
		t.Errorf("trimRaw(short) should pass-through unchanged")
	}

	long := strings.Repeat("y", RawMaxLen+500)
	got := trimRaw(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("trimRaw(long) should append ellipsis")
	}
	// Length in bytes is RawMaxLen + len("…") (UTF-8 ellipsis is 3
	// bytes). Check by string-length equality of the head.
	if !strings.HasPrefix(got, strings.Repeat("y", RawMaxLen)) {
		t.Errorf("trimRaw(long) should keep first %d chars", RawMaxLen)
	}
}

func TestValidStatusCode(t *testing.T) {
	for _, ok := range []int{100, 200, 304, 404, 500, 599} {
		if !validStatusCode(ok) {
			t.Errorf("validStatusCode(%d) = false, want true", ok)
		}
	}
	for _, bad := range []int{0, 99, 600, 999, -1} {
		if validStatusCode(bad) {
			t.Errorf("validStatusCode(%d) = true, want false", bad)
		}
	}
}
