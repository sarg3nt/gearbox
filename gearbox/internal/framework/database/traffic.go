package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// initTrafficSchema creates the traffic analysis tables.
func (d *DB) initTrafficSchema() error {
	schema := `
	-- Traffic sources: stores information about each unique IP address
	CREATE TABLE IF NOT EXISTS traffic_sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		ip_address TEXT NOT NULL,
		country TEXT NOT NULL DEFAULT 'Unknown',
		country_code TEXT NOT NULL DEFAULT 'XX',
		city TEXT,
		asn TEXT,
		asn_org TEXT,
		is_known_bot INTEGER NOT NULL DEFAULT 0,
		is_tor INTEGER NOT NULL DEFAULT 0,
		is_proxy INTEGER NOT NULL DEFAULT 0,
		is_vpn INTEGER NOT NULL DEFAULT 0,
		threat_score INTEGER NOT NULL DEFAULT 0,
		first_seen DATETIME NOT NULL,
		last_seen DATETIME NOT NULL,
		total_requests INTEGER NOT NULL DEFAULT 0,
		total_bytes INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(server_id, ip_address)
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_sources_server_ip
		ON traffic_sources(server_id, ip_address);
	CREATE INDEX IF NOT EXISTS idx_traffic_sources_country
		ON traffic_sources(server_id, country_code);
	CREATE INDEX IF NOT EXISTS idx_traffic_sources_last_seen
		ON traffic_sources(server_id, last_seen);

	-- Traffic flows: stores aggregated traffic data per time bucket
	CREATE TABLE IF NOT EXISTS traffic_flows (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		source_ip TEXT NOT NULL,
		country TEXT NOT NULL DEFAULT 'Unknown',
		country_code TEXT NOT NULL DEFAULT 'XX',
		backend_name TEXT NOT NULL,
		frontend_name TEXT NOT NULL DEFAULT '',
		bucket_time DATETIME NOT NULL,
		request_count INTEGER NOT NULL DEFAULT 0,
		bytes_in INTEGER NOT NULL DEFAULT 0,
		bytes_out INTEGER NOT NULL DEFAULT 0,
		response_2xx INTEGER NOT NULL DEFAULT 0,
		response_3xx INTEGER NOT NULL DEFAULT 0,
		response_4xx INTEGER NOT NULL DEFAULT 0,
		response_5xx INTEGER NOT NULL DEFAULT 0,
		avg_response_time INTEGER NOT NULL DEFAULT 0,
		max_response_time INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(server_id, source_ip, backend_name, bucket_time)
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_flows_server_time
		ON traffic_flows(server_id, bucket_time);
	CREATE INDEX IF NOT EXISTS idx_traffic_flows_server_backend
		ON traffic_flows(server_id, backend_name, bucket_time);
	CREATE INDEX IF NOT EXISTS idx_traffic_flows_server_ip
		ON traffic_flows(server_id, source_ip, bucket_time);
	CREATE INDEX IF NOT EXISTS idx_traffic_flows_country
		ON traffic_flows(server_id, country_code, bucket_time);

	-- Traffic snapshots: hourly aggregated summary
	CREATE TABLE IF NOT EXISTS traffic_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		collected_at DATETIME NOT NULL,
		total_requests INTEGER NOT NULL DEFAULT 0,
		total_bytes_in INTEGER NOT NULL DEFAULT 0,
		total_bytes_out INTEGER NOT NULL DEFAULT 0,
		unique_ips INTEGER NOT NULL DEFAULT 0,
		unique_countries INTEGER NOT NULL DEFAULT 0,
		total_2xx INTEGER NOT NULL DEFAULT 0,
		total_3xx INTEGER NOT NULL DEFAULT 0,
		total_4xx INTEGER NOT NULL DEFAULT 0,
		total_5xx INTEGER NOT NULL DEFAULT 0,
		avg_response_time INTEGER NOT NULL DEFAULT 0,
		p95_response_time INTEGER NOT NULL DEFAULT 0,
		p99_response_time INTEGER NOT NULL DEFAULT 0,
		requests_per_second REAL NOT NULL DEFAULT 0,
		bytes_per_second REAL NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_snapshots_server_time
		ON traffic_snapshots(server_id, collected_at);

	-- Traffic anomalies: detected issues and suspicious activity
	CREATE TABLE IF NOT EXISTS traffic_anomalies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		affected_ip TEXT,
		affected_backend TEXT,
		metrics TEXT,
		detected_at DATETIME NOT NULL,
		resolved_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_anomalies_server_time
		ON traffic_anomalies(server_id, detected_at);
	CREATE INDEX IF NOT EXISTS idx_traffic_anomalies_unresolved
		ON traffic_anomalies(server_id, resolved_at) WHERE resolved_at IS NULL;
	`

	_, err := d.db.Exec(schema)
	return err
}

// SaveTrafficFlow saves or updates a traffic flow record.
func (d *DB) SaveTrafficFlow(flow *models.TrafficFlow) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Calculate error rate
	totalResponses := flow.Response2xx + flow.Response3xx + flow.Response4xx + flow.Response5xx
	errorRate := float64(0)
	if totalResponses > 0 {
		errorRate = float64(flow.Response4xx+flow.Response5xx) / float64(totalResponses) * 100
	}
	flow.ErrorRate = errorRate

	// Upsert: insert or update if exists
	_, err := d.db.Exec(`
		INSERT INTO traffic_flows (
			server_id, source_ip, country, country_code, backend_name, frontend_name,
			bucket_time, request_count, bytes_in, bytes_out,
			response_2xx, response_3xx, response_4xx, response_5xx,
			avg_response_time, max_response_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, source_ip, backend_name, bucket_time) DO UPDATE SET
			request_count = request_count + excluded.request_count,
			bytes_in = bytes_in + excluded.bytes_in,
			bytes_out = bytes_out + excluded.bytes_out,
			response_2xx = response_2xx + excluded.response_2xx,
			response_3xx = response_3xx + excluded.response_3xx,
			response_4xx = response_4xx + excluded.response_4xx,
			response_5xx = response_5xx + excluded.response_5xx,
			avg_response_time = (avg_response_time + excluded.avg_response_time) / 2,
			max_response_time = MAX(max_response_time, excluded.max_response_time)
		`,
		flow.ServerID, flow.SourceIP, flow.Country, flow.CountryCode,
		flow.BackendName, flow.FrontendName, flow.Timestamp,
		flow.RequestCount, flow.BytesIn, flow.BytesOut,
		flow.Response2xx, flow.Response3xx, flow.Response4xx, flow.Response5xx,
		flow.AvgResponseTime, flow.MaxResponseTime,
	)
	return err
}

// SaveTrafficFlows batch saves multiple traffic flows.
func (d *DB) SaveTrafficFlows(flows []models.TrafficFlow) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO traffic_flows (
			server_id, source_ip, country, country_code, backend_name, frontend_name,
			bucket_time, request_count, bytes_in, bytes_out,
			response_2xx, response_3xx, response_4xx, response_5xx,
			avg_response_time, max_response_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, source_ip, backend_name, bucket_time) DO UPDATE SET
			request_count = request_count + excluded.request_count,
			bytes_in = bytes_in + excluded.bytes_in,
			bytes_out = bytes_out + excluded.bytes_out,
			response_2xx = response_2xx + excluded.response_2xx,
			response_3xx = response_3xx + excluded.response_3xx,
			response_4xx = response_4xx + excluded.response_4xx,
			response_5xx = response_5xx + excluded.response_5xx,
			avg_response_time = (avg_response_time + excluded.avg_response_time) / 2,
			max_response_time = MAX(max_response_time, excluded.max_response_time)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, flow := range flows {
		_, err := stmt.Exec(
			flow.ServerID, flow.SourceIP, flow.Country, flow.CountryCode,
			flow.BackendName, flow.FrontendName, flow.Timestamp,
			flow.RequestCount, flow.BytesIn, flow.BytesOut,
			flow.Response2xx, flow.Response3xx, flow.Response4xx, flow.Response5xx,
			flow.AvgResponseTime, flow.MaxResponseTime,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpdateTrafficSource updates or inserts a traffic source record.
func (d *DB) UpdateTrafficSource(source *models.TrafficSource, serverID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO traffic_sources (
			server_id, ip_address, country, country_code, city, asn, asn_org,
			is_known_bot, is_tor, is_proxy, is_vpn, threat_score,
			first_seen, last_seen, total_requests, total_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, ip_address) DO UPDATE SET
			country = excluded.country,
			country_code = excluded.country_code,
			city = excluded.city,
			asn = excluded.asn,
			asn_org = excluded.asn_org,
			is_known_bot = excluded.is_known_bot,
			is_tor = excluded.is_tor,
			is_proxy = excluded.is_proxy,
			is_vpn = excluded.is_vpn,
			threat_score = excluded.threat_score,
			last_seen = excluded.last_seen,
			total_requests = total_requests + excluded.total_requests,
			total_bytes = total_bytes + excluded.total_bytes,
			updated_at = CURRENT_TIMESTAMP
		`,
		serverID, source.IPAddress, source.Country, source.CountryCode, source.City,
		source.ASN, source.ASNOrg, source.IsKnownBot, source.IsTor, source.IsProxy, source.IsVPN,
		source.ThreatScore, source.FirstSeen, source.LastSeen, source.TotalRequests, source.TotalBytes,
	)
	return err
}

// GetTrafficSources retrieves traffic sources with optional filtering.
func (d *DB) GetTrafficSources(filter *models.TrafficFilter) ([]models.TrafficSourceStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT
			source_ip, country, country_code,
			SUM(request_count) as requests,
			SUM(bytes_in) as bytes_in,
			SUM(bytes_out) as bytes_out,
			AVG(avg_response_time) as avg_response_time,
			SUM(response_2xx) as response_2xx,
			SUM(response_3xx) as response_3xx,
			SUM(response_4xx) as response_4xx,
			SUM(response_5xx) as response_5xx,
			MIN(bucket_time) as first_seen,
			MAX(bucket_time) as last_seen
		FROM traffic_flows
		WHERE server_id = ? AND source_ip != '_aggregate' AND source_ip != '_unknown'
	`
	args := []interface{}{filter.BoxID}

	if filter.BackendName != "" {
		query += " AND backend_name = ?"
		args = append(args, filter.BackendName)
	}
	if filter.SourceIP != "" {
		query += " AND source_ip = ?"
		args = append(args, filter.SourceIP)
	}
	if filter.CountryCode != "" {
		query += " AND country_code = ?"
		args = append(args, filter.CountryCode)
	}
	if !filter.StartTime.IsZero() {
		query += " AND bucket_time >= ?"
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += " AND bucket_time <= ?"
		args = append(args, filter.EndTime)
	}

	query += " GROUP BY source_ip, country, country_code"

	// Sorting
	sortBy := "requests"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "bytes":
			sortBy = "bytes_in + bytes_out"
		case "latency":
			sortBy = "avg_response_time"
		case "errors":
			sortBy = "response_4xx + response_5xx"
		default:
			sortBy = "requests"
		}
	}
	sortOrder := "DESC"
	if filter.SortOrder == "asc" {
		sortOrder = "ASC"
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Limit
	limit := 100
	if filter.Limit > 0 && filter.Limit <= 1000 {
		limit = filter.Limit
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var sources []models.TrafficSourceStats
	for rows.Next() {
		var s models.TrafficSourceStats
		var firstSeenStr, lastSeenStr string
		err := rows.Scan(
			&s.IPAddress, &s.Country, &s.CountryCode,
			&s.Requests, &s.BytesIn, &s.BytesOut, &s.AvgResponseTime,
			&s.Response2xx, &s.Response3xx, &s.Response4xx, &s.Response5xx,
			&firstSeenStr, &lastSeenStr,
		)
		if err != nil {
			return nil, err
		}
		// Parse time strings - SQLite stores as "2006-01-02 15:04:05-07:00" format
		if firstSeenStr != "" {
			if t, err := time.Parse("2006-01-02 15:04:05-07:00", firstSeenStr); err == nil {
				s.FirstSeen = t
			}
		}
		if lastSeenStr != "" {
			if t, err := time.Parse("2006-01-02 15:04:05-07:00", lastSeenStr); err == nil {
				s.LastSeen = t
			}
		}
		// Calculate error rate
		total := s.Response2xx + s.Response3xx + s.Response4xx + s.Response5xx
		if total > 0 {
			s.ErrorRate = float64(s.Response4xx+s.Response5xx) / float64(total) * 100
		}
		s.ThreatLevel = s.CalculateThreatLevel()
		sources = append(sources, s)
	}

	return sources, nil
}

// GetTrafficByBackend retrieves traffic statistics grouped by backend.
func (d *DB) GetTrafficByBackend(filter *models.TrafficFilter) ([]models.TrafficBackendStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT
			backend_name,
			SUM(request_count) as total_requests,
			SUM(bytes_in) as bytes_in,
			SUM(bytes_out) as bytes_out,
			COUNT(DISTINCT source_ip) as unique_ips,
			AVG(avg_response_time) as avg_response_time,
			SUM(response_2xx) as response_2xx,
			SUM(response_3xx) as response_3xx,
			SUM(response_4xx) as response_4xx,
			SUM(response_5xx) as response_5xx
		FROM traffic_flows
		WHERE server_id = ?
	`
	args := []interface{}{filter.BoxID}

	if !filter.StartTime.IsZero() {
		query += " AND bucket_time >= ?"
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += " AND bucket_time <= ?"
		args = append(args, filter.EndTime)
	}

	query += " GROUP BY backend_name ORDER BY total_requests DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var backends []models.TrafficBackendStats
	for rows.Next() {
		var b models.TrafficBackendStats
		err := rows.Scan(
			&b.BackendName, &b.TotalRequests, &b.BytesIn, &b.BytesOut,
			&b.UniqueIPs, &b.AvgResponseTime,
			&b.Response2xx, &b.Response3xx, &b.Response4xx, &b.Response5xx,
		)
		if err != nil {
			return nil, err
		}
		// Calculate error rate
		total := b.Response2xx + b.Response3xx + b.Response4xx + b.Response5xx
		if total > 0 {
			b.ErrorRate = float64(b.Response4xx+b.Response5xx) / float64(total) * 100
		}
		backends = append(backends, b)
	}

	return backends, nil
}

// GetTrafficByCountry retrieves traffic statistics grouped by country.
func (d *DB) GetTrafficByCountry(filter *models.TrafficFilter) ([]models.TrafficCountryStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT
			country, country_code,
			SUM(request_count) as requests,
			COUNT(DISTINCT source_ip) as unique_ips,
			SUM(bytes_in) as bytes_in,
			SUM(bytes_out) as bytes_out,
			AVG(avg_response_time) as avg_response_time,
			SUM(response_4xx) + SUM(response_5xx) as errors,
			SUM(response_2xx) + SUM(response_3xx) + SUM(response_4xx) + SUM(response_5xx) as total_responses
		FROM traffic_flows
		WHERE server_id = ?
	`
	args := []interface{}{filter.BoxID}

	if !filter.StartTime.IsZero() {
		query += " AND bucket_time >= ?"
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += " AND bucket_time <= ?"
		args = append(args, filter.EndTime)
	}

	query += " GROUP BY country, country_code ORDER BY requests DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var countries []models.TrafficCountryStats
	var totalRequests int64

	for rows.Next() {
		var c models.TrafficCountryStats
		var errors, totalResponses int64
		err := rows.Scan(
			&c.Country, &c.CountryCode, &c.Requests, &c.UniqueIPs,
			&c.BytesIn, &c.BytesOut, &c.AvgResponseTime,
			&errors, &totalResponses,
		)
		if err != nil {
			return nil, err
		}
		if totalResponses > 0 {
			c.ErrorRate = float64(errors) / float64(totalResponses) * 100
		}
		totalRequests += c.Requests
		countries = append(countries, c)
	}

	// Calculate percentages
	for i := range countries {
		if totalRequests > 0 {
			countries[i].Percentage = float64(countries[i].Requests) / float64(totalRequests) * 100
		}
	}

	return countries, nil
}

// GetHourlyTraffic retrieves traffic aggregated by hour.
func (d *DB) GetHourlyTraffic(filter *models.TrafficFilter) ([]models.HourlyTrafficStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT
			strftime('%Y-%m-%d %H:00:00', bucket_time) as hour,
			SUM(request_count) as requests,
			SUM(bytes_in) as bytes_in,
			SUM(bytes_out) as bytes_out,
			COUNT(DISTINCT source_ip) as unique_ips,
			AVG(avg_response_time) as avg_response_time,
			SUM(response_2xx) as response_2xx,
			SUM(response_3xx) as response_3xx,
			SUM(response_4xx) as response_4xx,
			SUM(response_5xx) as response_5xx
		FROM traffic_flows
		WHERE server_id = ?
	`
	args := []interface{}{filter.BoxID}

	if !filter.StartTime.IsZero() {
		query += " AND bucket_time >= ?"
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += " AND bucket_time <= ?"
		args = append(args, filter.EndTime)
	}
	if filter.BackendName != "" {
		query += " AND backend_name = ?"
		args = append(args, filter.BackendName)
	}

	query += " GROUP BY strftime('%Y-%m-%d %H:00:00', bucket_time) ORDER BY hour DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	} else {
		query += " LIMIT 168" // Default: 7 days worth of hours
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var hourly []models.HourlyTrafficStats
	for rows.Next() {
		var h models.HourlyTrafficStats
		var hourStr string
		err := rows.Scan(
			&hourStr, &h.Requests, &h.BytesIn, &h.BytesOut, &h.UniqueIPs,
			&h.AvgResponseTime, &h.Response2xx, &h.Response3xx, &h.Response4xx, &h.Response5xx,
		)
		if err != nil {
			return nil, err
		}
		h.Timestamp, _ = time.Parse("2006-01-02 15:04:05", hourStr)
		h.Hour = h.Timestamp.Format("15:00")

		total := h.Response2xx + h.Response3xx + h.Response4xx + h.Response5xx
		if total > 0 {
			h.ErrorRate = float64(h.Response4xx+h.Response5xx) / float64(total) * 100
		}
		hourly = append(hourly, h)
	}

	// Reverse to get chronological order
	for i, j := 0, len(hourly)-1; i < j; i, j = i+1, j-1 {
		hourly[i], hourly[j] = hourly[j], hourly[i]
	}

	return hourly, nil
}

// GetTrafficSummary retrieves a summary of traffic for a time range.
func (d *DB) GetTrafficSummary(serverID string, since time.Time) (*models.TrafficSummary, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT
			COALESCE(SUM(request_count), 0) as total_requests,
			COALESCE(SUM(bytes_in), 0) as bytes_in,
			COALESCE(SUM(bytes_out), 0) as bytes_out,
			COUNT(DISTINCT source_ip) as unique_ips,
			COUNT(DISTINCT country_code) as unique_countries,
			COALESCE(AVG(avg_response_time), 0) as avg_response_time,
			COALESCE(SUM(response_2xx), 0) as response_2xx,
			COALESCE(SUM(response_3xx), 0) as response_3xx,
			COALESCE(SUM(response_4xx), 0) as response_4xx,
			COALESCE(SUM(response_5xx), 0) as response_5xx
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ?
	`

	var summary models.TrafficSummary
	var response2xx, response3xx, response4xx, response5xx int64

	err := d.db.QueryRow(query, serverID, since).Scan(
		&summary.TotalRequests, &summary.TotalBytesIn, &summary.TotalBytesOut,
		&summary.UniqueIPs, &summary.UniqueCountries, &summary.AvgResponseTime,
		&response2xx, &response3xx, &response4xx, &response5xx,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Calculate rates
	totalResponses := response2xx + response3xx + response4xx + response5xx
	if totalResponses > 0 {
		summary.ErrorRate = float64(response4xx+response5xx) / float64(totalResponses) * 100
		summary.SuccessRate = float64(response2xx+response3xx) / float64(totalResponses) * 100
	}

	// Calculate throughput (rough estimate based on time range)
	duration := time.Since(since).Seconds()
	if duration > 0 {
		summary.RequestsPerSecond = float64(summary.TotalRequests) / duration
		summary.ThroughputMbps = float64(summary.TotalBytesIn+summary.TotalBytesOut) * 8 / duration / 1000000
	}

	return &summary, nil
}

// SaveTrafficSnapshot saves an aggregated traffic snapshot.
func (d *DB) SaveTrafficSnapshot(snapshot *models.TrafficSnapshot) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO traffic_snapshots (
			server_id, collected_at, total_requests, total_bytes_in, total_bytes_out,
			unique_ips, unique_countries, total_2xx, total_3xx, total_4xx, total_5xx,
			avg_response_time, p95_response_time, p99_response_time,
			requests_per_second, bytes_per_second
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.ServerID, snapshot.CollectedAt, snapshot.TotalRequests,
		snapshot.TotalBytesIn, snapshot.TotalBytesOut,
		snapshot.UniqueIPs, snapshot.UniqueCountries,
		snapshot.TotalResponse2xx, snapshot.TotalResponse3xx,
		snapshot.TotalResponse4xx, snapshot.TotalResponse5xx,
		snapshot.AvgResponseTime, snapshot.P95ResponseTime, snapshot.P99ResponseTime,
		snapshot.RequestsPerSecond, snapshot.BytesPerSecond,
	)
	return err
}

// GetTrafficSnapshots retrieves historical traffic snapshots.
func (d *DB) GetTrafficSnapshots(serverID string, since time.Time, limit int) ([]models.TrafficSnapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, server_id, collected_at, total_requests, total_bytes_in, total_bytes_out,
			unique_ips, unique_countries, total_2xx, total_3xx, total_4xx, total_5xx,
			avg_response_time, p95_response_time, p99_response_time,
			requests_per_second, bytes_per_second
		FROM traffic_snapshots
		WHERE server_id = ? AND collected_at >= ?
		ORDER BY collected_at DESC
		LIMIT ?`,
		serverID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var snapshots []models.TrafficSnapshot
	for rows.Next() {
		var s models.TrafficSnapshot
		err := rows.Scan(
			&s.ID, &s.ServerID, &s.CollectedAt, &s.TotalRequests,
			&s.TotalBytesIn, &s.TotalBytesOut, &s.UniqueIPs, &s.UniqueCountries,
			&s.TotalResponse2xx, &s.TotalResponse3xx, &s.TotalResponse4xx, &s.TotalResponse5xx,
			&s.AvgResponseTime, &s.P95ResponseTime, &s.P99ResponseTime,
			&s.RequestsPerSecond, &s.BytesPerSecond,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	// Reverse to get chronological order
	for i, j := 0, len(snapshots)-1; i < j; i, j = i+1, j-1 {
		snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
	}

	return snapshots, nil
}

// CreateTrafficAnomaly creates a new traffic anomaly record.
func (d *DB) CreateTrafficAnomaly(anomaly *models.TrafficAnomaly) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO traffic_anomalies (
			server_id, type, severity, title, description,
			affected_ip, affected_backend, metrics, detected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		anomaly.AffectedBackend, // Using this as server_id placeholder
		anomaly.Type, anomaly.Severity, anomaly.Title, anomaly.Description,
		anomaly.AffectedIP, anomaly.AffectedBackend, "", anomaly.DetectedAt,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetActiveAnomalies retrieves unresolved traffic anomalies.
func (d *DB) GetActiveAnomalies(serverID string) ([]models.TrafficAnomaly, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, type, severity, title, description,
			affected_ip, affected_backend, detected_at, resolved_at
		FROM traffic_anomalies
		WHERE server_id = ? AND resolved_at IS NULL
		ORDER BY detected_at DESC`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var anomalies []models.TrafficAnomaly
	for rows.Next() {
		var a models.TrafficAnomaly
		var affectedIP, affectedBackend sql.NullString
		err := rows.Scan(
			&a.ID, &a.Type, &a.Severity, &a.Title, &a.Description,
			&affectedIP, &affectedBackend, &a.DetectedAt, &a.ResolvedAt,
		)
		if err != nil {
			return nil, err
		}
		if affectedIP.Valid {
			a.AffectedIP = affectedIP.String
		}
		if affectedBackend.Valid {
			a.AffectedBackend = affectedBackend.String
		}
		anomalies = append(anomalies, a)
	}
	return anomalies, nil
}

// CleanupOldTrafficData removes traffic data older than the retention period.
func (d *DB) CleanupOldTrafficData(serverID string, retention time.Duration) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-retention)
	var totalDeleted int64

	// Delete old traffic flows
	result, err := d.db.Exec(`DELETE FROM traffic_flows WHERE server_id = ? AND bucket_time < ?`, serverID, cutoff)
	if err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	totalDeleted += deleted

	// Delete old traffic snapshots
	result, err = d.db.Exec(`DELETE FROM traffic_snapshots WHERE server_id = ? AND collected_at < ?`, serverID, cutoff)
	if err != nil {
		return totalDeleted, err
	}
	deleted, _ = result.RowsAffected()
	totalDeleted += deleted

	// Delete old resolved anomalies
	result, err = d.db.Exec(`DELETE FROM traffic_anomalies WHERE server_id = ? AND resolved_at IS NOT NULL AND resolved_at < ?`, serverID, cutoff)
	if err != nil {
		return totalDeleted, err
	}
	deleted, _ = result.RowsAffected()
	totalDeleted += deleted

	return totalDeleted, nil
}

// GetRecentTrafficFlows retrieves recent traffic flow records.
func (d *DB) GetRecentTrafficFlows(filter *models.TrafficFilter) ([]models.TrafficFlow, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, server_id, source_ip, country, country_code,
			backend_name, frontend_name, bucket_time,
			request_count, bytes_in, bytes_out,
			response_2xx, response_3xx, response_4xx, response_5xx,
			avg_response_time, max_response_time
		FROM traffic_flows
		WHERE server_id = ?
	`
	args := []interface{}{filter.BoxID}

	if filter.BackendName != "" {
		query += " AND backend_name = ?"
		args = append(args, filter.BackendName)
	}
	if filter.SourceIP != "" {
		query += " AND source_ip = ?"
		args = append(args, filter.SourceIP)
	}
	if !filter.StartTime.IsZero() {
		query += " AND bucket_time >= ?"
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += " AND bucket_time <= ?"
		args = append(args, filter.EndTime)
	}

	query += " ORDER BY bucket_time DESC"

	limit := 100
	if filter.Limit > 0 && filter.Limit <= 1000 {
		limit = filter.Limit
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var flows []models.TrafficFlow
	for rows.Next() {
		var f models.TrafficFlow
		err := rows.Scan(
			&f.ID, &f.ServerID, &f.SourceIP, &f.Country, &f.CountryCode,
			&f.BackendName, &f.FrontendName, &f.Timestamp,
			&f.RequestCount, &f.BytesIn, &f.BytesOut,
			&f.Response2xx, &f.Response3xx, &f.Response4xx, &f.Response5xx,
			&f.AvgResponseTime, &f.MaxResponseTime,
		)
		if err != nil {
			return nil, err
		}
		// Calculate error rate
		total := f.Response2xx + f.Response3xx + f.Response4xx + f.Response5xx
		if total > 0 {
			f.ErrorRate = float64(f.Response4xx+f.Response5xx) / float64(total) * 100
		}
		flows = append(flows, f)
	}

	return flows, nil
}
