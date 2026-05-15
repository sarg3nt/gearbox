package accesslog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/accesslog"
)

// fakeFileInfo simulates a regular file or a directory for the
// readability check.
type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string         { return f.name }
func (fakeFileInfo) Size() int64            { return 0 }
func (f fakeFileInfo) Mode() os.FileMode    { return f.mode }
func (fakeFileInfo) ModTime() (t time.Time) { return }
func (f fakeFileInfo) IsDir() bool          { return f.mode.IsDir() }
func (fakeFileInfo) Sys() any               { return nil }

func statExisting(paths ...string) func(string) (os.FileInfo, error) {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}
	return func(p string) (os.FileInfo, error) {
		if _, ok := set[p]; ok {
			return fakeFileInfo{name: p, mode: 0o644}, nil // regular file
		}
		return nil, os.ErrNotExist
	}
}

// staticTail returns a fixed slice of raw log lines regardless of
// path/lines arguments. Used to feed deterministic content into the
// handler so the test can assert on parsing + filtering behaviour.
func staticTail(lines []string) func(ctx context.Context, path string, lines int) ([]string, error) {
	return func(context.Context, string, int) ([]string, error) {
		// Return a copy so the test can't accidentally mutate the
		// canned content through the slice header.
		out := make([]string, len(lines))
		copy(out, lines)
		return out, nil
	}
}

func newTestGear() *Gear {
	g := New()
	g.stat = statExisting() // nothing readable by default
	return g
}

func TestProbeAlwaysAvailable(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Errorf("status = %v, want Available", res.Status)
	}
}

func TestProbeRecordsReadablePathsInCapabilities(t *testing.T) {
	// Apache lives at the Debian-style path; nginx at its default;
	// haproxy and caddy paths don't exist on this host.
	g := newTestGear()
	g.stat = statExisting("/var/log/nginx/access.log", "/var/log/apache2/access.log")

	res := g.Probe(context.Background(), gear.Dependencies{})
	want := map[string]string{
		"nginx_log":  "/var/log/nginx/access.log",
		"apache_log": "/var/log/apache2/access.log",
	}
	for k, v := range want {
		if res.Capabilities[k] != v {
			t.Errorf("capabilities[%q] = %q, want %q", k, res.Capabilities[k], v)
		}
	}
	if _, ok := res.Capabilities["haproxy_log"]; ok {
		t.Errorf("haproxy_log should be absent — file not readable, but capability appeared")
	}
}

func TestProbeFallsBackToRHELApachePath(t *testing.T) {
	// Only the RHEL-style path is readable; the Debian default isn't.
	g := newTestGear()
	g.stat = statExisting("/var/log/httpd/access_log")

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Capabilities["apache_log"] != "/var/log/httpd/access_log" {
		t.Errorf("apache_log = %q, want RHEL fallback path", res.Capabilities["apache_log"])
	}
}

func TestProbeHonorsOverridePath(t *testing.T) {
	// Operator pointed at a non-standard nginx path — Probe must
	// report that path in capabilities even when our well-known
	// default isn't readable.
	g := newTestGear()
	deps := gear.Dependencies{NginxAccessLog: "/custom/nginx.log"}

	res := g.Probe(context.Background(), deps)
	if res.Capabilities["nginx_log"] != "/custom/nginx.log" {
		t.Errorf("nginx_log = %q, want override path", res.Capabilities["nginx_log"])
	}
}

func TestHandleRecentRejectsUnknownSource(t *testing.T) {
	g := newTestGear()
	r := chi.NewRouter()
	g.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/access-log/redis/recent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown source", rr.Code)
	}
}

func TestHandleRecentReturnsAvailableFalseWhenNoLogFile(t *testing.T) {
	// nginx has neither an override nor a readable default — the
	// envelope must include available=false plus an actionable
	// reason. We must not 500 in this case.
	g := newTestGear()
	r := chi.NewRouter()
	g.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/access-log/nginx/recent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Available {
		t.Errorf("Available = true, want false (no readable log)")
	}
	if resp.Reason == "" {
		t.Errorf("Reason should describe why the log is unavailable")
	}
}

func TestHandleRecentParsesAndFiltersByStatusMin(t *testing.T) {
	g := newTestGear()
	g.stat = statExisting("/var/log/nginx/access.log")
	g.tail = staticTail([]string{
		`1.1.1.1 - - [01/Jan/2026:00:00:00 +0000] "GET /a HTTP/1.1" 200 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:01 +0000] "GET /b HTTP/1.1" 503 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:02 +0000] "GET /c HTTP/1.1" 502 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:03 +0000] "GET /d HTTP/1.1" 404 100 "-" "-"`,
		`garbage that should not parse`,
	})
	r := chi.NewRouter()
	g.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/access-log/nginx/recent?status_min=500", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Available {
		t.Fatalf("Available = false: %s", resp.Reason)
	}
	if resp.MatchCount != 2 {
		t.Errorf("MatchCount = %d, want 2 (502 + 503)", resp.MatchCount)
	}
	if resp.Profile != accesslog.ProfileNginxCombined {
		t.Errorf("Profile = %q, want %q", resp.Profile, accesslog.ProfileNginxCombined)
	}
	// Records are returned newest-first; the 502 (line index 2)
	// should come before the 503 (line index 1).
	if len(resp.Records) != 2 || resp.Records[0].StatusCode != 502 || resp.Records[1].StatusCode != 503 {
		t.Errorf("records ordering wrong: %+v", resp.Records)
	}
}

func TestHandleRecentRespectsLimit(t *testing.T) {
	g := newTestGear()
	g.stat = statExisting("/var/log/nginx/access.log")
	// 5 matching lines, limit=2 → return only the 2 most recent.
	lines := []string{
		`1.1.1.1 - - [01/Jan/2026:00:00:00 +0000] "GET /1 HTTP/1.1" 500 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:01 +0000] "GET /2 HTTP/1.1" 500 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:02 +0000] "GET /3 HTTP/1.1" 500 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:03 +0000] "GET /4 HTTP/1.1" 500 100 "-" "-"`,
		`1.1.1.1 - - [01/Jan/2026:00:00:04 +0000] "GET /5 HTTP/1.1" 500 100 "-" "-"`,
	}
	g.tail = staticTail(lines)
	r := chi.NewRouter()
	g.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/access-log/nginx/recent?limit=2&status_min=500", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	var resp Response
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.MatchCount != 2 {
		t.Errorf("MatchCount = %d, want 2 (limit)", resp.MatchCount)
	}
	// Should have /5 and /4 — the newest two matches.
	if resp.Records[0].Path != "/5" || resp.Records[1].Path != "/4" {
		t.Errorf("expected newest-first ordering with /5 then /4; got %+v", resp.Records)
	}
}

func TestHandleRecentSurfacesTailFailure(t *testing.T) {
	g := newTestGear()
	g.stat = statExisting("/var/log/nginx/access.log")
	g.tail = func(context.Context, string, int) ([]string, error) {
		return nil, errors.New("permission denied")
	}
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/access-log/nginx/recent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	var resp Response
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Available {
		t.Error("Available should be false when tail errors")
	}
	if resp.Reason == "" || resp.Reason == "tail /var/log/nginx/access.log: " {
		t.Errorf("Reason should explain the failure, got %q", resp.Reason)
	}
}

func TestParseIntDefaultClamps(t *testing.T) {
	cases := []struct {
		name                string
		raw                 string
		def, min, max, want int
	}{
		{"empty falls back to def", "", 7, 1, 100, 7},
		{"invalid falls back to badDef", "abc", 0, 1, 100, 7},
		{"below min clamps up", "0", 5, 1, 100, 1},
		{"above max clamps down", "999", 5, 1, 100, 100},
		{"in range passes through", "42", 5, 1, 100, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// badDef and def share the same value in these cases —
			// keeps the table compact. parseIntDefault distinguishes
			// "empty" (def) from "parse failure" (badDef); we test
			// both branches by using the same value above.
			got := parseIntDefault(tc.raw, tc.def, tc.min, tc.max, 7)
			if got != tc.want {
				t.Errorf("parseIntDefault(%q, def=%d, min=%d, max=%d) = %d, want %d",
					tc.raw, tc.def, tc.min, tc.max, got, tc.want)
			}
		})
	}
}

func TestIsReadableRejectsNonRegularFiles(t *testing.T) {
	// A directory at the log path is a config mistake — must NOT
	// be treated as a readable log file.
	g := newTestGear()
	g.stat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{name: "x", mode: os.ModeDir}, nil
	}
	if g.isReadable("/anywhere") {
		t.Error("isReadable should return false for directories")
	}
}
