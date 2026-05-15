package probe

import (
	"context"
	"strings"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		name, url string
		want      bool
	}{
		{"plain 127.0.0.1", "http://127.0.0.1/x", true},
		{"127.x.y.z still loopback (whole 127/8 is loopback)", "http://127.5.6.7/x", true},
		{"IPv6 ::1 in brackets", "http://[::1]/x", true},
		{"localhost hostname", "http://localhost/x", true},
		{"localhost with port", "http://localhost:8080/x", true},
		{"non-loopback IP", "http://10.0.0.1/x", false},
		{"non-loopback hostname", "http://example.com/x", false},
		// Regression case for the Copilot review: substring matching
		// would have flagged this as loopback because "://localhost"
		// appears in the userinfo portion. Proper URL parsing strips
		// userinfo and leaves only the host (evil.com), which is not
		// loopback.
		{"userinfo spoof — host is NOT loopback", "https://localhost@evil.com/x", false},
		{"empty URL", "", false},
		{"unparseable", "://not a url", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLoopback(tc.url); got != tc.want {
				t.Errorf("isLoopback(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestHTTPGetRejectsNonPositiveMaxBody(t *testing.T) {
	// Defensive guard documented in the godoc: passing 0/negative
	// would silently return an empty body which can hide sentinel
	// mismatches (look like a 200 with empty body and confuse the
	// caller). Surface the misuse as an explicit error.
	for _, max := range []int64{0, -1, -1024} {
		_, err := HTTPGet(context.Background(), "http://127.0.0.1/", max)
		if err == nil {
			t.Errorf("HTTPGet(maxBody=%d) returned no error; expected validation failure", max)
			continue
		}
		if !strings.Contains(err.Error(), "maxBody must be positive") {
			t.Errorf("HTTPGet(maxBody=%d) error = %q, want it to name the validation failure", max, err)
		}
	}
}
