package auth

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

func setupTestManager(t *testing.T) (*Manager, *database.DB, func()) {
	t.Helper()

	// Create temp database
	tmpFile, err := os.CreateTemp("", "auth_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db, err := database.New(tmpFile.Name(), logger)
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to create database: %v", err)
	}

	sessionSecret := "this_is_a_very_long_session_secret_key_for_testing_purposes"
	manager, err := NewManager(db, sessionSecret, 24*time.Hour, logger)
	if err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to create manager: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return manager, db, cleanup
}

func TestNewManager(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "auth_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db, err := database.New(tmpFile.Name(), logger)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	tests := []struct {
		name          string
		sessionSecret string
		timeout       time.Duration
		wantErr       bool
	}{
		{
			name:          "valid config",
			sessionSecret: "this_is_a_very_long_session_secret_key_for_testing_purposes",
			timeout:       24 * time.Hour,
			wantErr:       false,
		},
		{
			name:          "short session secret",
			sessionSecret: "short",
			timeout:       24 * time.Hour,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(db, tt.sessionSecret, tt.timeout, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && manager == nil {
				t.Error("NewManager() returned nil manager without error")
			}
		})
	}
}

func TestManager_SetSecure(t *testing.T) {
	manager, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Test setting secure to true
	manager.SetSecure(true)
	if !manager.sessionStore.Options.Secure {
		t.Error("SetSecure(true) did not set Secure option")
	}

	// Test setting secure to false
	manager.SetSecure(false)
	if manager.sessionStore.Options.Secure {
		t.Error("SetSecure(false) did not unset Secure option")
	}
}

func TestManager_Login_Success(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()

	// Create test user
	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	_, _, err = db.EnsureAdminExists(passwordHash, false)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	// Create test request and response recorder
	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()

	// Attempt login
	user, err := manager.Login(w, req, "admin", "correct_password")
	if err != nil {
		t.Errorf("Login() with correct credentials failed: %v", err)
	}

	if user == nil {
		t.Error("Login() returned nil user on success")
	}

	// Check that cookie was set
	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Error("Login() did not set any cookies")
	}
}

func TestManager_Login_InvalidCredentials(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()

	// Create test user
	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	_, _, err = db.EnsureAdminExists(passwordHash, false)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "wrong password",
			username: "admin",
			password: "wrong_password",
		},
		{
			name:     "nonexistent user",
			username: "nonexistent",
			password: "any_password",
		},
		{
			name:     "empty username",
			username: "",
			password: "password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/login", nil)
			w := httptest.NewRecorder()

			_, err := manager.Login(w, req, tt.username, tt.password)
			if err == nil {
				t.Error("Login() with invalid credentials should fail")
			}
		})
	}
}

func TestManager_IsAuthenticated(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()

	// Create test user
	passwordHash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	_, _, err = db.EnsureAdminExists(passwordHash, false)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	// Test unauthenticated request
	req := httptest.NewRequest("GET", "/", nil)
	if manager.IsAuthenticated(req) {
		t.Error("IsAuthenticated() should return false for unauthenticated request")
	}

	// Create authenticated session
	w := httptest.NewRecorder()
	_, err = manager.Login(w, req, "admin", "password")
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}

	// Create new request with session cookie
	resp := w.Result()
	req2 := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range resp.Cookies() {
		req2.AddCookie(cookie)
	}

	// Test authenticated request
	if !manager.IsAuthenticated(req2) {
		t.Error("IsAuthenticated() should return true for authenticated request")
	}
}

func TestGenerateToken(t *testing.T) {
	// Generate multiple tokens
	token1, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}

	token2, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}

	// Verify tokens are not empty
	if token1 == "" {
		t.Error("generateToken() returned empty token")
	}

	// Verify tokens are unique
	if token1 == token2 {
		t.Error("generateToken() returned duplicate tokens")
	}

	// Verify token is base64 encoded (should not contain invalid characters)
	if len(token1) < 40 {
		t.Errorf("Token seems too short: %d characters", len(token1))
	}
}
