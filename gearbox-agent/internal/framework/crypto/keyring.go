package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// KeyRing is the on-disk + in-memory representation of an agent's accepted
// API keys. Multiple keys can be valid simultaneously to support zero-
// downtime rotation — the controller installs a new key as `secondary`,
// flips it to `primary`, then deletes the old one after the overlap window.
//
// Threading: callers MUST treat KeyRing as immutable after creation. To
// mutate (Phase 2's install/use/remove endpoints), build a fresh KeyRing
// and atomically replace the [*atomic.Pointer] holding it. The middleware
// reads via [atomic.Pointer.Load] on every request, so swap is lock-free.
//
// File format (on disk, JSON, optionally GBE1-encrypted):
//
//	{
//	  "version": 1,
//	  "entries": [
//	    {"kid": "legacy", "secret": "<hex-of-32-bytes>", "role": "primary",
//	     "created_at": "2026-05-17T15:00:00Z"}
//	  ]
//	}
type KeyRing struct {
	Version int            `json:"version"`
	Entries []KeyRingEntry `json:"entries"`
}

// KeyRingEntry is one accepted key.
//
// On disk Secret is stored as a 64-character hex string (32 random bytes);
// in memory it's the raw bytes. Role is "primary" (preferred for outbound
// signing — though agents don't sign anything, this is for symmetry with
// the dashboard side) or "secondary" (still accepted; not primary).
type KeyRingEntry struct {
	KID       string    `json:"kid"`
	Secret    []byte    `json:"-"`
	SecretHex string    `json:"secret"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// KeyRingMetadata is the safe-to-share view of a KeyRingEntry — never
// includes the actual secret. Used by [KeyRing.Snapshot] and the
// GET /api/v1/system/keyring endpoint.
type KeyRingMetadata struct {
	KID         string    `json:"kid"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	Fingerprint string    `json:"fingerprint"`
}

// MaxKeyRingEntries caps total entries to prevent runaway growth from a
// buggy rotator. Real overlap rotation needs 2; the buffer slots cover
// retry storms during partial rotations.
const MaxKeyRingEntries = 4

// Token format:
//
//	gbx_<kid>_<base64url(secret)>
//
// Where kid is 6 hex chars (3 random bytes), secret is 32 random bytes.
// Total length: 4 + 6 + 1 + 43 = 54 chars.
//
// Legacy tokens have no prefix — they're 64 hex chars (32 bytes) — and
// are matched against any entry's secret bytes via constant-time compare.
const (
	tokenPrefix    = "gbx_"
	kidLength      = 6
	roundedSecLen  = 43 // base64url(32 bytes) with no padding
	legacyTokenLen = 64 // hex of 32 bytes
)

// Errors returned by keyring operations.
var (
	ErrUnknownKID      = errors.New("keyring: unknown kid")
	ErrInvalidToken    = errors.New("keyring: invalid token format")
	ErrKIDAlreadyExists = errors.New("keyring: kid already exists")
	ErrCannotRemoveLast = errors.New("keyring: cannot remove the only remaining key")
	ErrKeyRingFull      = errors.New("keyring: at maximum entry count")
)

// LoadOrCreateKeyRing loads a keyring from path, migrating from a legacy
// single-key file at legacyAPIKeyPath if present, or generating a fresh
// entry if neither file exists.
//
// Returns the keyring and a boolean indicating a new keyring was created
// (so the caller can log/emit the initial key to the operator).
func LoadOrCreateKeyRing(path, legacyAPIKeyPath string) (*KeyRing, bool, error) {
	// Try to read existing keyring file.
	if kr, err := readKeyRingFile(path); err == nil {
		return kr, false, nil
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("read keyring: %w", err)
	}

	// No keyring file. Migrate from legacy api_key file if present.
	//
	// Failure modes we distinguish:
	//   - legacy file doesn't exist        → fall through to fresh-gen
	//   - legacy file exists but errors    → fatal (caller may have set
	//                                        GEARBOX_AGENT_ENCRYPTION_KEY
	//                                        wrong; silently generating
	//                                        a fresh keyring would lock
	//                                        the dashboard out for no
	//                                        good reason)
	//   - legacy file exists but is malformed (not 64 hex chars) →
	//                                        fatal for the same reason
	//                                        (a wedged file is operator-
	//                                        visible; a silent rotation
	//                                        is not).
	if legacyAPIKeyPath != "" {
		if _, statErr := os.Stat(legacyAPIKeyPath); statErr == nil {
			legacyKey, rerr := ReadAPIKey(legacyAPIKeyPath)
			if rerr != nil {
				return nil, false, fmt.Errorf("read legacy api-key file %q: %w", legacyAPIKeyPath, rerr)
			}
			secret, derr := hex.DecodeString(strings.TrimSpace(legacyKey))
			if derr != nil {
				return nil, false, fmt.Errorf("legacy api-key file %q is not valid hex: %w", legacyAPIKeyPath, derr)
			}
			if len(secret) != SecretLength {
				return nil, false, fmt.Errorf("legacy api-key file %q has secret length %d, want %d", legacyAPIKeyPath, len(secret), SecretLength)
			}
			kr := &KeyRing{
				Version: 1,
				Entries: []KeyRingEntry{{
					KID:       "legacy",
					Secret:    secret,
					SecretHex: hex.EncodeToString(secret),
					Role:      "primary",
					CreatedAt: time.Now().UTC(),
				}},
			}
			if werr := writeKeyRingFile(path, kr); werr != nil {
				return nil, false, fmt.Errorf("write migrated keyring: %w", werr)
			}
			return kr, false, nil
		} else if !os.IsNotExist(statErr) {
			return nil, false, fmt.Errorf("stat legacy api-key file %q: %w", legacyAPIKeyPath, statErr)
		}
	}

	// No legacy file either. Generate fresh.
	entry, err := newRandomEntry("primary")
	if err != nil {
		return nil, false, err
	}
	kr := &KeyRing{Version: 1, Entries: []KeyRingEntry{entry}}
	if err := writeKeyRingFile(path, kr); err != nil {
		return nil, false, fmt.Errorf("write keyring: %w", err)
	}
	return kr, true, nil
}

// SaveKeyRing writes kr to path atomically (tmpfile + rename). If the
// configured KeyProvider returns a key, the file is GBE1-encrypted on disk.
func SaveKeyRing(path string, kr *KeyRing) error {
	return writeKeyRingFile(path, kr)
}

// Snapshot returns a copy of kr's entry metadata (no secrets) — safe to
// serialize to authenticated callers.
func (kr *KeyRing) Snapshot() []KeyRingMetadata {
	out := make([]KeyRingMetadata, 0, len(kr.Entries))
	for _, e := range kr.Entries {
		out = append(out, KeyRingMetadata{
			KID:         e.KID,
			Role:        e.Role,
			CreatedAt:   e.CreatedAt,
			Fingerprint: fingerprint(e.Secret),
		})
	}
	return out
}

// Primary returns the entry currently marked role=primary, or nil if none.
// The dashboard's outbound client uses this to pick which key to sign with;
// the agent itself doesn't care which entry is primary for inbound auth.
func (kr *KeyRing) Primary() *KeyRingEntry {
	for i := range kr.Entries {
		if kr.Entries[i].Role == "primary" {
			return &kr.Entries[i]
		}
	}
	return nil
}

// MatchToken parses token (either prefixed `gbx_<kid>_<b64>` or legacy
// 64-hex) and returns the matching entry, or nil + ErrUnknownKID / nil +
// ErrInvalidToken on failure.
//
// Comparisons against entry secrets are constant-time (subtle.
// ConstantTimeCompare). The prefixed-token path walks every entry and
// performs the compare on each one regardless of whether the kid
// matched, so total runtime doesn't reveal which kids exist on this
// agent — kids are not strictly secret (they're sent in the request
// header), but leaking which ones an agent currently accepts via
// timing makes rotation-history enumeration cheap, which we'd rather
// not.
//
// Note: subtle.ConstantTimeCompare returns 0 (without timing-uniform
// comparison) when the two slices differ in length. The kid compare
// is therefore length-dependent — which is fine because every kid in
// the system is the same length (kidLength = 6 hex chars; the legacy
// kid "legacy" is also 6 chars by deliberate convention). Custom
// kids that don't match that length will hash-mismatch on lookup,
// which is the intended failure mode.
func (kr *KeyRing) MatchToken(token string) (*KeyRingEntry, error) {
	if strings.HasPrefix(token, tokenPrefix) {
		// Prefixed: gbx_<kid>_<b64secret>
		rest := token[len(tokenPrefix):]
		sep := strings.Index(rest, "_")
		if sep != kidLength {
			return nil, ErrInvalidToken
		}
		kid := rest[:sep]
		secB64 := rest[sep+1:]
		secret, err := base64.RawURLEncoding.DecodeString(secB64)
		if err != nil || len(secret) != SecretLength {
			return nil, ErrInvalidToken
		}
		// Walk every entry, compare every secret, record the match
		// without short-circuiting. The two ConstantTimeEq calls plus
		// the bitwise AND keep the timing uniform regardless of which
		// entry (if any) matches.
		var matched *KeyRingEntry
		for i := range kr.Entries {
			kidEq := subtle.ConstantTimeCompare([]byte(kr.Entries[i].KID), []byte(kid))
			secretEq := subtle.ConstantTimeCompare(kr.Entries[i].Secret, secret)
			if kidEq&secretEq == 1 {
				matched = &kr.Entries[i]
			}
		}
		if matched == nil {
			return nil, ErrUnknownKID
		}
		return matched, nil
	}

	// Legacy: 64 hex chars, brute-compare against every entry's secret.
	if len(token) != legacyTokenLen {
		return nil, ErrInvalidToken
	}
	secret, err := hex.DecodeString(token)
	if err != nil || len(secret) != SecretLength {
		return nil, ErrInvalidToken
	}
	// Constant-time loop: walk every entry, compare every secret, return
	// the matched one. We deliberately don't short-circuit on the first
	// mismatch to keep timing uniform across keyring contents.
	var matched *KeyRingEntry
	for i := range kr.Entries {
		if subtle.ConstantTimeCompare(kr.Entries[i].Secret, secret) == 1 {
			matched = &kr.Entries[i]
		}
	}
	if matched == nil {
		return nil, ErrUnknownKID
	}
	return matched, nil
}

// Add appends a new entry. Refuses duplicate kid or if the ring is at
// MaxKeyRingEntries. The caller is responsible for atomically swapping
// the keyring pointer afterwards.
func (kr *KeyRing) Add(entry KeyRingEntry) error {
	if len(kr.Entries) >= MaxKeyRingEntries {
		return ErrKeyRingFull
	}
	for _, e := range kr.Entries {
		if e.KID == entry.KID {
			return ErrKIDAlreadyExists
		}
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.SecretHex == "" {
		entry.SecretHex = hex.EncodeToString(entry.Secret)
	}
	kr.Entries = append(kr.Entries, entry)
	return nil
}

// SetPrimary flips the named entry's role to "primary" and demotes any
// other primary to "secondary".
func (kr *KeyRing) SetPrimary(kid string) error {
	found := false
	for i := range kr.Entries {
		if kr.Entries[i].KID == kid {
			found = true
		}
	}
	if !found {
		return ErrUnknownKID
	}
	for i := range kr.Entries {
		if kr.Entries[i].KID == kid {
			kr.Entries[i].Role = "primary"
		} else if kr.Entries[i].Role == "primary" {
			kr.Entries[i].Role = "secondary"
		}
	}
	return nil
}

// Remove deletes the named entry. Refuses to remove the only remaining
// entry — an operator error otherwise bricks the box.
func (kr *KeyRing) Remove(kid string) error {
	if len(kr.Entries) <= 1 {
		return ErrCannotRemoveLast
	}
	for i := range kr.Entries {
		if kr.Entries[i].KID == kid {
			kr.Entries = append(kr.Entries[:i], kr.Entries[i+1:]...)
			return nil
		}
	}
	return ErrUnknownKID
}

// Clone returns a deep copy of kr — useful for the read-modify-write
// pattern when mutating before pointer swap.
func (kr *KeyRing) Clone() *KeyRing {
	out := &KeyRing{Version: kr.Version, Entries: make([]KeyRingEntry, len(kr.Entries))}
	for i, e := range kr.Entries {
		secCopy := make([]byte, len(e.Secret))
		copy(secCopy, e.Secret)
		out.Entries[i] = KeyRingEntry{
			KID:       e.KID,
			Secret:    secCopy,
			SecretHex: e.SecretHex,
			Role:      e.Role,
			CreatedAt: e.CreatedAt,
		}
	}
	return out
}

// FormatToken builds the wire token `gbx_<kid>_<base64url(secret)>` for
// an entry. Used by the dashboard side, never by the agent itself.
func FormatToken(kid string, secret []byte) string {
	return tokenPrefix + kid + "_" + base64.RawURLEncoding.EncodeToString(secret)
}

// NewKID generates a fresh 6-hex-char key identifier. Random rather than
// sequential so multi-controller / fresh-install scenarios don't collide.
func NewKID() (string, error) {
	b := make([]byte, kidLength/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewSecret returns 32 random bytes for a new keyring entry.
func NewSecret() ([]byte, error) {
	b := make([]byte, SecretLength)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// newRandomEntry returns a fully-populated KeyRingEntry with a fresh kid
// + secret.
func newRandomEntry(role string) (KeyRingEntry, error) {
	kid, err := NewKID()
	if err != nil {
		return KeyRingEntry{}, err
	}
	secret, err := NewSecret()
	if err != nil {
		return KeyRingEntry{}, err
	}
	return KeyRingEntry{
		KID:       kid,
		Secret:    secret,
		SecretHex: hex.EncodeToString(secret),
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// fingerprint returns the first 8 hex chars of sha256(secret) — used only
// in the metadata view so a human can diff two keyrings without seeing
// the actual secret.
func fingerprint(secret []byte) string {
	sum := sha256.Sum256(secret)
	return hex.EncodeToString(sum[:4])
}

// readKeyRingFile reads, optionally decrypts, and JSON-unmarshals path.
func readKeyRingFile(path string) (*KeyRing, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isEncrypted(data) {
		key, kerr := DefaultKeyProvider.Key()
		if kerr != nil {
			return nil, fmt.Errorf("key provider: %w", kerr)
		}
		if key == nil {
			return nil, ErrKeyRequired
		}
		plaintext, derr := decryptSecret(data, key)
		if derr != nil {
			return nil, fmt.Errorf("decrypt keyring: %w", derr)
		}
		data = []byte(plaintext)
	}
	var kr KeyRing
	if err := json.Unmarshal(data, &kr); err != nil {
		return nil, fmt.Errorf("parse keyring: %w", err)
	}
	if err := hydrateSecrets(&kr); err != nil {
		return nil, err
	}
	return &kr, nil
}

// hydrateSecrets decodes the on-disk hex strings into raw bytes on each
// entry. Returns an error if any entry's secret is malformed.
func hydrateSecrets(kr *KeyRing) error {
	for i := range kr.Entries {
		raw, err := hex.DecodeString(kr.Entries[i].SecretHex)
		if err != nil {
			return fmt.Errorf("entry %q: decode secret: %w", kr.Entries[i].KID, err)
		}
		if len(raw) != SecretLength {
			return fmt.Errorf("entry %q: secret length %d, want %d", kr.Entries[i].KID, len(raw), SecretLength)
		}
		kr.Entries[i].Secret = raw
	}
	return nil
}

// writeKeyRingFile serializes kr to JSON and atomically writes to path,
// encrypting with the default KeyProvider when configured.
//
// Does NOT mutate the input keyring. KeyRing values are shared via
// [*atomic.Pointer] and treated as immutable; populating SecretHex
// directly on the live entries would race with concurrent readers in
// the auth middleware. We marshal off a local snapshot whose entries
// have SecretHex backfilled from Secret where needed.
func writeKeyRingFile(path string, kr *KeyRing) error {
	snapshot := &KeyRing{Version: kr.Version, Entries: make([]KeyRingEntry, len(kr.Entries))}
	for i, e := range kr.Entries {
		snapshot.Entries[i] = e
		if snapshot.Entries[i].SecretHex == "" {
			snapshot.Entries[i].SecretHex = hex.EncodeToString(e.Secret)
		}
	}

	plaintext, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal keyring: %w", err)
	}

	var data []byte
	key, kerr := DefaultKeyProvider.Key()
	if kerr != nil {
		return fmt.Errorf("key provider: %w", kerr)
	}
	if key != nil {
		data, err = encryptSecret(string(plaintext), key)
		if err != nil {
			return fmt.Errorf("encrypt keyring: %w", err)
		}
	} else {
		data = plaintext
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Atomic write: tmpfile in the same dir, then rename. Same-fs guarantee
	// makes the rename atomic on Linux. See `man 2 rename`.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".keyring-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Chmod(APIKeyFilePerms); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// KeyRingPointer is a tiny helper around [*atomic.Pointer[KeyRing]] —
// callers store one in the agent server struct and the auth middleware
// reads it once per request. Lock-free swaps are the whole point.
type KeyRingPointer struct {
	p atomic.Pointer[KeyRing]
}

// NewKeyRingPointer returns a pointer holding kr.
func NewKeyRingPointer(kr *KeyRing) *KeyRingPointer {
	p := &KeyRingPointer{}
	p.p.Store(kr)
	return p
}

// Load returns the current keyring snapshot. Always non-nil after
// NewKeyRingPointer.
func (p *KeyRingPointer) Load() *KeyRing {
	return p.p.Load()
}

// Store atomically replaces the keyring. The caller has typically cloned
// the previous keyring, mutated it, persisted it to disk, and now swaps
// the in-memory copy.
func (p *KeyRingPointer) Store(kr *KeyRing) {
	p.p.Store(kr)
}
