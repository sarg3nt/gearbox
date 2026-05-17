package database

import (
	"testing"
)

// TestBoxConsoleEnabled_DefaultsFalse confirms the migration's
// DEFAULT 0 actually reaches Create/Read code paths. Important because
// the column was added late in the box-table evolution; a regression
// that defaults it to true would silently enable console on every box
// after a restart, bypassing the per-box opt-in that's the whole
// point of #89 Phase 2c.
func TestBoxConsoleEnabled_DefaultsFalse(t *testing.T) {
	db := setupTestDB(t)
	box := &BoxDB{
		BoxID:           "box-default",
		Name:            "Default Console Box",
		AgentURL:        "https://example.invalid:8405",
		APIKeyEncrypted: []byte("k"),
		Enabled:         true,
		// ConsoleEnabled intentionally omitted — zero value.
	}
	if err := db.CreateBox(box); err != nil {
		t.Fatalf("CreateBox: %v", err)
	}
	got, err := db.GetBoxByBoxID("box-default")
	if err != nil {
		t.Fatalf("GetBoxByBoxID: %v", err)
	}
	if got == nil {
		t.Fatal("GetBoxByBoxID returned nil")
	}
	if got.ConsoleEnabled {
		t.Errorf("ConsoleEnabled = true, want false (per-box opt-in must default off)")
	}
}

// TestBoxConsoleEnabled_PersistsExplicitTrue covers the create + read
// path with an operator who deliberately turned it on.
func TestBoxConsoleEnabled_PersistsExplicitTrue(t *testing.T) {
	db := setupTestDB(t)
	box := &BoxDB{
		BoxID:           "box-explicit",
		Name:            "Explicit Console Box",
		AgentURL:        "https://example.invalid:8405",
		APIKeyEncrypted: []byte("k"),
		Enabled:         true,
		ConsoleEnabled:  true,
	}
	if err := db.CreateBox(box); err != nil {
		t.Fatalf("CreateBox: %v", err)
	}
	got, _ := db.GetBoxByBoxID("box-explicit")
	if got == nil || !got.ConsoleEnabled {
		t.Fatalf("ConsoleEnabled lost across CreateBox/GetBoxByBoxID: got %+v", got)
	}
}

// TestBoxConsoleEnabled_UpdateToggle proves an operator can flip the
// flag on, then back off, via UpdateBox. This is the load-bearing
// path for revoking access — if the off-toggle doesn't round-trip,
// a user revokes via the UI and is surprised when sessions still open.
func TestBoxConsoleEnabled_UpdateToggle(t *testing.T) {
	db := setupTestDB(t)
	box := &BoxDB{
		BoxID:           "box-toggle",
		Name:            "Toggleable Box",
		AgentURL:        "https://example.invalid:8405",
		APIKeyEncrypted: []byte("k"),
		Enabled:         true,
	}
	if err := db.CreateBox(box); err != nil {
		t.Fatalf("CreateBox: %v", err)
	}
	box.ConsoleEnabled = true
	if err := db.UpdateBox(box); err != nil {
		t.Fatalf("UpdateBox on: %v", err)
	}
	got, _ := db.GetBoxByBoxID("box-toggle")
	if !got.ConsoleEnabled {
		t.Fatal("after UpdateBox(on), ConsoleEnabled = false")
	}
	got.ConsoleEnabled = false
	if err := db.UpdateBox(got); err != nil {
		t.Fatalf("UpdateBox off: %v", err)
	}
	got2, _ := db.GetBoxByBoxID("box-toggle")
	if got2.ConsoleEnabled {
		t.Fatal("after UpdateBox(off), ConsoleEnabled = true (revoke didn't persist)")
	}
}

// TestBoxConsoleEnabled_GetEnabledBoxesIncludesFlag verifies the
// list query (used by the Bx fleet page) carries the flag through.
// A regression here would mean the per-box toggle works on the edit
// page but the Bx grid still tries to show the console icon on
// boxes that have it off.
func TestBoxConsoleEnabled_GetEnabledBoxesIncludesFlag(t *testing.T) {
	db := setupTestDB(t)
	for _, b := range []*BoxDB{
		{BoxID: "a", Name: "A", AgentURL: "https://x:8405", APIKeyEncrypted: []byte("k"), Enabled: true, ConsoleEnabled: true},
		{BoxID: "b", Name: "B", AgentURL: "https://x:8405", APIKeyEncrypted: []byte("k"), Enabled: true, ConsoleEnabled: false},
	} {
		if err := db.CreateBox(b); err != nil {
			t.Fatalf("CreateBox %s: %v", b.BoxID, err)
		}
	}
	got, err := db.GetEnabledBoxes()
	if err != nil {
		t.Fatalf("GetEnabledBoxes: %v", err)
	}
	byID := map[string]*BoxDB{}
	for _, b := range got {
		byID[b.BoxID] = b
	}
	if byID["a"] == nil || !byID["a"].ConsoleEnabled {
		t.Errorf("box a: ConsoleEnabled = false in GetEnabledBoxes, want true")
	}
	if byID["b"] == nil || byID["b"].ConsoleEnabled {
		t.Errorf("box b: ConsoleEnabled = true in GetEnabledBoxes, want false")
	}
}
