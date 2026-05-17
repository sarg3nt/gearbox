package accesslog

import (
	"regexp"
	"strconv"
	"time"
)

// nginx default "combined" log format:
//
//	'$remote_addr - $remote_user [$time_local] '
//	'"$request" $status $body_bytes_sent '
//	'"$http_referer" "$http_user_agent"';
//
// Concrete example:
//
//	192.168.1.1 - - [28/Aug/2025:10:24:13 +0000] "GET /path HTTP/1.1" 200 1234 "-" "Mozilla/5.0 (X11; Linux x86_64)"
//
// We use one regex that captures the conventional ordering. Custom
// log_format directives that reorder fields won't parse — operators
// running those rely on the parsed metrics being best-effort, which
// matches the agent's documented stance on access-log shape variance.
//
// Field-by-field:
//
//	1: remote_addr
//	2: time_local
//	3: method
//	4: path
//	5: status
//	6: body_bytes_sent
//	7: http_referer
//	8: http_user_agent
var reNginxCombined = regexp.MustCompile(
	`^([^ ]+) - [^ ]* \[([^\]]+)\] "([A-Z]+) ([^ "]+)[^"]*" (\d{3}) (\d+|-) "([^"]*)" "([^"]*)"`,
)

// nginxTimeLayout matches the default `$time_local` format
// `28/Aug/2025:10:24:13 +0000`. Parse failures don't fail the Record
// — TimestampRaw still carries the original string so the dashboard
// can render it as-is.
const nginxTimeLayout = "02/Jan/2006:15:04:05 -0700"

// NginxCombinedProfile parses lines emitted by nginx's default
// "combined" log_format. Apache "combined" is the same shape (apache
// inherited the name from NCSA), and ApacheCombinedProfile delegates
// to this parser body for that reason.
type NginxCombinedProfile struct{}

// Profile satisfies Parser.
func (NginxCombinedProfile) Profile() string { return ProfileNginxCombined }

// Parse returns a Record for one nginx combined-format log line, or
// nil when the line doesn't match the expected shape.
func (NginxCombinedProfile) Parse(raw string) *Record {
	return parseCombined(raw, ProfileNginxCombined)
}

// parseCombined is the workhorse for both nginx-combined and
// apache-combined. Same regex, same field layout — the only
// difference is the Profile name we stamp on the resulting Record.
func parseCombined(raw, profile string) *Record {
	m := reNginxCombined.FindStringSubmatch(raw)
	if len(m) < 9 {
		return nil
	}
	status, err := strconv.Atoi(m[5])
	if err != nil || !validStatusCode(status) {
		return nil
	}

	rec := &Record{
		Profile:      profile,
		SourceIP:     m[1],
		TimestampRaw: m[2],
		Method:       m[3],
		Path:         m[4],
		StatusCode:   status,
		Referer:      m[7],
		UserAgent:    m[8],
		Raw:          trimRaw(raw),
	}

	// body_bytes_sent is "-" when nginx didn't send a body (e.g.
	// 304). Treat as zero rather than failing the parse.
	if m[6] != "-" {
		if n, err := strconv.ParseInt(m[6], 10, 64); err == nil {
			rec.BytesSent = n
		}
	}

	if t, err := time.Parse(nginxTimeLayout, m[2]); err == nil {
		rec.Timestamp = t
	}

	return rec
}
