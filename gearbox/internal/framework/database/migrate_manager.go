package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed all:migrations/files
var migrationFiles embed.FS

// MigrateManager wraps golang-migrate functionality
type MigrateManager struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewMigrateManager creates a new migration manager
func NewMigrateManager(db *sql.DB, logger *slog.Logger) *MigrateManager {
	return &MigrateManager{
		db:     db,
		logger: logger,
	}
}

// hasMigrationFiles checks if there are any valid migration files in the migrations directory
func (m *MigrateManager) hasMigrationFiles() bool {
	entries, err := migrationFiles.ReadDir("migrations/files")
	if err != nil {
		return false
	}

	// Look for .sql files that aren't .gitkeep or other hidden files
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) > 4 && entry.Name()[len(entry.Name())-4:] == ".sql" {
			// Check if it follows migration naming pattern (has .up.sql or .down.sql)
			name := entry.Name()
			if len(name) > 7 && (name[len(name)-7:] == ".up.sql" || name[len(name)-9:] == ".down.sql") {
				return true
			}
		}
	}
	return false
}

// Up runs all pending migrations
func (m *MigrateManager) Up() error {
	// Check if old schema_migrations table exists and migrate it
	if err := m.migrateOldSchemaTable(); err != nil {
		return fmt.Errorf("failed to migrate old schema table: %w", err)
	}

	// Check if any migration files exist
	if !m.hasMigrationFiles() {
		m.logger.Info("no migration files found, skipping migrations")
		return nil
	}

	driver, err := sqlite3.WithInstance(m.db, &sqlite3.Config{
		NoTxWrap: false,
	})
	if err != nil {
		return fmt.Errorf("failed to create sqlite3 driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationFiles, "migrations/files")
	if err != nil {
		return fmt.Errorf("failed to create iofs source: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}
	// Note: Don't close the migrator when using WithInstance
	// as it would close the underlying database connection we passed in
	// The connection is owned by the DB struct and will be closed when DB.Close() is called
	defer func() {
		// Close only the source driver, not the database driver
		if cerr := sourceDriver.Close(); cerr != nil {
			m.logger.Warn("failed to close source driver", "error", cerr)
		}
	}()

	currentVersion, dirty, err := migrator.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	m.logger.Info("current migration version",
		"version", currentVersion,
		"dirty", dirty)

	// If database is dirty, force it to the current version
	// This can happen during the transition from old migration system to new
	if dirty {
		m.logger.Warn("database in dirty state, forcing to clean state",
			"version", currentVersion)

		// Force the version to the current version to mark it as clean
		forceVersion := int(currentVersion) //#nosec -- migration versions are small integers, no overflow risk
		if err := migrator.Force(forceVersion); err != nil {
			return fmt.Errorf("failed to force clean state: %w", err)
		}

		m.logger.Info("database forced to clean state", "version", currentVersion)
	}

	if err := migrator.Up(); err != nil {
		if err == migrate.ErrNoChange {
			m.logger.Info("no pending migrations")
			return nil
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	newVersion, _, _ := migrator.Version()
	m.logger.Info("migrations completed", "new_version", newVersion)

	return nil
}

// migrateOldSchemaTable checks for the old schema_migrations table and migrates it to the new format
func (m *MigrateManager) migrateOldSchemaTable() error {
	// Check if old table exists with old schema
	var hasDescription int
	err := m.db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('schema_migrations')
		WHERE name='description'
	`).Scan(&hasDescription)
	if err != nil {
		// Table doesn't exist yet, that's fine
		return nil
	}

	if hasDescription == 0 {
		// Table exists but already has new schema, nothing to do
		return nil
	}

	m.logger.Info("migrating old schema_migrations table to new format")

	// Get the current max version from old table
	var maxVersion int
	err = m.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&maxVersion)
	if err != nil {
		return fmt.Errorf("failed to get max version from old table: %w", err)
	}

	m.logger.Info("old schema_migrations table status", "max_version", maxVersion)

	// Rename old table
	_, err = m.db.Exec("ALTER TABLE schema_migrations RENAME TO schema_migrations_old")
	if err != nil {
		return fmt.Errorf("failed to rename old schema_migrations table: %w", err)
	}

	m.logger.Info("renamed old schema_migrations table to schema_migrations_old")

	// The new schema_migrations table will be created by golang-migrate
	// and it will automatically set the version based on what migrations have been applied
	// Since we're starting fresh, golang-migrate will run all migrations

	return nil
}

// Version returns the current migration version
func (m *MigrateManager) Version() (uint, bool, error) {
	driver, err := sqlite3.WithInstance(m.db, &sqlite3.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("failed to create sqlite3 driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationFiles, "migrations/files")
	if err != nil {
		return 0, false, fmt.Errorf("failed to create iofs source: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite3", driver)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migrator: %w", err)
	}
	// Note: Don't close the migrator when using WithInstance
	// as it would close the underlying database connection we passed in
	defer func() {
		// Close only the source driver, not the database driver
		if cerr := sourceDriver.Close(); cerr != nil {
			m.logger.Warn("failed to close source driver", "error", cerr)
		}
	}()

	version, dirty, err := migrator.Version()
	if err == migrate.ErrNilVersion {
		return 0, false, nil
	}
	return version, dirty, err
}
