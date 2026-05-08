package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// HomeTileType enumerates the tile shapes the Home gear renders.
type HomeTileType string

const (
	// TileTypeApp is a service tile bound to a predefined app (Sonarr, Radarr, etc.).
	TileTypeApp HomeTileType = "app"
	// TileTypeBookmark is a launcher-only link with no widget data or status checks.
	TileTypeBookmark HomeTileType = "bookmark"
	// TileTypeCustomAPI fetches JSON from a user-supplied endpoint and renders chosen fields.
	TileTypeCustomAPI HomeTileType = "customapi"
	// TileTypeIframe embeds a third-party page (Grafana, etc.).
	TileTypeIframe HomeTileType = "iframe"
	// TileTypeClock renders a live clock.
	TileTypeClock HomeTileType = "clock"
	// TileTypeWeather renders local weather (Open-Meteo).
	TileTypeWeather HomeTileType = "weather"
	// TileTypeSearch renders a centered search bar.
	TileTypeSearch HomeTileType = "search"
)

// HomeBoard is one tab/page within the Home dashboard.
type HomeBoard struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HomeTile is a single tile placed on a board.
type HomeTile struct {
	ID         int64           `json:"id"`
	BoardID    int64           `json:"board_id"`
	Type       HomeTileType    `json:"type"`
	X          int             `json:"x"`
	Y          int             `json:"y"`
	W          int             `json:"w"`
	H          int             `json:"h"`
	Config     json.RawMessage `json:"config"`
	SortOrder  int             `json:"sort_order"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// DefaultBoardSlug is the slug used for the auto-seeded "Home" board.
const DefaultBoardSlug = "default"

// initHomeSchema creates the tables backing the Home dashboard gear.
func (d *DB) initHomeSchema() error {
	schema := `
	-- Boards: one or more tabs/pages of tiles. Shared across all users in v1.
	CREATE TABLE IF NOT EXISTS home_boards (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		slug        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
		name        TEXT    NOT NULL,
		sort_order  INTEGER NOT NULL DEFAULT 0,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_home_boards_sort ON home_boards(sort_order);

	-- Tiles: a typed tile placed at (x,y) with size (w,h) on a board.
	-- 'config' is type-specific JSON (URL, app slug, mappings, etc.).
	CREATE TABLE IF NOT EXISTS home_tiles (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		board_id   INTEGER NOT NULL,
		type       TEXT    NOT NULL,
		x          INTEGER NOT NULL DEFAULT 0,
		y          INTEGER NOT NULL DEFAULT 0,
		w          INTEGER NOT NULL DEFAULT 2,
		h          INTEGER NOT NULL DEFAULT 1,
		config     TEXT    NOT NULL DEFAULT '{}',
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (board_id) REFERENCES home_boards(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_home_tiles_board ON home_tiles(board_id);

	-- Per-tile encrypted secrets (API keys, basic-auth passwords).
	-- Stored separately from the tile config so that decrypted values only
	-- materialise inside backend handlers that explicitly join this table.
	CREATE TABLE IF NOT EXISTS home_tile_secrets (
		tile_id           INTEGER PRIMARY KEY,
		encrypted_payload BLOB,
		updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (tile_id) REFERENCES home_tiles(id) ON DELETE CASCADE
	);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create home schema: %w", err)
	}
	return nil
}

// EnsureDefaultHomeBoard creates the default board on first use. Idempotent.
// Called lazily the first time the Home gear renders so that disabling the
// gear doesn't leave orphan rows behind for installs that never opt in.
func (d *DB) EnsureDefaultHomeBoard() (*HomeBoard, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	board, err := d.getBoardBySlugLocked(DefaultBoardSlug)
	if err != nil {
		return nil, err
	}
	if board != nil {
		return board, nil
	}

	now := time.Now()
	res, err := d.db.Exec(
		`INSERT INTO home_boards (slug, name, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		DefaultBoardSlug, "Home", 0, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create default home board: %w", err)
	}
	id, _ := res.LastInsertId()
	return &HomeBoard{
		ID: id, Slug: DefaultBoardSlug, Name: "Home",
		SortOrder: 0, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (d *DB) getBoardBySlugLocked(slug string) (*HomeBoard, error) {
	b := &HomeBoard{}
	err := d.db.QueryRow(
		`SELECT id, slug, name, sort_order, created_at, updated_at FROM home_boards WHERE slug = ? COLLATE NOCASE`,
		slug,
	).Scan(&b.ID, &b.Slug, &b.Name, &b.SortOrder, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load home board: %w", err)
	}
	return b, nil
}

// GetHomeBoardBySlug returns one board by slug, or nil when not found.
func (d *DB) GetHomeBoardBySlug(slug string) (*HomeBoard, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.getBoardBySlugLocked(slug)
}

// ListHomeBoards returns boards ordered by sort_order then name.
func (d *DB) ListHomeBoards() ([]HomeBoard, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(
		`SELECT id, slug, name, sort_order, created_at, updated_at FROM home_boards ORDER BY sort_order, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list home boards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var boards []HomeBoard
	for rows.Next() {
		var b HomeBoard
		if err := rows.Scan(&b.ID, &b.Slug, &b.Name, &b.SortOrder, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan home board: %w", err)
		}
		boards = append(boards, b)
	}
	return boards, rows.Err()
}

// CreateHomeBoard inserts a new board. Slug must be unique.
func (d *DB) CreateHomeBoard(slug, name string, sortOrder int) (*HomeBoard, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	res, err := d.db.Exec(
		`INSERT INTO home_boards (slug, name, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		slug, name, sortOrder, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create home board: %w", err)
	}
	id, _ := res.LastInsertId()
	return &HomeBoard{
		ID: id, Slug: slug, Name: name, SortOrder: sortOrder,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// UpdateHomeBoard updates the name and sort_order of a board.
func (d *DB) UpdateHomeBoard(id int64, name string, sortOrder int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`UPDATE home_boards SET name = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		name, sortOrder, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to update home board: %w", err)
	}
	return nil
}

// RenameHomeBoard updates a board's slug, name, and sort_order in one go.
// Used by the import path when remapping the survivor board onto the first
// imported board's identity.
func (d *DB) RenameHomeBoard(id int64, slug, name string, sortOrder int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(
		`UPDATE home_boards SET slug = ?, name = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		slug, name, sortOrder, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to rename home board: %w", err)
	}
	return nil
}

// UpdateGearConfig overwrites a gear's config JSON. Used by the Home gear's
// import path to apply the snapshot's system_default_landing_path etc.
func (d *DB) UpdateGearConfig(serverID, name string, config []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(config) == 0 {
		config = []byte("{}")
	}
	_, err := d.db.Exec(
		`UPDATE gears SET config = ?, updated_at = CURRENT_TIMESTAMP WHERE server_id = ? AND name = ?`,
		string(config), serverID, name,
	)
	if err != nil {
		return fmt.Errorf("failed to update gear config: %w", err)
	}
	return nil
}

// DeleteHomeBoard removes a board, its tiles, and any associated secrets.
// SQLite FK enforcement is off project-wide, so we cascade explicitly.
// Refuses to delete the last remaining board so the dashboard always has somewhere to render.
func (d *DB) DeleteHomeBoard(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM home_boards`).Scan(&count); err != nil {
		return fmt.Errorf("failed to count home boards: %w", err)
	}
	if count <= 1 {
		return fmt.Errorf("cannot delete the only remaining home board")
	}

	if _, err := d.db.Exec(
		`DELETE FROM home_tile_secrets WHERE tile_id IN (SELECT id FROM home_tiles WHERE board_id = ?)`, id,
	); err != nil {
		return fmt.Errorf("failed to delete home tile secrets for board: %w", err)
	}
	if _, err := d.db.Exec(`DELETE FROM home_tiles WHERE board_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete tiles for board: %w", err)
	}
	if _, err := d.db.Exec(`DELETE FROM home_boards WHERE id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete home board: %w", err)
	}
	return nil
}

// ListHomeTiles returns every tile on a board, ordered by sort_order.
func (d *DB) ListHomeTiles(boardID int64) ([]HomeTile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(
		`SELECT id, board_id, type, x, y, w, h, config, sort_order, created_at, updated_at
		 FROM home_tiles WHERE board_id = ? ORDER BY sort_order, id`,
		boardID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list home tiles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tiles []HomeTile
	for rows.Next() {
		var t HomeTile
		var configStr string
		if err := rows.Scan(
			&t.ID, &t.BoardID, &t.Type, &t.X, &t.Y, &t.W, &t.H,
			&configStr, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan home tile: %w", err)
		}
		t.Config = json.RawMessage(configStr)
		tiles = append(tiles, t)
	}
	return tiles, rows.Err()
}

// GetHomeTile returns one tile by id, or nil when not found.
func (d *DB) GetHomeTile(id int64) (*HomeTile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	t := &HomeTile{}
	var configStr string
	err := d.db.QueryRow(
		`SELECT id, board_id, type, x, y, w, h, config, sort_order, created_at, updated_at
		 FROM home_tiles WHERE id = ?`, id,
	).Scan(
		&t.ID, &t.BoardID, &t.Type, &t.X, &t.Y, &t.W, &t.H,
		&configStr, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get home tile: %w", err)
	}
	t.Config = json.RawMessage(configStr)
	return t, nil
}

// CreateHomeTile inserts a new tile. The caller is responsible for choosing
// (x,y,w,h) — the gridstack frontend supplies them.
func (d *DB) CreateHomeTile(t *HomeTile) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	configStr := string(t.Config)
	if configStr == "" {
		configStr = "{}"
	}
	now := time.Now()
	res, err := d.db.Exec(
		`INSERT INTO home_tiles (board_id, type, x, y, w, h, config, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.BoardID, t.Type, t.X, t.Y, t.W, t.H, configStr, t.SortOrder, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create home tile: %w", err)
	}
	return res.LastInsertId()
}

// UpdateHomeTileLayout updates only the grid coordinates of a tile.
// This is the hot path called after every drag/resize.
func (d *DB) UpdateHomeTileLayout(id int64, x, y, w, h, sortOrder int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`UPDATE home_tiles SET x = ?, y = ?, w = ?, h = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		x, y, w, h, sortOrder, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to update home tile layout: %w", err)
	}
	return nil
}

// UpdateHomeTileConfig replaces the type-specific config JSON for a tile.
func (d *DB) UpdateHomeTileConfig(id int64, config json.RawMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	configStr := string(config)
	if configStr == "" {
		configStr = "{}"
	}
	_, err := d.db.Exec(
		`UPDATE home_tiles SET config = ?, updated_at = ? WHERE id = ?`,
		configStr, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to update home tile config: %w", err)
	}
	return nil
}

// DeleteHomeTile removes a tile and its associated secret. The schema
// declares a FK with ON DELETE CASCADE, but SQLite doesn't enforce FKs
// unless `PRAGMA foreign_keys = ON` is set on every connection — and
// this codebase doesn't enable it project-wide. Delete the secret
// explicitly to keep the contract tight regardless.
func (d *DB) DeleteHomeTile(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.db.Exec(`DELETE FROM home_tile_secrets WHERE tile_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete home tile secret: %w", err)
	}
	if _, err := d.db.Exec(`DELETE FROM home_tiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete home tile: %w", err)
	}
	return nil
}

// SetHomeTileSecret stores or replaces the encrypted secret payload for a tile.
// Pass nil/empty to clear. The caller is responsible for encryption.
func (d *DB) SetHomeTileSecret(tileID int64, encryptedPayload []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(encryptedPayload) == 0 {
		_, err := d.db.Exec(`DELETE FROM home_tile_secrets WHERE tile_id = ?`, tileID)
		if err != nil {
			return fmt.Errorf("failed to clear home tile secret: %w", err)
		}
		return nil
	}

	_, err := d.db.Exec(
		`INSERT INTO home_tile_secrets (tile_id, encrypted_payload, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(tile_id) DO UPDATE SET encrypted_payload = excluded.encrypted_payload, updated_at = excluded.updated_at`,
		tileID, encryptedPayload, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to set home tile secret: %w", err)
	}
	return nil
}

// GetHomeTileSecret returns the raw encrypted payload, or nil when none is set.
// Decryption is the caller's responsibility — keep this method's return value
// inside backend handlers; never serialise it to the browser.
func (d *DB) GetHomeTileSecret(tileID int64) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var payload []byte
	err := d.db.QueryRow(
		`SELECT encrypted_payload FROM home_tile_secrets WHERE tile_id = ?`, tileID,
	).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load home tile secret: %w", err)
	}
	return payload, nil
}

// HasHomeTileSecret returns true when a tile has an encrypted secret on file,
// without decrypting or returning it. Useful for the "has_secret: true"
// indicator in API responses to the browser.
func (d *DB) HasHomeTileSecret(tileID int64) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM home_tile_secrets WHERE tile_id = ? AND encrypted_payload IS NOT NULL`,
		tileID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check home tile secret: %w", err)
	}
	return count > 0, nil
}
