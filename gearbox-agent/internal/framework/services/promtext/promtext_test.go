package promtext

import (
	"strings"
	"testing"
)

const realisticCaddyOutput = `# HELP caddy_http_requests_total Counter of HTTP requests
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="srv0",handler="reverse_proxy"} 42
caddy_http_requests_total{server="srv0",handler="file_server"} 17
# HELP caddy_http_response_status_total Status code counter
# TYPE caddy_http_response_status_total counter
caddy_http_response_status_total{code="200"} 50
caddy_http_response_status_total{code="404"} 9
caddy_http_response_status_total{code="500"} 2
# unlabeled gauge
caddy_admin_running 1
`

func TestParseRealisticOutput(t *testing.T) {
	samples := Parse(realisticCaddyOutput)
	if len(samples) != 6 {
		t.Fatalf("got %d samples, want 6 (helpful comments are skipped)", len(samples))
	}

	// requests_total split across two label sets, sums to 59
	if got := SumByName(samples, "caddy_http_requests_total"); got != 59 {
		t.Errorf("SumByName(requests_total) = %v, want 59", got)
	}
	// 5xx count: only the 500 series
	if got := SumByNameWithLabel(samples, "caddy_http_response_status_total", "code", "500"); got != 2 {
		t.Errorf("SumByNameWithLabel(...,'code','500') = %v, want 2", got)
	}
	// Unlabelled gauge accessible via FirstByName
	first := FirstByName(samples, "caddy_admin_running")
	if first == nil || first.Value != 1 {
		t.Errorf("FirstByName(admin_running) = %+v, want value=1", first)
	}
}

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	// Defensive: blank lines and comment lines never become samples.
	in := "\n\n# HELP only_comment\n# TYPE only_comment counter\nactual_metric 5\n\n"
	samples := Parse(in)
	if len(samples) != 1 || samples[0].Name != "actual_metric" || samples[0].Value != 5 {
		t.Errorf("Parse result = %+v, want one [actual_metric=5]", samples)
	}
}

func TestParseHandlesEscapedLabelValues(t *testing.T) {
	// Prometheus allows \\ \" \n inside quoted label values; the
	// parser must un-escape so callers comparing label values
	// against literal strings work as expected.
	in := `nginx_response_total{path="/has \" quote",msg="line\\one"} 3` + "\n"
	samples := Parse(in)
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}
	if got := samples[0].Labels["path"]; got != `/has " quote` {
		t.Errorf("escaped quote: got %q, want %q", got, `/has " quote`)
	}
	if got := samples[0].Labels["msg"]; got != `line\one` {
		t.Errorf("escaped backslash: got %q, want %q", got, `line\one`)
	}
}

func TestParseToleratesTrailingTimestamp(t *testing.T) {
	// Optional timestamp_ms is allowed by the spec; we don't use it
	// but the parser must not reject the line.
	in := `metric_with_ts 7.5 1693220653000` + "\n"
	samples := Parse(in)
	if len(samples) != 1 || samples[0].Value != 7.5 {
		t.Errorf("Parse with timestamp = %+v, want value=7.5", samples)
	}
}

func TestParseRejectsMalformedLines(t *testing.T) {
	// No value, malformed labels, garbage — all silently skipped.
	in := strings.Join([]string{
		`no_value_here`,
		`malformed{ broken= value} 1`,
		`{empty_name} 5`,
		`good_metric 9`,
	}, "\n")
	samples := Parse(in)
	if len(samples) != 1 || samples[0].Name != "good_metric" {
		t.Errorf("Parse result = %+v, want only [good_metric]", samples)
	}
}
