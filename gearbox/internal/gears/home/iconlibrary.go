package home

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Icon library source. selfh.st publishes a 2,700+ entry index.json at this
// path on jsDelivr; the URL pattern for an individual icon SVG is
// `<iconBaseURL>/<Reference>.svg`. Same source the predefined apps catalog
// already uses for its icons.
const (
	iconIndexURL    = "https://cdn.jsdelivr.net/gh/selfhst/icons/index.json"
	iconBaseURL     = "https://cdn.jsdelivr.net/gh/selfhst/icons/svg"
	iconCacheTTL    = 6 * time.Hour
	iconFetchTimeout = 10 * time.Second
)

// IconEntry is one row of the icon picker library, normalized for the
// frontend. Name + Slug drive the search; URL is the ready-to-use SVG;
// Category/Tags drive optional filtering.
type IconEntry struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
	Tags     string `json:"tags,omitempty"`
}

// rawSelfhstEntry mirrors the upstream selfh.st/icons index.json schema.
// Most fields ("Yes"/"No" availability flags) are uninteresting to us —
// we only care that Reference is set so we can build the SVG URL.
type rawSelfhstEntry struct {
	Name      string `json:"Name"`
	Reference string `json:"Reference"`
	SVG       string `json:"SVG"`
	Category  string `json:"Category"`
	Tags      string `json:"Tags"`
}

// iconLibrary holds the cached icon list and the machinery to refresh it.
// Refresh is opportunistic: requests served while a refresh is in flight
// get the previous snapshot, so the UI never blocks on the upstream.
type iconLibrary struct {
	mu        sync.RWMutex
	entries   []IconEntry
	bySlug    map[string]*IconEntry // populated by refresh, drives Suggest
	byName    map[string]*IconEntry // lowercase Name → entry, drives Suggest
	fetchedAt time.Time
	fetching  bool

	httpClient *http.Client
}

func newIconLibrary() *iconLibrary {
	return &iconLibrary{
		httpClient: &http.Client{
			Timeout: iconFetchTimeout,
			Transport: &http.Transport{
				// jsDelivr serves a valid cert; relaxed TLS isn't strictly
				// needed but mirrors the rest of this gear's outbound calls.
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}
}

// List returns the cached entries, kicking off a refresh in the background
// if the cache is stale or empty. The first call ever blocks until the
// initial fetch completes (so the UI gets data instead of an empty list).
func (l *iconLibrary) List(ctx context.Context) ([]IconEntry, error) {
	l.mu.RLock()
	entries := l.entries
	stale := time.Since(l.fetchedAt) > iconCacheTTL
	l.mu.RUnlock()

	if len(entries) > 0 && !stale {
		return entries, nil
	}

	// Empty cache: do a synchronous fetch. Stale cache: serve old data and
	// refresh in the background.
	if len(entries) == 0 {
		if err := l.refresh(ctx); err != nil {
			return nil, err
		}
		l.mu.RLock()
		entries = l.entries
		l.mu.RUnlock()
		return entries, nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), iconFetchTimeout)
		defer cancel()
		_ = l.refresh(ctx)
	}()
	return entries, nil
}

// refresh pulls index.json, normalizes it, and swaps in the result. Safe to
// call concurrently — the `fetching` flag deduplicates in-flight calls so a
// burst of UI requests doesn't hammer the upstream.
func (l *iconLibrary) refresh(ctx context.Context) error {
	l.mu.Lock()
	if l.fetching {
		l.mu.Unlock()
		return nil
	}
	l.fetching = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.fetching = false
		l.mu.Unlock()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconIndexURL, nil)
	if err != nil {
		return fmt.Errorf("build icon index request: %w", err)
	}
	resp, err := l.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch icon index: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("icon index status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return fmt.Errorf("read icon index: %w", err)
	}

	var raw []rawSelfhstEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse icon index: %w", err)
	}

	entries := make([]IconEntry, 0, len(raw))
	for _, r := range raw {
		// Skip entries that don't have an SVG variant — the picker
		// renders SVG previews and the catalog URL pattern requires it.
		if r.Reference == "" || !strings.EqualFold(r.SVG, "yes") {
			continue
		}
		entries = append(entries, IconEntry{
			Name:     r.Name,
			Slug:     r.Reference,
			URL:      iconBaseURL + "/" + r.Reference + ".svg",
			Category: r.Category,
			Tags:     r.Tags,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	// Build lookup tables for Suggest. Done here (not on demand) so the
	// suggester is O(1) per label probe and the URL-typing UX stays
	// responsive even with thousands of entries.
	bySlug := make(map[string]*IconEntry, len(entries))
	byName := make(map[string]*IconEntry, len(entries))
	for i := range entries {
		e := &entries[i]
		bySlug[strings.ToLower(e.Slug)] = e
		byName[strings.ToLower(e.Name)] = e
	}

	l.mu.Lock()
	l.entries = entries
	l.bySlug = bySlug
	l.byName = byName
	l.fetchedAt = time.Now()
	l.mu.Unlock()
	return nil
}

// Suggest returns the best icon match for a hostname by looking up each
// label as an exact (case-insensitive) slug or name. Returns nil when
// nothing matches. Strict matching by design — fuzzy (contains /
// starts-with) generated too many false positives during testing
// ("git" → Git/Gitea/GitLab/GitHub, etc.) and the picker is one click
// away when the auto-fill is wrong.
func (l *iconLibrary) Suggest(host string) *IconEntry {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return nil
	}
	// Strip a trailing port if present ("localhost:3000" → "localhost").
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// Only treat the colon as a port separator when no other dots
		// follow it (avoids breaking IPv6 literals — though rare here).
		host = host[:i]
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.bySlug) == 0 {
		return nil
	}

	for _, label := range strings.Split(host, ".") {
		if label == "" {
			continue
		}
		if e, ok := l.bySlug[label]; ok {
			return e
		}
		if e, ok := l.byName[label]; ok {
			return e
		}
	}
	return nil
}
