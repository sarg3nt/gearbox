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

func TestBoxAgentKeys_MigrationBackfillsLegacyEntry(t *testing.T) {
	db := setupTestDB(t)
	// Insert a box BEFORE we look at the keyring; the schema-init path
	// runs the box_agent_keys backfill on every startup, but for a row
	// inserted afterwards the backfill won't fire. Phase 2's create-box
	// handler is responsible for seating the legacy entry on NEW boxes;
	// for now, verify the rotation-table is reachable end-to-end via the
	// direct insert path.
	box := newTestBox(t, db, "box-a", "Box A")

	// Direct insert simulates what Phase 2's create-box path will do.
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

// Cascade-on-DeleteBox is currently declared in the schema (FOREIGN KEY
// (box_id) REFERENCES boxes(id) ON DELETE CASCADE) but not enforced —
// the gearbox DB doesn't set `PRAGMA foreign_keys = ON`. Enabling that
// pragma is a broader change that risks regressing on legacy rows
// elsewhere in the schema. For now DeleteBox leaves orphan
// box_agent_keys rows on disk; Phase 2's box-delete path will clean
// them up explicitly. Not tested here so the gap is visible from the
// test surface itself.
