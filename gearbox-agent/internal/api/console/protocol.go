package console

// Frame is the on-the-wire envelope for every console WebSocket message.
// JSON-tagged so the wire format stays self-describing even as we add new
// types in later phases (resize, signal, exit, error). Binary stdout/stdin
// rides as base64 in the Data field — text-framed WebSocket is easier to
// proxy through HTTP/2 intermediaries than the binary opcode.
//
// Phase 1a uses only "data" and "ping"/"pong"; the other variants are
// reserved so dashboard code written against this protocol now keeps
// working when 1b/1c land.
type Frame struct {
	// Type is the frame discriminator. See FrameType* constants.
	Type string `json:"t"`

	// Data carries base64-encoded stdin (client→agent) or stdout
	// (agent→client) bytes. Set only for FrameTypeData.
	Data string `json:"d,omitempty"`

	// Cols / Rows set the terminal size on FrameTypeResize. Wired in
	// Phase 1b once a real PTY is attached; Phase 1a accepts and
	// ignores the frame so dashboard test harnesses can prototype.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// Signal carries a named POSIX signal on FrameTypeSignal
	// (e.g. "INT", "TERM"). Reserved — Ctrl-C as a normal data byte
	// is the recommended path. Phase 1a accepts and ignores.
	Signal string `json:"s,omitempty"`

	// Code / Reason populate FrameTypeExit (process exit) and
	// FrameTypeErr (protocol- or session-level error).
	Code   int    `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`

	// Msg populates FrameTypeErr — a short human-readable string.
	Msg string `json:"msg,omitempty"`
}

// Frame type constants. The full set is defined now so the protocol is
// stable across phases — code that doesn't recognize a type should drop
// the frame rather than crash.
const (
	FrameTypeData   = "data"
	FrameTypeResize = "resize"
	FrameTypeSignal = "signal"
	FrameTypePing   = "ping"
	FrameTypePong   = "pong"
	FrameTypeExit   = "exit"
	FrameTypeErr    = "err"
)

// Error codes carried in Frame.Code when Type == FrameTypeErr.
// Stable strings so dashboards can branch on them; new codes append
// rather than rename.
const (
	ErrCodeAuthDenied          = "AUTH_DENIED"
	ErrCodeNoShell             = "NO_SHELL"
	ErrCodeContainerNoHostAcc  = "CONTAINER_NO_HOST_ACCESS"
	ErrCodeProtocolViolation   = "PROTOCOL_VIOLATION"
	ErrCodeIdleTimeout         = "IDLE_TIMEOUT"
	ErrCodeInternal            = "INTERNAL"
)
