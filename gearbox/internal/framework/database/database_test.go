package database

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// setupTestDB creates a temporary test database.
func setupTestDB(t *testing.T) *DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Reduce noise in tests
	}))

	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	db, err := New(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer db.Close()

	if db.db == nil {
		t.Error("expected database connection to be non-nil")
	}

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestGetDatabaseStats(t *testing.T) {
	db := setupTestDB(t)

	stats, err := db.GetDatabaseStats()
	if err != nil {
		t.Fatalf("failed to get database stats: %v", err)
	}

	if stats == nil {
		t.Fatal("expected stats to be non-nil")
	}

	// Check that stats contain expected tables
	expectedTables := []string{"stats_history", "backend_history", "system_metrics_history"}
	for _, table := range expectedTables {
		if _, found := stats[table]; !found {
			t.Errorf("expected table %s in stats, but not found", table)
		}
	}
}

func TestGetDatabaseStats_Whitelisting(t *testing.T) {
	db := setupTestDB(t)

	stats, err := db.GetDatabaseStats()
	if err != nil {
		t.Fatalf("GetDatabaseStats should not fail: %v", err)
	}

	// Verify only whitelisted table names are returned
	validTables := map[string]bool{
		"stats_history":          true,
		"backend_history":        true,
		"system_metrics_history": true,
		"incidents":              true,
	}

	for tableName := range stats {
		if !validTables[tableName] {
			t.Errorf("unexpected table in stats (not in whitelist): %s", tableName)
		}
	}
}

func TestDisableEntity(t *testing.T) {
	db := setupTestDB(t)
	boxID := "test-server-1"
	userID := "user-1"

	err := db.DisableEntity(boxID, EntityTypeBackend, "test-backend", &userID, "Test disable")
	if err != nil {
		t.Fatalf("failed to disable entity: %v", err)
	}

	// Verify entity was disabled
	entity, err := db.GetDisabledEntity(boxID, EntityTypeBackend, "test-backend")
	if err != nil {
		t.Fatalf("failed to get disabled entity: %v", err)
	}

	if entity == nil {
		t.Fatal("expected entity to be disabled")
	}

	if entity.EntityName != "test-backend" {
		t.Errorf("expected entity name 'test-backend', got %s", entity.EntityName)
	}

	if entity.EntityType != EntityTypeBackend {
		t.Errorf("expected entity type '%s', got %s", EntityTypeBackend, entity.EntityType)
	}

	if entity.DisabledBy == nil || *entity.DisabledBy != "user-1" {
		t.Errorf("expected disabled by 'user-1', got %v", entity.DisabledBy)
	}
}

func TestEnableEntity(t *testing.T) {
	db := setupTestDB(t)
	boxID := "test-server-1"
	userID := "user-1"

	// First disable an entity
	err := db.DisableEntity(boxID, EntityTypeBackend, "test-backend", &userID, "Test disable")
	if err != nil {
		t.Fatalf("failed to disable entity: %v", err)
	}

	// Verify it's disabled
	entity, err := db.GetDisabledEntity(boxID, EntityTypeBackend, "test-backend")
	if err != nil {
		t.Fatalf("failed to get disabled entity: %v", err)
	}
	if entity == nil {
		t.Fatal("expected entity to be disabled")
	}

	// Enable it
	err = db.EnableEntity(boxID, EntityTypeBackend, "test-backend")
	if err != nil {
		t.Fatalf("failed to enable entity: %v", err)
	}

	// Verify it's no longer disabled
	entity, err = db.GetDisabledEntity(boxID, EntityTypeBackend, "test-backend")
	if err != nil {
		t.Fatalf("failed to get disabled entity: %v", err)
	}
	if entity != nil {
		t.Error("expected entity to be enabled (not in disabled list)")
	}
}

func TestIsEntityDisabled(t *testing.T) {
	db := setupTestDB(t)
	boxID := "test-server-1"
	userID := "user-1"

	// Initially not disabled
	disabled, err := db.IsEntityDisabled(boxID, EntityTypeBackend, "test-backend")
	if err != nil {
		t.Fatalf("failed to check if entity disabled: %v", err)
	}
	if disabled {
		t.Error("expected entity to not be disabled initially")
	}

	// Disable it
	err = db.DisableEntity(boxID, EntityTypeBackend, "test-backend", &userID, "Test")
	if err != nil {
		t.Fatalf("failed to disable entity: %v", err)
	}

	// Check it's disabled
	disabled, err = db.IsEntityDisabled(boxID, EntityTypeBackend, "test-backend")
	if err != nil {
		t.Fatalf("failed to check if entity disabled: %v", err)
	}
	if !disabled {
		t.Error("expected entity to be disabled")
	}
}

func TestMigrationSystemInitialization(t *testing.T) {
	db := setupTestDB(t)

	// Check if migration files exist using the same logic as the migration manager
	mm := NewMigrateManager(db.db, db.logger)
	hasMigrations := mm.hasMigrationFiles()

	if !hasMigrations {
		t.Skip("no migration files present, skipping migration verification")
	}

	// Verify migration tracking table exists
	var count int
	err := db.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check for schema_migrations table: %v", err)
	}

	if count != 1 {
		t.Error("expected schema_migrations table to exist")
	}

	// Verify migrations have been run
	err = db.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}

	if count == 0 {
		t.Error("expected at least one migration to have been applied")
	}
}

func TestGetDB(t *testing.T) {
	db := setupTestDB(t)

	sqlDB := db.GetDB()
	if sqlDB == nil {
		t.Error("expected GetDB to return non-nil *sql.DB")
	}

	// Verify it's the same connection
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("expected GetDB to return working connection: %v", err)
	}
}
