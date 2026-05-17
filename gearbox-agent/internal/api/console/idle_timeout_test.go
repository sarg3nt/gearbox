package console

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNewHandler_IdleTimeoutEnvOverride checks the three branches of
// the env knob: valid value → applied, invalid value → keep default,
// non-positive → keep default. Important for operators tuning
// long-running sessions (e.g. a long apt upgrade) — a regression here
// silently caps every session at 15 minutes.
func TestNewHandler_IdleTimeoutEnvOverride(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		wantChange bool
		want       time.Duration
	}{
		{"valid_hours", "2h", true, 2 * time.Hour},
		{"valid_minutes", "45m", true, 45 * time.Minute},
		{"empty_keeps_default", "", false, 15 * time.Minute},
		{"garbage_keeps_default", "ten minutes", false, 15 * time.Minute},
		{"zero_keeps_default", "0", false, 15 * time.Minute},
		{"negative_keeps_default", "-5m", false, 15 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HAPROXY_AGENT_CONSOLE_IDLE_TIMEOUT", tc.env)
			bus := events.NewBus()
			defer bus.Close()
			h := NewHandler(bus, newSilentLogger())
			defer h.Close()
			if h.IdleTimeout != tc.want {
				t.Errorf("IdleTimeout = %v, want %v", h.IdleTimeout, tc.want)
			}
		})
	}
}
