package accesslog

import (
	"regexp"
	"strconv"
	"time"
)

// Apache CLF (common log format):
//
//	%h %l %u %t \"%r\" %>s %b
//
// Concrete example:
//
//	192.168.1.1 - - [28/Aug/2025:10:24:13 +0000] "GET /path HTTP/1.1" 200 1234
//
// Combined format adds two trailing quoted fields for Referer and
// User-Agent — see ApacheCombinedProfile, which delegates to
// parseCombined for that shape.
//
// We use a single regex for CLF: it's strict about the leading fields
// (IP, dash, dash, bracketed date, quoted request) but tolerant about
// what comes after the byte count, since some operators append %D
// (duration in µs) or trace IDs.
var reApacheCommon = regexp.MustCompile(
	`^([^ ]+) [^ ]+ [^ ]+ \[([^\]]+)\] "([A-Z]+) ([^ "]+)[^"]*" (\d{3}) (\d+|-)`,
)

// apacheTimeLayout is identical to nginx's `%t` format — both
// projects use the NCSA Common Log convention.
const apacheTimeLayout = "02/Jan/2006:15:04:05 -0700"

// ApacheCommonProfile parses Apache CLF lines (without Referer /
// User-Agent). Returns nil for lines that don't carry a valid HTTP
// status code.
type ApacheCommonProfile struct{}

// Profile satisfies Parser.
func (ApacheCommonProfile) Profile() string { return ProfileApacheCommon }

// Parse returns a Record for one Apache CLF log line, or nil on
// shape mismatch.
func (ApacheCommonProfile) Parse(raw string) *Record {
	m := reApacheCommon.FindStringSubmatch(raw)
	if len(m) < 7 {
		return nil
	}
	status, err := strconv.Atoi(m[5])
	if err != nil || !validStatusCode(status) {
		return nil
	}

	rec := &Record{
		Profile:      ProfileApacheCommon,
		SourceIP:     m[1],
		TimestampRaw: m[2],
		Method:       m[3],
		Path:         m[4],
		StatusCode:   status,
		Raw:          trimRaw(raw),
	}

	if m[6] != "-" {
		if n, err := strconv.ParseInt(m[6], 10, 64); err == nil {
			rec.BytesSent = n
		}
	}

	if t, err := time.Parse(apacheTimeLayout, m[2]); err == nil {
		rec.Timestamp = t
	}

	return rec
}

// ApacheCombinedProfile parses Apache "combined" format — CLF plus
// Referer and User-Agent. Same byte-for-byte shape as nginx's
// combined format (the Apache directive `combined` was inherited
// from NCSA), so we delegate to parseCombined and only differ in the
// Profile identifier we stamp on each Record.
type ApacheCombinedProfile struct{}

// Profile satisfies Parser.
func (ApacheCombinedProfile) Profile() string { return ProfileApacheCombined }

// Parse returns a Record for one Apache combined-format log line.
func (ApacheCombinedProfile) Parse(raw string) *Record {
	return parseCombined(raw, ProfileApacheCombined)
}
