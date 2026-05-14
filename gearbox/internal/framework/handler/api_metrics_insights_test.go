package handler

import (
	"testing"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

func TestMetricsRangeToTimes(t *testing.T) {
	tests := []struct {
		in            string
		wantLabel     string
		wantDeltaMin  float64 // minutes from now
	}{
		{"5m", "5m", 5},
		{"30m", "30m", 30},
		{"1h", "1h", 60},
		{"6h", "6h", 360},
		{"24h", "24h", 24 * 60},
		{"3d", "3d", 3 * 24 * 60},
		{"7d", "7d", 7 * 24 * 60},
		{"", "24h", 24 * 60},
		{"banana", "24h", 24 * 60},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			since, until, label := metricsRangeToTimes(tt.in)
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
			delta := until.Sub(since).Minutes()
			if delta < tt.wantDeltaMin-0.5 || delta > tt.wantDeltaMin+0.5 {
				t.Errorf("window = %v min, want ~%v min", delta, tt.wantDeltaMin)
			}
		})
	}
}

func TestPctDelta(t *testing.T) {
	tests := []struct {
		name       string
		curr, prev float64
		want       float64
	}{
		{"both zero → no change", 0, 0, 0},
		{"prev zero, curr non-zero → 100% jump", 50, 0, 100},
		{"50% up", 150, 100, 50},
		{"50% down", 50, 100, -50},
		{"flat", 100, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pctDelta(tt.curr, tt.prev)
			if got != tt.want {
				t.Errorf("pctDelta(%v, %v) = %v, want %v", tt.curr, tt.prev, got, tt.want)
			}
		})
	}
}

func TestPctOf(t *testing.T) {
	if got := pctOf(0, 0); got != 0 {
		t.Errorf("0/0 expected 0 to avoid NaN, got %v", got)
	}
	if got := pctOf(50, 100); got != 50 {
		t.Errorf("50/100 = 50%%, got %v", got)
	}
}

func TestPerMinute(t *testing.T) {
	now := time.Now()
	got := perMinute(120, now.Add(-2*time.Minute), now)
	if got != 60 {
		t.Errorf("120 reqs in 2 min should be 60/min, got %v", got)
	}
	if got := perMinute(0, now, now); got != 0 {
		t.Errorf("zero-length window should be 0, got %v", got)
	}
}

func TestStatusFromCount(t *testing.T) {
	if statusFromCount(0, 5, 50) != "good" {
		t.Error("count=0 should be good")
	}
	if statusFromCount(10, 5, 50) != "warn" {
		t.Error("count=10 should be warn")
	}
	if statusFromCount(100, 5, 50) != "bad" {
		t.Error("count=100 should be bad")
	}
}

func TestStatSparklineDownsamples(t *testing.T) {
	// Build a 100-element series of monotonically increasing values.
	rows := make([]database.StatsSnapshot, 100)
	for i := range rows {
		rows[i].TotalRequests = int64(i)
	}
	out := statSparkline(rows, func(s database.StatsSnapshot) float64 { return float64(s.TotalRequests) }, 30)
	if len(out) != 30 {
		t.Errorf("expected 30 sparkline points, got %d", len(out))
	}
	if out[0] != 0 {
		t.Errorf("first point should be 0, got %v", out[0])
	}
	// Last sampled index is int(29 * 100/30) = 96, so out[29] should be 96.
	if out[29] < 90 {
		t.Errorf("last sparkline point should be near the end of series, got %v", out[29])
	}
}

func TestStatSparklineShortSeriesPassThrough(t *testing.T) {
	rows := []database.StatsSnapshot{
		{TotalRequests: 1},
		{TotalRequests: 2},
		{TotalRequests: 3},
	}
	out := statSparkline(rows, func(s database.StatsSnapshot) float64 { return float64(s.TotalRequests) }, 30)
	if len(out) != 3 {
		t.Errorf("expected pass-through of 3 points, got %d", len(out))
	}
}

func TestAvgPositiveStatSkipsZeros(t *testing.T) {
	rows := []database.StatsSnapshot{
		{AvgResponseTime: 100},
		{AvgResponseTime: 0}, // empty bucket — should be skipped
		{AvgResponseTime: 200},
	}
	got := avgPositiveStat(rows, func(s database.StatsSnapshot) float64 { return float64(s.AvgResponseTime) })
	if got != 150 {
		t.Errorf("expected avg of (100, 200) = 150, got %v", got)
	}
}

func TestAvgPositiveSysFieldSkipsZeros(t *testing.T) {
	rows := []database.SystemMetricsSnapshot{
		{MemoryUsagePercent: 40},
		{MemoryUsagePercent: 0}, // empty bucket — should be skipped
		{MemoryUsagePercent: 60},
	}
	got := avgPositiveSysField(rows, func(s database.SystemMetricsSnapshot) float64 { return s.MemoryUsagePercent })
	if got != 50 {
		t.Errorf("expected avg of (40, 60) = 50, got %v", got)
	}
}

func TestSysSparklineDownsamples(t *testing.T) {
	rows := make([]database.SystemMetricsSnapshot, 100)
	for i := range rows {
		rows[i].MemoryUsagePercent = float64(i)
	}
	out := sysSparkline(rows, func(s database.SystemMetricsSnapshot) float64 { return s.MemoryUsagePercent }, 30)
	if len(out) != 30 {
		t.Errorf("expected 30 sparkline points, got %d", len(out))
	}
	if out[0] != 0 {
		t.Errorf("first point should be 0, got %v", out[0])
	}
	if out[29] < 90 {
		t.Errorf("last point should be near series end, got %v", out[29])
	}
}

func TestSysSparklineShortSeriesPassThrough(t *testing.T) {
	rows := []database.SystemMetricsSnapshot{
		{MemoryUsagePercent: 10},
		{MemoryUsagePercent: 20},
	}
	out := sysSparkline(rows, func(s database.SystemMetricsSnapshot) float64 { return s.MemoryUsagePercent }, 30)
	if len(out) != 2 {
		t.Errorf("expected pass-through of 2 points, got %d", len(out))
	}
}

func TestParseHAProxyLogLine_5xx(t *testing.T) {
	// Representative HAProxy HTTP log line with a 502 response.
	raw := `Apr 14 02:13:45 lighthugger haproxy[1234]: 203.0.113.42:51234 [14/Apr/2026:02:13:45.123] https-in~ app-backend/server1 0/0/1/0/1 502 1234 - - ---- 5/5/0/0/0 0/0 "GET /api/widgets HTTP/1.1"`
	got := parseHAProxyLogLine(raw)
	if got == nil {
		t.Fatal("expected to parse 502 line, got nil")
	}
	if got.StatusCode != 502 {
		t.Errorf("status = %d, want 502", got.StatusCode)
	}
	if got.SourceIP != "203.0.113.42" {
		t.Errorf("source_ip = %q, want 203.0.113.42", got.SourceIP)
	}
	if got.Backend != "app-backend" {
		t.Errorf("backend = %q, want app-backend", got.Backend)
	}
	if got.Server != "server1" {
		t.Errorf("server = %q, want server1", got.Server)
	}
	if got.Method != "GET" {
		t.Errorf("method = %q, want GET", got.Method)
	}
	if got.Path != "/api/widgets" {
		t.Errorf("path = %q, want /api/widgets", got.Path)
	}
}

func TestParseHAProxyLogLine_NoMatch(t *testing.T) {
	if parseHAProxyLogLine("") != nil {
		t.Error("empty line should not parse")
	}
	if parseHAProxyLogLine("not an haproxy log line at all") != nil {
		t.Error("garbage line should not parse")
	}
	if parseHAProxyLogLine("haproxy[42]: SSL handshake failure from 1.2.3.4") != nil {
		t.Error("non-HTTP-access-log line should not parse")
	}
}

func TestParseHAProxyLogLine_RawTruncated(t *testing.T) {
	// Build a 2000-char raw line.
	pad := ""
	for i := 0; i < 200; i++ {
		pad += "0123456789"
	}
	raw := `Apr 14 02:13:45 host haproxy[1]: 1.2.3.4:1234 [14/Apr/2026:02:13:45] f~ b/s 0/0/0/0/0 200 100 - - ---- 1/1/0/0/0 0/0 "GET /` + pad + ` HTTP/1.1"`
	got := parseHAProxyLogLine(raw)
	if got == nil {
		t.Fatal("expected to parse line")
	}
	if len(got.Raw) > 1100 {
		t.Errorf("raw should be truncated, got %d chars", len(got.Raw))
	}
}

func TestDownsampleFloats(t *testing.T) {
	if got := downsampleFloats(nil, 30); len(got) != 0 {
		t.Errorf("nil input should give empty output, got %d", len(got))
	}
	short := []float64{1, 2, 3}
	if got := downsampleFloats(short, 30); len(got) != 3 {
		t.Errorf("short input should pass through, got %d", len(got))
	}
	long := make([]float64, 100)
	for i := range long {
		long[i] = float64(i)
	}
	if got := downsampleFloats(long, 30); len(got) != 30 {
		t.Errorf("long input should sample to 30, got %d", len(got))
	}
}
