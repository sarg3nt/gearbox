package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient("https://example.com:8405", "test-api-key")

	if client.baseURL != "https://example.com:8405" {
		t.Errorf("expected baseURL https://example.com:8405, got %s", client.baseURL)
	}

	if client.apiKey != "test-api-key" {
		t.Errorf("expected apiKey test-api-key, got %s", client.apiKey)
	}
}

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	client := NewClient("https://example.com:8405/", "test-api-key")

	if client.baseURL != "https://example.com:8405" {
		t.Errorf("expected baseURL without trailing slash, got %s", client.baseURL)
	}
}

func TestHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected path /health, got %s", r.URL.Path)
		}

		// Health endpoint should not require auth
		if r.Header.Get("Authorization") != "" {
			t.Error("health endpoint should not require authorization")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{
			Status:    "healthy",
			Timestamp: "2026-01-17T12:00:00Z",
			Uptime:    "1h30m",
			Version:   "1.0.0",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.Health()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", resp.Status)
	}

	if resp.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", resp.Version)
	}
}

func TestGetInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/info" {
			t.Errorf("expected path /api/v1/haproxy/info, got %s", r.URL.Path)
		}

		// Check authorization
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-key" {
			t.Errorf("expected authorization Bearer test-key, got %s", authHeader)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RuntimeInfo{
			Version:   "2.8.3",
			Uptime:    "5d 3h 20m",
			UptimeSec: 450000,
			PID:       1234,
			CurrConn:  150,
			MaxConn:   10000,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Version != "2.8.3" {
		t.Errorf("expected version 2.8.3, got %s", resp.Version)
	}

	if resp.CurrConn != 150 {
		t.Errorf("expected curr_conn 150, got %d", resp.CurrConn)
	}
}

func TestGetStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/stats" {
			t.Errorf("expected path /api/v1/haproxy/stats, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(StatsResponse{
			Frontends: []FrontendStats{
				{Name: "http-in", Status: "OPEN", Scur: 10, Stot: 1000},
				{Name: "https-in", Status: "OPEN", Scur: 50, Stot: 5000},
			},
			Backends: []BackendStats{
				{Name: "app-backend", Status: "UP", Act: 3, HealthPercent: 100.0},
			},
			ParsedAt: "2026-01-17T12:00:00Z",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetStats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Frontends) != 2 {
		t.Errorf("expected 2 frontends, got %d", len(resp.Frontends))
	}

	if len(resp.Backends) != 1 {
		t.Errorf("expected 1 backend, got %d", len(resp.Backends))
	}

	if resp.Backends[0].Name != "app-backend" {
		t.Errorf("expected backend name app-backend, got %s", resp.Backends[0].Name)
	}
}

func TestGetStatsCSV(t *testing.T) {
	expectedCSV := "# pxname,svname,status\nhttp-in,FRONTEND,OPEN\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/stats" {
			t.Errorf("expected path /api/v1/haproxy/stats, got %s", r.URL.Path)
		}

		format := r.URL.Query().Get("format")
		if format != "csv" {
			t.Errorf("expected format=csv, got %s", format)
		}

		w.Header().Set("Content-Type", "text/csv")
		w.Write([]byte(expectedCSV))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	csv, err := client.GetStatsCSV()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if csv != expectedCSV {
		t.Errorf("expected CSV %q, got %q", expectedCSV, csv)
	}
}

func TestGetMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/metrics" {
			t.Errorf("expected path /api/v1/metrics, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MetricsResponse{
			Metrics: SystemMetrics{
				CPUCount:           4,
				LoadAverage1:       1.5,
				MemoryTotalBytes:   8589934592,
				MemoryUsedBytes:    4294967296,
				MemoryUsagePercent: 50.0,
			},
			Services: []ServiceStatus{
				{Name: "haproxy", Status: "active", Active: true},
				{Name: "fail2ban", Status: "active", Active: true},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetMetrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Metrics.CPUCount != 4 {
		t.Errorf("expected 4 CPUs, got %d", resp.Metrics.CPUCount)
	}

	if len(resp.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(resp.Services))
	}
}

func TestGetServices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services" {
			t.Errorf("expected path /api/v1/services, got %s", r.URL.Path)
		}

		services := r.URL.Query().Get("services")
		if services != "haproxy,nginx" {
			t.Errorf("expected services=haproxy,nginx, got %s", services)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ServicesResponse{
			Services: []ServiceStatus{
				{Name: "haproxy", Status: "active", Active: true},
				{Name: "nginx", Status: "inactive", Active: false},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetServices([]string{"haproxy", "nginx"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(resp.Services))
	}
}

func TestGetLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs/haproxy" {
			t.Errorf("expected path /api/v1/logs/haproxy, got %s", r.URL.Path)
		}

		lines := r.URL.Query().Get("lines")
		if lines != "100" {
			t.Errorf("expected lines=100, got %s", lines)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LogResponse{
			Name:    "haproxy",
			Lines:   100,
			Content: "Jan 17 12:00:00 haproxy[1234]: Connect from 10.0.0.1\n",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetLogs("haproxy", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Name != "haproxy" {
		t.Errorf("expected name haproxy, got %s", resp.Name)
	}

	if resp.Lines != 100 {
		t.Errorf("expected 100 lines, got %d", resp.Lines)
	}
}

func TestGetLogsMaxLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := r.URL.Query().Get("lines")
		// Should be capped at MaxLogLines
		if lines != "10000" {
			t.Errorf("expected lines=10000 (capped), got %s", lines)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LogResponse{
			Name:    "haproxy",
			Lines:   10000,
			Content: "log content",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	_, err := client.GetLogs("haproxy", 50000) // Request more than max
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/metadata" {
			t.Errorf("expected path /api/v1/metadata, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MetadataResponse{
			Metadata: Metadata{
				Version:     "1.0",
				GeneratedAt: "2026-01-17T12:00:00Z",
				Frontends: []FrontendMetadata{
					{Name: "https-in", Backends: []string{"app-backend"}},
				},
				Backends: []BackendMetadata{
					{Name: "app-backend", Hostname: "app.example.com", Public: true},
				},
			},
			LastSyncTime: "2026-01-17T12:00:00Z",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetMetadata()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Metadata.Backends) != 1 {
		t.Errorf("expected 1 backend, got %d", len(resp.Metadata.Backends))
	}

	if resp.Metadata.Backends[0].Hostname != "app.example.com" {
		t.Errorf("expected hostname app.example.com, got %s", resp.Metadata.Backends[0].Hostname)
	}
}

func TestGetSecuritySummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security/summary" {
			t.Errorf("expected path /api/v1/security/summary, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SecuritySummary{
			Fail2Ban: Fail2BanSummary{
				Available:   true,
				Running:     true,
				TotalBanned: 42,
				JailCount:   3,
			},
			Firewall: FirewallSummary{
				Available:    true,
				Running:      true,
				RecentBlocks: 15,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetSecuritySummary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Fail2Ban.TotalBanned != 42 {
		t.Errorf("expected 42 banned, got %d", resp.Fail2Ban.TotalBanned)
	}

	if resp.Firewall.RecentBlocks != 15 {
		t.Errorf("expected 15 recent blocks, got %d", resp.Firewall.RecentBlocks)
	}
}

func TestGetFail2BanStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security/fail2ban" {
			t.Errorf("expected path /api/v1/security/fail2ban, got %s", r.URL.Path)
		}

		includeIPs := r.URL.Query().Get("include_ips")
		recent := r.URL.Query().Get("recent")

		if includeIPs != "true" {
			t.Errorf("expected include_ips=true, got %s", includeIPs)
		}
		if recent != "10" {
			t.Errorf("expected recent=10, got %s", recent)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Fail2BanStats{
			Available:   true,
			Running:     true,
			TotalBanned: 100,
			Jails: []JailStats{
				{Name: "sshd", CurrentlyBanned: 5, BannedIPs: []string{"10.0.0.1", "10.0.0.2"}},
			},
			CollectedAt: time.Now(),
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetFail2BanStats(&Fail2BanOptions{
		IncludeIPs: true,
		Recent:     10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Jails) != 1 {
		t.Errorf("expected 1 jail, got %d", len(resp.Jails))
	}

	if len(resp.Jails[0].BannedIPs) != 2 {
		t.Errorf("expected 2 banned IPs, got %d", len(resp.Jails[0].BannedIPs))
	}
}

func TestGetSyncStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/status" {
			t.Errorf("expected path /api/v1/sync/status, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SyncStatusResponse{
			LastSyncTime: "2026-01-17T12:00:00Z",
			BackendCount: 15,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetSyncStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.BackendCount != 15 {
		t.Errorf("expected 15 backends, got %d", resp.BackendCount)
	}
}

func TestGetWebSocketToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events/token" {
			t.Errorf("expected path /api/v1/events/token, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WSTokenResponse{
			Token:     "abc123def456",
			ExpiresIn: 60,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetWebSocketToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Token != "abc123def456" {
		t.Errorf("expected token abc123def456, got %s", resp.Token)
	}

	if resp.ExpiresIn != 60 {
		t.Errorf("expected 60 second expiry, got %d", resp.ExpiresIn)
	}
}

func TestHTTPError401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid API key"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-key")
	_, err := client.GetInfo()
	if err == nil {
		t.Fatal("expected error for 401 response")
	}

	if !IsUnauthorized(err) {
		t.Errorf("expected unauthorized error, got %v", err)
	}

	apiErr, ok := IsAPIError(err)
	if !ok {
		t.Fatal("expected APIError")
	}

	if apiErr.StatusCode != 401 {
		t.Errorf("expected status code 401, got %d", apiErr.StatusCode)
	}

	if !strings.Contains(apiErr.Message, "invalid API key") {
		t.Errorf("expected message to contain 'invalid API key', got %s", apiErr.Message)
	}
}

func TestHTTPError404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	_, err := client.GetLogs("nonexistent", 100)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}

	if !IsNotFound(err) {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestHTTPError429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	_, err := client.GetStats()
	if err == nil {
		t.Fatal("expected error for 429 response")
	}

	if !IsTooManyRequests(err) {
		t.Errorf("expected too many requests error, got %v", err)
	}
}

func TestHTTPError503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	_, err := client.GetMetrics()
	if err == nil {
		t.Fatal("expected error for 503 response")
	}

	if !IsServiceUnavailable(err) {
		t.Errorf("expected service unavailable error, got %v", err)
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithTimeout(server.URL, "test-key", 50*time.Millisecond)
	_, err := client.Health()
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got %v", err)
	}
}

func TestGetHAProxyConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/config" {
			t.Errorf("expected path /api/v1/haproxy/config, got %s", r.URL.Path)
		}

		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HAProxyConfigResponse{
			Content:      "global\n  log /dev/log local0\n",
			SHA256:       "abc123def456",
			LastModified: time.Now(),
			FilePath:     "/data/config/haproxy.cfg",
			Sections: []ConfigSection{
				{Type: "global", Name: "", StartLine: 1, EndLine: 10, IsAutoGen: false},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetHAProxyConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SHA256 != "abc123def456" {
		t.Errorf("expected SHA256 abc123def456, got %s", resp.SHA256)
	}

	if len(resp.Sections) != 1 {
		t.Errorf("expected 1 section, got %d", len(resp.Sections))
	}

	if resp.Sections[0].Type != "global" {
		t.Errorf("expected section type global, got %s", resp.Sections[0].Type)
	}
}

func TestUpdateHAProxyConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/config" {
			t.Errorf("expected path /api/v1/haproxy/config, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		var req HAProxyConfigUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Content == "" {
			t.Error("expected content in request")
		}

		if req.DryRun {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(HAProxyConfigUpdateResponse{
				Success:          true,
				ValidationOutput: "Configuration is valid",
				Message:          "Configuration is valid (dry run)",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HAProxyConfigUpdateResponse{
			Success:          true,
			ValidationOutput: "Configuration is valid",
			BackupPath:       "/data/backups/haproxy_20240115_103000.cfg",
			Message:          "Configuration updated successfully",
			NewSHA256:        "newsha256",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	// Test dry run
	resp, err := client.UpdateHAProxyConfig(&HAProxyConfigUpdateRequest{
		Content:     "global\n  log /dev/log local0\n",
		ExpectedSHA: "abc123",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Error("expected success for dry run")
	}

	// Test actual update
	resp, err = client.UpdateHAProxyConfig(&HAProxyConfigUpdateRequest{
		Content:      "global\n  log /dev/log local0\n",
		ExpectedSHA:  "abc123",
		BackupReason: "Test update",
		DryRun:       false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.BackupPath == "" {
		t.Error("expected backup path in response")
	}

	if resp.NewSHA256 == "" {
		t.Error("expected new SHA256 in response")
	}
}

func TestGetHAProxyConfigBackups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/config/backups" {
			t.Errorf("expected path /api/v1/haproxy/config/backups, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HAProxyBackupsResponse{
			Backups: []ConfigBackup{
				{
					ID:        "haproxy_20240115_103000.cfg",
					Filename:  "haproxy_20240115_103000.cfg",
					CreatedAt: time.Now(),
					Reason:    "Manual backup",
					SHA256:    "abc123",
					SizeBytes: 4096,
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetHAProxyConfigBackups()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(resp.Backups))
	}

	if resp.Backups[0].Reason != "Manual backup" {
		t.Errorf("expected reason 'Manual backup', got %s", resp.Backups[0].Reason)
	}
}

func TestRestoreHAProxyConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/haproxy/config/restore" {
			t.Errorf("expected path /api/v1/haproxy/config/restore, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		var req HAProxyRestoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.BackupID == "" {
			t.Error("expected backup_id in request")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HAProxyRestoreResponse{
			Success:          true,
			Message:          "Configuration restored successfully",
			ValidationOutput: "Configuration is valid",
			NewSHA256:        "restoredsha256",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.RestoreHAProxyConfig(&HAProxyRestoreRequest{
		BackupID: "haproxy_20240115_103000.cfg",
		DryRun:   false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Error("expected success for restore")
	}

	if resp.NewSHA256 != "restoredsha256" {
		t.Errorf("expected new SHA256 restoredsha256, got %s", resp.NewSHA256)
	}
}

func TestGetFirewallConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/firewall/config" {
			t.Errorf("expected path /api/v1/firewall/config, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FirewallConfigResponse{
			Content:      "#!/usr/sbin/nft -f\ntable inet filter {\n}",
			SHA256:       "firewall123",
			LastModified: time.Now(),
			FilePath:     "/etc/nftables.conf",
			Sections: []ConfigSection{
				{Type: "table", Name: "inet filter", StartLine: 2, EndLine: 10},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetFirewallConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SHA256 != "firewall123" {
		t.Errorf("expected SHA256 firewall123, got %s", resp.SHA256)
	}

	if len(resp.Sections) != 1 {
		t.Errorf("expected 1 section, got %d", len(resp.Sections))
	}
}

func TestUpdateFirewallConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/firewall/config" {
			t.Errorf("expected path /api/v1/firewall/config, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FirewallConfigUpdateResponse{
			Success:          true,
			ValidationOutput: "",
			BackupPath:       "/etc/backups/nftables_20240115_103000.conf",
			Message:          "Configuration updated successfully",
			NewSHA256:        "newfwsha256",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.UpdateFirewallConfig(&FirewallConfigUpdateRequest{
		Content:      "#!/usr/sbin/nft -f\ntable inet filter {\n}",
		ExpectedSHA:  "firewall123",
		BackupReason: "Updated firewall rules",
		DryRun:       false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Error("expected success")
	}

	if resp.NewSHA256 != "newfwsha256" {
		t.Errorf("expected new SHA256 newfwsha256, got %s", resp.NewSHA256)
	}
}

func TestGetFirewallConfigBackups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/firewall/config/backups" {
			t.Errorf("expected path /api/v1/firewall/config/backups, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FirewallBackupsResponse{
			Backups: []ConfigBackup{
				{
					ID:        "nftables_20240115_103000.conf",
					Filename:  "nftables_20240115_103000.conf",
					CreatedAt: time.Now(),
					Reason:    "Firewall backup",
					SHA256:    "fwbackup123",
					SizeBytes: 2048,
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.GetFirewallConfigBackups()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(resp.Backups))
	}
}

func TestRestoreFirewallConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/firewall/config/restore" {
			t.Errorf("expected path /api/v1/firewall/config/restore, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FirewallRestoreResponse{
			Success:          true,
			Message:          "Firewall configuration restored successfully",
			ValidationOutput: "",
			NewSHA256:        "restoredfwsha256",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	resp, err := client.RestoreFirewallConfig(&FirewallRestoreRequest{
		BackupID: "nftables_20240115_103000.conf",
		DryRun:   false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Error("expected success for restore")
	}
}
