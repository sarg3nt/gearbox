// Package promtext provides a minimal Prometheus text-format parser
// scoped to the agent's needs — extracting specific metrics from
// scraped output without pulling in the prometheus/common dependency
// chain. The format is simple enough (see
// https://prometheus.io/docs/instrumenting/exposition_formats/) that
// a focused parser is more honest than dragging in a 50+-package
// transitive footprint for what amounts to a few `caddy_http_*` /
// `traefik_*` lookups.
//
// Scope:
//   - Counter and gauge lines (one numeric value per line).
//   - Optional labels, including multiple labels per series.
//   - HELP and TYPE comments are skipped.
//   - Histograms and summaries are intentionally NOT decomposed —
//     callers wanting `*_count` / `*_sum` pull those by name like
//     any other series; per-bucket extraction is out of scope until
//     the metrics gear actually needs it.
package promtext

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// Sample is one parsed Prometheus exposition line — a metric name,
// its label set, and the numeric value. Labels are kept in a flat
// map because the only operation we run on them is "match by exact
// label-value equality" (e.g. `code="500"`).
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// labelPattern matches Prometheus label syntax inside braces:
//
//	name="quoted value with \" escapes"
//
// Group 1 is the label name; group 2 is the (still-escaped) value
// content. We unescape `\\` and `\"` and `\n` afterwards.
var labelPattern = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)="((?:[^"\\]|\\.)*)"`)

// Parse splits a Prometheus exposition payload into Samples. Lines
// that aren't recognisable metric samples (comments, blanks,
// malformed entries) are silently skipped — the parser is best-
// effort, mirroring how prom clients treat unfamiliar lines.
func Parse(payload string) []Sample {
	var out []Sample
	scanner := bufio.NewScanner(strings.NewReader(payload))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if s, ok := parseLine(scanner.Text()); ok {
			out = append(out, s)
		}
	}
	return out
}

// SumByName returns the sum of all sample values for metric name.
// Useful for counters that are split across label values when the
// caller wants the total — e.g. `caddy_http_response_status_total`
// across all `code=` labels.
func SumByName(samples []Sample, name string) float64 {
	var total float64
	for _, s := range samples {
		if s.Name == name {
			total += s.Value
		}
	}
	return total
}

// SumByNameWithLabel sums sample values for metric name whose
// labelKey equals labelValue. Used to compute 5xx counts by
// filtering a status-code-keyed counter to just the 5xx range, etc.
func SumByNameWithLabel(samples []Sample, name, labelKey, labelValue string) float64 {
	var total float64
	for _, s := range samples {
		if s.Name == name && s.Labels[labelKey] == labelValue {
			total += s.Value
		}
	}
	return total
}

// FirstByName returns the first Sample with the given name, or nil
// if no sample matched. Convenient for gauges where there's only
// one series (e.g. `caddy_admin_http_requests_total` is per-handler
// labelled, but `nginx_active_connections` would not be — neither
// project guarantees a single series, so callers that care should
// use one of the Sum* helpers instead).
func FirstByName(samples []Sample, name string) *Sample {
	for i := range samples {
		if samples[i].Name == name {
			return &samples[i]
		}
	}
	return nil
}

// parseLine returns a Sample for one exposition line, or false when
// the line is a comment, blank, or otherwise unparseable.
func parseLine(line string) (Sample, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return Sample{}, false
	}

	// Walk the line: the identifier ends at the first whitespace
	// outside the label braces. After that, the value is the next
	// whitespace-separated token, and an optional Prometheus scrape
	// timestamp (which we ignore) may follow.
	header, rest := splitHeaderFromValue(line)
	if header == "" || rest == "" {
		return Sample{}, false
	}
	valueStr := rest
	if i := strings.IndexAny(rest, " \t"); i > 0 {
		valueStr = rest[:i]
	}
	val, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return Sample{}, false
	}

	name, labels, ok := splitNameAndLabels(header)
	if !ok || name == "" {
		return Sample{}, false
	}
	return Sample{Name: name, Labels: labels, Value: val}, true
}

// splitHeaderFromValue finds the boundary between the metric
// identifier (which may contain whitespace inside the `{...}` label
// block) and the value. Returns the header (name + optional labels)
// and the trimmed remainder starting with the value. Whitespace
// inside braces does NOT terminate the header — `metric{a="x y"} 5`
// must keep `a="x y"` as part of the header.
func splitHeaderFromValue(line string) (header, rest string) {
	inBraces := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '{':
			inBraces = true
		case '}':
			inBraces = false
		case ' ', '\t':
			if !inBraces {
				return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
			}
		}
	}
	return "", ""
}

// splitNameAndLabels splits `metric_name{label1="v1",label2="v2"}`
// into the bare name and the label map. The trailing bool reports
// whether the header was well-formed; callers reject the line on
// false (e.g. when the brace block contained content but no valid
// label syntax matched, which means the line is malformed).
func splitNameAndLabels(header string) (string, map[string]string, bool) {
	openIdx := strings.IndexByte(header, '{')
	if openIdx < 0 {
		// Bare metric name, no labels — always well-formed.
		return header, nil, true
	}
	name := strings.TrimSpace(header[:openIdx])
	if name == "" {
		return "", nil, false
	}
	closeIdx := strings.LastIndexByte(header, '}')
	if closeIdx < openIdx {
		return "", nil, false
	}
	inside := header[openIdx+1 : closeIdx]
	if strings.TrimSpace(inside) == "" {
		// `metric{}` — empty label block; well-formed per spec.
		return name, nil, true
	}

	matches := labelPattern.FindAllStringSubmatch(inside, -1)
	if len(matches) == 0 {
		// Brace block has content but no `name="value"` pairs —
		// malformed. Treat as garbage so the parser stays strict
		// about misshapen scrapes.
		return "", nil, false
	}
	labels := make(map[string]string, len(matches))
	for _, m := range matches {
		labels[m[1]] = unescapeLabelValue(m[2])
	}
	return name, labels, true
}

// unescapeLabelValue reverses the Prometheus exposition format's
// in-string escapes: `\\` → `\`, `\"` → `"`, `\n` → newline. Other
// escapes are passed through verbatim (the spec doesn't define more).
func unescapeLabelValue(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case '\\':
			b.WriteByte('\\')
			i++
		case '"':
			b.WriteByte('"')
			i++
		case 'n':
			b.WriteByte('\n')
			i++
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
