package home

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//go:embed catalog/apps.json
var catalogFS embed.FS

// CatalogEntry is one row from the predefined apps catalog.
type CatalogEntry struct {
	Slug         string               `json:"slug"`
	Name         string               `json:"name"`
	Category     string               `json:"category,omitempty"`
	IconURL      string               `json:"icon_url"`
	DefaultPorts []int                `json:"default_ports,omitempty"`
	Auth         string               `json:"auth,omitempty"`        // none | apikey | basic | bearer | header
	Fingerprint  *Fingerprint         `json:"fingerprint,omitempty"` // nil for catalog entries with no auto-detect probe
	WidgetFields []CatalogWidgetField `json:"widget_fields,omitempty"`
	APIHelp      *APIHelp             `json:"api_help,omitempty"` // optional: instructions for getting an API key
}

// APIHelp describes how a user can find an API key / token for a given app.
// Surfaced in the Add/Edit-tile modal next to the green "Detected" banner so
// the question "where do I get this key?" never sends the user out to Google.
//
// SettingsPath, when set, is appended to the tile's base URL to produce a
// "Open <App> settings" deep link in the instructions dialog. Apps whose
// token isn't fetchable from a settings page (e.g. Plex) leave it empty.
type APIHelp struct {
	Title        string   `json:"title,omitempty"`         // dialog header — "Find your <App> API key"
	SettingsPath string   `json:"settings_path,omitempty"` // optional path appended to the tile's base URL for the deep-link button
	Steps        []string `json:"steps,omitempty"`         // ordered, plain-text instructions; backticks render as inline <code>
	Note         string   `json:"note,omitempty"`          // optional trailing context (security caveats, scope, etc.)
}

// CatalogWidgetField describes one selectable data point a tier-1 widget can render.
type CatalogWidgetField struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// Fingerprint describes how to detect an app by probing a URL.
//
//   - type: "json"           — fetch URL+path, parse JSON, walk dot-notation `field`,
//                              compare to `equals` OR check `exists`.
//   - type: "body_contains"  — fetch URL+path, search response body for `value`.
type Fingerprint struct {
	Path   string `json:"path"`
	Type   string `json:"type"` // json | body_contains
	Field  string `json:"field,omitempty"`
	Equals string `json:"equals,omitempty"`
	Exists bool   `json:"exists,omitempty"`
	Value  string `json:"value,omitempty"`
}

// catalogState lazily loads & caches the embedded catalog. The single-load
// path is hot during page renders so we keep it lock-free after warm-up.
var catalogState struct {
	once    sync.Once
	entries []CatalogEntry
	bySlug  map[string]CatalogEntry
	loadErr error
}

// loadCatalog parses catalog/apps.json on first call and caches the result.
func loadCatalog() ([]CatalogEntry, map[string]CatalogEntry, error) {
	catalogState.once.Do(func() {
		raw, err := catalogFS.ReadFile("catalog/apps.json")
		if err != nil {
			catalogState.loadErr = fmt.Errorf("read catalog: %w", err)
			return
		}
		var entries []CatalogEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			catalogState.loadErr = fmt.Errorf("parse catalog: %w", err)
			return
		}
		bySlug := make(map[string]CatalogEntry, len(entries))
		for _, e := range entries {
			bySlug[e.Slug] = e
		}
		catalogState.entries = entries
		catalogState.bySlug = bySlug
	})
	return catalogState.entries, catalogState.bySlug, catalogState.loadErr
}

// Catalog returns all catalog entries.
func Catalog() ([]CatalogEntry, error) {
	entries, _, err := loadCatalog()
	return entries, err
}

// CatalogBySlug looks up one entry by slug.
func CatalogBySlug(slug string) (CatalogEntry, bool) {
	_, m, err := loadCatalog()
	if err != nil {
		return CatalogEntry{}, false
	}
	e, ok := m[slug]
	return e, ok
}

// fingerprintProber runs catalog fingerprint probes against a target URL.
// It uses the same lax TLS settings as the status worker and a tight 2s
// per-probe timeout so the picker UX stays snappy.
type fingerprintProber struct {
	client *http.Client
}

// newFingerprintProber builds a prober.
func newFingerprintProber() *fingerprintProber {
	return &fingerprintProber{
		client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		},
	}
}

// Detect fingerprints the target URL by running every catalog probe in
// parallel and returning the first match. Returns (slug, true) on a hit;
// ("", false) when nothing matched. Probes that error out are silently
// ignored — they're trying every probe against URLs that mostly won't
// match, so noisy logging would drown the signal.
func (p *fingerprintProber) Detect(ctx context.Context, target string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}

	entries, _, err := loadCatalog()
	if err != nil {
		return "", false
	}

	type result struct {
		slug string
		ok   bool
	}
	results := make(chan result, len(entries))
	var wg sync.WaitGroup

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	for _, e := range entries {
		if e.Fingerprint == nil {
			continue
		}
		wg.Add(1)
		go func(entry CatalogEntry) {
			defer wg.Done()
			if p.matches(probeCtx, parsed, entry.Fingerprint) {
				results <- result{slug: entry.Slug, ok: true}
			}
		}(e)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// First match wins. Drain channel after to avoid leaking goroutines.
	if r, ok := <-results; ok && r.ok {
		go func() {
			for range results {
			}
		}()
		return r.slug, true
	}
	return "", false
}

// matches runs one fingerprint probe and reports success.
func (p *fingerprintProber) matches(ctx context.Context, base *url.URL, fp *Fingerprint) bool {
	if fp == nil || fp.Path == "" {
		return false
	}
	probeURL := *base // copy
	probeURL.Path = strings.TrimSuffix(probeURL.Path, "/") + fp.Path
	// Strip any query from the user URL; replace with the fingerprint's query
	// (most fingerprint paths include their own e.g. ?summary).
	if i := strings.Index(fp.Path, "?"); i >= 0 {
		probeURL.RawQuery = fp.Path[i+1:]
		probeURL.Path = strings.TrimSuffix(base.Path, "/") + fp.Path[:i]
	} else {
		probeURL.RawQuery = ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL.String(), nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return false
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	switch fp.Type {
	case "json":
		var doc any
		if err := json.Unmarshal(body, &doc); err != nil {
			return false
		}
		val, ok := walk(doc, fp.Field)
		if !ok {
			return false
		}
		if fp.Exists {
			return true
		}
		s, _ := val.(string)
		return s == fp.Equals
	case "body_contains":
		return strings.Contains(string(body), fp.Value)
	}
	return false
}

// walk traverses a decoded JSON tree by dot-notation. Returns (value, true)
// on a hit, (nil, false) on missing path. Numeric path segments index arrays.
func walk(doc any, path string) (any, bool) {
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
			// Array index segment.
			idx := -1
			for i := 0; i < len(p); i++ {
				if p[i] < '0' || p[i] > '9' {
					return nil, false
				}
			}
			fmt.Sscanf(p, "%d", &idx)
			if idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}
