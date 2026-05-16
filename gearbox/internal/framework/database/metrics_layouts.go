// Package database — per-user, per-box layout persistence for the
// Metrics gear's GridStack-driven page (issue #103).
//
// The Metrics page renders a fixed set of chart cards (HAProxy
// stats, host metrics, plus capability-gated per-source cards).
// Operators rearrange/resize those cards in edit mode; the saved
// layout lives here. Storage shape: one JSON blob per (user, box)
// holding GridStack's `save()` output verbatim — array of
// `{id, x, y, w, h}` objects keyed by the stable tile DOM id
// (card-cpu, card-memory, card-nginx, …). JSON beats per-tile rows
// because the dashboard always reads/writes the whole layout at
// once and there's no querying inside it; the JSON column keeps
// the schema additive when we add tile shapes later.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MetricsLayout is one persisted layout — the GridStack `save()`
// payload kept verbatim plus the (user, server) coordinates it
// belongs to.
//
// Layout is stored as []byte rather than a typed struct because
// GridStack's save format is the canonical form here: every field
// it emits round-trips back through `load()` unchanged. Typing it
// would mean keeping our Go shape in lockstep with GridStack's
// minor-version changes — not worth the maintenance cost for a
// JSON blob the dashboard never inspects.
type MetricsLayout struct {
	UserID    string    `json:"user_id"`
	ServerID  string    `json:"server_id"`
	Layout    []byte    `json:"layout"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ErrNoMetricsLayout is returned by GetMetricsLayout when no row
// exists for (userID, serverID). Callers translate this into "use
// the default layout from the page template" rather than an HTTP
// 500 — first-visit users hit this path every time.
var ErrNoMetricsLayout = errors.New("no metrics_layouts row for the user/server pair")

// initMetricsLayoutsSchema creates the per-user-per-box layout
// table. Called from initSchema during database startup, alongside
// the other gear schemas. No foreign keys to users(id) intentionally:
// if a user is deleted we'd rather orphan their saved layouts than
// take an FK cascade — the orphans are tiny (one JSON blob each) and
// the simpler schema sidesteps a delete-cascade ordering question.
//
// (server_id is text because the dashboard's box / server addressing
// is string-keyed — server_id matches the convention used by
// stats_history, traffic_flows, etc.)
func (d *DB) initMetricsLayoutsSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS metrics_layouts (
		user_id     TEXT NOT NULL,
		server_id   TEXT NOT NULL,
		layout_json TEXT NOT NULL,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, server_id)
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_layouts_user
		ON metrics_layouts(user_id);
	`
	_, err := d.db.Exec(schema)
	return err
}

// GetMetricsLayout returns the saved layout for one (user, box) pair.
// Returns ErrNoMetricsLayout when nothing's saved yet — callers
// (the handler) translate to "render the default layout from the
// page template" so first visits work without an extra round trip.
func (d *DB) GetMetricsLayout(userID string, serverID string) (*MetricsLayout, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT user_id, server_id, layout_json, updated_at
		FROM metrics_layouts
		WHERE user_id = ? AND server_id = ?`,
		userID, serverID,
	)
	var (
		out    MetricsLayout
		layout string
	)
	if err := row.Scan(&out.UserID, &out.ServerID, &layout, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoMetricsLayout
		}
		return nil, fmt.Errorf("scan metrics_layouts: %w", err)
	}
	out.Layout = []byte(layout)
	return &out, nil
}

// SaveMetricsLayout upserts the layout for one (user, box) pair.
// `layoutJSON` must already be a valid JSON string — the handler
// is responsible for shape-checking before calling. We don't
// re-validate here because the canonical schema lives in
// GridStack's JS, and our agnostic-blob approach intentionally
// avoids tracking that schema in Go.
//
// updated_at is bumped server-side so the dashboard can show
// "last edited X minutes ago" without trusting client clocks.
func (d *DB) SaveMetricsLayout(userID string, serverID string, layoutJSON []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO metrics_layouts (user_id, server_id, layout_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, server_id) DO UPDATE SET
			layout_json = excluded.layout_json,
			updated_at  = excluded.updated_at`,
		userID, serverID, string(layoutJSON), time.Now().UTC(),
	)
	return err
}

// DeleteMetricsLayout removes the saved layout for one (user, box)
// pair, effectively reverting the user's view to the template
// default on next render. Used by the "reset to default" button in
// the edit-mode toolbar.
func (d *DB) DeleteMetricsLayout(userID string, serverID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		DELETE FROM metrics_layouts
		WHERE user_id = ? AND server_id = ?`,
		userID, serverID,
	)
	return err
}
