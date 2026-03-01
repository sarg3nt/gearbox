package metrics

import (
	"testing"
	"time"
)

func TestParseSystemdTimestamp(t *testing.T) {
	// These tests focus on parsing correctness, not uptime calculation.
	// We verify the parsed time components to ensure correct parsing.
	//
	// Systemd outputs timestamps in various formats depending on locale and version:
	// - "Mon 2006-01-02 15:04:05 MST" (with day name and timezone abbrev)
	// - "Mon 2006-01-02 15:04:05 -0700" (with day name and numeric offset)
	// - "2006-01-02 15:04:05 MST" (without day name)
	// - "2006-01-02 15:04:05 -0700" (without day name, numeric offset)
	//
	// IMPORTANT: Go's time.Parse with MST format only reliably handles UTC/GMT.
	// Timezone abbreviations like EST/PST are parsed but may not have correct
	// offset information unless the system has the timezone database loaded.
	// For reliable timezone handling, numeric offsets (-0500, +0530) are preferred.
	// Most systemd installations output UTC timestamps by default.

	tests := []struct {
		name          string
		timestamp     string
		wantYear      int // Expected year (0 means don't check - for failure cases)
		wantMonth     time.Month
		wantDay       int
		wantHour      int // Expected hour (in parsed timezone, not necessarily UTC)
		wantMinute    int
		wantSecond    int
		shouldSucceed bool
	}{
		// UTC timezone tests - most common systemd format
		{
			name:          "UTC timezone with day name",
			timestamp:     "Wed 2026-01-21 06:01:18 UTC",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      6,
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		{
			name:          "UTC timezone with different day",
			timestamp:     "Fri 2026-01-02 05:09:56 UTC",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       2,
			wantHour:      5,
			wantMinute:    9,
			wantSecond:    56,
			shouldSucceed: true,
		},
		{
			name:          "UTC timezone Sunday",
			timestamp:     "Sun 2026-01-04 23:28:39 UTC",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       4,
			wantHour:      23,
			wantMinute:    28,
			wantSecond:    39,
			shouldSucceed: true,
		},
		// Numeric offset format tests - these reliably convert to UTC
		{
			name:          "numeric offset -0500 (EST equivalent)",
			timestamp:     "Wed 2026-01-21 01:01:18 -0500",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      6, // 01:01 -0500 = 06:01 UTC
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		{
			name:          "numeric offset +0000 (UTC)",
			timestamp:     "Wed 2026-01-21 06:01:18 +0000",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      6,
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		{
			name:          "numeric offset +0530 (IST - India)",
			timestamp:     "Wed 2026-01-21 11:31:18 +0530",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      6, // 11:31 +0530 = 06:01 UTC
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		{
			name:          "numeric offset -0800 (PST equivalent)",
			timestamp:     "Wed 2026-01-21 22:01:18 -0800",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       22,
			wantHour:      6, // 22:01 -0800 Jan 21 = 06:01 UTC Jan 22
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		{
			name:          "year boundary - Dec 31 late night with numeric offset",
			timestamp:     "Wed 2025-12-31 22:00:00 -0500",
			wantYear:      2026, // 22:00 -0500 Dec 31 = 03:00 UTC Jan 1
			wantMonth:     time.January,
			wantDay:       1,
			wantHour:      3,
			wantMinute:    0,
			wantSecond:    0,
			shouldSucceed: true,
		},
		// Format without day name (some systemd versions)
		{
			name:          "without day name UTC",
			timestamp:     "2026-01-21 06:01:18 UTC",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      6,
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		{
			name:          "without day name numeric offset",
			timestamp:     "2026-01-21 01:01:18 -0500",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      6,
			wantMinute:    1,
			wantSecond:    18,
			shouldSucceed: true,
		},
		// Edge cases
		{
			name:          "leap year date",
			timestamp:     "Sat 2028-02-29 12:00:00 UTC",
			wantYear:      2028,
			wantMonth:     time.February,
			wantDay:       29,
			wantHour:      12,
			wantMinute:    0,
			wantSecond:    0,
			shouldSucceed: true,
		},
		{
			name:          "midnight UTC",
			timestamp:     "Wed 2026-01-21 00:00:00 UTC",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      0,
			wantMinute:    0,
			wantSecond:    0,
			shouldSucceed: true,
		},
		{
			name:          "end of day UTC",
			timestamp:     "Wed 2026-01-21 23:59:59 UTC",
			wantYear:      2026,
			wantMonth:     time.January,
			wantDay:       21,
			wantHour:      23,
			wantMinute:    59,
			wantSecond:    59,
			shouldSucceed: true,
		},
		// Invalid/empty inputs
		{
			name:          "empty string",
			timestamp:     "",
			shouldSucceed: false,
		},
		{
			name:          "n/a value",
			timestamp:     "n/a",
			shouldSucceed: false,
		},
		{
			name:          "zero value",
			timestamp:     "0",
			shouldSucceed: false,
		},
		{
			name:          "invalid format",
			timestamp:     "not a timestamp",
			shouldSucceed: false,
		},
		{
			name:          "partial timestamp - date only",
			timestamp:     "2026-01-21",
			shouldSucceed: false,
		},
		{
			name:          "invalid date",
			timestamp:     "Wed 2026-02-30 12:00:00 UTC",
			shouldSucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotISO, gotUptime := parseSystemdTimestamp(tt.timestamp)

			if tt.shouldSucceed {
				if gotISO == "" {
					t.Errorf("parseSystemdTimestamp(%q) returned empty ISO, expected non-empty", tt.timestamp)
					return
				}
				if gotUptime == "" {
					t.Errorf("parseSystemdTimestamp(%q) returned empty uptime, expected non-empty", tt.timestamp)
				}
				// Parse the result and verify the date/time components
				gotTime, err := time.Parse(time.RFC3339, gotISO)
				if err != nil {
					t.Errorf("parseSystemdTimestamp(%q) returned invalid ISO %q: %v", tt.timestamp, gotISO, err)
					return
				}
				// Convert to UTC for consistent comparison
				gotUTC := gotTime.UTC()
				if gotUTC.Year() != tt.wantYear {
					t.Errorf("parseSystemdTimestamp(%q) year = %d, want %d", tt.timestamp, gotUTC.Year(), tt.wantYear)
				}
				if gotUTC.Month() != tt.wantMonth {
					t.Errorf("parseSystemdTimestamp(%q) month = %v, want %v", tt.timestamp, gotUTC.Month(), tt.wantMonth)
				}
				if gotUTC.Day() != tt.wantDay {
					t.Errorf("parseSystemdTimestamp(%q) day = %d, want %d", tt.timestamp, gotUTC.Day(), tt.wantDay)
				}
				if gotUTC.Hour() != tt.wantHour {
					t.Errorf("parseSystemdTimestamp(%q) hour = %d, want %d", tt.timestamp, gotUTC.Hour(), tt.wantHour)
				}
				if gotUTC.Minute() != tt.wantMinute {
					t.Errorf("parseSystemdTimestamp(%q) minute = %d, want %d", tt.timestamp, gotUTC.Minute(), tt.wantMinute)
				}
				if gotUTC.Second() != tt.wantSecond {
					t.Errorf("parseSystemdTimestamp(%q) second = %d, want %d", tt.timestamp, gotUTC.Second(), tt.wantSecond)
				}
			} else {
				if gotISO != "" {
					t.Errorf("parseSystemdTimestamp(%q) = %q, want empty string", tt.timestamp, gotISO)
				}
				if gotUptime != "" {
					t.Errorf("parseSystemdTimestamp(%q) uptime = %q, want empty string", tt.timestamp, gotUptime)
				}
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "seconds only",
			duration: 45 * time.Second,
			want:     "45s",
		},
		{
			name:     "minutes and seconds",
			duration: 5*time.Minute + 30*time.Second,
			want:     "5m 30s",
		},
		{
			name:     "minutes only (no seconds)",
			duration: 5 * time.Minute,
			want:     "5m",
		},
		{
			name:     "hours and minutes",
			duration: 3*time.Hour + 45*time.Minute,
			want:     "3h 45m",
		},
		{
			name:     "hours only (no minutes)",
			duration: 3 * time.Hour,
			want:     "3h",
		},
		{
			name:     "days and hours",
			duration: 5*24*time.Hour + 12*time.Hour,
			want:     "5d 12h",
		},
		{
			name:     "days only (no hours)",
			duration: 5 * 24 * time.Hour,
			want:     "5d",
		},
		{
			name:     "19 days",
			duration: 19 * 24 * time.Hour,
			want:     "19d",
		},
		{
			name:     "19 days and 5 hours",
			duration: 19*24*time.Hour + 5*time.Hour,
			want:     "19d 5h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestCollectServiceStatuses(t *testing.T) {
	// This test requires systemd to be running
	// Skip if not on Linux
	c := NewCollector()

	// Test with a service that should exist on most Linux systems
	statuses := c.CollectServiceStatuses([]string{"nonexistent-service-12345"})

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	status := statuses[0]
	if status.Name != "nonexistent-service-12345" {
		t.Errorf("expected name 'nonexistent-service-12345', got %q", status.Name)
	}

	// Non-existent service should be inactive or unknown
	if status.Active {
		t.Errorf("expected non-existent service to be inactive")
	}
}
