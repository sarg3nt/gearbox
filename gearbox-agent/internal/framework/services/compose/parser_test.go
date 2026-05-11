package compose

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewParser(t *testing.T) {
	logger := testLogger()
	parser := NewParser("/apps", logger)

	if parser.appsFolder != "/apps" {
		t.Errorf("appsFolder = %q, want %q", parser.appsFolder, "/apps")
	}
	if parser.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestParseFile_BasicBackend(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "myapp")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  web:
    image: nginx
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "myapp.example.com"
      haproxy.backend.server: "web:8080"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(backends) != 1 {
		t.Fatalf("ParseFile() returned %d backends, want 1", len(backends))
	}

	backend := backends[0]
	if backend.Hostname != "myapp.example.com" {
		t.Errorf("Hostname = %q, want %q", backend.Hostname, "myapp.example.com")
	}
	if backend.Server != "web:8080" {
		t.Errorf("Server = %q, want %q", backend.Server, "web:8080")
	}
	if backend.AppName != "myapp" {
		t.Errorf("AppName = %q, want %q", backend.AppName, "myapp")
	}
}

func TestParseFile_AllLabels(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "fullapp")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  api:
    image: api:latest
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "api.example.com"
      haproxy.backend.server: "api:3000"
      haproxy.backend.name: "custom_backend"
      haproxy.backend.mode: "http"
      haproxy.backend.balance: "leastconn"
      haproxy.backend.check: "true"
      haproxy.backend.check.interval: "10s"
      haproxy.backend.check.fall: "5"
      haproxy.backend.check.rise: "3"
      haproxy.ssl.redirect: "true"
      haproxy.public: "true"
      haproxy.rate_limit: "200"
      haproxy.acl.ip: "10.0.0.0/8"
      haproxy.backend.ssl: "true"
      haproxy.backend.ssl.verify: "required"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(backends) != 1 {
		t.Fatalf("ParseFile() returned %d backends, want 1", len(backends))
	}

	b := backends[0]
	if b.BackendName != "custom_backend" {
		t.Errorf("BackendName = %q, want %q", b.BackendName, "custom_backend")
	}
	if b.Mode != "http" {
		t.Errorf("Mode = %q, want %q", b.Mode, "http")
	}
	if b.Balance != "leastconn" {
		t.Errorf("Balance = %q, want %q", b.Balance, "leastconn")
	}
	if !b.Check {
		t.Error("Check should be true")
	}
	if b.CheckInterval != "10s" {
		t.Errorf("CheckInterval = %q, want %q", b.CheckInterval, "10s")
	}
	if b.CheckFall != "5" {
		t.Errorf("CheckFall = %q, want %q", b.CheckFall, "5")
	}
	if b.CheckRise != "3" {
		t.Errorf("CheckRise = %q, want %q", b.CheckRise, "3")
	}
	if !b.SSLRedirect {
		t.Error("SSLRedirect should be true")
	}
	if !b.Public {
		t.Error("Public should be true")
	}
	if b.RateLimit != "200" {
		t.Errorf("RateLimit = %q, want %q", b.RateLimit, "200")
	}
	if b.ACLIP != "10.0.0.0/8" {
		t.Errorf("ACLIP = %q, want %q", b.ACLIP, "10.0.0.0/8")
	}
	if !b.BackendSSL {
		t.Error("BackendSSL should be true")
	}
	if b.BackendSSLVerify != "required" {
		t.Errorf("BackendSSLVerify = %q, want %q", b.BackendSSLVerify, "required")
	}
}

func TestParseFile_DisabledService(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "disabled")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  web:
    image: nginx
    labels:
      haproxy.enable: "false"
      haproxy.hostname: "disabled.example.com"
      haproxy.backend.server: "web:8080"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backends, want 0 (disabled)", len(backends))
	}
}

func TestParseFile_NoHAProxyLabels(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "nolabels")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  web:
    image: nginx
    labels:
      some.other.label: "value"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backends, want 0", len(backends))
	}
}

func TestParseFile_MissingRequiredLabels(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "missing")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	// Missing haproxy.backend.server
	composeContent := `
services:
  web:
    image: nginx
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "example.com"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	// Should skip services with missing required labels
	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backends, want 0 (missing required labels)", len(backends))
	}
}

func TestParseFile_InvalidHostname(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "invalidhost")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  web:
    image: nginx
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "invalid_hostname!"
      haproxy.backend.server: "web:8080"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	// Should skip services with invalid hostname
	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backends, want 0 (invalid hostname)", len(backends))
	}
}

func TestParseFile_InvalidServerAddress(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "invalidserver")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  web:
    image: nginx
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "example.com"
      haproxy.backend.server: "invalid server address"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	// Should skip services with invalid server address
	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backends, want 0 (invalid server)", len(backends))
	}
}

func TestParseFile_MultipleServices(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "multi")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := `
services:
  web:
    image: nginx
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "web.example.com"
      haproxy.backend.server: "web:80"
  api:
    image: api
    labels:
      haproxy.enable: "true"
      haproxy.hostname: "api.example.com"
      haproxy.backend.server: "api:3000"
  db:
    image: postgres
    labels:
      haproxy.enable: "false"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(backends) != 2 {
		t.Errorf("ParseFile() returned %d backends, want 2", len(backends))
	}
}

func TestParseFile_ListFormatLabels(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "listformat")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	// Docker Compose also supports list format for labels
	composeContent := `
services:
  web:
    image: nginx
    labels:
      - "haproxy.enable=true"
      - "haproxy.hostname=list.example.com"
      - "haproxy.backend.server=web:8080"
`
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(backends) != 1 {
		t.Fatalf("ParseFile() returned %d backends, want 1", len(backends))
	}

	if backends[0].Hostname != "list.example.com" {
		t.Errorf("Hostname = %q, want %q", backends[0].Hostname, "list.example.com")
	}
}

func TestFindComposeFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple app directories
	apps := []string{"app1", "app2", "app3"}
	for _, app := range apps {
		appDir := filepath.Join(tmpDir, app)
		if err := os.MkdirAll(appDir, 0755); err != nil {
			t.Fatalf("Failed to create app dir: %v", err)
		}
		composePath := filepath.Join(appDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, []byte("services: {}"), 0644); err != nil {
			t.Fatalf("Failed to write compose file: %v", err)
		}
	}

	// Create a dir without docker-compose.yml
	noComposeDir := filepath.Join(tmpDir, "nocompose")
	if err := os.MkdirAll(noComposeDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	files, err := parser.FindComposeFiles()
	if err != nil {
		t.Fatalf("FindComposeFiles() error = %v", err)
	}

	if len(files) != 3 {
		t.Errorf("FindComposeFiles() found %d files, want 3", len(files))
	}
}

func TestFindComposeFiles_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	parser := NewParser(tmpDir, testLogger())
	files, err := parser.FindComposeFiles()
	if err != nil {
		t.Fatalf("FindComposeFiles() error = %v", err)
	}

	if len(files) != 0 {
		t.Errorf("FindComposeFiles() found %d files, want 0", len(files))
	}
}

func TestFindComposeFiles_NonexistentDir(t *testing.T) {
	parser := NewParser("/nonexistent/path", testLogger())
	files, err := parser.FindComposeFiles()
	if err != nil {
		t.Fatalf("FindComposeFiles() error = %v", err)
	}

	// Should return empty list, not error, for nonexistent dir
	if len(files) != 0 {
		t.Errorf("FindComposeFiles() found %d files, want 0", len(files))
	}
}

func TestFindHardwareServicesFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create hardware services file
	hwPath := filepath.Join(tmpDir, HardwareServicesFile)
	if err := os.WriteFile(hwPath, []byte("services: {}"), 0644); err != nil {
		t.Fatalf("Failed to write hardware services file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	found := parser.FindHardwareServicesFile()

	if found != hwPath {
		t.Errorf("FindHardwareServicesFile() = %q, want %q", found, hwPath)
	}
}

func TestFindHardwareServicesFile_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	parser := NewParser(tmpDir, testLogger())
	found := parser.FindHardwareServicesFile()

	if found != "" {
		t.Errorf("FindHardwareServicesFile() = %q, want empty string", found)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"with.dot", "with.dot"},
		{"with spaces", "with_spaces"},
		{"with@special#chars", "with_special_chars"},
		{"multiple___underscores", "multiple_underscores"},
		{"_leading_underscore", "leading_underscore"},
		{"trailing_underscore_", "trailing_underscore"},
		{"MixedCase", "MixedCase"},
		{"123numeric", "123numeric"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetOrDefault(t *testing.T) {
	m := map[string]string{
		"key1": "value1",
		"key2": "",
	}

	tests := []struct {
		key      string
		defVal   string
		want     string
	}{
		{"key1", "default", "value1"},
		{"key2", "default", "default"}, // Empty string uses default
		{"key3", "default", "default"}, // Missing key uses default
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := getOrDefault(m, tt.key, tt.defVal)
			if got != tt.want {
				t.Errorf("getOrDefault(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestGetValidatedOrDefault_ValidValues(t *testing.T) {
	m := map[string]string{
		"mode":    "tcp",
		"balance": "roundrobin",
		"rate":    "500",
	}

	if got := getValidatedOrDefault(m, "mode", "http", validMode); got != "tcp" {
		t.Errorf("mode = %q, want %q", got, "tcp")
	}
	if got := getValidatedOrDefault(m, "balance", "source", validBalance); got != "roundrobin" {
		t.Errorf("balance = %q, want %q", got, "roundrobin")
	}
	if got := getValidatedOrDefault(m, "rate", "100", validRateLimit); got != "500" {
		t.Errorf("rate = %q, want %q", got, "500")
	}
}

func TestGetValidatedOrDefault_InvalidValues(t *testing.T) {
	m := map[string]string{
		"mode":    "invalid_mode",
		"balance": "invalid_balance",
		"rate":    "not_a_number",
	}

	if got := getValidatedOrDefault(m, "mode", "http", validMode); got != "http" {
		t.Errorf("mode = %q, want default %q", got, "http")
	}
	if got := getValidatedOrDefault(m, "balance", "roundrobin", validBalance); got != "roundrobin" {
		t.Errorf("balance = %q, want default %q", got, "roundrobin")
	}
	if got := getValidatedOrDefault(m, "rate", "100", validRateLimit); got != "100" {
		t.Errorf("rate = %q, want default %q", got, "100")
	}
}

func TestGetValidatedOrDefault_MissingKey(t *testing.T) {
	m := map[string]string{}

	if got := getValidatedOrDefault(m, "mode", "http", validMode); got != "http" {
		t.Errorf("mode = %q, want default %q", got, "http")
	}
}

func TestValidationPatterns(t *testing.T) {
	// Test validHostname
	validHosts := []string{"example.com", "sub.example.com", "a.b.c.d.example.com", "localhost", "a1b2c3"}
	invalidHosts := []string{"-invalid.com", "invalid-.com", "invalid..com", "invalid_host.com"}

	for _, h := range validHosts {
		if !validHostname.MatchString(h) {
			t.Errorf("validHostname should match %q", h)
		}
	}
	for _, h := range invalidHosts {
		if validHostname.MatchString(h) {
			t.Errorf("validHostname should not match %q", h)
		}
	}

	// Test validServerAddr
	validServers := []string{"host:8080", "server:443", "10.0.0.1:80", "my-server:3000"}
	invalidServers := []string{"host", ":8080", "host:", "host:abc", "host with space:80"}

	for _, s := range validServers {
		if !validServerAddr.MatchString(s) {
			t.Errorf("validServerAddr should match %q", s)
		}
	}
	for _, s := range invalidServers {
		if validServerAddr.MatchString(s) {
			t.Errorf("validServerAddr should not match %q", s)
		}
	}

	// Test validMode
	if !validMode.MatchString("http") || !validMode.MatchString("tcp") {
		t.Error("validMode should match http and tcp")
	}
	if validMode.MatchString("invalid") {
		t.Error("validMode should not match invalid")
	}

	// Test validBalance
	validBalances := []string{"roundrobin", "leastconn", "source", "uri", "hdr", "first", "random"}
	for _, b := range validBalances {
		if !validBalance.MatchString(b) {
			t.Errorf("validBalance should match %q", b)
		}
	}
	if validBalance.MatchString("invalid") {
		t.Error("validBalance should not match invalid")
	}

	// Test validBackendName (2026-05 audit P0-1).
	validBackendNames := []string{
		"myapp_backend", "web-app", "a", "backend.1", "x9-y8.z7",
	}
	invalidBackendNames := []string{
		"",
		"-leading-hyphen",
		".leading-dot",
		"name with space",
		"name\nwith-newline",
		"name\twith-tab",
		"name;with-semicolon",
		"backend\n  lua-load /tmp/x.lua", // The actual P0-1 exploit payload
	}
	for _, n := range validBackendNames {
		if !validBackendName.MatchString(n) {
			t.Errorf("validBackendName should match %q", n)
		}
	}
	for _, n := range invalidBackendNames {
		if validBackendName.MatchString(n) {
			t.Errorf("validBackendName should NOT match %q (would allow HAProxy directive injection)", n)
		}
	}
}

func TestValidateACLIPList(t *testing.T) {
	// 2026-05 audit P0-2: haproxy.acl.ip must reject anything that isn't a
	// strict comma-separated IP/CIDR list, to prevent injection of additional
	// HAProxy directives into the generated config.
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"empty", "", true},
		{"single-ip", "10.0.0.1", true},
		{"single-cidr", "10.0.0.0/8", true},
		{"mixed-list", "10.0.0.0/8, 192.168.1.0/24, 172.16.0.5", true},
		{"newline-injection", "10.0.0.0/8\n  acl admin src 0.0.0.0/0", false}, // The actual P0-2 exploit
		{"haproxy-keyword", "10.0.0.0/8, acl_open 0.0.0.0/0", false},
		{"garbage", "not-an-ip", false},
		{"out-of-range-octet", "999.999.999.999", false},
		{"semicolon", "10.0.0.0/8; foo", false},
		{"tab-injection", "10.0.0.0/8\t10.1.0.0/16", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := validateACLIPList(tt.input)
			if ok != tt.ok {
				t.Errorf("validateACLIPList(%q) ok=%v, want %v", tt.input, ok, tt.ok)
			}
		})
	}
}

func TestParseFile_InjectionViaBackendName(t *testing.T) {
	// 2026-05 audit P0-1: a malicious haproxy.backend.name with embedded
	// newlines must be rejected at parse time so it never reaches the
	// generator's fmt.Sprintf("backend %s", …) call.
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "evil")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	// Note: YAML's literal-block style "|" preserves newlines, which is the
	// exact shape of the exploit payload as it would appear in a docker-
	// compose.yml authored by an attacker with push access to the repo.
	composeContent := "services:\n" +
		"  web:\n" +
		"    image: nginx\n" +
		"    labels:\n" +
		"      haproxy.enable: \"true\"\n" +
		"      haproxy.hostname: \"evil.example.com\"\n" +
		"      haproxy.backend.server: \"10.0.0.1:80\"\n" +
		"      haproxy.backend.name: |\n" +
		"        myapp\n" +
		"          lua-load /tmp/evil.lua\n"

	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backend(s) for an injection payload; want 0 (rejected)", len(backends))
	}
}

func TestParseFile_InjectionViaACLIP(t *testing.T) {
	// 2026-05 audit P0-2.
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "aclip")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	composeContent := "services:\n" +
		"  web:\n" +
		"    image: nginx\n" +
		"    labels:\n" +
		"      haproxy.enable: \"true\"\n" +
		"      haproxy.hostname: \"evil.example.com\"\n" +
		"      haproxy.backend.server: \"10.0.0.1:80\"\n" +
		"      haproxy.acl.ip: |\n" +
		"        10.0.0.0/8\n" +
		"          acl admin src 0.0.0.0/0\n"

	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(backends) != 0 {
		t.Errorf("ParseFile() returned %d backend(s) for an injection payload; want 0 (rejected)", len(backends))
	}
}

// 2026-05 audit P2-10: haproxy.acl.path and haproxy.acl.header are
// validated at parse time even though no current generator emits them.
// Closes the latent injection class before a future PR wires them in.
func TestValidACLPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", true}, // empty handled by call-site short-circuit
		{"root", "/", true},
		{"single-segment", "/api", true},
		{"multi-segment", "/api/v1/users", true},
		{"trailing-slash", "/api/", true},
		{"with-dash-underscore", "/api/_v2-beta", true},
		{"url-encoded-allowed", "/api/%20space", true},

		{"no-leading-slash", "api", false},
		{"newline-injection", "/api\n  http-request deny", false},
		{"semicolon", "/api; default-src *", false},
		{"space", "/api /v1", false},
		{"backslash", "/api\\v1", false},
		{"query-string", "/api?x=1", false},
		{"fragment", "/api#section", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.input == "" {
				return // call-site skips validation for empty
			}
			if got := validACLPath.MatchString(tt.input); got != tt.want {
				t.Errorf("validACLPath.MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidACLHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple", "X-Custom-Header", true},
		{"all-lower", "authorization", true},
		{"underscore", "X_Custom", true},
		{"dot-allowed-by-rfc", "X.Forwarded.For", true},

		{"empty", "", false},
		{"space", "X Custom", false},
		{"newline-injection", "X-Custom\nhttp-request deny", false},
		{"semicolon", "X-Custom;evil", false},
		{"colon", "X-Custom:value", false},
		{"slash", "X-Custom/extra", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validACLHeader.MatchString(tt.input); got != tt.want {
				t.Errorf("validACLHeader.MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFile_InjectionViaACLPath(t *testing.T) {
	// 2026-05 audit P2-10: even though the generator doesn't emit
	// haproxy.acl.path today, a malicious value must not be stored on
	// BackendConfig — otherwise a future generator wire-up would re-open
	// the P0-1/P0-2 class of injection.
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "aclpath")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	composeContent := "services:\n" +
		"  web:\n" +
		"    image: nginx\n" +
		"    labels:\n" +
		"      haproxy.enable: \"true\"\n" +
		"      haproxy.hostname: \"evil.example.com\"\n" +
		"      haproxy.backend.server: \"10.0.0.1:80\"\n" +
		"      haproxy.acl.path: |\n" +
		"        /api\n" +
		"          http-request deny\n"

	composePath := filepath.Join(appDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	parser := NewParser(tmpDir, testLogger())
	backends, err := parser.ParseFile(composePath)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(backends) != 0 {
		t.Errorf("ParseFile returned %d backend(s); want 0 (rejected on aclpath injection)", len(backends))
	}
}
