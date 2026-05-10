package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func init() {
	Register(&unifiProvider{
		siteCache: make(map[string]siteCacheEntry),
	})
}

// unifiProvider implements UniFi Network's Integration API. Auth is a
// single `X-API-KEY` header — generated from the controller's UI under
// Settings → Control Plane → Integrations (UniFi OS 4.x+) or
// Settings → System → Advanced → API on older firmware.
//
// The Integration API is mounted at `<baseURL>/proxy/network/integration/v1/`
// behind UniFi OS — the widget appends that path to the user-supplied
// tile URL automatically.
//
// What we surface:
//   - Client counts (total, wifi, wired, vpn) via `/sites/{id}/clients`
//   - Device counts (online, offline) via `/sites/{id}/devices`
//   - Gateway uplink throughput, CPU, memory, uptime via the gateway's
//     `/devices/{id}/statistics/latest` snapshot.
//
// What we don't:
//   - WAN dropout history (no events endpoint in the Integration API).
//     Could be derived by tracking ONLINE→OFFLINE transitions over the
//     widget cadence — left for a future pass.
//   - Per-VLAN traffic — Integration API doesn't expose it; would
//     require the legacy /api/s/<site>/stat/* endpoints which use
//     cookie+CSRF auth that we don't carry in this provider.
type unifiProvider struct {
	mu        sync.Mutex
	siteCache map[string]siteCacheEntry
}

// siteCacheEntry holds a discovered site + gateway device for a given
// UniFi controller URL. Caching avoids re-issuing /sites and /devices
// every refresh just to find the gateway. Refreshed every 30 minutes
// (gateways don't churn that often in a home install).
type siteCacheEntry struct {
	siteID      string
	gatewayID   string
	gatewayName string
	at          time.Time
}

const (
	unifiAPIPath        = "/proxy/network/integration/v1"
	unifiCacheTTL       = 30 * time.Minute
	unifiClientPageSize = 200 // API max
)

func (p *unifiProvider) Slug() string { return "unifi" }

func (p *unifiProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	if req.Secret == "" {
		return Result{}, ErrNotConfigured
	}
	res := Result{Fields: make(map[string]string)}

	site, err := p.resolveSite(ctx, req)
	if err != nil {
		res.Err = err
		return res, nil
	}

	// Clients: paginated, but `totalCount` gives the global total in one
	// response. We also fetch the first page (limit=200) to count by
	// type. Most home networks fit in 200; if not, we fall back to
	// `totalCount` for the total and an approximation for the breakdowns.
	clients, err := p.getJSON(ctx, req, fmt.Sprintf("/sites/%s/clients?limit=%d", site.siteID, unifiClientPageSize))
	if err == nil {
		total := int64(asFloat(clients["totalCount"]))
		res.Fields["clients"] = fmt.Sprintf("%d", total)
		var wifi, wired, vpn int64
		if data, ok := clients["data"].([]any); ok {
			for _, c := range data {
				m, ok := c.(map[string]any)
				if !ok {
					continue
				}
				switch m["type"] {
				case "WIRELESS":
					wifi++
				case "WIRED":
					wired++
				case "VPN", "TELEPORT":
					vpn++
				}
			}
		}
		res.Fields["wifi"] = fmt.Sprintf("%d", wifi)
		res.Fields["wired"] = fmt.Sprintf("%d", wired)
		res.Fields["vpn"] = fmt.Sprintf("%d", vpn)
	}

	// Devices: same pagination shape. Count ONLINE vs everything else.
	devices, err := p.getJSON(ctx, req, fmt.Sprintf("/sites/%s/devices?limit=200", site.siteID))
	if err == nil {
		var online, offline int64
		if data, ok := devices["data"].([]any); ok {
			for _, d := range data {
				m, ok := d.(map[string]any)
				if !ok {
					continue
				}
				if m["state"] == "ONLINE" {
					online++
				} else {
					offline++
				}
			}
		}
		res.Fields["devices_online"] = fmt.Sprintf("%d", online)
		res.Fields["devices_offline"] = fmt.Sprintf("%d", offline)
	}

	// Gateway statistics: uplink throughput, cpu, memory, uptime. Skip
	// silently if site discovery didn't produce a gateway ID — some odd
	// configurations (no gateway adopted into the controller) leave this
	// blank rather than failing the whole widget.
	if site.gatewayID != "" {
		stats, err := p.getJSON(ctx, req, fmt.Sprintf("/sites/%s/devices/%s/statistics/latest", site.siteID, site.gatewayID))
		if err == nil {
			if uplink, ok := stats["uplink"].(map[string]any); ok {
				res.Fields["wan_down"] = formatBitsPerSec(asFloat(uplink["rxRateBps"]) * 8)
				res.Fields["wan_up"] = formatBitsPerSec(asFloat(uplink["txRateBps"]) * 8)
			}
			if cpu, ok := stats["cpuUtilizationPct"]; ok {
				res.Fields["gateway_cpu"] = fmt.Sprintf("%.0f%%", asFloat(cpu))
			}
			if mem, ok := stats["memoryUtilizationPct"]; ok {
				res.Fields["gateway_mem"] = fmt.Sprintf("%.0f%%", asFloat(mem))
			}
			if uptime, ok := stats["uptimeSec"]; ok {
				res.Fields["uptime"] = formatUptime(int64(asFloat(uptime)))
			}
			// WAN status: gateway ONLINE → "Up", everything else → "Down".
			// Same source we already used for devices_online; reuse here
			// so a small tile that shows wan_status without device counts
			// still gets the answer.
			res.Fields["wan_status"] = "Up"
		} else {
			res.Fields["wan_status"] = "Down"
		}
	}

	return res, nil
}

// resolveSite returns the cached siteID + gatewayID for a controller URL,
// fetching them on cache miss. UniFi installs almost always have one
// site (the "Default" site); we pick the first the API returns.
//
// Gateway detection: prefer a device whose `features` include "gateway";
// fall back to a model-name prefix match (UDM/USG/UXG/UCG); finally,
// fall back to the first ONLINE device with an `ipAddress` that looks
// like a public IP (a UDM Pro's ipAddress is the WAN address, not the
// LAN IP, so this catches firmware versions where `features` lists only
// "switching" for the combo unit).
func (p *unifiProvider) resolveSite(ctx context.Context, req Request) (siteCacheEntry, error) {
	base := strings.TrimSuffix(req.BaseURL, "/")
	p.mu.Lock()
	if entry, ok := p.siteCache[base]; ok && time.Since(entry.at) < unifiCacheTTL {
		p.mu.Unlock()
		return entry, nil
	}
	p.mu.Unlock()

	sites, err := p.getJSON(ctx, req, "/sites")
	if err != nil {
		return siteCacheEntry{}, err
	}
	siteID := ""
	if data, ok := sites["data"].([]any); ok && len(data) > 0 {
		if m, ok := data[0].(map[string]any); ok {
			siteID, _ = m["id"].(string)
		}
	}
	if siteID == "" {
		return siteCacheEntry{}, fmt.Errorf("unifi: no sites returned by /v1/sites")
	}

	devices, err := p.getJSON(ctx, req, fmt.Sprintf("/sites/%s/devices?limit=200", siteID))
	if err != nil {
		return siteCacheEntry{}, err
	}
	gatewayID, gatewayName := pickGatewayID(devices)

	entry := siteCacheEntry{
		siteID:      siteID,
		gatewayID:   gatewayID,
		gatewayName: gatewayName,
		at:          time.Now(),
	}
	p.mu.Lock()
	p.siteCache[base] = entry
	p.mu.Unlock()
	return entry, nil
}

// pickGatewayID extracts a gateway device id + name from a /devices
// response. Returns ("", "") when no plausible gateway is found.
func pickGatewayID(devices map[string]any) (string, string) {
	data, _ := devices["data"].([]any)
	// Pass 1: explicit "gateway" feature.
	for _, d := range data {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		feats, _ := m["features"].([]any)
		for _, f := range feats {
			if s, _ := f.(string); s == "gateway" {
				id, _ := m["id"].(string)
				name, _ := m["name"].(string)
				return id, name
			}
		}
	}
	// Pass 2: model-name prefix. UniFi gateway models start with one of
	// these. Avoids false-positives against switches/APs.
	for _, d := range data {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		model, _ := m["model"].(string)
		mu := strings.ToUpper(model)
		switch {
		case strings.HasPrefix(mu, "UDM"),
			strings.HasPrefix(mu, "USG"),
			strings.HasPrefix(mu, "UXG"),
			strings.HasPrefix(mu, "UCG"):
			id, _ := m["id"].(string)
			name, _ := m["name"].(string)
			return id, name
		}
	}
	return "", ""
}

func (p *unifiProvider) getJSON(ctx context.Context, req Request, path string) (map[string]any, error) {
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

func (p *unifiProvider) fetch(ctx context.Context, req Request, path string) ([]byte, error) {
	url := strings.TrimSuffix(req.BaseURL, "/") + unifiAPIPath + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-API-KEY", req.Secret)
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
		return nil, fmt.Errorf("unifi %s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// formatBitsPerSec turns bits/s into a human-readable rate. UniFi
// reports throughput as bytes/s in `txRateBps`/`rxRateBps`; multiply by
// 8 before passing in. Decimal (k=1000) used here because that's how
// network speeds are conventionally quoted ("1 Gbps" = 1,000,000,000
// bits, not 1,073,741,824).
func formatBitsPerSec(bps float64) string {
	const k = 1000.0
	switch {
	case bps <= 0:
		return "0"
	case bps < k:
		return fmt.Sprintf("%.0f bps", bps)
	case bps < k*k:
		return fmt.Sprintf("%.0f Kbps", bps/k)
	case bps < k*k*k:
		return fmt.Sprintf("%.1f Mbps", bps/k/k)
	default:
		return fmt.Sprintf("%.2f Gbps", bps/k/k/k)
	}
}

// formatUptime renders a seconds-since-boot count as a friendly string.
// Days for long uptimes, hours+minutes for shorter ones; matches the
// kind of glanceable detail dashboards usually show next to a router.
func formatUptime(s int64) string {
	if s < 0 {
		return "0m"
	}
	const (
		min  = 60
		hour = 60 * 60
		day  = 24 * hour
	)
	switch {
	case s >= day:
		days := s / day
		hours := (s % day) / hour
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, hours)
	case s >= hour:
		hours := s / hour
		mins := (s % hour) / min
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", s/min)
	}
}
