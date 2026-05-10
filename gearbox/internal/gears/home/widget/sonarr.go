package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func init() {
	Register(&arrProvider{slug: "sonarr", apiVersion: "v3", isMovies: false})
	Register(&arrProvider{slug: "radarr", apiVersion: "v3", isMovies: true})
	Register(&arrProvider{slug: "prowlarr", apiVersion: "v1", isProwlarr: true})
	Register(&arrProvider{slug: "lidarr", apiVersion: "v1", isMusic: true})
	Register(&arrProvider{slug: "readarr", apiVersion: "v1", isBooks: true})
}

// arrProvider implements the Sonarr/Radarr/Prowlarr/Lidarr/Readarr family
// — they share the same API shape and authentication model (X-Api-Key header).
type arrProvider struct {
	slug       string
	apiVersion string
	isMovies   bool
	isMusic    bool
	isBooks    bool
	isProwlarr bool
}

func (p *arrProvider) Slug() string { return p.slug }

func (p *arrProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	if req.Secret == "" {
		return Result{}, ErrNotConfigured
	}
	res := Result{Fields: make(map[string]string)}

	if p.isProwlarr {
		// Prowlarr's stats live at /api/v1/indexerstats.
		stats, err := p.getJSON(ctx, req, "/api/v1/indexerstats")
		if err != nil {
			res.Err = err
			return res, nil
		}
		var totalGrabs, totalQueries, totalFailed int64
		var indexerCount int
		if hosts, ok := stats["indexers"].([]any); ok {
			indexerCount = len(hosts)
			for _, h := range hosts {
				if m, ok := h.(map[string]any); ok {
					totalGrabs += int64(asFloat(m["numberOfGrabs"]))
					totalQueries += int64(asFloat(m["numberOfQueries"]))
					totalFailed += int64(asFloat(m["numberOfFailedQueries"]))
				}
			}
		}
		res.Fields["numIndexers"] = fmt.Sprintf("%d", indexerCount)
		res.Fields["numGrabs"] = fmt.Sprintf("%d", totalGrabs)
		res.Fields["numQueries"] = fmt.Sprintf("%d", totalQueries)
		res.Fields["numFailQueries"] = fmt.Sprintf("%d", totalFailed)
		return res, nil
	}

	// Wanted/missing/queued common to Sonarr & friends.
	wanted, _ := p.getJSON(ctx, req, fmt.Sprintf("/api/%s/wanted/missing?pageSize=1", p.apiVersion))
	if total, ok := wanted["totalRecords"].(float64); ok {
		res.Fields["wanted"] = fmt.Sprintf("%.0f", total)
		res.Fields["missing"] = fmt.Sprintf("%.0f", total)
	}

	queue, _ := p.getJSON(ctx, req, fmt.Sprintf("/api/%s/queue?pageSize=1", p.apiVersion))
	if total, ok := queue["totalRecords"].(float64); ok {
		res.Fields["queued"] = fmt.Sprintf("%.0f", total)
	}

	// Series/movie/artist/book counts. These each return arrays at /<entity>.
	var entityPath, entityKey string
	switch {
	case p.isMovies:
		entityPath = fmt.Sprintf("/api/%s/movie", p.apiVersion)
		entityKey = "movies"
	case p.isMusic:
		entityPath = fmt.Sprintf("/api/%s/artist", p.apiVersion)
		entityKey = "artists"
	case p.isBooks:
		entityPath = fmt.Sprintf("/api/%s/book", p.apiVersion)
		entityKey = "books"
	default:
		entityPath = fmt.Sprintf("/api/%s/series", p.apiVersion)
		entityKey = "series"
	}
	if entityPath != "" {
		if list, err := p.getJSONArray(ctx, req, entityPath); err == nil {
			res.Fields[entityKey] = fmt.Sprintf("%d", len(list))
		}
	}
	return res, nil
}

// getJSON fetches a JSON object response.
func (p *arrProvider) getJSON(ctx context.Context, req Request, path string) (map[string]any, error) {
	body, err := p.fetch(ctx, req, path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// getJSONArray fetches a JSON array response.
func (p *arrProvider) getJSONArray(ctx context.Context, req Request, path string) ([]any, error) {
	body, err := p.fetch(ctx, req, path)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *arrProvider) fetch(ctx context.Context, req Request, path string) ([]byte, error) {
	url := strings.TrimSuffix(req.BaseURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Api-Key", req.Secret)
	httpReq.Header.Set("Accept", "application/json")
	client := req.HTTPClient
	if client == nil {
		client = DefaultClient(req.Timeout)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: HTTP %d", p.slug, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// asFloat returns the float64 value of an interface{} that may be int / int64 / float64 / string.
func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
