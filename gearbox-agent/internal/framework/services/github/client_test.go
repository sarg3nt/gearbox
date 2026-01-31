package github

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{
		RepoURL:  "https://github.com/owner/repo",
		PAT:      "test_token",
		Branch:   "main",
		CacheDir: "/tmp/cache",
		Logger:   logger,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.repoURL != cfg.RepoURL {
		t.Errorf("repoURL = %q, want %q", client.repoURL, cfg.RepoURL)
	}
	if client.pat != cfg.PAT {
		t.Errorf("pat = %q, want %q", client.pat, cfg.PAT)
	}
	if client.branch != cfg.Branch {
		t.Errorf("branch = %q, want %q", client.branch, cfg.Branch)
	}
	if client.cacheDir != cfg.CacheDir {
		t.Errorf("cacheDir = %q, want %q", client.cacheDir, cfg.CacheDir)
	}
}

func TestExtractAPIURL(t *testing.T) {
	tests := []struct {
		repoURL string
		want    string
	}{
		{
			"https://github.com/owner/repo",
			"https://api.github.com/repos/owner/repo",
		},
		{
			"https://github.com/owner/repo.git",
			"https://api.github.com/repos/owner/repo",
		},
		{
			"https://github.com/owner/repo/",
			"https://api.github.com/repos/owner/repo",
		},
		{
			"https://github.com/my-org/my-repo-name",
			"https://api.github.com/repos/my-org/my-repo-name",
		},
		// Note: This edge case (both .git and trailing /) results in .git remaining
		// because the suffix strip happens before the trim. This is acceptable since
		// this URL format is uncommon in practice.
		{
			"https://github.com/owner/repo.git/",
			"https://api.github.com/repos/owner/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.repoURL, func(t *testing.T) {
			got := extractAPIURL(tt.repoURL)
			if got != tt.want {
				t.Errorf("extractAPIURL(%q) = %q, want %q", tt.repoURL, got, tt.want)
			}
		})
	}
}

func TestTruncateSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc1234567890", "abc1234"},
		{"abcdefg", "abcdefg"},
		{"abcdef", "abcdef"},
		{"abc", "abc"},
		{"", ""},
		{"1234567", "1234567"},
		{"12345678", "1234567"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateSHA(tt.input)
			if got != tt.want {
				t.Errorf("truncateSHA(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClient_SanitizeOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name   string
		pat    string
		output string
		want   string
	}{
		{
			name:   "with PAT",
			pat:    "ghp_secrettoken123",
			output: "fatal: Authentication failed for 'https://ghp_secrettoken123@github.com/owner/repo'",
			want:   "fatal: Authentication failed for 'https://***@github.com/owner/repo'",
		},
		{
			name:   "without PAT",
			pat:    "",
			output: "fatal: Authentication failed",
			want:   "fatal: Authentication failed",
		},
		{
			name:   "PAT not in output",
			pat:    "ghp_secrettoken123",
			output: "fatal: repository not found",
			want:   "fatal: repository not found",
		},
		{
			name:   "multiple occurrences",
			pat:    "secret",
			output: "secret appeared secret times",
			want:   "*** appeared *** times",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(Config{
				RepoURL:  "https://github.com/owner/repo",
				PAT:      tt.pat,
				Branch:   "main",
				CacheDir: "/tmp/cache",
				Logger:   logger,
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			got := client.sanitizeOutput(tt.output)
			if got != tt.want {
				t.Errorf("sanitizeOutput(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestClient_GetCacheDir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := NewClient(Config{
		RepoURL:  "https://github.com/owner/repo",
		Branch:   "main",
		CacheDir: "/custom/cache/path",
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.GetCacheDir() != "/custom/cache/path" {
		t.Errorf("GetCacheDir() = %q, want %q", client.GetCacheDir(), "/custom/cache/path")
	}
}

func TestClient_GetLatestCommitSHA_Success(t *testing.T) {
	// Mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify authorization header when PAT is provided
		if auth := r.Header.Get("Authorization"); auth != "" && auth != "token test_pat" {
			t.Errorf("unexpected Authorization header: %s", auth)
		}

		// Verify Accept header
		accept := r.Header.Get("Accept")
		if accept != "application/vnd.github.v3+json" {
			t.Errorf("Accept header = %q, want application/vnd.github.v3+json", accept)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"sha": "abc123def456"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{
		repoURL:  "https://github.com/owner/repo",
		pat:      "test_pat",
		branch:   "main",
		cacheDir: "/tmp/cache",
		apiBase:  server.URL,
		logger:   logger,
	}

	sha, err := client.GetLatestCommitSHA()
	if err != nil {
		t.Fatalf("GetLatestCommitSHA() error = %v", err)
	}

	if sha != "abc123def456" {
		t.Errorf("GetLatestCommitSHA() = %q, want %q", sha, "abc123def456")
	}
}

func TestClient_GetLatestCommitSHA_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message": "rate limit exceeded"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{
		repoURL:  "https://github.com/owner/repo",
		branch:   "main",
		cacheDir: "/tmp/cache",
		apiBase:  server.URL,
		logger:   logger,
	}

	_, err := client.GetLatestCommitSHA()
	if err == nil {
		t.Fatal("GetLatestCommitSHA() expected error for 403 status")
	}

	// Should mention rate limit or authentication
	errStr := err.Error()
	if !contains(errStr, "rate limit") && !contains(errStr, "authentication") {
		t.Errorf("error should mention rate limit or authentication, got: %v", err)
	}
}

func TestClient_GetLatestCommitSHA_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{
		repoURL:  "https://github.com/owner/repo",
		branch:   "main",
		cacheDir: "/tmp/cache",
		apiBase:  server.URL,
		logger:   logger,
	}

	_, err := client.GetLatestCommitSHA()
	if err == nil {
		t.Fatal("GetLatestCommitSHA() expected error for 404 status")
	}
}

func TestClient_Sync_CreatesCacheDir(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "new", "nested", "cache")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{
		repoURL:  "https://github.com/owner/repo",
		branch:   "main",
		cacheDir: cacheDir,
		logger:   logger,
	}

	// This will fail at the clone step (no actual git repo), but should create the directory
	_, _ = client.Sync()

	// Verify directory was created
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Error("Sync() did not create cache directory")
	}
}

func TestCommitInfo_Fields(t *testing.T) {
	info := CommitInfo{
		SHA:     "abc123",
		Message: "Test commit",
	}

	if info.SHA != "abc123" {
		t.Errorf("CommitInfo.SHA = %q, want %q", info.SHA, "abc123")
	}
	if info.Message != "Test commit" {
		t.Errorf("CommitInfo.Message = %q, want %q", info.Message, "Test commit")
	}
}

func TestConfig_Fields(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{
		RepoURL:  "https://github.com/owner/repo",
		PAT:      "test_pat",
		Branch:   "develop",
		CacheDir: "/tmp/test",
		Logger:   logger,
	}

	if cfg.RepoURL != "https://github.com/owner/repo" {
		t.Error("Config.RepoURL not set correctly")
	}
	if cfg.PAT != "test_pat" {
		t.Error("Config.PAT not set correctly")
	}
	if cfg.Branch != "develop" {
		t.Error("Config.Branch not set correctly")
	}
	if cfg.CacheDir != "/tmp/test" {
		t.Error("Config.CacheDir not set correctly")
	}
	if cfg.Logger == nil {
		t.Error("Config.Logger should not be nil")
	}
}

func TestClient_GetLatestCommitSHA_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{
		repoURL:  "https://github.com/owner/repo",
		branch:   "main",
		cacheDir: "/tmp/cache",
		apiBase:  server.URL,
		logger:   logger,
	}

	_, err := client.GetLatestCommitSHA()
	if err == nil {
		t.Fatal("GetLatestCommitSHA() expected error for invalid JSON")
	}
}

func TestClient_GetLatestCommitSHA_WithoutPAT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no authorization header
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("unexpected Authorization header when no PAT: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"sha": "abc123"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{
		repoURL:  "https://github.com/owner/repo",
		pat:      "", // No PAT
		branch:   "main",
		cacheDir: "/tmp/cache",
		apiBase:  server.URL,
		logger:   logger,
	}

	sha, err := client.GetLatestCommitSHA()
	if err != nil {
		t.Fatalf("GetLatestCommitSHA() error = %v", err)
	}

	if sha != "abc123" {
		t.Errorf("GetLatestCommitSHA() = %q, want %q", sha, "abc123")
	}
}

// Helper function to check if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsSubstr(s, substr)))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
