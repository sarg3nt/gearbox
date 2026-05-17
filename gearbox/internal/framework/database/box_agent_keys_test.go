package database

import (
	"testing"
)

func newTestBox(t *testing.T, db *DB, boxIDStr, name string) *BoxDB {
	t.Helper()
	box := &BoxDB{
		BoxID:           boxIDStr,
		Name:            name,
		AgentURL:        "https://example.test:8405",
		APIKeyEncrypted: []byte("encrypted-placeholder"),
		Enabled:         true,
	}
	if err := db.CreateBox(box); err != nil {
		t.Fatalf("CreateBox: %v", err)
	}
	return box
}

// TestBoxAgentKeys_InsertAndLookup exercises the Insert/Get round-
// trip — what the rotator (Phase 2) and the per-box rotate handler
// (Phase 3) rely on for a freshly-created box.
//
// This does NOT exercise the migration's backfill INSERT-FROM-boxes
// path; see TestBoxAgentKeys_MigrationBackfillStatementWorks below
// for that.
func TestBoxAgentKeys_InsertAndLookup(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-a", "Box A")

	if err := db.InsertBoxAgentKey(&BoxAgentKey{
		BoxID:           box.ID,
		KID:             "legacy",
		SecretEncrypted: []byte("encrypted-placeholder"),
		Role:            "primary",
	}); err != nil {
		t.Fatalf("InsertBoxAgentKey: %v", err)
	}

	primary, err := db.GetBoxPrimaryKey(box.ID)
	if err != nil {
		t.Fatalf("GetBoxPrimaryKey: %v", err)
	}
	if primary == nil || primary.KID != "legacy" {
		t.Fatalf("expected legacy primary, got %+v", primary)
	}
}

// TestBoxAgentKeys_MigrationBackfillStatementWorks exercises the
// idempotent INSERT-FROM-boxes statement from migration 000002. We
// can't easily replay the migration on a per-test DB (it only runs
// once), so this test wipes the migrated rows for a single box, then
// re-executes the same backfill SQL against the live DB and asserts
// the expected rows appear. Catches regressions to the SQL itself.
func TestBoxAgentKeys_MigrationBackfillStatementWorks(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-mig", "Migration Box")

	rawDB := db.GetDB()
	if _, err := rawDB.Exec(`DELETE FROM box_agent_keys WHERE box_id = ?`, box.ID); err != nil {
		t.Fatalf("wipe: %v", err)
	}

	const backfillSQL = `
		INSERT INTO box_agent_keys (box_id, kid, secret_encrypted, role, created_at)
		SELECT id, 'legacy', api_key_encrypted, 'primary', created_at
		FROM boxes
		WHERE id = ? AND NOT EXISTS (
		    SELECT 1 FROM box_agent_keys WHERE box_id = boxes.id AND kid = 'legacy'
		)`
	if _, err := rawDB.Exec(backfillSQL, box.ID); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	primary, err := db.GetBoxPrimaryKey(box.ID)
	if err != nil {
		t.Fatalf("GetBoxPrimaryKey: %v", err)
	}
	if primary == nil || primary.KID != "legacy" {
		t.Fatalf("backfill: got %+v, want kid=legacy primary", primary)
	}

	// Re-running the backfill must be a no-op (idempotency guard).
	if _, err := rawDB.Exec(backfillSQL, box.ID); err != nil {
		t.Fatalf("backfill rerun: %v", err)
	}
	keys, err := db.GetBoxAgentKeys(box.ID)
	if err != nil {
		t.Fatalf("GetBoxAgentKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("backfill rerun was not idempotent: %d rows, want 1", len(keys))
	}
}

func TestBoxAgentKeys_SetPrimary_FlipsRolesAtomically(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-b", "Box B")

	for _, kid := range []string{"legacy", "v2"} {
		role := "secondary"
		if kid == "legacy" {
			role = "primary"
		}
		if err := db.InsertBoxAgentKey(&BoxAgentKey{
			BoxID: box.ID, KID: kid, SecretEncrypted: []byte("e"), Role: role,
		}); err != nil {
			t.Fatalf("insert %s: %v", kid, err)
		}
	}

	if err := db.SetBoxPrimaryKey(box.ID, "v2"); err != nil {
		t.Fatalf("SetBoxPrimaryKey: %v", err)
	}

	keys, err := db.GetBoxAgentKeys(box.ID)
	if err != nil {
		t.Fatalf("GetBoxAgentKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
	rolesByKID := map[string]string{}
	for _, k := range keys {
		rolesByKID[k.KID] = k.Role
	}
	if rolesByKID["v2"] != "primary" {
		t.Errorf("v2 role = %q, want primary", rolesByKID["v2"])
	}
	if rolesByKID["legacy"] != "secondary" {
		t.Errorf("legacy role = %q, want secondary", rolesByKID["legacy"])
	}
}

func TestBoxAgentKeys_Delete_RefusesLast(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-c", "Box C")
	if err := db.InsertBoxAgentKey(&BoxAgentKey{
		BoxID: box.ID, KID: "legacy", SecretEncrypted: []byte("e"), Role: "primary",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.DeleteBoxAgentKey(box.ID, "legacy"); err == nil {
		t.Errorf("expected error on deleting only remaining key")
	}
}

func TestBoxAgentKeys_Delete_OK(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-d", "Box D")
	for _, kid := range []string{"legacy", "v2"} {
		role := "secondary"
		if kid == "legacy" {
			role = "primary"
		}
		if err := db.InsertBoxAgentKey(&BoxAgentKey{
			BoxID: box.ID, KID: kid, SecretEncrypted: []byte("e"), Role: role,
		}); err != nil {
			t.Fatalf("insert %s: %v", kid, err)
		}
	}

	if err := db.DeleteBoxAgentKey(box.ID, "v2"); err != nil {
		t.Errorf("DeleteBoxAgentKey: %v", err)
	}
	keys, _ := db.GetBoxAgentKeys(box.ID)
	if len(keys) != 1 || keys[0].KID != "legacy" {
		t.Errorf("after delete: %+v", keys)
	}
}

func TestBoxAgentKeys_TouchLastUsed(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-e", "Box E")
	if err := db.InsertBoxAgentKey(&BoxAgentKey{
		BoxID: box.ID, KID: "legacy", SecretEncrypted: []byte("e"), Role: "primary",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.TouchBoxAgentKeyLastUsed(box.ID, "legacy"); err != nil {
		t.Errorf("TouchBoxAgentKeyLastUsed: %v", err)
	}
	k, _ := db.GetBoxPrimaryKey(box.ID)
	if !k.LastUsedAt.Valid {
		t.Errorf("LastUsedAt not populated after touch")
	}
}

// TestBoxAgentKeys_DeleteBoxClearsDependentKeys verifies that
// DeleteBox removes rows from box_agent_keys for that box, even
// though SQLite's PRAGMA foreign_keys is off in this codebase (so
// the schema-declared CASCADE doesn't run). DeleteBox now wipes
// dependents inside its transaction, which is what this test pins.
func TestBoxAgentKeys_DeleteBoxClearsDependentKeys(t *testing.T) {
	db := setupTestDB(t)
	box := newTestBox(t, db, "box-f", "Box F")
	if err := db.InsertBoxAgentKey(&BoxAgentKey{
		BoxID: box.ID, KID: "legacy", SecretEncrypted: []byte("e"), Role: "primary",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertBoxAgentKey(&BoxAgentKey{
		BoxID: box.ID, KID: "v2", SecretEncrypted: []byte("e2"), Role: "secondary",
	}); err != nil {
		t.Fatalf("insert v2: %v", err)
	}

	if err := db.DeleteBox(box.ID); err != nil {
		t.Fatalf("DeleteBox: %v", err)
	}

	keys, err := db.GetBoxAgentKeys(box.ID)
	if err != nil {
		t.Fatalf("GetBoxAgentKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("keys not removed on box delete: %+v", keys)
	}
}
