package handler

import (
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

// snapshotField is a function returning a numeric field from one
// StatsSnapshot row — lets us reuse the sparkline / aggregate helpers
// across the different KPIs without writing per-field boilerplate.
type snapshotField func(s database.StatsSnapshot) float64

// lastStat returns the most recent value of f, or 0 for an empty slice.
// Snapshots are stored chronologically (oldest → newest).
func lastStat(curr []database.StatsSnapshot, f snapshotField) float64 {
	if len(curr) == 0 {
		return 0
	}
	return f(curr[len(curr)-1])
}

// maxStatField returns max(f(s)) across the slice. Used for cumulative
// counters where we want the highest observation in the window — the
// snapshot table stores absolute counters, so max == "value at the end of
// the window" for monotonically-increasing fields like TotalRequests.
func maxStatField(curr []database.StatsSnapshot, f snapshotField) float64 {
	var max float64
	for i := range curr {
		v := f(curr[i])
		if v > max {
			max = v
		}
	}
	return max
}

// avgPositiveStat averages f(s) over rows where f(s) > 0. Skipping zero
// rows matters for fields like AvgResponseTime, where "no traffic in this
// bucket" gets recorded as 0 and would otherwise drag the average down.
func avgPositiveStat(curr []database.StatsSnapshot, f snapshotField) float64 {
	var sum float64
	var n int
	for i := range curr {
		v := f(curr[i])
		if v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// statSparkline downsamples a stats slice to roughly `points` values for
// rendering inline sparklines in KPI cards. Returns at most `points`
// floats; for short series it returns the full series.
func statSparkline(curr []database.StatsSnapshot, f snapshotField, points int) []float64 {
	if len(curr) == 0 {
		return []float64{}
	}
	if len(curr) <= points {
		out := make([]float64, len(curr))
		for i := range curr {
			out[i] = f(curr[i])
		}
		return out
	}
	out := make([]float64, 0, points)
	step := float64(len(curr)) / float64(points)
	for i := 0; i < points; i++ {
		idx := int(float64(i) * step)
		if idx >= len(curr) {
			idx = len(curr) - 1
		}
		out = append(out, f(curr[idx]))
	}
	return out
}

// errorTotals returns (5xx, 4xx, total responses) over the window. The
// numbers come from traffic_flows because that table stores actual
// per-bucket response counts rather than cumulative counters — it
// answers "how many errors in the last hour" directly without delta
// computation.
//
// All three int64s default to 0 on error so callers don't have to special-
// case it; we log warnings but never fail the whole KPI fetch over an
// error-rate cell.
func (h *Handler) errorTotals(boxID string, since, until time.Time) (int64, int64, int64) {
	row := h.db.QueryRowContext(boxID, since, until)
	if row == nil {
		return 0, 0, 0
	}
	return row.Response5xx, row.Response4xx, row.Total
}

// errorRateSparkline returns ~`points` per-bucket error-rate percentages
// across the window — used to draw the Error Rate KPI's sparkline.
func (h *Handler) errorRateSparkline(boxID string, since, until time.Time, points int) []float64 {
	buckets := h.db.GetErrorRateBuckets(boxID, since, until)
	return downsampleFloats(buckets, points)
}

// errorCountSparkline returns ~`points` per-bucket 5xx counts.
func (h *Handler) errorCountSparkline(boxID string, since, until time.Time, points int) []float64 {
	buckets := h.db.Get5xxCountBuckets(boxID, since, until)
	return downsampleFloats(buckets, points)
}

func downsampleFloats(in []float64, points int) []float64 {
	if len(in) == 0 {
		return []float64{}
	}
	if len(in) <= points {
		return in
	}
	out := make([]float64, 0, points)
	step := float64(len(in)) / float64(points)
	for i := 0; i < points; i++ {
		idx := int(float64(i) * step)
		if idx >= len(in) {
			idx = len(in) - 1
		}
		out = append(out, in[idx])
	}
	return out
}

func pctDelta(curr, prev float64) float64 {
	if prev == 0 {
		if curr == 0 {
			return 0
		}
		return 100
	}
	return (curr - prev) / prev * 100
}

func pctOf(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func perMinute(value float64, since, until time.Time) float64 {
	mins := until.Sub(since).Minutes()
	if mins <= 0 {
		return 0
	}
	return value / mins
}

func statusFromCount(count int64, warnAt, badAt int64) string {
	if count >= badAt {
		return "bad"
	}
	if count >= warnAt {
		return "warn"
	}
	return "good"
}

// sysField mirrors snapshotField for SystemMetricsSnapshot rows, so the
// host KPI cards can reuse the same aggregate/sparkline shape as the
// HAProxy cards without templated generics.
type sysField func(s database.SystemMetricsSnapshot) float64

// avgPositiveSysField is the SystemMetricsSnapshot counterpart of
// avgPositiveStat — averages f over rows where f(s) > 0 so empty buckets
// don't pull down legitimate readings.
func avgPositiveSysField(curr []database.SystemMetricsSnapshot, f sysField) float64 {
	var sum float64
	var n int
	for i := range curr {
		v := f(curr[i])
		if v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// sysSparkline downsamples a SystemMetricsSnapshot slice to ~points values
// for KPI-card sparklines. Mirrors statSparkline so host and HAProxy
// cards render identically.
func sysSparkline(curr []database.SystemMetricsSnapshot, f sysField, points int) []float64 {
	if len(curr) == 0 {
		return []float64{}
	}
	if len(curr) <= points {
		out := make([]float64, len(curr))
		for i := range curr {
			out[i] = f(curr[i])
		}
		return out
	}
	out := make([]float64, 0, points)
	step := float64(len(curr)) / float64(points)
	for i := 0; i < points; i++ {
		idx := int(float64(i) * step)
		if idx >= len(curr) {
			idx = len(curr) - 1
		}
		out = append(out, f(curr[idx]))
	}
	return out
}
