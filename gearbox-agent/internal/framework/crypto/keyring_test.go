package crypto

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeyRing_NewRandomEntry(t *testing.T) {
	e, err := newRandomEntry("primary")
	if err != nil {
		t.Fatalf("newRandomEntry: %v", err)
	}
	if len(e.KID) != kidLength {
		t.Errorf("kid length = %d, want %d", len(e.KID), kidLength)
	}
	if len(e.Secret) != SecretLength {
		t.Errorf("secret length = %d, want %d", len(e.Secret), SecretLength)
	}
	if e.Role != "primary" {
		t.Errorf("role = %q, want primary", e.Role)
	}
	if e.CreatedAt.IsZero() {
		t.Errorf("CreatedAt is zero")
	}
}

func TestKeyRing_MatchToken_PrefixedFormat(t *testing.T) {
	e, err := newRandomEntry("primary")
	if err != nil {
		t.Fatalf("newRandomEntry: %v", err)
	}
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}

	token := FormatToken(e.KID, e.Secret)
	got, err := kr.MatchToken(token)
	if err != nil {
		t.Fatalf("MatchToken: %v", err)
	}
	if got == nil || got.KID != e.KID {
		t.Errorf("got %+v, want kid=%q", got, e.KID)
	}
}

func TestKeyRing_MatchToken_LegacyFormat(t *testing.T) {
	secret, _ := NewSecret()
	hexKey := hex.EncodeToString(secret)
	kr := &KeyRing{
		Version: 1,
		Entries: []KeyRingEntry{{
			KID: "legacy", Secret: secret, SecretHex: hexKey,
			Role: "primary", CreatedAt: time.Now().UTC(),
		}},
	}

	got, err := kr.MatchToken(hexKey)
	if err != nil {
		t.Fatalf("MatchToken legacy: %v", err)
	}
	if got == nil || got.KID != "legacy" {
		t.Errorf("got %+v, want kid=legacy", got)
	}
}

func TestKeyRing_MatchToken_UnknownKID(t *testing.T) {
	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}

	fakeSecret, _ := NewSecret()
	bogus := FormatToken("000000", fakeSecret)
	if _, err := kr.MatchToken(bogus); err != ErrUnknownKID {
		t.Errorf("MatchToken bogus kid: err=%v, want ErrUnknownKID", err)
	}
}

func TestKeyRing_MatchToken_WrongSecretRightKID(t *testing.T) {
	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}

	wrongSecret, _ := NewSecret()
	mismatched := FormatToken(e.KID, wrongSecret)
	if _, err := kr.MatchToken(mismatched); err != ErrUnknownKID {
		t.Errorf("MatchToken right-kid-wrong-secret: err=%v, want ErrUnknownKID", err)
	}
}

func TestKeyRing_MatchToken_Invalid(t *testing.T) {
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{}}
	cases := []string{
		"",
		"not-a-token",
		"gbx_",
		"gbx_abc_short",
		"gbx_short_" + strings.Repeat("a", roundedSecLen),
		strings.Repeat("z", 64),   // 64 chars but not hex
		strings.Repeat("ab", 30),  // hex but wrong length
	}
	for _, tc := range cases {
		if _, err := kr.MatchToken(tc); err == nil {
			t.Errorf("MatchToken(%q): nil err, want error", tc)
		}
	}
}

func TestKeyRing_MultipleEntries_BothAccepted(t *testing.T) {
	e1, _ := newRandomEntry("primary")
	e2, _ := newRandomEntry("secondary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e1, e2}}

	for _, e := range []KeyRingEntry{e1, e2} {
		token := FormatToken(e.KID, e.Secret)
		got, err := kr.MatchToken(token)
		if err != nil {
			t.Fatalf("MatchToken(%s): %v", e.KID, err)
		}
		if got.KID != e.KID {
			t.Errorf("got kid=%q, want %q", got.KID, e.KID)
		}
	}
}

func TestKeyRing_Add_Cap(t *testing.T) {
	kr := &KeyRing{Version: 1}
	for i := 0; i < MaxKeyRingEntries; i++ {
		e, _ := newRandomEntry("secondary")
		if err := kr.Add(e); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	e, _ := newRandomEntry("secondary")
	if err := kr.Add(e); err != ErrKeyRingFull {
		t.Errorf("Add over cap: err=%v, want ErrKeyRingFull", err)
	}
}

func TestKeyRing_Add_DuplicateKID(t *testing.T) {
	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}
	dup := e
	dup.Secret, _ = NewSecret()
	if err := kr.Add(dup); err != ErrKIDAlreadyExists {
		t.Errorf("Add duplicate kid: err=%v, want ErrKIDAlreadyExists", err)
	}
}

func TestKeyRing_SetPrimary_FlipsRoles(t *testing.T) {
	e1, _ := newRandomEntry("primary")
	e2, _ := newRandomEntry("secondary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e1, e2}}

	if err := kr.SetPrimary(e2.KID); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}
	if kr.Entries[0].Role != "secondary" {
		t.Errorf("e1 role = %q, want secondary", kr.Entries[0].Role)
	}
	if kr.Entries[1].Role != "primary" {
		t.Errorf("e2 role = %q, want primary", kr.Entries[1].Role)
	}
}

func TestKeyRing_Remove_RefusesLast(t *testing.T) {
	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}
	if err := kr.Remove(e.KID); err != ErrCannotRemoveLast {
		t.Errorf("Remove last: err=%v, want ErrCannotRemoveLast", err)
	}
}

func TestKeyRing_Remove_OK(t *testing.T) {
	e1, _ := newRandomEntry("primary")
	e2, _ := newRandomEntry("secondary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e1, e2}}
	if err := kr.Remove(e2.KID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(kr.Entries) != 1 || kr.Entries[0].KID != e1.KID {
		t.Errorf("after remove: %+v", kr.Entries)
	}
}

func TestKeyRing_Snapshot_NoSecrets(t *testing.T) {
	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}
	snap := kr.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].KID != e.KID {
		t.Errorf("snap kid = %q, want %q", snap[0].KID, e.KID)
	}
	if len(snap[0].Fingerprint) != 8 {
		t.Errorf("fingerprint length = %d, want 8", len(snap[0].Fingerprint))
	}
	// Snapshot is just metadata; no secret-leaking fields by construction.
}

func TestKeyRing_Clone_IsDeepCopy(t *testing.T) {
	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}
	clone := kr.Clone()

	clone.Entries[0].Role = "secondary"
	clone.Entries[0].Secret[0] ^= 0xff

	if kr.Entries[0].Role != "primary" {
		t.Errorf("original mutated: role = %q", kr.Entries[0].Role)
	}
	if kr.Entries[0].Secret[0] == clone.Entries[0].Secret[0] {
		t.Errorf("secrets share backing array")
	}
}

func TestKeyRing_FileRoundTrip_Plaintext(t *testing.T) {
	withoutEncryption(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "keyring.json")

	e1, _ := newRandomEntry("primary")
	e2, _ := newRandomEntry("secondary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e1, e2}}

	if err := SaveKeyRing(path, kr); err != nil {
		t.Fatalf("SaveKeyRing: %v", err)
	}

	// File should be human-readable JSON (no GBE1 header when encryption off).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if isEncrypted(raw) {
		t.Errorf("file unexpectedly encrypted (no key configured)")
	}

	loaded, _, err := LoadOrCreateKeyRing(path, "")
	if err != nil {
		t.Fatalf("LoadOrCreateKeyRing: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("loaded entries = %d, want 2", len(loaded.Entries))
	}
	for i, exp := range []KeyRingEntry{e1, e2} {
		if loaded.Entries[i].KID != exp.KID {
			t.Errorf("entry %d kid = %q, want %q", i, loaded.Entries[i].KID, exp.KID)
		}
		if hex.EncodeToString(loaded.Entries[i].Secret) != hex.EncodeToString(exp.Secret) {
			t.Errorf("entry %d secret mismatch after round-trip", i)
		}
	}
}

func TestKeyRing_FileRoundTrip_Encrypted(t *testing.T) {
	withEncryption(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "keyring.json")

	e, _ := newRandomEntry("primary")
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{e}}

	if err := SaveKeyRing(path, kr); err != nil {
		t.Fatalf("SaveKeyRing: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !isEncrypted(raw) {
		t.Errorf("file not encrypted but encryption is configured")
	}

	loaded, _, err := LoadOrCreateKeyRing(path, "")
	if err != nil {
		t.Fatalf("LoadOrCreateKeyRing: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].KID != e.KID {
		t.Errorf("loaded: %+v", loaded.Entries)
	}
}

func TestKeyRing_MigrateFromLegacyAPIKeyFile(t *testing.T) {
	withoutEncryption(t)

	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "api-key")
	keyringPath := filepath.Join(dir, "keyring.json")

	legacyKey, _ := GenerateAPIKey()
	if err := WriteAPIKey(legacyPath, legacyKey); err != nil {
		t.Fatalf("WriteAPIKey: %v", err)
	}

	kr, isNew, err := LoadOrCreateKeyRing(keyringPath, legacyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyRing: %v", err)
	}
	if isNew {
		t.Errorf("isNew = true, want false (migration)")
	}
	if len(kr.Entries) != 1 || kr.Entries[0].KID != "legacy" {
		t.Errorf("expected single legacy entry, got %+v", kr.Entries)
	}
	if hex.EncodeToString(kr.Entries[0].Secret) != legacyKey {
		t.Errorf("migrated secret doesn't match legacy key")
	}

	// Subsequent load should NOT re-migrate.
	kr2, isNew, err := LoadOrCreateKeyRing(keyringPath, legacyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyRing second: %v", err)
	}
	if isNew {
		t.Errorf("re-load: isNew = true, want false")
	}
	if kr2.Entries[0].KID != kr.Entries[0].KID {
		t.Errorf("kid mismatch on reload")
	}

	// Legacy api_key file should still be on disk (read-only fallback).
	if _, err := os.Stat(legacyPath); err != nil {
		t.Errorf("legacy api-key file removed during migration: %v", err)
	}
}

func TestKeyRing_LoadOrCreate_FreshGen(t *testing.T) {
	withoutEncryption(t)

	dir := t.TempDir()
	keyringPath := filepath.Join(dir, "keyring.json")

	kr, isNew, err := LoadOrCreateKeyRing(keyringPath, "")
	if err != nil {
		t.Fatalf("LoadOrCreateKeyRing: %v", err)
	}
	if !isNew {
		t.Errorf("isNew = false, want true")
	}
	if len(kr.Entries) != 1 || kr.Entries[0].Role != "primary" {
		t.Errorf("expected single primary entry, got %+v", kr.Entries)
	}
}

func TestKeyRingPointer_Swap(t *testing.T) {
	e1, _ := newRandomEntry("primary")
	kr1 := &KeyRing{Version: 1, Entries: []KeyRingEntry{e1}}
	p := NewKeyRingPointer(kr1)

	if p.Load() != kr1 {
		t.Errorf("Load() != kr1")
	}

	e2, _ := newRandomEntry("primary")
	kr2 := &KeyRing{Version: 1, Entries: []KeyRingEntry{e2}}
	p.Store(kr2)
	if p.Load() != kr2 {
		t.Errorf("Load() != kr2 after Store")
	}
}

// withEncryption sets the test's GEARBOX_AGENT_ENCRYPTION_KEY for the
// duration of t. withoutEncryption clears it. Both restore prior state on
// cleanup.
func withEncryption(t *testing.T) {
	t.Helper()
	prev, prevSet := os.LookupEnv("GEARBOX_AGENT_ENCRYPTION_KEY")
	// 64 hex chars = 32 bytes
	t.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", strings.Repeat("ab", 32))
	t.Cleanup(func() {
		if prevSet {
			_ = os.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", prev)
		} else {
			_ = os.Unsetenv("GEARBOX_AGENT_ENCRYPTION_KEY")
		}
	})
}

func withoutEncryption(t *testing.T) {
	t.Helper()
	prev, prevSet := os.LookupEnv("GEARBOX_AGENT_ENCRYPTION_KEY")
	_ = os.Unsetenv("GEARBOX_AGENT_ENCRYPTION_KEY")
	t.Cleanup(func() {
		if prevSet {
			_ = os.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", prev)
		}
	})
}
