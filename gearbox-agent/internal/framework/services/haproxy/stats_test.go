package haproxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewStatsClient(t *testing.T) {
	client := NewStatsClient("/run/haproxy/admin.sock", "http://localhost:8404/stats", "admin", "pass")

	if client == nil {
		t.Fatal("NewStatsClient returned nil")
	}
	if client.socketPath != "/run/haproxy/admin.sock" {
		t.Errorf("socketPath = %s, want /run/haproxy/admin.sock", client.socketPath)
	}
	if client.statsURL != "http://localhost:8404/stats" {
		t.Errorf("statsURL = %s, want http://localhost:8404/stats", client.statsURL)
	}
}

func TestStatsClient_GetStatsFromHTTP(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/csv")
		w.Write([]byte("# pxname,svname,qcur,qmax,scur,smax\nhttps_front,FRONTEND,0,0,5,100\n"))
	}))
	defer server.Close()

	client := NewStatsClient("", server.URL, "admin", "secret")
	csv, err := client.GetStatsCSV()

	if err != nil {
		t.Fatalf("GetStatsCSV error: %v", err)
	}

	if csv == "" {
		t.Error("GetStatsCSV returned empty string")
	}

	if len(csv) < 10 {
		t.Errorf("GetStatsCSV returned too short response: %s", csv)
	}
}

func TestStatsClient_GetStatsFromHTTP_BadAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewStatsClient("", server.URL, "wrong", "wrong")
	_, err := client.GetStatsCSV()

	if err == nil {
		t.Error("GetStatsCSV should return error for bad auth")
	}
}

func TestStatsClient_GetStatsFromHTTP_NoSource(t *testing.T) {
	client := NewStatsClient("", "", "", "")
	_, err := client.GetStatsCSV()

	if err == nil {
		t.Error("GetStatsCSV should return error when no source configured")
	}
}

func TestParseInfoField(t *testing.T) {
	info := &RuntimeInfo{
		Extra: make(map[string]string),
	}

	tests := []struct {
		key      string
		value    string
		checkFn  func() bool
		expected string
	}{
		{"Version", "2.8.0", func() bool { return info.Version == "2.8.0" }, "Version"},
		{"Uptime", "5d 2h 30m", func() bool { return info.Uptime == "5d 2h 30m" }, "Uptime"},
		{"Uptime_sec", "123456", func() bool { return info.UptimeSeconds == 123456 }, "UptimeSeconds"},
		{"Pid", "12345", func() bool { return info.Pid == 12345 }, "Pid"},
		{"Nbthread", "8", func() bool { return info.Nbthread == 8 }, "Nbthread"},
		{"Maxconn", "20000", func() bool { return info.MaxConn == 20000 }, "MaxConn"},
		{"CurrConns", "150", func() bool { return info.CurrentConn == 150 }, "CurrentConn"},
		{"Idle_pct", "85", func() bool { return info.IdlePct == 85 }, "IdlePct"},
	}

	for _, tt := range tests {
		err := parseInfoField(info, tt.key, tt.value)
		if err != nil {
			t.Errorf("parseInfoField(%s, %s) error: %v", tt.key, tt.value, err)
		}
		if !tt.checkFn() {
			t.Errorf("parseInfoField(%s, %s) did not set %s correctly", tt.key, tt.value, tt.expected)
		}
	}
}

func TestParseInfoField_Unknown(t *testing.T) {
	info := &RuntimeInfo{
		Extra: make(map[string]string),
	}

	err := parseInfoField(info, "UnknownField", "some_value")
	if err == nil {
		t.Error("parseInfoField should return error for unknown field")
	}
}

func TestParseTableLine(t *testing.T) {
	tests := []struct {
		line     string
		expected StickTableInfo
	}{
		{
			"# table: https_front, type: ip, size:102400, used:5",
			StickTableInfo{Name: "https_front", Type: "ip", Size: 102400, Used: 5},
		},
		{
			"# table: rate_limit, type: ip, size:50000, used:0",
			StickTableInfo{Name: "rate_limit", Type: "ip", Size: 50000, Used: 0},
		},
	}

	for _, tt := range tests {
		result := parseTableLine(tt.line)
		if result.Name != tt.expected.Name {
			t.Errorf("parseTableLine Name = %s, want %s", result.Name, tt.expected.Name)
		}
		if result.Type != tt.expected.Type {
			t.Errorf("parseTableLine Type = %s, want %s", result.Type, tt.expected.Type)
		}
		if result.Size != tt.expected.Size {
			t.Errorf("parseTableLine Size = %d, want %d", result.Size, tt.expected.Size)
		}
		if result.Used != tt.expected.Used {
			t.Errorf("parseTableLine Used = %d, want %d", result.Used, tt.expected.Used)
		}
	}
}

func TestValidateConfig_InvalidPath(t *testing.T) {
	valid, output, err := ValidateConfig("/nonexistent/path/haproxy.cfg")

	if err != nil {
		// Command execution might fail if haproxy isn't installed
		t.Skipf("Skipping test - haproxy not available: %v", err)
	}

	// If haproxy is installed, it should return invalid for nonexistent file
	if valid {
		t.Error("ValidateConfig should return false for nonexistent file")
	}

	// Output might be empty on some systems if file doesn't exist
	// The important thing is that valid=false
	_ = output
}
