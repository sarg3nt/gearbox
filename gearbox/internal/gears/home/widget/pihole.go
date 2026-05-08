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
	Register(&piholeProvider{})
}

// piholeProvider hits the v5 admin API. Pi-hole 5.x uses /admin/api.php
// with a "auth=<api-token>" query param. Pi-hole v6 changed the API; this
// provider targets v5 (the most widely deployed) for v1.
type piholeProvider struct{}

func (p *piholeProvider) Slug() string { return "pihole" }

func (p *piholeProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	res := Result{Fields: make(map[string]string)}
	if req.Secret == "" {
		return res, ErrNotConfigured
	}

	url := strings.TrimSuffix(req.BaseURL, "/") + "/admin/api.php?summaryRaw&auth=" + req.Secret
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return res, err
	}
	client := req.HTTPClient
	if client == nil {
		client = DefaultClient(req.Timeout)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return res, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return res, fmt.Errorf("pihole: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return res, err
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return res, err
	}

	if v, ok := doc["dns_queries_today"].(float64); ok {
		res.Fields["queries"] = fmt.Sprintf("%.0f", v)
	}
	if v, ok := doc["ads_blocked_today"].(float64); ok {
		res.Fields["blocked"] = fmt.Sprintf("%.0f", v)
	}
	if v, ok := doc["ads_percentage_today"].(float64); ok {
		res.Fields["blocked_percent"] = fmt.Sprintf("%.1f%%", v)
	}
	if v, ok := doc["domains_being_blocked"].(float64); ok {
		res.Fields["gravity"] = fmt.Sprintf("%.0f", v)
	}
	return res, nil
}
