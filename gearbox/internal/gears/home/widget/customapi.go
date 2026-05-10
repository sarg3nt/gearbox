package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// CustomAPIConfig is the per-tile config the provider needs at fetch time.
// It mirrors home.CustomAPIConfig (keeping the widget package self-contained;
// callers rebuild this from their own representation).
type CustomAPIConfig struct {
	URL           string             `json:"url"`
	Method        string             `json:"method,omitempty"`
	Headers       map[string]string  `json:"headers,omitempty"`
	RequestBody   string             `json:"request_body,omitempty"`
	Auth          string             `json:"auth,omitempty"`           // none|basic|bearer|header
	BasicUsername string             `json:"basic_username,omitempty"`
	HeaderName    string             `json:"header_name,omitempty"`
	Mappings      []CustomAPIMapping `json:"mappings,omitempty"`
}

// CustomAPIMapping is one field-to-display mapping.
type CustomAPIMapping struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Format string `json:"format,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

// FetchCustomAPI runs one customapi probe with the supplied config and
// secret, returning the rendered field map. Lives outside the Provider
// interface because the home gear builds its own per-call config rather
// than registering a singleton.
func FetchCustomAPI(ctx context.Context, baseURL, secret string, cfg CustomAPIConfig, client *http.Client) (Result, error) {
	res := Result{Fields: make(map[string]string)}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodGet
	}
	target := cfg.URL
	if target == "" {
		target = baseURL
	}

	var body io.Reader
	if cfg.RequestBody != "" {
		body = strings.NewReader(cfg.RequestBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return res, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	switch cfg.Auth {
	case "basic":
		if cfg.BasicUsername != "" && secret != "" {
			req.SetBasicAuth(cfg.BasicUsername, secret)
		}
	case "bearer":
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	case "header":
		if cfg.HeaderName != "" && secret != "" {
			req.Header.Set(cfg.HeaderName, secret)
		}
	}

	if client == nil {
		client = DefaultClient(0)
	}
	resp, err := client.Do(req)
	if err != nil {
		return res, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return res, fmt.Errorf("customapi: HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return res, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return res, fmt.Errorf("customapi: not JSON: %w", err)
	}

	for _, m := range cfg.Mappings {
		val, ok := walkAny(doc, m.Field)
		if !ok {
			continue
		}
		text := formatValue(val, m.Format)
		if m.Prefix != "" {
			text = m.Prefix + text
		}
		if m.Suffix != "" {
			text = text + m.Suffix
		}
		key := m.Label
		if key == "" {
			key = m.Field
		}
		res.Fields[key] = text
	}
	return res, nil
}

// walkAny implements dot-notation traversal — same shape as home.walk but
// inside this package so it has no cross-package dependency.
func walkAny(doc any, path string) (any, bool) {
	if path == "" {
		return doc, true
	}
	parts := strings.Split(path, ".")
	cur := doc
	for _, p := range parts {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[p]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// formatValue renders a JSON value as a string using the requested format.
// Supported: text (default), number, float, percent, bytes, bitrate, duration, date.
func formatValue(v any, format string) string {
	switch format {
	case "number":
		if n, ok := asFloatVal(v); ok {
			return fmt.Sprintf("%.0f", n)
		}
	case "float":
		if n, ok := asFloatVal(v); ok {
			return fmt.Sprintf("%.2f", n)
		}
	case "percent":
		if n, ok := asFloatVal(v); ok {
			return fmt.Sprintf("%.1f%%", n)
		}
	case "bytes":
		if n, ok := asFloatVal(v); ok {
			return formatBytes(n)
		}
	case "bitrate":
		if n, ok := asFloatVal(v); ok {
			return formatBitrate(n)
		}
	case "duration":
		if n, ok := asFloatVal(v); ok {
			return formatDuration(n)
		}
	}
	return fmt.Sprintf("%v", v)
}

func asFloatVal(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func formatBytes(n float64) string {
	const k = 1024.0
	switch {
	case n < k:
		return fmt.Sprintf("%.0f B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KB", n/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MB", n/k/k)
	case n < k*k*k*k:
		return fmt.Sprintf("%.1f GB", n/k/k/k)
	default:
		return fmt.Sprintf("%.2f TB", n/k/k/k/k)
	}
}

func formatBitrate(n float64) string {
	const k = 1000.0
	switch {
	case n < k:
		return fmt.Sprintf("%.0f bps", n)
	case n < k*k:
		return fmt.Sprintf("%.1f Kbps", n/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f Mbps", n/k/k)
	default:
		return fmt.Sprintf("%.1f Gbps", n/k/k/k)
	}
}

func formatDuration(secs float64) string {
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", int(secs/60), int(secs)%60)
	}
	if secs < 86400 {
		return fmt.Sprintf("%dh %dm", int(secs/3600), (int(secs)%3600)/60)
	}
	return fmt.Sprintf("%dd %dh", int(secs/86400), (int(secs)%86400)/3600)
}
