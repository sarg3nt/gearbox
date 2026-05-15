package accesslog

import (
	"strings"
	"testing"
)

func TestHAProxyProfileParsesTypicalLine(t *testing.T) {
	// Real-shape HAProxy HTTP log line. Tt = 12 in the Tq/Tw/Tc/Tr/Tt
	// timings; the parser should surface that as DurationMs.
	raw := `Aug 28 10:24:13 host haproxy[1234]: 192.168.1.10:54321 [28/Aug/2025:10:24:13.567] frontend backend1/server2 0/0/0/12/12 200 1234 - - ---- 1/1/0/0/0 0/0 "GET /healthz HTTP/1.1"`
	rec := HAProxyProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record, got nil")
	}
	checks := map[string]string{
		"profile":   rec.Profile,
		"source_ip": rec.SourceIP,
		"method":    rec.Method,
		"path":      rec.Path,
		"backend":   rec.Backend,
		"server":    rec.Server,
		"timestamp": rec.TimestampRaw,
	}
	wantString := map[string]string{
		"profile":   ProfileHAProxy,
		"source_ip": "192.168.1.10",
		"method":    "GET",
		"path":      "/healthz",
		"backend":   "backend1",
		"server":    "server2",
		"timestamp": "28/Aug/2025:10:24:13.567",
	}
	for k, want := range wantString {
		if checks[k] != want {
			t.Errorf("%s = %q, want %q", k, checks[k], want)
		}
	}
	if rec.StatusCode != 200 {
		t.Errorf("status_code = %d, want 200", rec.StatusCode)
	}
	if rec.DurationMs != 12 {
		t.Errorf("duration_ms = %v, want 12 (from Tt field)", rec.DurationMs)
	}
}

func TestHAProxyProfileRejectsNonHTTPLines(t *testing.T) {
	// Lines that don't carry a valid status code — SSL handshake
	// errors, connection diagnostics, the empty line — must return
	// nil so the caller filters them as noise.
	for _, raw := range []string{
		"",
		"not an haproxy log line at all",
		"haproxy[42]: SSL handshake failure from 1.2.3.4",
		// Status 999 is technically three digits but outside HTTP's
		// 100-599 range; defence against false positives.
		`Aug 28 10:24:13 host haproxy[1]: 1.2.3.4:11 [28/Aug/2025:10:24:13] f b/s 0/0/0/0/0 999 100 - - ---- 0/0/0/0/0 0/0 "X / HTTP/1.1"`,
	} {
		if got := (HAProxyProfile{}).Parse(raw); got != nil {
			t.Errorf("expected nil for %q, got %+v", raw, got)
		}
	}
}

func TestHAProxyProfileNegativeTtIsIgnored(t *testing.T) {
	// Tt = -1 means the session was aborted before that timer was
	// set; we shouldn't surface negative latency.
	raw := `Aug 28 host haproxy[1]: 1.2.3.4:5 [28/Aug/2025:10:24:13] f b/s 0/0/0/0/-1 503 100 - - SC-- 0/0/0/0/0 0/0 "GET / HTTP/1.1"`
	rec := HAProxyProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record")
	}
	if rec.DurationMs != 0 {
		t.Errorf("duration_ms = %v, want 0 for negative Tt", rec.DurationMs)
	}
}

func TestNginxCombinedProfileParsesTypicalLine(t *testing.T) {
	raw := `192.168.1.10 - - [28/Aug/2025:10:24:13 +0000] "GET /healthz HTTP/1.1" 200 1234 "https://ref/" "Mozilla/5.0 (X11; Linux x86_64)"`
	rec := NginxCombinedProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record")
	}
	if rec.Profile != ProfileNginxCombined {
		t.Errorf("profile = %q, want %q", rec.Profile, ProfileNginxCombined)
	}
	if rec.SourceIP != "192.168.1.10" || rec.Method != "GET" || rec.Path != "/healthz" {
		t.Errorf("request fields wrong: %+v", rec)
	}
	if rec.StatusCode != 200 || rec.BytesSent != 1234 {
		t.Errorf("response fields wrong: status=%d bytes=%d", rec.StatusCode, rec.BytesSent)
	}
	if rec.Referer != "https://ref/" {
		t.Errorf("referer = %q", rec.Referer)
	}
	if !strings.Contains(rec.UserAgent, "Mozilla") {
		t.Errorf("user_agent = %q", rec.UserAgent)
	}
	if rec.Timestamp.IsZero() {
		t.Error("Timestamp should be parsed from the bracketed date")
	}
}

func TestNginxCombinedProfileTreatsDashBytesAsZero(t *testing.T) {
	// 304 responses commonly emit `-` for body_bytes_sent; the
	// parser must accept it without failing the line.
	raw := `1.1.1.1 - - [28/Aug/2025:10:24:13 +0000] "GET / HTTP/1.1" 304 - "-" "-"`
	rec := NginxCombinedProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record")
	}
	if rec.BytesSent != 0 {
		t.Errorf("bytes_sent = %d, want 0 for '-'", rec.BytesSent)
	}
}

func TestApacheCommonProfileParsesTypicalLine(t *testing.T) {
	raw := `192.168.1.10 - - [28/Aug/2025:10:24:13 +0000] "GET /index.html HTTP/1.1" 200 5678`
	rec := ApacheCommonProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record")
	}
	if rec.Profile != ProfileApacheCommon {
		t.Errorf("profile = %q", rec.Profile)
	}
	if rec.StatusCode != 200 || rec.BytesSent != 5678 {
		t.Errorf("status/bytes wrong: %+v", rec)
	}
	// CLF has no Referer / User-Agent — both should be zero-valued.
	if rec.Referer != "" || rec.UserAgent != "" {
		t.Errorf("CLF parser should leave Referer / UA empty, got %q / %q", rec.Referer, rec.UserAgent)
	}
}

func TestApacheCombinedProfileSharesShapeWithNginx(t *testing.T) {
	// Combined format is identical between Apache and nginx; only
	// the Profile identifier differs.
	raw := `1.1.1.1 - - [28/Aug/2025:10:24:13 +0000] "GET /x HTTP/1.1" 200 100 "-" "curl/8.0"`
	rec := ApacheCombinedProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record")
	}
	if rec.Profile != ProfileApacheCombined {
		t.Errorf("profile = %q, want %q", rec.Profile, ProfileApacheCombined)
	}
	if !strings.Contains(rec.UserAgent, "curl") {
		t.Errorf("user_agent = %q", rec.UserAgent)
	}
}

func TestCaddyJSONProfileParsesAccessEntry(t *testing.T) {
	raw := `{"level":"info","ts":1693220653.567,"logger":"http.log.access","msg":"handled request","request":{"remote_ip":"192.168.1.10","method":"GET","uri":"/path","host":"example.com","headers":{"User-Agent":["curl/8.0"],"Referer":["https://ref/"]}},"duration":0.012,"size":1234,"status":200}`
	rec := CaddyJSONProfile{}.Parse(raw)
	if rec == nil {
		t.Fatal("expected a Record")
	}
	if rec.Profile != ProfileCaddyJSON {
		t.Errorf("profile = %q", rec.Profile)
	}
	if rec.StatusCode != 200 || rec.BytesSent != 1234 {
		t.Errorf("status/bytes wrong: %+v", rec)
	}
	if rec.DurationMs != 12.0 {
		t.Errorf("duration_ms = %v, want 12.0 (Caddy reports seconds → ms)", rec.DurationMs)
	}
	if rec.SourceIP != "192.168.1.10" || rec.Method != "GET" || rec.Path != "/path" {
		t.Errorf("request fields wrong: %+v", rec)
	}
	if rec.Host != "example.com" {
		t.Errorf("host = %q", rec.Host)
	}
	if rec.UserAgent != "curl/8.0" || rec.Referer != "https://ref/" {
		t.Errorf("UA/Referer wrong: %q / %q", rec.UserAgent, rec.Referer)
	}
	if rec.Timestamp.IsZero() {
		t.Error("Timestamp should be parsed from the float ts field")
	}
}

func TestCaddyJSONProfileRejectsNonHTTPEntries(t *testing.T) {
	// Caddy's logger can emit non-access events to the same stream
	// if the operator misconfigures the logger. Without a valid
	// status we must return nil.
	for _, raw := range []string{
		``,
		`not json at all`,
		`{"level":"info","msg":"server started"}`,
		`{"status":99}`,
		`{"status":700}`,
	} {
		if got := (CaddyJSONProfile{}).Parse(raw); got != nil {
			t.Errorf("expected nil for %q, got %+v", raw, got)
		}
	}
}
