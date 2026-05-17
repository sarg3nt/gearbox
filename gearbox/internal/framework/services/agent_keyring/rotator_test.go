package agent_keyring

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	dashcrypto "github.com/sarg3nt/gearbox/internal/framework/services/crypto"
)

// agentMock fakes the subset of the agent's /api/v1/system/keyring/*
// surface the rotator hits. It keeps a thread-safe in-memory keyring
// and mimics the install/use/remove behaviour so rotator orchestration
// can be tested without importing the agent module.
type agentMock struct {
	mu      sync.Mutex
	entries []agentEntry
}

type agentEntry struct {
	KID       string `json:"kid"`
	SecretB64 string `json:"secret_b64,omitempty"`
	Secret    []byte `json:"-"`
	Role      string `json:"role"`
}

func (a *agentMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/keyring":
			a.handleGet(w)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/keyring/install":
			a.handleInstall(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/keyring/use":
			a.handleUse(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/system/keyring/"):
			kid := strings.TrimPrefix(r.URL.Path, "/api/v1/system/keyring/")
			a.handleDelete(w, kid)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

func (a *agentMock) handleGet(w http.ResponseWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version": 1,
		"entries": a.entries,
	})
}

func (a *agentMock) handleInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KID       string `json:"kid"`
		SecretB64 string `json:"secret_b64"`
		Role      string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	secret, err := base64.RawURLEncoding.DecodeString(body.SecretB64)
	if err != nil || len(secret) != 32 {
		http.Error(w, "bad secret", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "secondary"
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.entries {
		if e.KID == body.KID {
			http.Error(w, "kid exists", http.StatusConflict)
			return
		}
	}
	a.entries = append(a.entries, agentEntry{
		KID: body.KID, Secret: secret, SecretB64: body.SecretB64, Role: body.Role,
	})
	_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "entries": a.entries})
}

func (a *agentMock) handleUse(w http.ResponseWriter, r *http.Request) {
	var body struct{ KID string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for i := range a.entries {
		if a.entries[i].KID == body.KID {
			found = true
		}
	}
	if !found {
		http.Error(w, "unknown kid", http.StatusNotFound)
		return
	}
	for i := range a.entries {
		if a.entries[i].KID == body.KID {
			a.entries[i].Role = "primary"
		} else if a.entries[i].Role == "primary" {
			a.entries[i].Role = "secondary"
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "entries": a.entries})
}

func (a *agentMock) handleDelete(w http.ResponseWriter, kid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.entries) <= 1 {
		http.Error(w, "cannot remove last", http.StatusConflict)
		return
	}
	for i := range a.entries {
		if a.entries[i].KID == kid {
			a.entries = append(a.entries[:i], a.entries[i+1:]...)
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "entries": a.entries})
			return
		}
	}
	http.Error(w, "unknown kid", http.StatusNotFound)
}

func (a *agentMock) entryCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.entries)
}

func (a *agentMock) primaryKID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.entries {
		if e.Role == "primary" {
			return e.KID
		}
	}
	return ""
}

// setupRotator wires a mock agent, a fresh DB, and a seeded box with
// a legacy keyring entry that exists on both sides.
func setupRotator(t *testing.T) (*Rotator, *agentMock, *database.DB, *database.BoxDB) {
	t.Helper()
	t.Setenv("GEARBOX_INSECURE_TLS", "true")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Mock agent — seeded with a "legacy" entry whose secret matches
	// what we'll seed in the dashboard's DB.
	rawSecret := make([]byte, 32)
	for i := range rawSecret {
		rawSecret[i] = byte(i)
	}
	mock := &agentMock{
		entries: []agentEntry{{
			KID:       "legacy",
			Secret:    rawSecret,
			SecretB64: base64.RawURLEncoding.EncodeToString(rawSecret),
			Role:      "primary",
		}},
	}
	srv := httptest.NewServer(mock.handler())
	t.Cleanup(srv.Close)

	// DB + encryptor.
	dbDir := t.TempDir()
	db, err := database.New(filepath.Join(dbDir, "gearbox.db"), logger)
	if err != nil {
		t.Fatalf("database.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	enc, err := dashcrypto.NewEncryptor("test-encryption-secret-32-bytes!")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	box := &database.BoxDB{
		BoxID:           "test-box",
		Name:            "Test Box",
		AgentURL:        srv.URL,
		APIKeyEncrypted: []byte("unused-here"),
		Enabled:         true,
	}
	if err := db.CreateBox(box); err != nil {
		t.Fatalf("CreateBox: %v", err)
	}
	encryptedHex, err := enc.EncryptString(hex.EncodeToString(rawSecret))
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	if err := db.InsertBoxAgentKey(&database.BoxAgentKey{
		BoxID:           box.ID,
		KID:             "legacy",
		SecretEncrypted: encryptedHex,
		Role:            "primary",
	}); err != nil {
		t.Fatalf("InsertBoxAgentKey: %v", err)
	}

	return New(db, enc, logger), mock, db, box
}

func TestRotator_HappyPath(t *testing.T) {
	rotator, mock, db, box := setupRotator(t)

	result, err := rotator.RotateBox(box.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("RotateBox: %v", err)
	}
	if result.OldKID != "legacy" {
		t.Errorf("OldKID = %q, want legacy", result.OldKID)
	}
	if result.NewKID == "" {
		t.Errorf("NewKID is empty")
	}

	// Agent side: 2 entries, new is primary.
	if got := mock.entryCount(); got != 2 {
		t.Errorf("agent entries = %d, want 2", got)
	}
	if got := mock.primaryKID(); got != result.NewKID {
		t.Errorf("agent primary kid = %q, want %q", got, result.NewKID)
	}

	// DB side: new kid is primary, legacy is secondary with retired_at.
	keys, _ := db.GetBoxAgentKeys(box.ID)
	if len(keys) != 2 {
		t.Fatalf("db entries = %d, want 2", len(keys))
	}
	for _, k := range keys {
		switch k.KID {
		case result.NewKID:
			if k.Role != "primary" {
				t.Errorf("new kid role = %q, want primary", k.Role)
			}
		case "legacy":
			if k.Role != "secondary" {
				t.Errorf("legacy role = %q, want secondary", k.Role)
			}
			if !k.RetiredAt.Valid {
				t.Errorf("legacy retired_at not set")
			}
		default:
			t.Errorf("unexpected kid: %q", k.KID)
		}
	}
}

func TestRotator_CleanupRetiredKeys_PastOverlap(t *testing.T) {
	rotator, mock, db, box := setupRotator(t)
	if _, err := rotator.RotateBox(box.ID, 24*time.Hour); err != nil {
		t.Fatalf("RotateBox: %v", err)
	}

	// Tiny overlap → anything retired more than 1ms ago is sweepable.
	time.Sleep(10 * time.Millisecond)
	removed, err := rotator.CleanupRetiredKeys(box.ID, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("CleanupRetiredKeys: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if got := mock.entryCount(); got != 1 {
		t.Errorf("agent entries after cleanup = %d, want 1", got)
	}
	keys, _ := db.GetBoxAgentKeys(box.ID)
	if len(keys) != 1 || keys[0].KID == "legacy" {
		t.Errorf("db keys after cleanup: %+v", keys)
	}
}

func TestRotator_CleanupRetiredKeys_WithinOverlap(t *testing.T) {
	rotator, mock, db, box := setupRotator(t)
	if _, err := rotator.RotateBox(box.ID, 24*time.Hour); err != nil {
		t.Fatalf("RotateBox: %v", err)
	}

	// Within the (generous) overlap → no cleanup.
	removed, err := rotator.CleanupRetiredKeys(box.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupRetiredKeys: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	if got := mock.entryCount(); got != 2 {
		t.Errorf("agent entries = %d, want 2", got)
	}
	keys, _ := db.GetBoxAgentKeys(box.ID)
	if len(keys) != 2 {
		t.Errorf("db keys = %+v, want 2", keys)
	}
}

func TestRotator_NoPrimary_Errors(t *testing.T) {
	rotator, _, _, _ := setupRotator(t)
	if _, err := rotator.RotateBox(9999, 24*time.Hour); err == nil {
		t.Errorf("expected error rotating non-existent box")
	}
}

// Silence unused-import lints when working on the file in isolation.
var _ = fmt.Sprintf
