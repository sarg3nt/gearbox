package middleware

import (
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func buildTestKeyRing(t *testing.T, n int) (*crypto.KeyRingPointer, []crypto.KeyRingEntry) {
	t.Helper()
	entries := make([]crypto.KeyRingEntry, n)
	for i := range entries {
		kid, err := crypto.NewKID()
		if err != nil {
			t.Fatalf("NewKID: %v", err)
		}
		secret, err := crypto.NewSecret()
		if err != nil {
			t.Fatalf("NewSecret: %v", err)
		}
		role := "secondary"
		if i == 0 {
			role = "primary"
		}
		entries[i] = crypto.KeyRingEntry{
			KID:       kid,
			Secret:    secret,
			SecretHex: hex.EncodeToString(secret),
			Role:      role,
			CreatedAt: time.Now().UTC(),
		}
	}
	kr := &crypto.KeyRing{Version: 1, Entries: entries}
	return crypto.NewKeyRingPointer(kr), entries
}

func wrapTest(t *testing.T, ptr *crypto.KeyRingPointer) http.Handler {
	t.Helper()
	logger := newSilentLogger()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return APIKeyAuth(ptr, logger, nil)(h)
}

func TestAPIKeyAuth_AcceptsPrefixedToken(t *testing.T) {
	ptr, entries := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	token := crypto.FormatToken(entries[0].KID, entries[0].Secret)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}
	if got := w.Header().Get(ResponseHeaderKID); got != entries[0].KID {
		t.Errorf("X-Gearbox-Kid = %q, want %q", got, entries[0].KID)
	}
}

func TestAPIKeyAuth_AcceptsLegacyHexToken(t *testing.T) {
	ptr, entries := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+entries[0].SecretHex)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get(ResponseHeaderKID); got != entries[0].KID {
		t.Errorf("X-Gearbox-Kid = %q, want %q", got, entries[0].KID)
	}
}

func TestAPIKeyAuth_AcceptsSecondaryKey(t *testing.T) {
	ptr, entries := buildTestKeyRing(t, 2)
	h := wrapTest(t, ptr)

	// Use the SECONDARY key (entries[1]).
	token := crypto.FormatToken(entries[1].KID, entries[1].Secret)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get(ResponseHeaderKID); got != entries[1].KID {
		t.Errorf("X-Gearbox-Kid = %q, want %q", got, entries[1].KID)
	}
}

func TestAPIKeyAuth_RejectsMissingHeader(t *testing.T) {
	ptr, _ := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAPIKeyAuth_RejectsBadFormat(t *testing.T) {
	ptr, _ := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic xyz")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAPIKeyAuth_RejectsUnknownKID(t *testing.T) {
	ptr, _ := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	fakeSecret, _ := crypto.NewSecret()
	bogus := crypto.FormatToken("aaaaaa", fakeSecret)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+bogus)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	// No kid header on failure.
	if got := w.Header().Get(ResponseHeaderKID); got != "" {
		t.Errorf("X-Gearbox-Kid leaked on failure: %q", got)
	}
}

func TestAPIKeyAuth_RejectsRightKIDWrongSecret(t *testing.T) {
	ptr, entries := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	wrongSecret, _ := crypto.NewSecret()
	bogus := crypto.FormatToken(entries[0].KID, wrongSecret)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+bogus)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAPIKeyAuth_HotSwapVisibleImmediately(t *testing.T) {
	// Verifies that atomically swapping the keyring pointer is reflected
	// on the next request without a restart. This is the property the
	// Phase 2 rotation endpoints rely on.
	ptr, entries := buildTestKeyRing(t, 1)
	h := wrapTest(t, ptr)

	oldToken := crypto.FormatToken(entries[0].KID, entries[0].Secret)

	// Swap in a fresh keyring with a new kid only.
	newKID, _ := crypto.NewKID()
	newSecret, _ := crypto.NewSecret()
	newEntry := crypto.KeyRingEntry{
		KID:       newKID,
		Secret:    newSecret,
		SecretHex: hex.EncodeToString(newSecret),
		Role:      "primary",
		CreatedAt: time.Now().UTC(),
	}
	ptr.Store(&crypto.KeyRing{Version: 1, Entries: []crypto.KeyRingEntry{newEntry}})

	// Old token should now be rejected.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+oldToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("old token after swap: status = %d, want 401", w.Code)
	}

	// New token works.
	newToken := crypto.FormatToken(newEntry.KID, newEntry.Secret)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+newToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("new token after swap: status = %d, want 200", w.Code)
	}
}
