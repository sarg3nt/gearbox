package geoip

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Location represents geographic location data for an IP.
type Location struct {
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
}

// ipAPIResponse represents the response from ip-api.com.
type ipAPIResponse struct {
	Status      string `json:"status"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
}

// Client provides GeoIP lookup functionality with caching.
type Client struct {
	cache      map[string]*cacheEntry
	cacheMu    sync.RWMutex
	httpClient *http.Client
	cacheTTL   time.Duration
}

type cacheEntry struct {
	location  *Location
	expiresAt time.Time
}

// NewClient creates a new GeoIP client with caching.
func NewClient() *Client {
	return &Client{
		cache: make(map[string]*cacheEntry),
		httpClient: &http.Client{
			Timeout: 2 * time.Second, // Short timeout to not block traffic data
		},
		cacheTTL: 24 * time.Hour, // Cache for 24 hours
	}
}

// isPrivateIP checks if an IP address is private/local.
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// Check for loopback
	if ip.IsLoopback() {
		return true
	}

	// Check for private ranges
	privateRanges := []string{
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"fc00::/7",       // IPv6 unique local addr
		"fe80::/10",      // IPv6 link-local
	}

	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet != nil && subnet.Contains(ip) {
			return true
		}
	}

	return false
}

// Lookup returns the geographic location for an IP address.
// Results are cached to minimize external API calls.
// Never returns nil - returns "Location not found" on any error.
func (c *Client) Lookup(ip string) *Location {
	// Check if it's a private/local IP first
	if isPrivateIP(ip) {
		return &Location{
			Country:     "Local Network",
			CountryCode: "LAN",
		}
	}

	// Check cache first
	c.cacheMu.RLock()
	if entry, ok := c.cache[ip]; ok && time.Now().Before(entry.expiresAt) {
		c.cacheMu.RUnlock()
		return entry.location
	}
	c.cacheMu.RUnlock()

	// Fetch from API (with fallback on any error)
	location := c.fetchFromAPI(ip)

	// Cache the result
	c.cacheMu.Lock()
	c.cache[ip] = &cacheEntry{
		location:  location,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
	c.cacheMu.Unlock()

	return location
}

// LookupAsync performs a lookup in a goroutine and doesn't block.
// Use this when you want to pre-populate the cache.
func (c *Client) LookupAsync(ip string) {
	go func() {
		c.Lookup(ip)
	}()
}

// GetCached returns cached location if available, nil otherwise.
// This never makes an API call.
func (c *Client) GetCached(ip string) *Location {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	if entry, ok := c.cache[ip]; ok && time.Now().Before(entry.expiresAt) {
		return entry.location
	}
	return nil
}

// LookupBatch looks up multiple IPs concurrently and returns a map of results.
// This is more efficient for enriching multiple sources at once.
// Never blocks for too long - uses timeouts and returns "Location not found" on errors.
func (c *Client) LookupBatch(ips []string) map[string]*Location {
	results := make(map[string]*Location)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrent lookups to avoid rate limiting
	semaphore := make(chan struct{}, 10)

	for _, ip := range ips {
		// Check cache first
		if cached := c.GetCached(ip); cached != nil {
			mu.Lock()
			results[ip] = cached
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(ipAddr string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			location := c.Lookup(ipAddr)
			mu.Lock()
			results[ipAddr] = location
			mu.Unlock()
		}(ip)
	}

	wg.Wait()
	return results
}

func (c *Client) fetchFromAPI(ip string) *Location {
	// Use ip-api.com (free tier: 45 requests/minute)
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,countryCode", ip)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return &Location{Country: "Location not found", CountryCode: "??"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &Location{Country: "Location not found", CountryCode: "??"}
	}

	var apiResp ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return &Location{Country: "Location not found", CountryCode: "??"}
	}

	if apiResp.Status != "success" {
		return &Location{Country: "Location not found", CountryCode: "??"}
	}

	return &Location{
		Country:     apiResp.Country,
		CountryCode: apiResp.CountryCode,
	}
}

// CleanupExpired removes expired entries from the cache.
func (c *Client) CleanupExpired() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	now := time.Now()
	for ip, entry := range c.cache {
		if now.After(entry.expiresAt) {
			delete(c.cache, ip)
		}
	}
}
