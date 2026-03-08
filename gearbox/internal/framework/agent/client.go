package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// DefaultLogLines is the default number of log lines to fetch.
	DefaultLogLines = 500

	// MaxLogLines is the maximum number of log lines that can be requested.
	MaxLogLines = 10000
)

// Client is an HTTP client for the HAProxy Agent API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new HAProxy Agent API client.
func NewClient(baseURL, apiKey string) *Client {
	return NewClientWithTimeout(baseURL, apiKey, DefaultTimeout)
}

// NewClientWithTimeout creates a new HAProxy Agent API client with a custom timeout.
func NewClientWithTimeout(baseURL, apiKey string, timeout time.Duration) *Client {
	// Ensure baseURL doesn't have trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	// SECURITY FIX: Implement certificate pinning instead of InsecureSkipVerify
	// Create TLS configuration with certificate verification
	tlsConfig := createTLSConfig()

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}

			// Retry up to 3 times on "no route to host" errors.
			// macOS with multiple NICs on the same subnet can have
			// intermittent routing failures due to IFSCOPE route expiry.
			var lastErr error
			for attempt := range 3 {
				conn, err := dialer.DialContext(ctx, network, addr)
				if err == nil {
					if attempt > 0 {
						log.Printf("[agent-client] dial succeeded on attempt %d for %s", attempt+1, addr)
					}
					return conn, nil
				}
				lastErr = err

				// Only retry on "no route to host" errors
				if !strings.Contains(err.Error(), "no route to host") {
					return nil, err
				}
				log.Printf("[agent-client] dial attempt %d failed for %s: %v, retrying...", attempt+1, addr, err)

				// Brief pause before retry to allow route refresh
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(500 * time.Millisecond):
				}
			}
			return nil, lastErr
		},
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// createTLSConfig creates a TLS configuration with certificate verification.
// Supports three modes via environment variables:
// 1. AGENT_CA_CERT_PATH: Path to CA certificate for validation (RECOMMENDED)
// 2. GEARBOX_INSECURE_TLS=true: Skip verification (NOT RECOMMENDED for production)
// 3. Default: Use system certificate pool
func createTLSConfig() *tls.Config {
	// Check if user wants to skip TLS verification (insecure mode)
	if os.Getenv("GEARBOX_INSECURE_TLS") == "true" {
		// Log warning about insecure mode
		// Note: In production code, use a proper logger
		fmt.Fprintf(os.Stderr, "WARNING: TLS certificate verification is DISABLED (GEARBOX_INSECURE_TLS=true)\n")
		fmt.Fprintf(os.Stderr, "WARNING: This is NOT recommended for production use\n")
		return &tls.Config{
			InsecureSkipVerify: true, //#nosec G402 -- User explicitly opted in via GEARBOX_INSECURE_TLS env var
			MinVersion:         tls.VersionTLS12,
		}
	}

	// Check if custom CA certificate is provided
	caCertPath := os.Getenv("AGENT_CA_CERT_PATH")
	if caCertPath != "" {
		// Load CA certificate for validating agent certificates
		caCertPEM, err := os.ReadFile(caCertPath) //#nosec G304 -- Path from trusted env var AGENT_CA_CERT_PATH
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to read CA certificate from %s: %v\n", caCertPath, err)
			fmt.Fprintf(os.Stderr, "ERROR: Falling back to system certificate pool\n")
			return &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		}

		// Create certificate pool with the CA certificate
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCertPEM) {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to parse CA certificate from %s\n", caCertPath)
			fmt.Fprintf(os.Stderr, "ERROR: Falling back to system certificate pool\n")
			return &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		}

		// Return TLS config with custom CA pool
		return &tls.Config{
			RootCAs:    caCertPool,
			MinVersion: tls.VersionTLS12,
		}
	}

	// Default: Use system certificate pool with TLS 1.2+
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
}

// LongOperationTimeout is used for operations like apt update/install that may take several minutes.
const LongOperationTimeout = 5 * time.Minute

// parseErrorMessage extracts a meaningful error message from an HTTP error response body.
// It tries, in order:
//  1. JSON {"error": "..."} — used by structured API responses
//  2. JSON {"message": "..."} — used by jsonError responses
//  3. Plain text body (trimmed) — used by http.Error() in agent handlers
//  4. Generic "HTTP <code>: <status>" fallback
func parseErrorMessage(body []byte, statusCode int) string {
	// Try JSON with "error" field
	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return errResp.Error
	}

	// Try JSON with "message" field
	var msgResp struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &msgResp) == nil && msgResp.Message != "" {
		return msgResp.Message
	}

	// Fall back to plain text body (http.Error sends text/plain)
	if text := strings.TrimSpace(string(body)); text != "" {
		return text
	}

	// Generic fallback
	return fmt.Sprintf("HTTP %d: %s", statusCode, http.StatusText(statusCode))
}

// doRequest performs an HTTP request with authentication.
func (c *Client) doRequest(method, path string, query url.Values) ([]byte, error) {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add bearer token authentication
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle error status codes
	if resp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    parseErrorMessage(body, resp.StatusCode),
		}
	}

	return body, nil
}

// doRequestLongRunning performs an HTTP request with an extended timeout for long-running operations
// like apt update/install that may take several minutes.
func (c *Client) doRequestLongRunning(method, path string, query url.Values) ([]byte, error) {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	ctx, cancel := context.WithTimeout(context.Background(), LongOperationTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	// Use a separate client with extended timeout, sharing the same transport
	longClient := &http.Client{
		Timeout:   LongOperationTimeout,
		Transport: c.httpClient.Transport,
	}

	resp, err := longClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    parseErrorMessage(body, resp.StatusCode),
		}
	}

	return body, nil
}

// doRequestWithBody performs an HTTP request with a JSON body.
func (c *Client) doRequestWithBody(method, path string, reqBody interface{}) ([]byte, error) {
	return c.doRequestWithBodyAndQuery(method, path, reqBody, nil)
}



// doRequestWithBodyAndQuery performs an HTTP request with a JSON body and query parameters.
func (c *Client) doRequestWithBodyAndQuery(method, path string, reqBody interface{}, query url.Values) ([]byte, error) {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = strings.NewReader(string(jsonBody))
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    parseErrorMessage(body, resp.StatusCode),
		}
	}

	return body, nil
}

// doRequestNoAuth performs an HTTP request without authentication.
func (c *Client) doRequestNoAuth(method, path string) ([]byte, error) {
	fullURL := c.baseURL + path

	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}
	}

	return body, nil
}

// Health checks if the agent is reachable (no authentication required).
func (c *Client) Health() (*HealthResponse, error) {
	body, err := c.doRequestNoAuth("GET", "/health")
	if err != nil {
		return nil, err
	}

	var resp HealthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse health response: %w", err)
	}

	return &resp, nil
}

// GetInfo retrieves HAProxy runtime information.
func (c *Client) GetInfo() (*RuntimeInfo, error) {
	body, err := c.doRequest("GET", "/api/v1/haproxy/info", nil)
	if err != nil {
		return nil, err
	}

	var resp RuntimeInfo
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse info response: %w", err)
	}

	return &resp, nil
}

// GetStats retrieves HAProxy statistics as parsed JSON.
func (c *Client) GetStats() (*StatsResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/haproxy/stats", nil)
	if err != nil {
		return nil, err
	}

	var resp StatsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse stats response: %w", err)
	}

	return &resp, nil
}

// GetStatsCSV retrieves HAProxy statistics in raw CSV format.
func (c *Client) GetStatsCSV() (string, error) {
	query := url.Values{}
	query.Set("format", "csv")

	body, err := c.doRequest("GET", "/api/v1/haproxy/stats", query)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// GetStickTables retrieves HAProxy stick table information.
func (c *Client) GetStickTables() (*StickTablesResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/haproxy/tables", nil)
	if err != nil {
		return nil, err
	}

	var resp StickTablesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse stick tables response: %w", err)
	}

	return &resp, nil
}

// ValidateConfig validates the HAProxy configuration.
func (c *Client) ValidateConfig() (*ConfigValidationResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/haproxy/validate", nil)
	if err != nil {
		return nil, err
	}

	var resp ConfigValidationResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse validation response: %w", err)
	}

	return &resp, nil
}

// GetMetrics retrieves system metrics.
func (c *Client) GetMetrics() (*MetricsResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/metrics", nil)
	if err != nil {
		return nil, err
	}

	var resp MetricsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse metrics response: %w", err)
	}

	return &resp, nil
}

// GetServices retrieves the status of specified services.
// If services is nil or empty, returns the default monitored services.
func (c *Client) GetServices(services []string) (*ServicesResponse, error) {
	var query url.Values
	if len(services) > 0 {
		query = url.Values{}
		query.Set("services", strings.Join(services, ","))
	}

	body, err := c.doRequest("GET", "/api/v1/services", query)
	if err != nil {
		return nil, err
	}

	var resp ServicesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse services response: %w", err)
	}

	return &resp, nil
}

// GetAvailableServices retrieves the list of all available systemd services on the target system.
func (c *Client) GetAvailableServices() (*AvailableServicesResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/services/available", nil)
	if err != nil {
		return nil, err
	}

	var resp AvailableServicesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse available services response: %w", err)
	}

	return &resp, nil
}

// GetLogSources retrieves the list of available log sources.
func (c *Client) GetLogSources() (*LogSourcesResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/logs", nil)
	if err != nil {
		return nil, err
	}

	var resp LogSourcesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse log sources response: %w", err)
	}

	return &resp, nil
}

// GetLogs retrieves logs from a specific source.
// Lines must be between 1 and 10000, or 0 to use the default (500).
func (c *Client) GetLogs(name string, lines int) (*LogResponse, error) {
	if lines < 0 {
		lines = DefaultLogLines
	}
	if lines > MaxLogLines {
		lines = MaxLogLines
	}

	query := url.Values{}
	if lines > 0 {
		query.Set("lines", strconv.Itoa(lines))
	}

	path := "/api/v1/logs/" + url.PathEscape(name)
	body, err := c.doRequest("GET", path, query)
	if err != nil {
		return nil, err
	}

	var resp LogResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse log response: %w", err)
	}

	return &resp, nil
}

// GetSecuritySummary retrieves a brief security overview.
func (c *Client) GetSecuritySummary() (*SecuritySummary, error) {
	body, err := c.doRequest("GET", "/api/v1/security/summary", nil)
	if err != nil {
		return nil, err
	}

	var resp SecuritySummary
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse security summary response: %w", err)
	}

	return &resp, nil
}

// Fail2BanOptions configures the GetFail2BanStats request.
type Fail2BanOptions struct {
	IncludeIPs bool // Include banned IP addresses
	Recent     int  // Number of recent bans to include (0-100)
}

// GetFail2BanStats retrieves detailed fail2ban statistics.
func (c *Client) GetFail2BanStats(opts *Fail2BanOptions) (*Fail2BanStats, error) {
	var query url.Values
	if opts != nil {
		query = url.Values{}
		if opts.IncludeIPs {
			query.Set("include_ips", "true")
		}
		if opts.Recent > 0 {
			if opts.Recent > 100 {
				opts.Recent = 100
			}
			query.Set("recent", strconv.Itoa(opts.Recent))
		}
	}

	body, err := c.doRequest("GET", "/api/v1/security/fail2ban", query)
	if err != nil {
		return nil, err
	}

	var resp Fail2BanStats
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse fail2ban stats response: %w", err)
	}

	return &resp, nil
}

// FirewallOptions configures the GetFirewallStats request.
type FirewallOptions struct {
	IncludeRules bool // Include firewall rules
	Recent       int  // Number of recent blocks to include (0-100)
}

// GetFirewallStats retrieves detailed firewall statistics.
func (c *Client) GetFirewallStats(opts *FirewallOptions) (*FirewallStats, error) {
	var query url.Values
	if opts != nil {
		query = url.Values{}
		if opts.IncludeRules {
			query.Set("include_rules", "true")
		}
		if opts.Recent > 0 {
			if opts.Recent > 100 {
				opts.Recent = 100
			}
			query.Set("recent", strconv.Itoa(opts.Recent))
		}
	}

	body, err := c.doRequest("GET", "/api/v1/security/firewall", query)
	if err != nil {
		return nil, err
	}

	var resp FirewallStats
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse firewall stats response: %w", err)
	}

	return &resp, nil
}

// GetSyncStatus retrieves the git sync status.
func (c *Client) GetSyncStatus() (*SyncStatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/sync/status", nil)
	if err != nil {
		return nil, err
	}

	var resp SyncStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse sync status response: %w", err)
	}

	return &resp, nil
}

// GetMetadata retrieves HAProxy configuration metadata.
func (c *Client) GetMetadata() (*MetadataResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/metadata", nil)
	if err != nil {
		return nil, err
	}

	var resp MetadataResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse metadata response: %w", err)
	}

	return &resp, nil
}

// GetWebSocketInfo retrieves WebSocket configuration information.
func (c *Client) GetWebSocketInfo() (*WSInfoResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/events/info", nil)
	if err != nil {
		return nil, err
	}

	var resp WSInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse websocket info response: %w", err)
	}

	return &resp, nil
}

// GetWebSocketToken retrieves a token for WebSocket authentication.
// The token is valid for 60 seconds.
func (c *Client) GetWebSocketToken() (*WSTokenResponse, error) {
	body, err := c.doRequest("POST", "/api/v1/events/token", nil)
	if err != nil {
		return nil, err
	}

	var resp WSTokenResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse websocket token response: %w", err)
	}

	return &resp, nil
}

// GetWebhookInfo retrieves webhook configuration information.
func (c *Client) GetWebhookInfo() (*WebhookInfoResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/webhook/info", nil)
	if err != nil {
		return nil, err
	}

	var resp WebhookInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse webhook info response: %w", err)
	}

	return &resp, nil
}

// GetCertificates retrieves certificate information and metrics.
func (c *Client) GetCertificates() (*CertificatesResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/certificates", nil)
	if err != nil {
		return nil, err
	}

	var resp CertificatesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse certificates response: %w", err)
	}

	return &resp, nil
}

// RefreshCertificate triggers a certificate renewal for the specified domain.
// This will renew the certificate using acme.sh, install it to HAProxy, and reload HAProxy.
func (c *Client) RefreshCertificate(domain string) (*RefreshCertificateResponse, error) {
	path := "/api/v1/certificates/" + url.PathEscape(domain) + "/refresh"
	body, err := c.doRequest("POST", path, nil)
	if err != nil {
		return nil, err
	}

	var resp RefreshCertificateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh certificate response: %w", err)
	}

	return &resp, nil
}

// DownloadCertificate downloads a certificate PEM file for the specified domain.
// Returns the certificate data, filename, and any error.
func (c *Client) DownloadCertificate(domain string) ([]byte, string, error) {
	path := "/api/v1/certificates/" + url.PathEscape(domain) + "/download"
	fullURL := c.baseURL + path

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Extract filename from Content-Disposition header, or use default
	filename := domain + ".pem"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		// Parse "attachment; filename="example.pem""
		if strings.Contains(cd, "filename=") {
			parts := strings.Split(cd, "filename=")
			if len(parts) > 1 {
				filename = strings.Trim(parts[1], "\"")
			}
		}
	}

	return data, filename, nil
}

// GetTraffic retrieves detailed traffic analysis data.
func (c *Client) GetTraffic(limit, topN int) (*TrafficResponse, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if topN > 0 {
		query.Set("top_n", strconv.Itoa(topN))
	}

	body, err := c.doRequest("GET", "/api/v1/traffic", query)
	if err != nil {
		return nil, err
	}

	var resp TrafficResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse traffic response: %w", err)
	}

	return &resp, nil
}

// GetTrafficSummary retrieves a summary of traffic statistics.
func (c *Client) GetTrafficSummary() (*TrafficSummaryResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/traffic/summary", nil)
	if err != nil {
		return nil, err
	}

	var resp TrafficSummaryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse traffic summary response: %w", err)
	}

	return &resp, nil
}

// IsAPIError checks if an error is an API error and returns it.
func IsAPIError(err error) (*APIError, bool) {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr, true
	}
	return nil, false
}

// BlockIPRequest represents a request to block an IP address.
type BlockIPRequest struct {
	IP     string `json:"ip"`
	Reason string `json:"reason"`
}

// BlockIPResponse represents the response from blocking an IP.
type BlockIPResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	IP      string `json:"ip"`
}

// BlockedIPInfo represents information about a blocked IP.
type BlockedIPInfo struct {
	IP      string `json:"ip"`
	Packets int64  `json:"packets"`
	Bytes   int64  `json:"bytes"`
}

// BlockedIPsResponse represents the list of blocked IPs from the agent.
type BlockedIPsResponse struct {
	Available  bool            `json:"available"`
	BlockedIPs []BlockedIPInfo `json:"blocked_ips"`
	Count      int             `json:"count"`
}

// GetBlockedIPs retrieves the list of blocked IPs from the firewall.
func (c *Client) GetBlockedIPs() (*BlockedIPsResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/firewall/blocked", nil)
	if err != nil {
		return nil, err
	}

	var resp BlockedIPsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse blocked IPs response: %w", err)
	}

	return &resp, nil
}

// BlockIP blocks an IP address in the firewall.
func (c *Client) BlockIP(ip, reason string) (*BlockIPResponse, error) {
	req := BlockIPRequest{
		IP:     ip,
		Reason: reason,
	}

	body, err := c.doRequestWithBody("POST", "/api/v1/firewall/block", req)
	if err != nil {
		return nil, err
	}

	var resp BlockIPResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse block IP response: %w", err)
	}

	return &resp, nil
}

// UnblockIP removes an IP address from the firewall blocklist.
func (c *Client) UnblockIP(ip string) (*BlockIPResponse, error) {
	path := "/api/v1/firewall/block/" + url.PathEscape(ip)
	body, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return nil, err
	}

	var resp BlockIPResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse unblock IP response: %w", err)
	}

	return &resp, nil
}

// CheckIPBlocked checks if an IP address is in the firewall blocklist.
func (c *Client) CheckIPBlocked(ip string) (bool, error) {
	path := "/api/v1/firewall/blocked/" + url.PathEscape(ip)
	body, err := c.doRequest("GET", path, nil)
	if err != nil {
		return false, err
	}

	var resp struct {
		IP      string `json:"ip"`
		Blocked bool   `json:"blocked"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, fmt.Errorf("failed to parse check blocked response: %w", err)
	}

	return resp.Blocked, nil
}

// IsUnauthorized checks if an error indicates authentication failure.
func IsUnauthorized(err error) bool {
	if apiErr, ok := IsAPIError(err); ok {
		return apiErr.StatusCode == http.StatusUnauthorized
	}
	return false
}

// IsNotFound checks if an error indicates a resource was not found.
func IsNotFound(err error) bool {
	if apiErr, ok := IsAPIError(err); ok {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsServiceUnavailable checks if an error indicates the service is unavailable.
func IsServiceUnavailable(err error) bool {
	if apiErr, ok := IsAPIError(err); ok {
		return apiErr.StatusCode == http.StatusServiceUnavailable
	}
	return false
}

// IsTooManyRequests checks if an error indicates rate limiting.
func IsTooManyRequests(err error) bool {
	if apiErr, ok := IsAPIError(err); ok {
		return apiErr.StatusCode == http.StatusTooManyRequests
	}
	return false
}

// ServiceControl controls a systemd service (start, stop, restart).
func (c *Client) ServiceControl(service, action string) (*ServiceControlResponse, error) {
	reqBody := ServiceControlRequest{
		Service: service,
		Action:  action,
	}

	body, err := c.doRequestWithBody("POST", "/api/v1/services/control", reqBody)
	if err != nil {
		return nil, err
	}

	var resp ServiceControlResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse service control response: %w", err)
	}

	return &resp, nil
}

// HAProxy Config Management Methods

// GetHAProxyConfig retrieves the current HAProxy configuration.
func (c *Client) GetHAProxyConfig() (*HAProxyConfigResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/haproxy/config", nil)
	if err != nil {
		return nil, err
	}

	var resp HAProxyConfigResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse config response: %w", err)
	}

	return &resp, nil
}

// UpdateHAProxyConfig updates the HAProxy configuration.
// Note: Returns a response even on validation failure (400) - check resp.Success field.
func (c *Client) UpdateHAProxyConfig(req *HAProxyConfigUpdateRequest) (*HAProxyConfigUpdateResponse, error) {
	// We use a custom request here because validation failures return 400 with a valid JSON body
	// containing validation details, not a generic error format
	fullURL := c.baseURL + "/api/v1/haproxy/config"

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", fullURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Try to parse the response as ConfigUpdateResponse regardless of status code
	// The agent returns 400 with a valid JSON body on validation failure
	var resp HAProxyConfigUpdateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// If we can't parse it as ConfigUpdateResponse, return generic error
		if httpResp.StatusCode >= 400 {
			return nil, &APIError{
				StatusCode: httpResp.StatusCode,
				Message:    fmt.Sprintf("HTTP %d: %s", httpResp.StatusCode, http.StatusText(httpResp.StatusCode)),
			}
		}
		return nil, fmt.Errorf("failed to parse config update response: %w", err)
	}

	return &resp, nil
}

// GetHAProxyConfigBackups lists available configuration backups.
func (c *Client) GetHAProxyConfigBackups() (*HAProxyBackupsResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/haproxy/config/backups", nil)
	if err != nil {
		return nil, err
	}

	var resp HAProxyBackupsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse backups response: %w", err)
	}

	return &resp, nil
}

// RestoreHAProxyConfig restores configuration from a backup.
func (c *Client) RestoreHAProxyConfig(req *HAProxyRestoreRequest) (*HAProxyRestoreResponse, error) {
	body, err := c.doRequestWithBody("POST", "/api/v1/haproxy/config/restore", req)
	if err != nil {
		return nil, err
	}

	var resp HAProxyRestoreResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse restore response: %w", err)
	}

	return &resp, nil
}

// Firewall Config Management Methods (similar to HAProxy)

// GetFirewallConfig retrieves the current firewall configuration.
func (c *Client) GetFirewallConfig() (*FirewallConfigResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/firewall/config", nil)
	if err != nil {
		return nil, err
	}

	var resp FirewallConfigResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse firewall config response: %w", err)
	}

	return &resp, nil
}

// UpdateFirewallConfig updates the firewall configuration.
func (c *Client) UpdateFirewallConfig(req *FirewallConfigUpdateRequest) (*FirewallConfigUpdateResponse, error) {
	body, err := c.doRequestWithBody("POST", "/api/v1/firewall/config", req)
	if err != nil {
		return nil, err
	}

	var resp FirewallConfigUpdateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse firewall config update response: %w", err)
	}

	return &resp, nil
}

// GetFirewallConfigBackups lists available firewall configuration backups.
func (c *Client) GetFirewallConfigBackups() (*FirewallBackupsResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/firewall/config/backups", nil)
	if err != nil {
		return nil, err
	}

	var resp FirewallBackupsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse firewall backups response: %w", err)
	}

	return &resp, nil
}

// RestoreFirewallConfig restores firewall configuration from a backup.
func (c *Client) RestoreFirewallConfig(req *FirewallRestoreRequest) (*FirewallRestoreResponse, error) {
	body, err := c.doRequestWithBody("POST", "/api/v1/firewall/config/restore", req)
	if err != nil {
		return nil, err
	}

	var resp FirewallRestoreResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse firewall restore response: %w", err)
	}

	return &resp, nil
}

// OS Updates Methods

// GetUpdateStatus retrieves the current OS update status.
func (c *Client) GetUpdateStatus() (*UpdateStatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/updates", nil)
	if err != nil {
		return nil, err
	}

	var resp UpdateStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse update status response: %w", err)
	}

	return &resp, nil
}

// ListUpgradablePackages returns the list of packages that can be upgraded.
func (c *Client) ListUpgradablePackages() (*PackageListResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/updates/list", nil)
	if err != nil {
		return nil, err
	}

	var resp PackageListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse package list response: %w", err)
	}

	return &resp, nil
}

// TriggerUpdateCheck triggers an apt update to refresh package lists.
// Uses extended timeout since apt update can take several minutes.
func (c *Client) TriggerUpdateCheck() (*UpdateStatusResponse, error) {
	body, err := c.doRequestLongRunning("POST", "/api/v1/system/updates/check", nil)
	if err != nil {
		return nil, err
	}

	var resp UpdateStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse update status response: %w", err)
	}

	return &resp, nil
}

// InstallUpdates installs available updates.
func (c *Client) InstallUpdates(req *InstallUpdatesRequest) (*InstallUpdatesResponse, error) {
	body, err := c.doRequestWithBody("POST", "/api/v1/system/updates/install", req)
	if err != nil {
		return nil, err
	}

	var resp InstallUpdatesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse install updates response: %w", err)
	}

	return &resp, nil
}

// InstallUpdatesStreaming installs available updates with streaming output.
// Returns an operation ID that can be used to track progress via WebSocket events.
func (c *Client) InstallUpdatesStreaming(req *InstallUpdatesRequest) (*StreamingInstallResponse, error) {
	body, err := c.doRequestWithBodyAndQuery("POST", "/api/v1/system/updates/install", req, url.Values{"stream": []string{"true"}})
	if err != nil {
		return nil, err
	}

	var resp StreamingInstallResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse streaming install response: %w", err)
	}

	return &resp, nil
}

// TriggerUpdateCheckStreaming triggers an apt update with streaming output.
// Returns an operation ID that can be used to track progress via WebSocket events.
func (c *Client) TriggerUpdateCheckStreaming() (*StreamingInstallResponse, error) {
	body, err := c.doRequest("POST", "/api/v1/system/updates/check", url.Values{"stream": []string{"true"}})
	if err != nil {
		return nil, err
	}

	var resp StreamingInstallResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse streaming check response: %w", err)
	}

	return &resp, nil
}

// GetOperationStatus retrieves the status of an apt operation.
func (c *Client) GetOperationStatus(operationID string) (*OperationStatusResponse, error) {
	path := "/api/v1/system/updates/operation/" + url.PathEscape(operationID)
	body, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp OperationStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse operation status response: %w", err)
	}

	return &resp, nil
}

// CancelOperation cancels a running apt operation.
func (c *Client) CancelOperation(operationID string) error {
	path := "/api/v1/system/updates/operation/" + url.PathEscape(operationID)
	_, err := c.doRequest("DELETE", path, nil)
	return err
}

// ListUpdateLogs returns persisted update operation logs.
func (c *Client) ListUpdateLogs(limit int) (*UpdateLogsResponse, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	body, err := c.doRequest("GET", "/api/v1/system/updates/logs", query)
	if err != nil {
		return nil, err
	}

	var resp UpdateLogsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse update logs response: %w", err)
	}

	return &resp, nil
}

// GetUpdateLog returns a specific update log with full output.
func (c *Client) GetUpdateLog(logID string) (*UpdateLogEntry, error) {
	path := "/api/v1/system/updates/logs/" + url.PathEscape(logID)
	body, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp UpdateLogEntry
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse update log response: %w", err)
	}

	return &resp, nil
}

// GetUpdateHistory returns recent package update history.
func (c *Client) GetUpdateHistory(limit int) (*UpdateHistoryResponse, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}

	body, err := c.doRequest("GET", "/api/v1/system/updates/history", query)
	if err != nil {
		return nil, err
	}

	var resp UpdateHistoryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse update history response: %w", err)
	}

	return &resp, nil
}

// GetRebootRequired checks if a system reboot is required.
func (c *Client) GetRebootRequired() (*RebootRequiredResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/updates/reboot-required", nil)
	if err != nil {
		return nil, err
	}

	var resp RebootRequiredResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse reboot required response: %w", err)
	}

	return &resp, nil
}

// ScheduleReboot schedules a system reboot.
func (c *Client) ScheduleReboot(when string) (*ScheduleRebootResponse, error) {
	req := ScheduleRebootRequest{When: when}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/updates/reboot", req)
	if err != nil {
		return nil, err
	}

	var resp ScheduleRebootResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse schedule reboot response: %w", err)
	}

	return &resp, nil
}

// CancelReboot cancels a scheduled reboot.
func (c *Client) CancelReboot() (*ScheduleRebootResponse, error) {
	body, err := c.doRequest("DELETE", "/api/v1/system/updates/reboot", nil)
	if err != nil {
		return nil, err
	}

	var resp ScheduleRebootResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse cancel reboot response: %w", err)
	}

	return &resp, nil
}

// Snapshot Methods

// ListSnapshots returns available package snapshots.
func (c *Client) ListSnapshots() (*SnapshotsResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/updates/snapshots", nil)
	if err != nil {
		return nil, err
	}

	var resp SnapshotsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse snapshots response: %w", err)
	}

	return &resp, nil
}

// CreateSnapshot creates a package snapshot.
func (c *Client) CreateSnapshot(reason string) (*CreateSnapshotResponse, error) {
	req := CreateSnapshotRequest{Reason: reason}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/updates/snapshots", req)
	if err != nil {
		return nil, err
	}

	var resp CreateSnapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse create snapshot response: %w", err)
	}

	return &resp, nil
}

// RestoreSnapshot restores packages to a previous snapshot state (sync mode).
func (c *Client) RestoreSnapshot(snapshotID string) (*RestoreSnapshotResponse, error) {
	req := RestoreSnapshotRequest{SnapshotID: snapshotID}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/updates/snapshots/restore", req)
	if err != nil {
		return nil, err
	}

	var resp RestoreSnapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse restore snapshot response: %w", err)
	}

	return &resp, nil
}

// RestoreSnapshotStreaming restores packages to a previous snapshot state with streaming output.
// Returns an operation ID that can be used to track progress via WebSocket events.
func (c *Client) RestoreSnapshotStreaming(snapshotID string) (*StreamingInstallResponse, error) {
	req := RestoreSnapshotRequest{SnapshotID: snapshotID}
	body, err := c.doRequestWithBodyAndQuery("POST", "/api/v1/system/updates/snapshots/restore", req, url.Values{"stream": []string{"true"}})
	if err != nil {
		return nil, err
	}

	var resp StreamingInstallResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse streaming restore response: %w", err)
	}

	return &resp, nil
}

// DeleteSnapshot deletes a package snapshot.
func (c *Client) DeleteSnapshot(snapshotID string) (*RestoreSnapshotResponse, error) {
	path := "/api/v1/system/updates/snapshots/" + url.PathEscape(snapshotID)
	body, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return nil, err
	}

	var resp RestoreSnapshotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse delete snapshot response: %w", err)
	}

	return &resp, nil
}

// PreviewSnapshot returns the changes that would be applied by restoring a snapshot.
func (c *Client) PreviewSnapshot(snapshotID string) (*SnapshotPreviewResponse, error) {
	path := "/api/v1/system/updates/snapshots/" + url.PathEscape(snapshotID) + "/preview"
	body, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp SnapshotPreviewResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot preview response: %w", err)
	}

	return &resp, nil
}

// Package Management Methods

// SearchPackages searches for available packages.
func (c *Client) SearchPackages(query string, limit int) (*PackageSearchResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	body, err := c.doRequest("GET", "/api/v1/system/packages/search", params)
	if err != nil {
		return nil, err
	}

	var resp PackageSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse package search response: %w", err)
	}

	return &resp, nil
}

// InstallPackage installs a new package.
func (c *Client) InstallPackage(name string) (*InstallPackageResponse, error) {
	req := InstallPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/packages/install", req)
	if err != nil {
		return nil, err
	}

	var resp InstallPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse install package response: %w", err)
	}

	return &resp, nil
}

// RemovePackage removes a package.
func (c *Client) RemovePackage(name string, purge bool) (*InstallPackageResponse, error) {
	req := RemovePackageRequest{Name: name, Purge: purge}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/packages/remove", req)
	if err != nil {
		return nil, err
	}

	var resp InstallPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse remove package response: %w", err)
	}

	return &resp, nil
}

// Pipx Methods

// GetPipxStatus returns pipx availability and installed packages.
func (c *Client) GetPipxStatus() (*PipxStatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/pipx", nil)
	if err != nil {
		return nil, err
	}

	var resp PipxStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pipx status response: %w", err)
	}

	return &resp, nil
}

// InstallPipxPackage installs a package via pipx.
func (c *Client) InstallPipxPackage(name string) (*PipxPackageResponse, error) {
	req := PipxPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/pipx/install", req)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pipx install response: %w", err)
	}

	return &resp, nil
}

// UninstallPipxPackage uninstalls a package via pipx.
func (c *Client) UninstallPipxPackage(name string) (*PipxPackageResponse, error) {
	req := PipxPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/pipx/uninstall", req)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pipx uninstall response: %w", err)
	}

	return &resp, nil
}

// UpgradePipxPackage upgrades a pipx package.
func (c *Client) UpgradePipxPackage(name string) (*PipxPackageResponse, error) {
	req := PipxPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/pipx/upgrade", req)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pipx upgrade response: %w", err)
	}

	return &resp, nil
}

// UpgradeAllPipxPackages upgrades all pipx packages.
func (c *Client) UpgradeAllPipxPackages() (*PipxPackageResponse, error) {
	body, err := c.doRequest("POST", "/api/v1/system/pipx/upgrade-all", nil)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pipx upgrade-all response: %w", err)
	}

	return &resp, nil
}

// Pip Methods

// GetPipStatus returns pip availability and installed packages.
func (c *Client) GetPipStatus() (*PipStatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/pip", nil)
	if err != nil {
		return nil, err
	}

	var resp PipStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pip status response: %w", err)
	}

	return &resp, nil
}

// InstallPipPackage installs a package via pip.
func (c *Client) InstallPipPackage(name string) (*PipxPackageResponse, error) {
	req := PipxPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/pip/install", req)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pip install response: %w", err)
	}

	return &resp, nil
}

// UninstallPipPackage uninstalls a package via pip.
func (c *Client) UninstallPipPackage(name string) (*PipxPackageResponse, error) {
	req := PipxPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/pip/uninstall", req)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pip uninstall response: %w", err)
	}

	return &resp, nil
}

// UpgradePipPackage upgrades a pip package.
func (c *Client) UpgradePipPackage(name string) (*PipxPackageResponse, error) {
	req := PipxPackageRequest{Name: name}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/pip/upgrade", req)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pip upgrade response: %w", err)
	}

	return &resp, nil
}

// UpgradeAllPipPackages upgrades all pip packages.
func (c *Client) UpgradeAllPipPackages() (*PipxPackageResponse, error) {
	body, err := c.doRequest("POST", "/api/v1/system/pip/upgrade-all", nil)
	if err != nil {
		return nil, err
	}

	var resp PipxPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse pip upgrade-all response: %w", err)
	}

	return &resp, nil
}

// GetPythonToolsStatus returns combined pip + pipx status (fast, no version check).
func (c *Client) GetPythonToolsStatus() (*PythonToolsStatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/python-tools", nil)
	if err != nil {
		return nil, err
	}

	var resp PythonToolsStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse python tools status response: %w", err)
	}

	return &resp, nil
}

// GetPythonToolsVersions returns combined pip + pipx status with latest PyPI version info (slow).
func (c *Client) GetPythonToolsVersions() (*PythonToolsStatusResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/python-tools/versions", nil)
	if err != nil {
		return nil, err
	}

	var resp PythonToolsStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse python tools versions response: %w", err)
	}

	return &resp, nil
}

// Unattended Upgrades Methods

// GetUnattendedConfig retrieves unattended-upgrades configuration.
func (c *Client) GetUnattendedConfig() (*UnattendedConfigResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/updates/unattended", nil)
	if err != nil {
		return nil, err
	}

	var resp UnattendedConfigResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse unattended config response: %w", err)
	}

	return &resp, nil
}

// ConfigureUnattended configures automatic security updates.
func (c *Client) ConfigureUnattended(enabled, autoReboot bool) (*UnattendedConfigResponse, error) {
	req := ConfigureUnattendedRequest{Enabled: enabled, AutoReboot: autoReboot}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/updates/unattended", req)
	if err != nil {
		return nil, err
	}

	var resp UnattendedConfigResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse unattended config response: %w", err)
	}

	return &resp, nil
}
