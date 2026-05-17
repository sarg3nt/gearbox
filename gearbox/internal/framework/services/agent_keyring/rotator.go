// Package agent_keyring orchestrates the three-phase
// install -> use -> remove rotation flow against the agent's
// /api/v1/system/keyring endpoints (issue #72).
//
// The rotator never deletes the old key until an explicit cleanup
// step fires — that's the "overlap window" property that lets a
// rotation survive a controller crash mid-dance. While both keys
// coexist on the agent, the dashboard signs with the new one and the
// agent accepts either, so an interrupted rotation degrades to "two
// working keys, slightly noisier audit log" rather than "agent locked
// out".
package agent_keyring

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
)

// DefaultOverlapWindow is the time that elapses between flipping
// primary and being eligible to remove the old key. 24h chosen to be
// long enough to survive operator-overnight outages but short enough
// to keep secondaries from accumulating. Configurable per-call.
const DefaultOverlapWindow = 24 * time.Hour

// Rotator coordinates the install->use->remove dance for a single box.
// Holds no state beyond the DB + encryptor + logger; every call is
// reentrant and safe to invoke from concurrent goroutines (DB-level
// locking guards the transactional pieces).
type Rotator struct {
	db        *database.DB
	encryptor *crypto.Encryptor
	logger    *slog.Logger
}

// New builds a Rotator. encryptor decrypts box secrets so the rotator
// can build an authenticated agent.Client; db carries the state.
func New(db *database.DB, encryptor *crypto.Encryptor, logger *slog.Logger) *Rotator {
	return &Rotator{db: db, encryptor: encryptor, logger: logger}
}

// RotationResult summarises a successful rotation.
type RotationResult struct {
	NewKID      string
	OldKID      string
	RetireAfter time.Time // when CleanupRetiredKeys would remove the old key
}

// RotateBox runs the three-phase rotation against the box's agent:
//
//  1. Generate a fresh (kid, secret).
//  2. Call agent's keyring/install with role=secondary. Agent now
//     accepts both keys.
//  3. Insert the new entry into box_agent_keys (still secondary).
//  4. Call agent's keyring/use { kid: new }. Agent's primary flips;
//     both keys remain accepted.
//  5. In a DB transaction, flip the row's role to primary and stamp
//     retired_at on the previous primary.
//
// Returns the rotation result with the new and old kids and the time
// at which the old key becomes eligible for removal (default 24h).
//
// Failure handling is by intent simple: if any step errors the caller
// gets it; both old and new keys remain accepted on the agent (since
// install is idempotent and use is idempotent), so retrying is safe.
// The user-facing rotate button surfaces the error.
func (r *Rotator) RotateBox(boxID int64, overlapWindow time.Duration) (*RotationResult, error) {
	if overlapWindow <= 0 {
		overlapWindow = DefaultOverlapWindow
	}

	box, err := r.db.GetBoxByID(boxID)
	if err != nil {
		return nil, fmt.Errorf("load box: %w", err)
	}
	if box == nil {
		return nil, fmt.Errorf("box %d not found", boxID)
	}

	current, err := r.db.GetBoxPrimaryKey(boxID)
	if err != nil {
		return nil, fmt.Errorf("load current primary: %w", err)
	}
	if current == nil {
		return nil, fmt.Errorf("box %d has no primary key — bootstrap a key first", boxID)
	}

	client, err := r.clientForKey(box.AgentURL, current)
	if err != nil {
		return nil, fmt.Errorf("build agent client: %w", err)
	}

	// Generate the new key.
	newKID, err := newKID()
	if err != nil {
		return nil, fmt.Errorf("generate kid: %w", err)
	}
	newSecret, err := newSecret()
	if err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	r.logger.Info("rotation: starting", "box_id", boxID, "old_kid", current.KID, "new_kid", newKID)

	// Step 1 — install on the agent as secondary.
	if _, err := client.KeyRingInstall(newKID, newSecret, "secondary"); err != nil {
		return nil, fmt.Errorf("agent install: %w", err)
	}

	// Step 2 — persist the new key on our side. The on-disk format
	// matches the existing legacy entry: AES-256-GCM ciphertext of the
	// 64-char hex form of the secret, so callers can decrypt uniformly.
	encrypted, err := r.encryptor.EncryptString(hex.EncodeToString(newSecret))
	if err != nil {
		return nil, fmt.Errorf("encrypt new secret: %w", err)
	}
	if err := r.db.InsertBoxAgentKey(&database.BoxAgentKey{
		BoxID:           boxID,
		KID:             newKID,
		SecretEncrypted: encrypted,
		Role:            "secondary",
	}); err != nil {
		return nil, fmt.Errorf("insert new key row: %w", err)
	}

	// Step 3 — flip primary on the agent. Both keys remain accepted.
	if _, err := client.KeyRingUse(newKID); err != nil {
		return nil, fmt.Errorf("agent use: %w", err)
	}

	// Step 4 — flip primary in our DB. Demoted entry gets retired_at
	// stamped so CleanupRetiredKeys can sweep it once overlapWindow
	// elapses.
	if err := r.db.SetBoxPrimaryKey(boxID, newKID); err != nil {
		return nil, fmt.Errorf("flip db primary: %w", err)
	}

	r.logger.Info("rotation: success",
		"box_id", boxID, "old_kid", current.KID, "new_kid", newKID,
		"retire_after", time.Now().Add(overlapWindow))

	return &RotationResult{
		NewKID:      newKID,
		OldKID:      current.KID,
		RetireAfter: time.Now().Add(overlapWindow),
	}, nil
}

// CleanupRetiredKeys removes secondary keys whose retired_at is older
// than overlapWindow. Called from the manual rotate path (immediately
// after a rotation isn't ready to clean up; the operator clicks again
// later, or Phase 4's scheduler does it on a tick).
//
// Returns the number of keys removed.
func (r *Rotator) CleanupRetiredKeys(boxID int64, overlapWindow time.Duration) (int, error) {
	if overlapWindow <= 0 {
		overlapWindow = DefaultOverlapWindow
	}

	box, err := r.db.GetBoxByID(boxID)
	if err != nil {
		return 0, fmt.Errorf("load box: %w", err)
	}
	if box == nil {
		return 0, fmt.Errorf("box %d not found", boxID)
	}
	primary, err := r.db.GetBoxPrimaryKey(boxID)
	if err != nil {
		return 0, fmt.Errorf("load primary: %w", err)
	}
	if primary == nil {
		return 0, fmt.Errorf("box %d has no primary key", boxID)
	}

	all, err := r.db.GetBoxAgentKeys(boxID)
	if err != nil {
		return 0, fmt.Errorf("load keys: %w", err)
	}

	client, err := r.clientForKey(box.AgentURL, primary)
	if err != nil {
		return 0, fmt.Errorf("build agent client: %w", err)
	}

	removed := 0
	for _, k := range all {
		if k.Role == "primary" {
			continue
		}
		if !k.RetiredAt.Valid {
			continue
		}
		// time.Since uses the underlying instant, not the wall-clock zone,
		// so this comparison is correct even when the SQLite driver
		// scanned retired_at as a different (or "naive") timezone.
		if time.Since(k.RetiredAt.Time) < overlapWindow {
			continue
		}

		// Best-effort: remove from agent first, then DB. If the agent
		// 404s, the row is stale and we can still drop our side.
		if _, err := client.KeyRingDelete(k.KID); err != nil {
			if !isNotFoundOrConflict(err) {
				r.logger.Warn("cleanup: agent delete failed; leaving DB row",
					"box_id", boxID, "kid", k.KID, "error", err)
				continue
			}
		}
		if err := r.db.DeleteBoxAgentKey(boxID, k.KID); err != nil {
			r.logger.Warn("cleanup: db delete failed", "box_id", boxID, "kid", k.KID, "error", err)
			continue
		}
		removed++
	}
	return removed, nil
}

// clientForKey decrypts the given key entry and builds an authenticated
// agent.Client tagged with its kid (so X-Gearbox-Kid gets echoed back).
func (r *Rotator) clientForKey(agentURL string, key *database.BoxAgentKey) (*agent.Client, error) {
	hexSecret, err := r.encryptor.DecryptString(key.SecretEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	secret, err := hex.DecodeString(hexSecret)
	if err != nil {
		return nil, fmt.Errorf("decode secret: %w", err)
	}
	// Agent accepts both legacy 64-hex and prefixed gbx_<kid>_<b64>;
	// always send the prefixed form so the audit log sees a real kid.
	token := "gbx_" + key.KID + "_" + base64URLNoPad(secret)
	return agent.NewClientWithKID(agentURL, token, key.KID), nil
}

// isNotFoundOrConflict is true for APIError 404 / 409 — meaning the
// agent already doesn't have the key (404) or refused because it's the
// only one left (409). Both are tolerable during cleanup.
func isNotFoundOrConflict(err error) bool {
	var apiErr *agent.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == 404 || apiErr.StatusCode == 409
}
