package api

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
)

func newKeyRingTestHandler(t *testing.T) (*KeyRingHandler, *chi.Mux, *crypto.KeyRingPointer, string) {
	t.Helper()
	t.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "keyring.json")

	// Seed with one entry so tests can start from a sane baseline.
	kid, _ := crypto.NewKID()
	secret, _ := crypto.NewSecret()
	kr := &crypto.KeyRing{Version: 1, Entries: []crypto.KeyRingEntry{{
		KID:       kid,
		Secret:    secret,
		SecretHex: hex.EncodeToString(secret),
		Role:      "primary",
		CreatedAt: time.Now().UTC(),
	}}}
	if err := crypto.SaveKeyRing(path, kr); err != nil {
		t.Fatalf("SaveKeyRing: %v", err)
	}

	ptr := crypto.NewKeyRingPointer(kr)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewKeyRingHandler(ptr, path, logger)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return h, r, ptr, path
}

func TestKeyRing_Get_ReturnsMetadataNoSecrets(t *testing.T) {
	_, r, _, _ := newKeyRingTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/keyring", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !bytes.Contains(w.Body.Bytes(), []byte("entries")) {
		t.Errorf("missing entries field: %s", body)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("\"secret\"")) {
		t.Errorf("response leaked secret field: %s", body)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("\"fingerprint\"")) {
		t.Errorf("missing fingerprint field: %s", body)
	}
}

func TestKeyRing_Install_AddsSecondary(t *testing.T) {
	_, r, ptr, _ := newKeyRingTestHandler(t)

	newSecret, _ := crypto.NewSecret()
	body, _ := json.Marshal(map[string]string{
		"kid":        "abc123",
		"secret_b64": base64.RawURLEncoding.EncodeToString(newSecret),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	kr := ptr.Load()
	if len(kr.Entries) != 2 {
		t.Fatalf("expected 2 entries after install, got %d", len(kr.Entries))
	}
	// Find the new one.
	found := false
	for _, e := range kr.Entries {
		if e.KID == "abc123" {
			if e.Role != "secondary" {
				t.Errorf("new entry role = %q, want secondary", e.Role)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("new kid not in keyring: %+v", kr.Entries)
	}
}

func TestKeyRing_Install_Idempotent(t *testing.T) {
	_, r, _, _ := newKeyRingTestHandler(t)

	newSecret, _ := crypto.NewSecret()
	body, _ := json.Marshal(map[string]string{
		"kid":        "abc123",
		"secret_b64": base64.RawURLEncoding.EncodeToString(newSecret),
	})

	// First install.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first install: status=%d body=%s", w.Code, w.Body.String())
	}

	// Same kid + same secret → 200, no-op.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("second install (idempotent): status=%d, want 200", w.Code)
	}
}

func TestKeyRing_Install_KIDClashDifferentSecret_409(t *testing.T) {
	_, r, _, _ := newKeyRingTestHandler(t)

	secretA, _ := crypto.NewSecret()
	bodyA, _ := json.Marshal(map[string]string{
		"kid": "shared", "secret_b64": base64.RawURLEncoding.EncodeToString(secretA),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(bodyA))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("install A: %d", w.Code)
	}

	secretB, _ := crypto.NewSecret()
	bodyB, _ := json.Marshal(map[string]string{
		"kid": "shared", "secret_b64": base64.RawURLEncoding.EncodeToString(secretB),
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(bodyB))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("install B (collision): status=%d, want 409", w.Code)
	}
}

func TestKeyRing_Install_BadSecretLength(t *testing.T) {
	_, r, _, _ := newKeyRingTestHandler(t)
	body, _ := json.Marshal(map[string]string{
		"kid": "ok", "secret_b64": base64.RawURLEncoding.EncodeToString([]byte("short")),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestKeyRing_Use_FlipsPrimary(t *testing.T) {
	_, r, ptr, _ := newKeyRingTestHandler(t)

	// Add a secondary.
	newSecret, _ := crypto.NewSecret()
	body, _ := json.Marshal(map[string]string{
		"kid":        "v2",
		"secret_b64": base64.RawURLEncoding.EncodeToString(newSecret),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
	r.ServeHTTP(httptest.NewRecorder(), req)

	// Use it.
	body, _ = json.Marshal(map[string]string{"kid": "v2"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/use", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("use: status=%d body=%s", w.Code, w.Body.String())
	}

	kr := ptr.Load()
	primary := kr.Primary()
	if primary == nil || primary.KID != "v2" {
		t.Errorf("primary after use = %+v, want kid=v2", primary)
	}
}

func TestKeyRing_Use_UnknownKID_404(t *testing.T) {
	_, r, _, _ := newKeyRingTestHandler(t)
	body, _ := json.Marshal(map[string]string{"kid": "nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/use", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestKeyRing_Remove_OK(t *testing.T) {
	_, r, ptr, _ := newKeyRingTestHandler(t)

	// Add a secondary first.
	secret, _ := crypto.NewSecret()
	body, _ := json.Marshal(map[string]string{
		"kid": "v2", "secret_b64": base64.RawURLEncoding.EncodeToString(secret),
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body)))

	if got := len(ptr.Load().Entries); got != 2 {
		t.Fatalf("setup: entries = %d, want 2", got)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/system/keyring/v2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status=%d body=%s", w.Code, w.Body.String())
	}
	if got := len(ptr.Load().Entries); got != 1 {
		t.Errorf("after delete: entries = %d, want 1", got)
	}
}

func TestKeyRing_Remove_LastEntry_409(t *testing.T) {
	_, r, ptr, _ := newKeyRingTestHandler(t)
	kr := ptr.Load()
	if len(kr.Entries) != 1 {
		t.Fatalf("setup: want 1 entry, got %d", len(kr.Entries))
	}
	url := "/api/v1/system/keyring/" + kr.Entries[0].KID
	req := httptest.NewRequest(http.MethodDelete, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestKeyRing_Mutations_PersistAcrossReload(t *testing.T) {
	_, r, ptr, path := newKeyRingTestHandler(t)

	// Install + use a second key.
	secret, _ := crypto.NewSecret()
	body, _ := json.Marshal(map[string]string{
		"kid": "v2", "secret_b64": base64.RawURLEncoding.EncodeToString(secret),
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body)))
	useBody, _ := json.Marshal(map[string]string{"kid": "v2"})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/use", bytes.NewReader(useBody)))

	// Reload from disk; the change should be there.
	reloaded, _, err := crypto.LoadOrCreateKeyRing(path, "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	prim := reloaded.Primary()
	if prim == nil || prim.KID != "v2" {
		t.Errorf("reloaded primary = %+v, want kid=v2", prim)
	}

	// In-memory pointer should match.
	if got := ptr.Load().Primary().KID; got != "v2" {
		t.Errorf("in-memory primary = %q", got)
	}

	// Verify the on-disk file mode is 0600 — secret files must not be
	// world-readable even when GBE1 encryption isn't configured.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestKeyRing_Install_FullKeyRing_507(t *testing.T) {
	_, r, _, _ := newKeyRingTestHandler(t)
	// Seed already has 1 entry; fill to MaxKeyRingEntries.
	for i := 0; i < crypto.MaxKeyRingEntries-1; i++ {
		s, _ := crypto.NewSecret()
		body, _ := json.Marshal(map[string]string{
			"kid":        fmt.Sprintf("k%d", i),
			"secret_b64": base64.RawURLEncoding.EncodeToString(s),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("install %d: status=%d body=%s", i, w.Code, w.Body.String())
		}
	}

	// One more should overflow.
	s, _ := crypto.NewSecret()
	body, _ := json.Marshal(map[string]string{
		"kid": "overflow", "secret_b64": base64.RawURLEncoding.EncodeToString(s),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/keyring/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInsufficientStorage {
		t.Errorf("overflow install: status=%d, want 507", w.Code)
	}
}
