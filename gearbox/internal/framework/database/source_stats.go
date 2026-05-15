// Package database — per-source aggregate rollups for the Metrics
// gear's multi-source pivot (issue #91 Phase 4 / 7 dashboard side).
//
// The table is created in traffic.go's initTrafficSchema alongside
// the other traffic-analysis tables; this file holds the typed
// save/query helpers the collector and handlers call. Kept in a
// separate file so it's easy to spot when reading the database/
// package for the first time — "ah, source_stats has its own
// helpers, those go with the source-aware Metrics endpoints".
package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SourceStatsSnapshot is the typed shape of one source_stats row.
// Mirrors the schema in initTrafficSchema; ExtraJSON unmarshals on
// demand into per-source structs (the handler picks the right one
// based on Source). Kept in this package rather than `models/` so
// it stays the database layer's owned shape — the handler maps it
// to a JSON response shape that the dashboard consumes.
type SourceStatsSnapshot struct {
	ServerID          string    `json:"server_id"`
	Source            string    `json:"source"`
	CollectedAt       time.Time `json:"collected_at"`
	RequestsTotal     int64     `json:"requests_total"`
	Response2xx       int64     `json:"response_2xx"`
	Response3xx       int64     `json:"response_3xx"`
	Response4xx       int64     `json:"response_4xx"`
	Response5xx       int64     `json:"response_5xx"`
	ActiveConnections int64     `json:"active_connections"`
	// Extra carries source-specific fields the common columns don't
	// capture (nginx reading/writing/waiting; Apache busy/idle
	// workers; Caddy request_errors_total; Traefik entrypoints).
	// Stored as a JSON string in the DB; handlers unmarshal into
	// the right per-source struct.
	Extra map[string]any `json:"extra,omitempty"`
}

// ErrNoSourceStats is returned by GetLatestSourceStats when no
// row exists for the requested (server_id, source) pair. Callers
// (the handler) translate it into a "this source hasn't been
// polled yet" envelope rather than a 500.
var ErrNoSourceStats = errors.New("no source_stats row for the requested server/source")

// SaveSourceStats inserts a new snapshot row. We don't upsert on
// collected_at the way SaveTrafficFlow does because each scrape is
// a distinct point-in-time observation — the dashboard derives
// rates by diffing successive rows, and merging two scrapes into
// one would lie about timing. The table is bounded by the cleanup
// loop (TTL via prune_source_stats below), not by per-row upsert.
func (d *DB) SaveSourceStats(s *SourceStatsSnapshot) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var extraJSON sql.NullString
	if len(s.Extra) > 0 {
		b, err := json.Marshal(s.Extra)
		if err != nil {
			return fmt.Errorf("marshal extra: %w", err)
		}
		extraJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := d.db.Exec(`
		INSERT INTO source_stats (
			server_id, source, collected_at,
			requests_total, response_2xx, response_3xx, response_4xx, response_5xx,
			active_connections, extra_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ServerID, s.Source, s.CollectedAt,
		s.RequestsTotal, s.Response2xx, s.Response3xx, s.Response4xx, s.Response5xx,
		s.ActiveConnections, extraJSON,
	)
	return err
}

// GetLatestSourceStats returns the most recent snapshot for one
// (server, source) pair. The handler uses this for the KPI band's
// current-value cards. Returns ErrNoSourceStats when nothing has
// been collected yet so the caller can render a clean "not yet
// collected" envelope.
func (d *DB) GetLatestSourceStats(serverID, source string) (*SourceStatsSnapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT server_id, source, collected_at,
		       requests_total, response_2xx, response_3xx, response_4xx, response_5xx,
		       active_connections, extra_json
		FROM source_stats
		WHERE server_id = ? AND source = ?
		ORDER BY collected_at DESC
		LIMIT 1`,
		serverID, source,
	)
	s, err := scanSourceStats(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSourceStats
	}
	return s, err
}

// GetSourceStatsRange returns snapshots in [since, until] order by
// collected_at ascending. Used by the time-series chart cards
// (request rate over the last hour, etc.). Caller clamps the
// limit; we just translate the SQL.
func (d *DB) GetSourceStatsRange(serverID, source string, since, until time.Time, limit int) ([]SourceStatsSnapshot, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT server_id, source, collected_at,
		       requests_total, response_2xx, response_3xx, response_4xx, response_5xx,
		       active_connections, extra_json
		FROM source_stats
		WHERE server_id = ? AND source = ?
		  AND collected_at >= ? AND collected_at <= ?
		ORDER BY collected_at ASC
		LIMIT ?`,
		serverID, source, since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]SourceStatsSnapshot, 0, 64)
	for rows.Next() {
		s, err := scanSourceStats(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// PruneSourceStats deletes rows older than `before` for the given
// server. Called by the existing TTL cleanup loop (same cadence as
// the traffic_flows prune). Returns the number of rows removed so
// the caller can log it without re-counting.
func (d *DB) PruneSourceStats(serverID string, before time.Time) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(`
		DELETE FROM source_stats
		WHERE server_id = ? AND collected_at < ?`,
		serverID, before,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// scanSourceStats reads one row from either *sql.Row or *sql.Rows
// — the small interface is enough to share the column ordering
// between the latest-snapshot and range queries above.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSourceStats(r rowScanner) (*SourceStatsSnapshot, error) {
	var s SourceStatsSnapshot
	var extra sql.NullString
	if err := r.Scan(
		&s.ServerID, &s.Source, &s.CollectedAt,
		&s.RequestsTotal, &s.Response2xx, &s.Response3xx, &s.Response4xx, &s.Response5xx,
		&s.ActiveConnections, &extra,
	); err != nil {
		return nil, err
	}
	if extra.Valid && extra.String != "" {
		if err := json.Unmarshal([]byte(extra.String), &s.Extra); err != nil {
			// Corrupt JSON shouldn't fail the read — log via the
			// caller's logger when needed; here we just blank
			// the field so the row still surfaces.
			s.Extra = nil
		}
	}
	return &s, nil
}
