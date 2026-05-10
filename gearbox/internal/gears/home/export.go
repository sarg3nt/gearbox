package home

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

// CurrentSchemaVersion is the version embedded in every exported snapshot.
// Bump when the export shape changes, then add a corresponding migration to
// migrations below so older exports still import cleanly.
const CurrentSchemaVersion = 1

// Snapshot is the on-disk shape of a Home dashboard backup.
//
// Notably *not* included: encrypted secrets. Bringing them across machines
// requires the destination to share the source's master key, which is a
// trap; users re-enter API keys after import.
type Snapshot struct {
	SchemaVersion int                 `json:"schema_version"`
	ExportedAt    time.Time           `json:"exported_at"`
	GearVersion   string              `json:"gear_version,omitempty"`
	Config        *database.HomeConfig `json:"config,omitempty"`
	Boards        []SnapshotBoard     `json:"boards"`
}

// SnapshotBoard is one board with all of its tiles.
type SnapshotBoard struct {
	Slug      string          `json:"slug"`
	Name      string          `json:"name"`
	SortOrder int             `json:"sort_order"`
	Tiles     []SnapshotTile  `json:"tiles"`
}

// SnapshotTile is one tile in an export. The tile's id and timestamps
// are intentionally elided — they get re-assigned on import.
type SnapshotTile struct {
	Type      database.HomeTileType `json:"type"`
	X         int                   `json:"x"`
	Y         int                   `json:"y"`
	W         int                   `json:"w"`
	H         int                   `json:"h"`
	Config    json.RawMessage       `json:"config"`
	SortOrder int                   `json:"sort_order"`
}

// buildSnapshot reads the current state into a Snapshot.
func buildSnapshot(db *database.DB) (*Snapshot, error) {
	snap := &Snapshot{
		SchemaVersion: CurrentSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		GearVersion:   "0.1.0",
	}

	// System gear config — the SystemDefaultLandingPath etc.
	if g, err := db.GetGear(database.SystemServerID, database.GearHome); err == nil && g != nil && len(g.Config) > 0 {
		var cfg database.HomeConfig
		if json.Unmarshal(g.Config, &cfg) == nil {
			snap.Config = &cfg
		}
	}

	boards, err := db.ListHomeBoards()
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	for _, b := range boards {
		sb := SnapshotBoard{
			Slug:      b.Slug,
			Name:      b.Name,
			SortOrder: b.SortOrder,
		}
		tiles, err := db.ListHomeTiles(b.ID)
		if err != nil {
			return nil, fmt.Errorf("list tiles for board %d: %w", b.ID, err)
		}
		for _, t := range tiles {
			sb.Tiles = append(sb.Tiles, SnapshotTile{
				Type:      t.Type,
				X:         t.X,
				Y:         t.Y,
				W:         t.W,
				H:         t.H,
				Config:    t.Config,
				SortOrder: t.SortOrder,
			})
		}
		snap.Boards = append(snap.Boards, sb)
	}
	return snap, nil
}

// importSnapshot replaces all boards & tiles with the ones in s. Snapshots
// older than CurrentSchemaVersion are migrated forward first.
//
// This is intentionally destructive — that's what users expect from
// "restore a backup." Existing secrets are not touched (they live in
// home_tile_secrets keyed by tile id, and we delete those tiles, so the
// FK-cascade-by-hand inside DeleteHomeBoard cleans them up).
func importSnapshot(db *database.DB, s *Snapshot) error {
	migrated, err := migrateSnapshot(s)
	if err != nil {
		return fmt.Errorf("migrate snapshot: %w", err)
	}
	s = migrated

	// Wipe existing boards. DeleteHomeBoard refuses to delete the only
	// remaining board — work around it by emptying every-but-one then
	// renaming the survivor to one of the imported slugs (or simply
	// renaming and clearing tiles when no imports exist).
	existing, err := db.ListHomeBoards()
	if err != nil {
		return fmt.Errorf("list boards: %w", err)
	}

	// Delete tiles on every existing board first.
	for _, b := range existing {
		tiles, _ := db.ListHomeTiles(b.ID)
		for _, t := range tiles {
			_ = db.DeleteHomeTile(t.ID)
		}
	}
	// Drop all but the first board.
	for i, b := range existing {
		if i == 0 {
			continue
		}
		_ = db.DeleteHomeBoard(b.ID)
	}

	// Re-insert. The first imported board is mapped onto the surviving
	// existing row (rename + slug change); subsequent boards are created.
	if len(s.Boards) == 0 {
		// Nothing to import — leave the surviving board with its current
		// name, just empty.
		return nil
	}

	survivor := existing[0]
	first := s.Boards[0]
	if err := db.RenameHomeBoard(survivor.ID, first.Slug, first.Name, first.SortOrder); err != nil {
		return fmt.Errorf("rename survivor board: %w", err)
	}
	for _, t := range first.Tiles {
		_, _ = db.CreateHomeTile(&database.HomeTile{
			BoardID:   survivor.ID,
			Type:      t.Type,
			X:         t.X,
			Y:         t.Y,
			W:         t.W,
			H:         t.H,
			Config:    t.Config,
			SortOrder: t.SortOrder,
		})
	}
	for _, b := range s.Boards[1:] {
		newBoard, err := db.CreateHomeBoard(b.Slug, b.Name, b.SortOrder)
		if err != nil {
			return fmt.Errorf("create board %q: %w", b.Slug, err)
		}
		for _, t := range b.Tiles {
			_, _ = db.CreateHomeTile(&database.HomeTile{
				BoardID:   newBoard.ID,
				Type:      t.Type,
				X:         t.X,
				Y:         t.Y,
				W:         t.W,
				H:         t.H,
				Config:    t.Config,
				SortOrder: t.SortOrder,
			})
		}
	}

	// System config (optional).
	if s.Config != nil {
		cfgBytes, _ := json.Marshal(s.Config)
		_ = db.UpdateGearConfig(database.SystemServerID, database.GearHome, cfgBytes)
	}
	return nil
}

// migration is one schema-version-to-next-version transformation.
// Each migration takes a snapshot at version N and returns it at version N+1.
type migration func(*Snapshot) (*Snapshot, error)

// migrations[i] upgrades a snapshot from version i to version i+1.
// New entries get appended as we bump CurrentSchemaVersion.
var migrations = []migration{
	// migrations[0] would upgrade v0 -> v1, but v1 is our floor.
	// Add migrations[1] when bumping CurrentSchemaVersion to 2.
}

// migrateSnapshot walks the migrations list to bring s up to current.
func migrateSnapshot(s *Snapshot) (*Snapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("nil snapshot")
	}
	if s.SchemaVersion <= 0 {
		s.SchemaVersion = 1
	}
	if s.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("snapshot schema_version %d is newer than supported %d — upgrade Gearbox to import", s.SchemaVersion, CurrentSchemaVersion)
	}
	for s.SchemaVersion < CurrentSchemaVersion {
		idx := s.SchemaVersion - 1
		if idx < 0 || idx >= len(migrations) {
			return nil, fmt.Errorf("no migration registered for schema_version %d", s.SchemaVersion)
		}
		next, err := migrations[idx](s)
		if err != nil {
			return nil, err
		}
		next.SchemaVersion = s.SchemaVersion + 1
		s = next
	}
	return s, nil
}
