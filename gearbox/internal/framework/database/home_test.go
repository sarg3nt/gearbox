package database

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnsureDefaultHomeBoard_CreatesAndIsIdempotent(t *testing.T) {
	db := setupTestDB(t)

	first, err := db.EnsureDefaultHomeBoard()
	if err != nil {
		t.Fatalf("first EnsureDefaultHomeBoard failed: %v", err)
	}
	if first == nil {
		t.Fatalf("expected board, got nil")
	}
	if first.Slug != DefaultBoardSlug {
		t.Errorf("expected slug %q, got %q", DefaultBoardSlug, first.Slug)
	}

	second, err := db.EnsureDefaultHomeBoard()
	if err != nil {
		t.Fatalf("second EnsureDefaultHomeBoard failed: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("expected idempotent reuse of board %d, got new id %d", first.ID, second.ID)
	}
}

func TestHomeTilesCRUD(t *testing.T) {
	db := setupTestDB(t)
	board, err := db.EnsureDefaultHomeBoard()
	if err != nil {
		t.Fatalf("EnsureDefaultHomeBoard: %v", err)
	}

	tile := &HomeTile{
		BoardID: board.ID,
		Type:    TileTypeBookmark,
		X:       0, Y: 0, W: 2, H: 1,
		Config: json.RawMessage(`{"url":"https://example.com","name":"Example"}`),
	}
	id, err := db.CreateHomeTile(tile)
	if err != nil {
		t.Fatalf("CreateHomeTile: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero tile id")
	}

	fetched, err := db.GetHomeTile(id)
	if err != nil || fetched == nil {
		t.Fatalf("GetHomeTile: tile=%v err=%v", fetched, err)
	}
	if fetched.Type != TileTypeBookmark {
		t.Errorf("expected type %q, got %q", TileTypeBookmark, fetched.Type)
	}

	if err := db.UpdateHomeTileLayout(id, 4, 2, 4, 2, 5); err != nil {
		t.Fatalf("UpdateHomeTileLayout: %v", err)
	}
	fetched, _ = db.GetHomeTile(id)
	if fetched.X != 4 || fetched.Y != 2 || fetched.W != 4 || fetched.H != 2 || fetched.SortOrder != 5 {
		t.Errorf("layout not applied: %+v", fetched)
	}

	newCfg := json.RawMessage(`{"url":"https://changed.test","name":"Changed"}`)
	if err := db.UpdateHomeTileConfig(id, newCfg); err != nil {
		t.Fatalf("UpdateHomeTileConfig: %v", err)
	}
	fetched, _ = db.GetHomeTile(id)
	if !strings.Contains(string(fetched.Config), "changed.test") {
		t.Errorf("config not updated: %s", string(fetched.Config))
	}

	tiles, err := db.ListHomeTiles(board.ID)
	if err != nil || len(tiles) != 1 {
		t.Fatalf("ListHomeTiles: tiles=%v err=%v", tiles, err)
	}

	if err := db.DeleteHomeTile(id); err != nil {
		t.Fatalf("DeleteHomeTile: %v", err)
	}
	gone, _ := db.GetHomeTile(id)
	if gone != nil {
		t.Errorf("expected tile to be gone, got %+v", gone)
	}
}

func TestHomeTileSecret_RoundTripAndCascade(t *testing.T) {
	db := setupTestDB(t)
	board, _ := db.EnsureDefaultHomeBoard()

	tile := &HomeTile{BoardID: board.ID, Type: TileTypeApp, W: 2, H: 1, Config: json.RawMessage(`{}`)}
	id, err := db.CreateHomeTile(tile)
	if err != nil {
		t.Fatalf("CreateHomeTile: %v", err)
	}

	// No secret yet.
	if has, _ := db.HasHomeTileSecret(id); has {
		t.Errorf("expected no secret for new tile")
	}

	payload := []byte("encrypted-bytes")
	if err := db.SetHomeTileSecret(id, payload); err != nil {
		t.Fatalf("SetHomeTileSecret: %v", err)
	}

	got, err := db.GetHomeTileSecret(id)
	if err != nil {
		t.Fatalf("GetHomeTileSecret: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("secret round-trip mismatch: got %q, want %q", got, payload)
	}

	// Replacing the payload overwrites the row.
	replaced := []byte("rotated-bytes")
	if err := db.SetHomeTileSecret(id, replaced); err != nil {
		t.Fatalf("SetHomeTileSecret (replace): %v", err)
	}
	got, _ = db.GetHomeTileSecret(id)
	if string(got) != string(replaced) {
		t.Errorf("secret not replaced: got %q", got)
	}

	// Clearing with an empty payload removes the row.
	if err := db.SetHomeTileSecret(id, nil); err != nil {
		t.Fatalf("SetHomeTileSecret (clear): %v", err)
	}
	if has, _ := db.HasHomeTileSecret(id); has {
		t.Errorf("expected secret to be cleared")
	}

	// Re-add and verify the FK cascade removes it when the tile dies.
	_ = db.SetHomeTileSecret(id, payload)
	if err := db.DeleteHomeTile(id); err != nil {
		t.Fatalf("DeleteHomeTile: %v", err)
	}
	if has, _ := db.HasHomeTileSecret(id); has {
		t.Errorf("secret should have been cascaded with the tile")
	}
}

func TestDeleteHomeBoard_RefusesOnLastBoard(t *testing.T) {
	db := setupTestDB(t)
	board, _ := db.EnsureDefaultHomeBoard()

	err := db.DeleteHomeBoard(board.ID)
	if err == nil {
		t.Fatal("expected error when deleting only board")
	}
	if !strings.Contains(err.Error(), "only remaining") {
		t.Errorf("expected 'only remaining' error, got %v", err)
	}
}
