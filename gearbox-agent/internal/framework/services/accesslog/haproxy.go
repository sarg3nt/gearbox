package accesslog

import (
	"regexp"
	"strconv"
)

// HAProxy HTTP log format (from `option httplog` / the default
// log-format), as the field positions appear in practice:
//
//	<date> <host> haproxy[<pid>]: <client_ip>:<port> [<accept_date>] <frontend>~ <backend>/<server> <Tq>/<Tw>/<Tc>/<Tr>/<Tt> <status> <bytes_read> ... "<method> <path> HTTP/1.x"
//
// We parse defensively — fields can vary by HAProxy version and the
// operator's custom log-format. Anything we can't pull out stays
// zero-valued on the Record. This is a direct port of the dashboard's
// original `parseHAProxyLogLine`, kept identical in behaviour so the
// metrics-page Error Insights panel sees no diff when the dashboard
// flips to consuming the agent endpoint instead of parsing locally.
var (
	reHAProxyStatus = regexp.MustCompile(`\s(\d{3})\s+\d+\s`)
	reHAProxyReq    = regexp.MustCompile(`"([A-Z]+)\s+([^\s"]+)`)
	reHAProxyBkSvr  = regexp.MustCompile(`\s([A-Za-z0-9_.\-]+)/([A-Za-z0-9_.\-]+)\s+\d+\/\-?\d+\/\-?\d+\/\-?\d+\/\-?\d+\s`)
	reHAProxyClient = regexp.MustCompile(`(?:^|\s)(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}|[0-9a-fA-F:]+):\d+\s+\[`)
	// Anchored on the date shape (day/Mon/year:hh:mm:ss) so the
	// syslog-style `haproxy[1234]:` PID bracket doesn't claim the
	// match. The dashboard-side original had this latent bug; the
	// fixtures there never carried the syslog wrapper.
	reHAProxyDate = regexp.MustCompile(`\[(\d{1,2}/\w{3}/\d{4}:[^\]]+)\]`)
	// Tt is the total time field; it appears as the fifth slash-
	// separated number in Tq/Tw/Tc/Tr/Tt. Negative values mean the
	// session was aborted before that timer was set; we surface ms
	// only when Tt is non-negative.
	reHAProxyTimings = regexp.MustCompile(`\s\-?\d+\/\-?\d+\/\-?\d+\/\-?\d+\/(\-?\d+)\s+\d{3}\s`)
)

// HAProxyProfile parses HAProxy HTTP access log lines. Returns nil
// for lines that don't carry a valid HTTP status code (SSL handshake
// errors, connection diagnostics, etc.).
type HAProxyProfile struct{}

// Profile satisfies Parser.
func (HAProxyProfile) Profile() string { return ProfileHAProxy }

// Parse pulls structured fields out of one HAProxy HTTP log line.
func (HAProxyProfile) Parse(raw string) *Record {
	statusMatch := reHAProxyStatus.FindStringSubmatch(raw)
	if len(statusMatch) < 2 {
		return nil
	}
	status, _ := strconv.Atoi(statusMatch[1])
	if !validStatusCode(status) {
		return nil
	}

	rec := &Record{
		Profile:    ProfileHAProxy,
		StatusCode: status,
		Raw:        trimRaw(raw),
	}

	if m := reHAProxyDate.FindStringSubmatch(raw); len(m) > 1 {
		rec.TimestampRaw = m[1]
	}
	if m := reHAProxyClient.FindStringSubmatch(raw); len(m) > 1 {
		rec.SourceIP = m[1]
	}
	if m := reHAProxyBkSvr.FindStringSubmatch(raw); len(m) > 2 {
		rec.Backend = m[1]
		rec.Server = m[2]
	}
	if m := reHAProxyReq.FindStringSubmatch(raw); len(m) > 2 {
		rec.Method = m[1]
		rec.Path = m[2]
	}
	if m := reHAProxyTimings.FindStringSubmatch(raw); len(m) > 1 {
		if tt, err := strconv.Atoi(m[1]); err == nil && tt >= 0 {
			rec.DurationMs = float64(tt)
		}
	}

	return rec
}
