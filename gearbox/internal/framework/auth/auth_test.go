package auth

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
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
	// Tests use a sliding timeout of 24h and disable the absolute hard TTL
	// (0). Individual tests that exercise the hard TTL construct their own
	// Manager directly.
	manager, err := NewManager(db, sessionSecret, 24*time.Hour, 0, logger)
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
		name            string
		sessionSecret   string
		timeout         time.Duration
		absoluteTimeout time.Duration
		wantErr         bool
	}{
		{
			name:            "valid config, no hard TTL",
			sessionSecret:   "this_is_a_very_long_session_secret_key_for_testing_purposes",
			timeout:         24 * time.Hour,
			absoluteTimeout: 0,
			wantErr:         false,
		},
		{
			name:            "valid config with hard TTL",
			sessionSecret:   "this_is_a_very_long_session_secret_key_for_testing_purposes",
			timeout:         1 * time.Hour,
			absoluteTimeout: 24 * time.Hour,
			wantErr:         false,
		},
		{
			name:            "short session secret",
			sessionSecret:   "short",
			timeout:         24 * time.Hour,
			absoluteTimeout: 0,
			wantErr:         true,
		},
		{
			name:            "absolute smaller than sliding rejected",
			sessionSecret:   "this_is_a_very_long_session_secret_key_for_testing_purposes",
			timeout:         24 * time.Hour,
			absoluteTimeout: 1 * time.Hour,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(db, tt.sessionSecret, tt.timeout, tt.absoluteTimeout, logger)
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

// 2026-05 audit P2-3: a session that's been kept warm by activity must
// still be expired once its absolute lifetime passes. This test mirrors
// the existing TestManager_Login_Success pattern, then rewinds the
// session-start timestamp in the cookie store and verifies that GetUser
// rejects the request despite the sliding-window timestamp being fresh.
func TestManager_GetUser_AbsoluteTimeout(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()
	// Override timeouts after construction: a generous 24h sliding window
	// but a 1-minute hard TTL. The sliding window never trips here, so a
	// failure must be the hard TTL doing its job.
	manager.timeout = 24 * time.Hour
	manager.absoluteTimeout = 1 * time.Minute

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("Failed to create admin: %v", err)
	}

	// Log in and capture the session cookie.
	loginReq := httptest.NewRequest("GET", "/login", nil)
	loginResp := httptest.NewRecorder()
	if _, err := manager.Login(loginResp, loginReq, "admin", "correct_password"); err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if len(loginResp.Result().Cookies()) == 0 {
		t.Fatal("Login produced no cookie")
	}
	cookie := loginResp.Result().Cookies()[0]

	// Sanity: GetUser succeeds right after login.
	check := httptest.NewRequest("GET", "/", nil)
	check.AddCookie(cookie)
	if _, err := manager.GetUser(check); err != nil {
		t.Fatalf("GetUser right after login failed: %v", err)
	}

	// Rewind sessionStartKey to 2 minutes ago (past the 1-minute hard TTL)
	// while keeping the sliding-window timestamp fresh.
	rewind := httptest.NewRequest("GET", "/", nil)
	rewind.AddCookie(cookie)
	sess, err := manager.sessionStore.Get(rewind, sessionName)
	if err != nil {
		t.Fatalf("could not read session: %v", err)
	}
	sess.Values[sessionStartKey] = time.Now().Add(-2 * time.Minute).Unix()
	sess.Values[sessionLoginKey] = time.Now().Unix()
	rewindResp := httptest.NewRecorder()
	if err := sess.Save(rewind, rewindResp); err != nil {
		t.Fatalf("could not save rewound session: %v", err)
	}
	expired := rewindResp.Result().Cookies()[0]

	expiredReq := httptest.NewRequest("GET", "/", nil)
	expiredReq.AddCookie(expired)
	if _, err := manager.GetUser(expiredReq); err == nil {
		t.Errorf("GetUser returned no error past the absolute hard TTL; want an error")
	}
}

// Follow-up to P2-3 (PR #50 Copilot review): a legacy session that
// pre-dates the sessionStartKey field must get anchored on the next
// ExtendSession call, not slide forever via the loginTime fallback.
func TestManager_ExtendSession_AnchorsLegacySession(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()
	manager.timeout = 24 * time.Hour
	manager.absoluteTimeout = 1 * time.Hour

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}

	// Log in to get a real cookie, then strip the sessionStartKey to
	// simulate a legacy session.
	loginReq := httptest.NewRequest("GET", "/login", nil)
	loginResp := httptest.NewRecorder()
	if _, err := manager.Login(loginResp, loginReq, "admin", "correct_password"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	cookie := loginResp.Result().Cookies()[0]

	stripReq := httptest.NewRequest("GET", "/", nil)
	stripReq.AddCookie(cookie)
	sess, err := manager.sessionStore.Get(stripReq, sessionName)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	delete(sess.Values, sessionStartKey)
	stripResp := httptest.NewRecorder()
	if err := sess.Save(stripReq, stripResp); err != nil {
		t.Fatalf("save stripped session: %v", err)
	}
	legacyCookie := stripResp.Result().Cookies()[0]

	// First ExtendSession on the legacy cookie must anchor sessionStartKey.
	extendReq := httptest.NewRequest("POST", "/", nil)
	extendReq.AddCookie(legacyCookie)
	extendResp := httptest.NewRecorder()
	if err := manager.ExtendSession(extendResp, extendReq); err != nil {
		t.Fatalf("ExtendSession: %v", err)
	}
	anchoredCookie := extendResp.Result().Cookies()[0]

	// Read back: sessionStartKey must now be present.
	readReq := httptest.NewRequest("GET", "/", nil)
	readReq.AddCookie(anchoredCookie)
	anchored, err := manager.sessionStore.Get(readReq, sessionName)
	if err != nil {
		t.Fatalf("read anchored session: %v", err)
	}
	if _, ok := anchored.Values[sessionStartKey].(int64); !ok {
		t.Errorf("sessionStartKey missing after ExtendSession; legacy session is unanchored")
	}
}

// Follow-up to P2-3 (PR #50 Copilot review): the per-save cookie MaxAge
// shrinks as the hard TTL approaches, so the browser drops the cookie
// at the absolute boundary instead of after each save's sliding window.
func TestManager_ExtendSession_CookieMaxAgeShrinks(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()
	// Sliding window 1h, hard TTL 1h (degenerate but unambiguous: any
	// remaining absolute < 1h must cap MaxAge below 1h).
	manager.timeout = 1 * time.Hour
	manager.absoluteTimeout = 1 * time.Hour

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}

	loginReq := httptest.NewRequest("GET", "/login", nil)
	loginResp := httptest.NewRecorder()
	if _, err := manager.Login(loginResp, loginReq, "admin", "correct_password"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	cookie := loginResp.Result().Cookies()[0]

	// Rewind sessionStartKey to 30 minutes ago: 30m left on the hard TTL.
	rewind := httptest.NewRequest("GET", "/", nil)
	rewind.AddCookie(cookie)
	sess, err := manager.sessionStore.Get(rewind, sessionName)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	sess.Values[sessionStartKey] = time.Now().Add(-30 * time.Minute).Unix()
	rewindResp := httptest.NewRecorder()
	if err := sess.Save(rewind, rewindResp); err != nil {
		t.Fatalf("save rewound session: %v", err)
	}
	midCookie := rewindResp.Result().Cookies()[0]

	// ExtendSession should now emit a Set-Cookie with MaxAge ~30 minutes
	// (the remaining absolute), NOT the 1h sliding window.
	extendReq := httptest.NewRequest("POST", "/", nil)
	extendReq.AddCookie(midCookie)
	extendResp := httptest.NewRecorder()
	if err := manager.ExtendSession(extendResp, extendReq); err != nil {
		t.Fatalf("ExtendSession: %v", err)
	}
	out := extendResp.Result().Cookies()[0]

	// MaxAge in seconds; allow a 10-second jitter for test execution time.
	if out.MaxAge < 1700 || out.MaxAge > 1810 { // 28m20s..30m10s
		t.Errorf("Set-Cookie MaxAge=%d, want ~1800 (= remaining absolute TTL); should not be 3600 (= sliding)", out.MaxAge)
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

// 2026-05 audit P2-4: failed-attempt count resets when the previous
// failure was outside the sliding window.
func TestRecordLoginAttempt_WindowReset(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()
	_ = manager // we only need the DB; manager.db is the same instance

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	user, err := db.GetUserByEmail("admin")
	if err != nil || user == nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}

	// 2 fresh failures: count = 2, no cooldown yet.
	for i := 0; i < 2; i++ {
		if err := db.RecordLoginAttempt(user.ID, false); err != nil {
			t.Fatalf("RecordLoginAttempt[%d]: %v", i, err)
		}
	}
	u, _ := db.GetUserByID(user.ID)
	if u.FailedLoginAttempts != 2 {
		t.Errorf("after 2 failures: FailedLoginAttempts = %d, want 2", u.FailedLoginAttempts)
	}
	if u.IsLocked() {
		t.Errorf("after 2 failures: IsLocked = true; want false (below threshold)")
	}

	// Rewind last_failed_attempt to >5min ago via direct DB update — the
	// next failure should reset count to 1, not increment to 3.
	if _, err := db.GetDB().Exec(
		`UPDATE users SET last_failed_attempt = datetime('now', '-10 minutes') WHERE id = ?`, user.ID,
	); err != nil {
		t.Fatalf("rewind last_failed_attempt: %v", err)
	}

	if err := db.RecordLoginAttempt(user.ID, false); err != nil {
		t.Fatalf("RecordLoginAttempt after rewind: %v", err)
	}
	u, _ = db.GetUserByID(user.ID)
	if u.FailedLoginAttempts != 1 {
		t.Errorf("after window-reset: FailedLoginAttempts = %d, want 1 (count restarted)", u.FailedLoginAttempts)
	}
}

// Cooldown tiers: count=3 → 1m cooldown, count=4 → 5m, count=5+ → 15m.
func TestRecordLoginAttempt_TieredCooldown(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()
	_ = manager

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	user, err := db.GetUserByEmail("admin")
	if err != nil || user == nil {
		t.Fatalf("GetUserByEmail: user=%v err=%v", user, err)
	}

	// 2 failures: still no cooldown.
	for i := 0; i < 2; i++ {
		if err := db.RecordLoginAttempt(user.ID, false); err != nil {
			t.Fatalf("RecordLoginAttempt[%d]: %v", i, err)
		}
	}
	u, _ := db.GetUserByID(user.ID)
	if u.LockedUntil != nil {
		t.Errorf("after 2 failures: LockedUntil set; want nil")
	}

	// 3rd: ~1 minute cooldown.
	if err := db.RecordLoginAttempt(user.ID, false); err != nil {
		t.Fatalf("RecordLoginAttempt (3rd): %v", err)
	}
	u, _ = db.GetUserByID(user.ID)
	if u.LockedUntil == nil {
		t.Fatalf("after 3 failures: LockedUntil nil; want ~1min")
	}
	if d := time.Until(*u.LockedUntil); d < 50*time.Second || d > 70*time.Second {
		t.Errorf("after 3 failures: LockedUntil in %v, want ~60s", d)
	}

	// 4th: ~5 minute cooldown.
	if err := db.RecordLoginAttempt(user.ID, false); err != nil {
		t.Fatalf("RecordLoginAttempt (4th): %v", err)
	}
	u, _ = db.GetUserByID(user.ID)
	if d := time.Until(*u.LockedUntil); d < 4*time.Minute+50*time.Second || d > 5*time.Minute+10*time.Second {
		t.Errorf("after 4 failures: LockedUntil in %v, want ~5min", d)
	}

	// 5th: hard 15-minute lockout (existing behavior).
	if err := db.RecordLoginAttempt(user.ID, false); err != nil {
		t.Fatalf("RecordLoginAttempt (5th): %v", err)
	}
	u, _ = db.GetUserByID(user.ID)
	if d := time.Until(*u.LockedUntil); d < 14*time.Minute+50*time.Second || d > 15*time.Minute+10*time.Second {
		t.Errorf("after 5 failures: LockedUntil in %v, want ~15min", d)
	}
}

// A still-active lockout MUST survive a sliding-window reset. The 15-min
// hard lock outlives the 5-min failure window; an attacker could otherwise
// wait 6 min mid-lockout, attempt one more failure, watch the count reset
// to 1, and have locked_until cleared back to NULL — releasing the lock
// 9 min early. Regression test for 2026-05 audit P2-4 follow-up.
func TestRecordLoginAttempt_LockoutSurvivesWindowReset(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()
	_ = manager

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	user, err := db.GetUserByEmail("admin")
	if err != nil || user == nil {
		t.Fatalf("GetUserByEmail: user=%v err=%v", user, err)
	}

	// Trigger the 15-min hard lockout.
	for i := 0; i < 5; i++ {
		if err := db.RecordLoginAttempt(user.ID, false); err != nil {
			t.Fatalf("RecordLoginAttempt[%d]: %v", i, err)
		}
	}
	u, _ := db.GetUserByID(user.ID)
	if u.LockedUntil == nil {
		t.Fatal("setup: expected lockout after 5 failures")
	}
	originalLock := *u.LockedUntil

	// Rewind last_failed_attempt to >5 min ago — the failure window has
	// expired, but the 15-min lockout has not.
	if _, err := db.GetDB().Exec(
		`UPDATE users SET last_failed_attempt = datetime('now', '-10 minutes') WHERE id = ?`,
		user.ID,
	); err != nil {
		t.Fatalf("rewind last_failed_attempt: %v", err)
	}

	// One more failure: the count resets to 1 (window expired), but the
	// existing future locked_until MUST be preserved.
	if err := db.RecordLoginAttempt(user.ID, false); err != nil {
		t.Fatalf("RecordLoginAttempt after rewind: %v", err)
	}
	u, _ = db.GetUserByID(user.ID)
	if u.LockedUntil == nil {
		t.Fatal("locked_until cleared by sliding-window reset; should still be active")
	}
	// The preserved lock should match (or be later than) the original.
	if u.LockedUntil.Before(originalLock.Add(-time.Second)) {
		t.Errorf("locked_until shortened from %v to %v", originalLock, *u.LockedUntil)
	}
}

// The Login() flow returns the SAME generic error for a locked account as
// for a wrong-password or missing-user — preventing email enumeration via
// error-message variance.
func TestManager_Login_LockedAccountReturnsGenericError(t *testing.T) {
	manager, db, cleanup := setupTestManager(t)
	defer cleanup()

	passwordHash, err := HashPassword("correct_password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, _, err := db.EnsureAdminExists(passwordHash, false); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	user, err := db.GetUserByEmail("admin")
	if err != nil || user == nil {
		t.Fatalf("GetUserByEmail: user=%v err=%v", user, err)
	}

	// Lock the account by hitting the cooldown threshold.
	for i := 0; i < 5; i++ {
		if err := db.RecordLoginAttempt(user.ID, false); err != nil {
			t.Fatalf("RecordLoginAttempt[%d]: %v", i, err)
		}
	}
	u, _ := db.GetUserByID(user.ID)
	if !u.IsLocked() {
		t.Fatalf("expected account to be locked after 5 failures")
	}

	// Now try to log in — even with the CORRECT password, we should get
	// the generic "invalid credentials" error, not "account locked".
	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	_, err = manager.Login(w, req, "admin", "correct_password")
	if err == nil {
		t.Fatal("expected error for locked account; got nil")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("locked-account error = %q, want to contain 'invalid credentials' (generic). "+
			"Leaking lockout state lets attackers enumerate valid emails.", err.Error())
	}
	if strings.Contains(strings.ToLower(err.Error()), "lock") {
		t.Errorf("locked-account error mentions 'lock': %q. This is an enumeration aid; "+
			"must be the same generic error as wrong-password.", err.Error())
	}
}
