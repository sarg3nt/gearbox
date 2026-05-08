package home

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

// setupTestDB mirrors the helper in the database package's own tests.
func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := database.New(filepath.Join(t.TempDir(), "test.db"), logger)
	if err != nil {
		t.Fatalf("database.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSnapshotRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	board, err := db.EnsureDefaultHomeBoard()
	if err != nil {
		t.Fatalf("EnsureDefaultHomeBoard: %v", err)
	}
	// Add a couple of tiles, including one with a non-default config.
	tiles := []database.HomeTile{
		{BoardID: board.ID, Type: database.TileTypeBookmark, X: 0, Y: 0, W: 2, H: 1,
			Config: json.RawMessage(`{"url":"https://a.example","name":"A"}`)},
		{BoardID: board.ID, Type: database.TileTypeApp, X: 2, Y: 0, W: 2, H: 1,
			Config: json.RawMessage(`{"url":"https://b.example","name":"B","app_slug":"sonarr"}`)},
	}
	for _, tt := range tiles {
		if _, err := db.CreateHomeTile(&tt); err != nil {
			t.Fatalf("CreateHomeTile: %v", err)
		}
	}
	// Add a second board so the round-trip exercises multi-board logic.
	if _, err := db.CreateHomeBoard("media", "Media", 1); err != nil {
		t.Fatalf("CreateHomeBoard: %v", err)
	}

	// Build snapshot.
	snap, err := buildSnapshot(db)
	if err != nil {
		t.Fatalf("buildSnapshot: %v", err)
	}
	if snap.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema version: got %d, want %d", snap.SchemaVersion, CurrentSchemaVersion)
	}
	if len(snap.Boards) != 2 {
		t.Fatalf("expected 2 boards, got %d", len(snap.Boards))
	}
	if len(snap.Boards[0].Tiles) != 2 {
		t.Fatalf("expected 2 tiles on default board, got %d", len(snap.Boards[0].Tiles))
	}

	// Re-import. The destructive replace should land on the same shape.
	if err := importSnapshot(db, snap); err != nil {
		t.Fatalf("importSnapshot: %v", err)
	}
	postBoards, _ := db.ListHomeBoards()
	if len(postBoards) != 2 {
		t.Errorf("expected 2 boards after import, got %d", len(postBoards))
	}

	// Tiles on the default board should match what we put in.
	defaultBoard, _ := db.GetHomeBoardBySlug(database.DefaultBoardSlug)
	if defaultBoard == nil {
		t.Fatalf("default board missing after import")
	}
	postTiles, _ := db.ListHomeTiles(defaultBoard.ID)
	if len(postTiles) != 2 {
		t.Errorf("expected 2 tiles after import, got %d", len(postTiles))
	}
}

func TestMigrateSnapshot_RejectsNewerVersion(t *testing.T) {
	snap := &Snapshot{SchemaVersion: 99}
	if _, err := migrateSnapshot(snap); err == nil {
		t.Errorf("expected error for newer-than-supported snapshot")
	}
}

func TestMigrateSnapshot_TreatsZeroAsCurrent(t *testing.T) {
	snap := &Snapshot{SchemaVersion: 0}
	got, err := migrateSnapshot(snap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected schema_version %d, got %d", CurrentSchemaVersion, got.SchemaVersion)
	}
}
