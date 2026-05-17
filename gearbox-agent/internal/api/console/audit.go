package console

import (
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// auditPublisher is the narrow surface we need from events.Bus. Allows
// tests to capture audit emissions without standing up a real bus.
type auditPublisher interface {
	Publish(events.Event)
}

// emitSessionStart records the opening of a console session.
//
// Every session — including echo-mode sessions in Phase 1a — emits one
// of these. The event is the load-bearing record for "who got a shell on
// what box and when." Fields are intentionally flat (no nested map) so
// log-forwarders can index them without a JSON unmarshal step.
func emitSessionStart(bus auditPublisher, sessionID, remoteAddr, mode string, effectiveUID int, startedAt time.Time) {
	if bus == nil {
		return
	}
	bus.Publish(events.Event{
		Type:      events.EventConsoleSessionStart,
		Timestamp: startedAt,
		Data: map[string]any{
			"session_id":    sessionID,
			"remote_addr":   remoteAddr,
			"mode":          mode,
			"effective_uid": effectiveUID,
		},
	})
}

// emitSessionEnd records the close of a console session. reason is a
// short tag ("client_close", "idle_timeout", "exit", "error") rather than
// a free-form string so dashboards can group sessions by close cause.
// bytesIn / bytesOut count payload bytes (decoded data frames, not the
// JSON envelopes) so operators can spot "user pasted the entire log"
// outliers without recording session content.
//
// exitCode is the child process's exit status when a real PTY was
// attached; -1 for echo mode (no child), -1 when the child was killed
// before producing a code. The dashboard distinguishes "operator
// closed the tab" from "shell exited" by combining reason + exit code.
func emitSessionEnd(bus auditPublisher, sessionID, reason string, bytesIn, bytesOut int64, duration time.Duration, exitCode int) {
	if bus == nil {
		return
	}
	bus.Publish(events.Event{
		Type:      events.EventConsoleSessionEnd,
		Timestamp: time.Now(),
		Data: map[string]any{
			"session_id":  sessionID,
			"reason":      reason,
			"bytes_in":    bytesIn,
			"bytes_out":   bytesOut,
			"duration_ms": duration.Milliseconds(),
			"exit_code":   exitCode,
		},
	})
}
