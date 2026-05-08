package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

func init() {
	Register(&qbitProvider{})
}

// qbitProvider implements the qBittorrent Web API. qBit doesn't accept an
// API-key header — you POST credentials to /api/v2/auth/login and get back
// a session cookie. We use a per-request CookieJar so the secret never
// outlasts the fetch.
type qbitProvider struct{}

func (p *qbitProvider) Slug() string { return "qbittorrent" }

func (p *qbitProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	res := Result{Fields: make(map[string]string)}
	if req.Secret == "" {
		// qBit allows anonymous local access in some configs, but the
		// default is auth-required. Treat missing secret as "not configured".
		return res, ErrNotConfigured
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return res, err
	}
	client := req.HTTPClient
	if client == nil {
		client = DefaultClient(req.Timeout)
	}
	// Clone with a cookie jar attached.
	c := &http.Client{
		Timeout:   client.Timeout,
		Transport: client.Transport,
		Jar:       jar,
	}

	base := strings.TrimSuffix(req.BaseURL, "/")

	// Authenticate.
	form := url.Values{}
	user := req.BasicUsername
	if user == "" {
		user = "admin"
	}
	form.Set("username", user)
	form.Set("password", req.Secret)
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return res, err
	}
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("Referer", base)
	loginResp, err := c.Do(loginReq)
	if err != nil {
		return res, err
	}
	body, _ := io.ReadAll(io.LimitReader(loginResp.Body, 1<<10))
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "Ok." {
		return res, fmt.Errorf("qbittorrent login failed: %d %s", loginResp.StatusCode, string(body))
	}

	// Fetch global transfer info: dl_info_speed, up_info_speed.
	xfer, err := p.getJSONObject(ctx, c, base+"/api/v2/transfer/info")
	if err == nil {
		if dl, ok := xfer["dl_info_speed"].(float64); ok {
			res.Fields["download"] = formatBytesPerSec(dl)
		}
		if up, ok := xfer["up_info_speed"].(float64); ok {
			res.Fields["upload"] = formatBytesPerSec(up)
		}
	}

	// Torrents list — count active leechers and seeders.
	torrents, err := p.getJSONArray(ctx, c, base+"/api/v2/torrents/info")
	if err == nil {
		var leech, seed int
		for _, t := range torrents {
			m, ok := t.(map[string]any)
			if !ok {
				continue
			}
			state, _ := m["state"].(string)
			switch state {
			case "downloading", "metaDL", "stalledDL", "queuedDL", "checkingDL":
				leech++
			case "uploading", "stalledUP", "queuedUP", "checkingUP":
				seed++
			}
		}
		res.Fields["leech"] = fmt.Sprintf("%d", leech)
		res.Fields["seed"] = fmt.Sprintf("%d", seed)
	}

	return res, nil
}

func (p *qbitProvider) getJSONObject(ctx context.Context, c *http.Client, url string) (map[string]any, error) {
	body, err := p.get(ctx, c, url)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *qbitProvider) getJSONArray(ctx context.Context, c *http.Client, url string) ([]any, error) {
	body, err := p.get(ctx, c, url)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *qbitProvider) get(ctx context.Context, c *http.Client, urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("qbittorrent %s: HTTP %d", urlStr, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// formatBytesPerSec turns bytes/s into a human-readable rate.
func formatBytesPerSec(bytes float64) string {
	const k = 1024.0
	switch {
	case bytes < k:
		return fmt.Sprintf("%.0f B/s", bytes)
	case bytes < k*k:
		return fmt.Sprintf("%.1f KB/s", bytes/k)
	case bytes < k*k*k:
		return fmt.Sprintf("%.1f MB/s", bytes/k/k)
	default:
		return fmt.Sprintf("%.1f GB/s", bytes/k/k/k)
	}
}
