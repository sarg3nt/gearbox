package database

import (
	"time"
)

// ErrorTotals holds the response totals over a window for the error-rate KPI.
type ErrorTotals struct {
	Response2xx int64
	Response3xx int64
	Response4xx int64
	Response5xx int64
	Total       int64
}

// QueryRowContext sums response counts from traffic_flows over the window.
// It never returns nil — callers can rely on the zero value if no rows match.
func (d *DB) QueryRowContext(boxID string, since, until time.Time) *ErrorTotals {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT
			COALESCE(SUM(response_2xx), 0),
			COALESCE(SUM(response_3xx), 0),
			COALESCE(SUM(response_4xx), 0),
			COALESCE(SUM(response_5xx), 0)
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ? AND bucket_time <= ?`,
		boxID, since, until,
	)

	t := &ErrorTotals{}
	if err := row.Scan(&t.Response2xx, &t.Response3xx, &t.Response4xx, &t.Response5xx); err != nil {
		return t
	}
	t.Total = t.Response2xx + t.Response3xx + t.Response4xx + t.Response5xx
	return t
}

// GetErrorRateBuckets returns per-minute error-rate percentages over the
// window, suitable for sparkline rendering.
func (d *DB) GetErrorRateBuckets(boxID string, since, until time.Time) []float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT
			COALESCE(SUM(response_4xx + response_5xx), 0) AS errs,
			COALESCE(SUM(response_2xx + response_3xx + response_4xx + response_5xx), 0) AS total
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ? AND bucket_time <= ?
		GROUP BY bucket_time
		ORDER BY bucket_time ASC`,
		boxID, since, until,
	)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var out []float64
	for rows.Next() {
		var errs, total int64
		if err := rows.Scan(&errs, &total); err != nil {
			continue
		}
		if total == 0 {
			out = append(out, 0)
			continue
		}
		out = append(out, float64(errs)/float64(total)*100)
	}
	return out
}

// Get5xxCountBuckets returns per-minute 5xx counts over the window.
func (d *DB) Get5xxCountBuckets(boxID string, since, until time.Time) []float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT COALESCE(SUM(response_5xx), 0)
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ? AND bucket_time <= ?
		GROUP BY bucket_time
		ORDER BY bucket_time ASC`,
		boxID, since, until,
	)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var out []float64
	for rows.Next() {
		var c int64
		if err := rows.Scan(&c); err != nil {
			continue
		}
		out = append(out, float64(c))
	}
	return out
}

// ErrorBackendRow describes a backend's traffic + error breakdown over a window.
// Used by the Error Insights panel on the metrics page.
type ErrorBackendRow struct {
	BackendName   string  `json:"backend_name"`
	Requests      int64   `json:"requests"`
	Response2xx   int64   `json:"response_2xx"`
	Response3xx   int64   `json:"response_3xx"`
	Response4xx   int64   `json:"response_4xx"`
	Response5xx   int64   `json:"response_5xx"`
	ErrorRate     float64 `json:"error_rate"`
	BytesIn       int64   `json:"bytes_in"`
	BytesOut      int64   `json:"bytes_out"`
	UniqueIPs     int     `json:"unique_ips"`
	AvgResponseMs int64   `json:"avg_response_ms"`
}

// ErrorSourceRow describes a source IP's traffic + error breakdown over a window.
type ErrorSourceRow struct {
	SourceIP      string  `json:"source_ip"`
	Country       string  `json:"country"`
	CountryCode   string  `json:"country_code"`
	Requests      int64   `json:"requests"`
	Response4xx   int64   `json:"response_4xx"`
	Response5xx   int64   `json:"response_5xx"`
	ErrorCount    int64   `json:"error_count"`
	ErrorRate     float64 `json:"error_rate"`
	Backends      int     `json:"backends_hit"`
}

// ErrorCountryRow describes a country's request + error totals.
type ErrorCountryRow struct {
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Requests    int64   `json:"requests"`
	Errors      int64   `json:"errors"`
	ErrorRate   float64 `json:"error_rate"`
	UniqueIPs   int     `json:"unique_ips"`
}

// BackendTimeBucket is a single per-minute aggregate row for one backend.
type BackendTimeBucket struct {
	BucketTime    time.Time `json:"bucket_time"`
	Requests      int64     `json:"requests"`
	Response2xx   int64     `json:"response_2xx"`
	Response3xx   int64     `json:"response_3xx"`
	Response4xx   int64     `json:"response_4xx"`
	Response5xx   int64     `json:"response_5xx"`
	AvgResponseMs int64     `json:"avg_response_ms"`
	BytesIn       int64     `json:"bytes_in"`
	BytesOut      int64     `json:"bytes_out"`
}

// GetTopErrorBackends returns backends ordered by 5xx error count (desc) over
// the time window. Backends with zero 5xx are excluded — this view is for
// "what's broken?", not "what's quiet?".
func (d *DB) GetTopErrorBackends(boxID string, since, until time.Time, limit int) ([]ErrorBackendRow, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT
			backend_name,
			COALESCE(SUM(request_count), 0)  AS requests,
			COALESCE(SUM(response_2xx), 0)   AS r2xx,
			COALESCE(SUM(response_3xx), 0)   AS r3xx,
			COALESCE(SUM(response_4xx), 0)   AS r4xx,
			COALESCE(SUM(response_5xx), 0)   AS r5xx,
			COALESCE(AVG(avg_response_time), 0) AS avg_rt,
			COALESCE(SUM(bytes_in), 0)       AS bin,
			COALESCE(SUM(bytes_out), 0)      AS bout,
			COUNT(DISTINCT source_ip)        AS unique_ips
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ? AND bucket_time <= ?
		  AND backend_name != '' AND backend_name != '_unknown'
		GROUP BY backend_name
		HAVING r5xx > 0 OR r4xx > 0
		ORDER BY r5xx DESC, r4xx DESC
		LIMIT ?`,
		boxID, since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ErrorBackendRow
	for rows.Next() {
		var r ErrorBackendRow
		if err := rows.Scan(
			&r.BackendName, &r.Requests,
			&r.Response2xx, &r.Response3xx, &r.Response4xx, &r.Response5xx,
			&r.AvgResponseMs, &r.BytesIn, &r.BytesOut, &r.UniqueIPs,
		); err != nil {
			return nil, err
		}
		total := r.Response2xx + r.Response3xx + r.Response4xx + r.Response5xx
		if total > 0 {
			r.ErrorRate = float64(r.Response4xx+r.Response5xx) / float64(total) * 100
		}
		out = append(out, r)
	}
	return out, nil
}

// GetTopErrorSources returns source IPs ranked by 4xx+5xx error count over the
// window. Source IPs marked as aggregates (the "_aggregate" sentinel used in
// persistTrafficData) and local addresses are excluded.
func (d *DB) GetTopErrorSources(boxID string, since, until time.Time, limit int) ([]ErrorSourceRow, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT
			source_ip,
			MAX(country)       AS country,
			MAX(country_code)  AS country_code,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(response_4xx), 0)  AS r4xx,
			COALESCE(SUM(response_5xx), 0)  AS r5xx,
			COALESCE(SUM(response_2xx + response_3xx + response_4xx + response_5xx), 0) AS total_resp,
			COUNT(DISTINCT backend_name) AS backends
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ? AND bucket_time <= ?
		  AND source_ip != '' AND source_ip != '_aggregate'
		GROUP BY source_ip
		HAVING (r4xx + r5xx) > 0
		ORDER BY (r4xx + r5xx) DESC, r5xx DESC
		LIMIT ?`,
		boxID, since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ErrorSourceRow
	for rows.Next() {
		var r ErrorSourceRow
		var totalResp int64
		if err := rows.Scan(
			&r.SourceIP, &r.Country, &r.CountryCode,
			&r.Requests, &r.Response4xx, &r.Response5xx, &totalResp, &r.Backends,
		); err != nil {
			return nil, err
		}
		r.ErrorCount = r.Response4xx + r.Response5xx
		if totalResp > 0 {
			r.ErrorRate = float64(r.ErrorCount) / float64(totalResp) * 100
		}
		out = append(out, r)
	}
	return out, nil
}

// GetTopErrorCountries returns countries ranked by error count.
func (d *DB) GetTopErrorCountries(boxID string, since, until time.Time, limit int) ([]ErrorCountryRow, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT
			country, country_code,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(response_4xx + response_5xx), 0) AS errors,
			COALESCE(SUM(response_2xx + response_3xx + response_4xx + response_5xx), 0) AS total_resp,
			COUNT(DISTINCT source_ip) AS unique_ips
		FROM traffic_flows
		WHERE server_id = ? AND bucket_time >= ? AND bucket_time <= ?
		  AND country_code != '' AND country_code != 'AG'
		GROUP BY country, country_code
		HAVING errors > 0
		ORDER BY errors DESC
		LIMIT ?`,
		boxID, since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ErrorCountryRow
	for rows.Next() {
		var r ErrorCountryRow
		var totalResp int64
		if err := rows.Scan(&r.Country, &r.CountryCode, &r.Requests, &r.Errors, &totalResp, &r.UniqueIPs); err != nil {
			return nil, err
		}
		if totalResp > 0 {
			r.ErrorRate = float64(r.Errors) / float64(totalResp) * 100
		}
		out = append(out, r)
	}
	return out, nil
}

// GetBackendTimeSeries returns per-minute buckets for one backend in the
// window — used by the drill-down drawer's mini-charts.
func (d *DB) GetBackendTimeSeries(boxID, backendName string, since, until time.Time, limit int) ([]BackendTimeBucket, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT
			bucket_time,
			COALESCE(SUM(request_count), 0),
			COALESCE(SUM(response_2xx), 0),
			COALESCE(SUM(response_3xx), 0),
			COALESCE(SUM(response_4xx), 0),
			COALESCE(SUM(response_5xx), 0),
			COALESCE(AVG(avg_response_time), 0),
			COALESCE(SUM(bytes_in), 0),
			COALESCE(SUM(bytes_out), 0)
		FROM traffic_flows
		WHERE server_id = ? AND backend_name = ? AND bucket_time >= ? AND bucket_time <= ?
		GROUP BY bucket_time
		ORDER BY bucket_time ASC
		LIMIT ?`,
		boxID, backendName, since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []BackendTimeBucket
	for rows.Next() {
		var b BackendTimeBucket
		if err := rows.Scan(
			&b.BucketTime, &b.Requests,
			&b.Response2xx, &b.Response3xx, &b.Response4xx, &b.Response5xx,
			&b.AvgResponseMs, &b.BytesIn, &b.BytesOut,
		); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// GetTopSourcesForBackend returns the top source IPs sending traffic to the
// given backend in the window.
func (d *DB) GetTopSourcesForBackend(boxID, backendName string, since, until time.Time, limit int) ([]ErrorSourceRow, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT
			source_ip,
			MAX(country) AS country,
			MAX(country_code) AS country_code,
			COALESCE(SUM(request_count), 0)  AS requests,
			COALESCE(SUM(response_4xx), 0)   AS r4xx,
			COALESCE(SUM(response_5xx), 0)   AS r5xx,
			COALESCE(SUM(response_2xx + response_3xx + response_4xx + response_5xx), 0) AS total_resp
		FROM traffic_flows
		WHERE server_id = ? AND backend_name = ?
		  AND bucket_time >= ? AND bucket_time <= ?
		  AND source_ip != '' AND source_ip != '_aggregate'
		GROUP BY source_ip
		ORDER BY requests DESC
		LIMIT ?`,
		boxID, backendName, since, until, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ErrorSourceRow
	for rows.Next() {
		var r ErrorSourceRow
		var totalResp int64
		if err := rows.Scan(
			&r.SourceIP, &r.Country, &r.CountryCode,
			&r.Requests, &r.Response4xx, &r.Response5xx, &totalResp,
		); err != nil {
			return nil, err
		}
		r.ErrorCount = r.Response4xx + r.Response5xx
		if totalResp > 0 {
			r.ErrorRate = float64(r.ErrorCount) / float64(totalResp) * 100
		}
		out = append(out, r)
	}
	return out, nil
}
