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
	Register(&plexProvider{})
}

// plexProvider implements the Plex Media Server API. Auth is a single
// `X-Plex-Token` header — same token Plex uses for its own API calls. The
// token is per-user and grants the user's library scope (admin tokens see
// every library; managed-user tokens see only what the admin shared).
//
// We pull two snapshots per refresh:
//   - /status/sessions  → live "now playing" stream count, transcoder usage,
//     total bandwidth across active streams.
//   - /library/sections → list of libraries; for each Movie/Show library we
//     issue a cheap totalSize-only query to get the count without paginating
//     the whole catalogue.
//
// Plex defaults to XML responses but happily emits JSON when asked nicely
// via `Accept: application/json` — so all parsing here is JSON.
type plexProvider struct{}

func (p *plexProvider) Slug() string { return "plex" }

func (p *plexProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	if req.Secret == "" {
		return Result{}, ErrNotConfigured
	}
	res := Result{Fields: make(map[string]string)}

	// /status/sessions: live streams. MediaContainer.size is the active
	// session count; each Metadata entry can carry a TranscodeSession (=
	// transcoded) and a Session.bandwidth (kbps).
	if sessions, err := p.getJSON(ctx, req, "/status/sessions"); err == nil {
		mc := mapAt(sessions, "MediaContainer")
		streamCount := int64(asFloat(mc["size"]))
		var transcodes int64
		var totalKbps float64
		if items, ok := mc["Metadata"].([]any); ok {
			for _, it := range items {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				// TranscodeSession can be either a single object or an array
				// in different Plex versions; presence is what matters.
				if _, has := m["TranscodeSession"]; has {
					transcodes++
				}
				if sess, ok := m["Session"].(map[string]any); ok {
					totalKbps += asFloat(sess["bandwidth"])
				}
			}
		}
		res.Fields["streams"] = fmt.Sprintf("%d", streamCount)
		res.Fields["transcodes"] = fmt.Sprintf("%d", transcodes)
		res.Fields["bandwidth"] = formatKbps(totalKbps)
	} else {
		res.Err = err
	}

	// /library/sections: enumerate libraries by type so we can issue
	// a per-library totalSize query.
	libs, err := p.getJSON(ctx, req, "/library/sections")
	if err != nil {
		// Streams may have succeeded — return what we have.
		if res.Err == nil {
			res.Err = err
		}
		return res, nil
	}
	dirs, _ := mapAt(libs, "MediaContainer")["Directory"].([]any)
	res.Fields["libraries"] = fmt.Sprintf("%d", len(dirs))

	var movieKeys, showKeys []string
	for _, d := range dirs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		if key == "" {
			continue
		}
		switch m["type"] {
		case "movie":
			movieKeys = append(movieKeys, key)
		case "show":
			showKeys = append(showKeys, key)
		}
	}

	if len(movieKeys) > 0 {
		res.Fields["movies"] = fmt.Sprintf("%d", p.sumTotalSize(ctx, req, movieKeys, ""))
	}
	if len(showKeys) > 0 {
		// Shows: totalSize at the section root counts series, not episodes.
		res.Fields["tv"] = fmt.Sprintf("%d", p.sumTotalSize(ctx, req, showKeys, ""))
		// type=4 narrows the same endpoint to episodes for an episode count.
		res.Fields["episodes"] = fmt.Sprintf("%d", p.sumTotalSize(ctx, req, showKeys, "?type=4"))
	}

	return res, nil
}

// sumTotalSize returns the summed MediaContainer.totalSize across the given
// section keys. We pass `X-Plex-Container-Size=0` so the server reports the
// totalSize without serializing any actual items — the response is tiny no
// matter how many movies the user owns.
func (p *plexProvider) sumTotalSize(ctx context.Context, req Request, keys []string, extra string) int64 {
	var total int64
	for _, k := range keys {
		path := fmt.Sprintf("/library/sections/%s/all%s", k, sizeZero(extra))
		body, err := p.getJSON(ctx, req, path)
		if err != nil {
			continue
		}
		total += int64(asFloat(mapAt(body, "MediaContainer")["totalSize"]))
	}
	return total
}

// sizeZero appends X-Plex-Container-Size=0 to a path, merging with an
// existing query string if one is present.
func sizeZero(extra string) string {
	const sizeQS = "X-Plex-Container-Size=0&X-Plex-Container-Start=0"
	if extra == "" {
		return "?" + sizeQS
	}
	if strings.HasPrefix(extra, "?") {
		return extra + "&" + sizeQS
	}
	return "?" + extra + "&" + sizeQS
}

func (p *plexProvider) getJSON(ctx context.Context, req Request, path string) (map[string]any, error) {
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

func (p *plexProvider) fetch(ctx context.Context, req Request, path string) ([]byte, error) {
	url := strings.TrimSuffix(req.BaseURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Plex-Token", req.Secret)
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
		return nil, fmt.Errorf("plex %s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// mapAt returns m[key] as map[string]any, or an empty map when missing /
// not an object. Keeps the caller free from nil-checking ladders.
func mapAt(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

// formatKbps renders a kilobits-per-second total as a human-readable
// bandwidth string. Plex reports session bandwidth in kbps already, so we
// only need to scale up to Mbps / Gbps for readability.
func formatKbps(kbps float64) string {
	switch {
	case kbps <= 0:
		return "0"
	case kbps < 1000:
		return fmt.Sprintf("%.0f Kbps", kbps)
	case kbps < 1000*1000:
		return fmt.Sprintf("%.1f Mbps", kbps/1000)
	default:
		return fmt.Sprintf("%.1f Gbps", kbps/1000/1000)
	}
}
