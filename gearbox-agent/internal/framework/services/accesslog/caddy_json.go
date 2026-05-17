package accesslog

import (
	"encoding/json"
	"time"
)

// caddyAccessLog is the shape Caddy's `http.log.access` logger emits
// when the operator hasn't disabled fields. We only need a handful
// of values for the Record; everything else is left in the raw line
// for the dashboard's "see the original" affordance.
//
// Field-by-field (Caddy v2.x):
//
//	ts:       float Unix seconds with sub-second precision
//	duration: float seconds (we convert to ms)
//	size:     int response body bytes
//	status:   int HTTP status code
//	request:  embedded {remote_ip, method, uri, host, headers{User-Agent,Referer}}
type caddyAccessLog struct {
	TS       float64 `json:"ts"`
	Duration float64 `json:"duration"`
	Size     int64   `json:"size"`
	Status   int     `json:"status"`
	Request  struct {
		RemoteIP string              `json:"remote_ip"`
		Method   string              `json:"method"`
		URI      string              `json:"uri"`
		Host     string              `json:"host"`
		Headers  map[string][]string `json:"headers"`
	} `json:"request"`
}

// CaddyJSONProfile parses Caddy's structured JSON access log. Each
// line must be one valid JSON object; multi-line / pretty-printed
// output returns nil. Returns nil for any object that's missing a
// recognisable HTTP status code (heartbeats, admin events, etc.
// that Caddy may also write to the same logger if misconfigured).
type CaddyJSONProfile struct{}

// Profile satisfies Parser.
func (CaddyJSONProfile) Profile() string { return ProfileCaddyJSON }

// Parse returns a Record for one Caddy JSON access-log entry, or nil
// when the line isn't a recognisable HTTP access event.
func (CaddyJSONProfile) Parse(raw string) *Record {
	var entry caddyAccessLog
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return nil
	}
	if !validStatusCode(entry.Status) {
		return nil
	}

	rec := &Record{
		Profile:    ProfileCaddyJSON,
		StatusCode: entry.Status,
		BytesSent:  entry.Size,
		DurationMs: entry.Duration * 1000.0,
		SourceIP:   entry.Request.RemoteIP,
		Method:     entry.Request.Method,
		Path:       entry.Request.URI,
		Host:       entry.Request.Host,
		Raw:        trimRaw(raw),
	}

	if entry.TS > 0 {
		// Caddy's `ts` is float Unix seconds with sub-second
		// precision; time.UnixMicro keeps that precision when the
		// caller wants to render at ms granularity downstream.
		rec.Timestamp = time.UnixMicro(int64(entry.TS * 1e6)).UTC()
		rec.TimestampRaw = rec.Timestamp.Format(time.RFC3339Nano)
	}

	if h := entry.Request.Headers; h != nil {
		if v := h["User-Agent"]; len(v) > 0 {
			rec.UserAgent = v[0]
		}
		if v := h["Referer"]; len(v) > 0 {
			rec.Referer = v[0]
		}
	}

	return rec
}
