package console

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Recorder writes a session's wire traffic to disk as newline-delimited
// JSON, one record per frame, both directions. Off by default —
// recording shells is sensitive and the operator opts in via
// HAPROXY_AGENT_CONSOLE_RECORD=true.
//
// File format (one JSON object per line):
//
//	{"t":"open",  "ts":"...", "session":"...", "uid":0, "mode":"host_pty"}
//	{"t":"in",    "ts":"...", "d":"<base64>"}   // bytes from client → PTY
//	{"t":"out",   "ts":"...", "d":"<base64>"}   // bytes from PTY → client
//	{"t":"resize","ts":"...", "cols":120, "rows":40}
//	{"t":"close", "ts":"...", "reason":"...", "exit_code":N}
//
// Files live at `${DataDir}/console-sessions/<box>-<utc>-<session>.ndjson`
// with mode 0600 (owner read/write only). The Recorder is
// goroutine-safe; the handler can drive in/out pumps concurrently.
type Recorder struct {
	mu     sync.Mutex
	f      *os.File
	closed bool
}

// recorderRoot returns the directory recordings live under, creating
// it if necessary. We mkdir 0700 so the directory tree is as
// restrictive as the files inside it.
func recorderRoot(dataDir string) (string, error) {
	if dataDir == "" {
		return "", fmt.Errorf("recorder: empty data dir")
	}
	dir := filepath.Join(dataDir, "console-sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("recorder: mkdir %s: %w", dir, err)
	}
	return dir, nil
}

// OpenRecorder creates a new file for the given session. boxName is
// folded into the filename so operators browsing /var/lib/gearbox-agent
// can see at a glance which box each recording is from; non-filename-
// safe characters are sanitized so a malicious box name (unlikely since
// the operator owns the names) can't escape the directory.
func OpenRecorder(dataDir, boxName, sessionID string) (*Recorder, error) {
	dir, err := recorderRoot(dataDir)
	if err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	name := fmt.Sprintf("%s-%s-%s.ndjson", sanitizeForFilename(boxName), stamp, sessionID)
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("recorder: open %s: %w", path, err)
	}
	return &Recorder{f: f}, nil
}

// LogOpen writes the session-open record. Best-effort — recording
// failures must not break the session, so all write errors are
// swallowed; the on-disk record is the source of truth for "was this
// session recorded" and operators who care should `ls` the directory.
func (r *Recorder) LogOpen(sessionID, mode string, uid int) {
	r.writeRecord(map[string]any{
		"t":       "open",
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"session": sessionID,
		"mode":    mode,
		"uid":     uid,
	})
}

// LogIn records bytes flowing client → PTY (stdin).
func (r *Recorder) LogIn(data []byte) {
	r.writeRecord(map[string]any{
		"t":  "in",
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
		"d":  base64.StdEncoding.EncodeToString(data),
	})
}

// LogOut records bytes flowing PTY → client (stdout/stderr).
func (r *Recorder) LogOut(data []byte) {
	r.writeRecord(map[string]any{
		"t":  "out",
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
		"d":  base64.StdEncoding.EncodeToString(data),
	})
}

// LogResize records a window-change event.
func (r *Recorder) LogResize(cols, rows int) {
	r.writeRecord(map[string]any{
		"t":    "resize",
		"ts":   time.Now().UTC().Format(time.RFC3339Nano),
		"cols": cols,
		"rows": rows,
	})
}

// Close writes the session-close record and releases the file.
func (r *Recorder) Close(reason string, exitCode int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.f != nil {
		_ = r.writeRecordLocked(map[string]any{
			"t":         "close",
			"ts":        time.Now().UTC().Format(time.RFC3339Nano),
			"reason":    reason,
			"exit_code": exitCode,
		})
		err := r.f.Close()
		r.f = nil
		return err
	}
	return nil
}

func (r *Recorder) writeRecord(rec map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.writeRecordLocked(rec)
}

func (r *Recorder) writeRecordLocked(rec map[string]any) error {
	if r.f == nil {
		return nil
	}
	return json.NewEncoder(r.f).Encode(rec)
}

// sanitizeForFilename replaces anything outside the conservative
// portable filename set with '_'. Box names are operator-controlled,
// but defending against a literal '/' in a name keeps the directory
// traversal threat at zero rather than "trust the operator."
func sanitizeForFilename(s string) string {
	if s == "" {
		return "box"
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-' || c == '_' || c == '.':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	out := strings.TrimLeft(string(b), ".") // no hidden files
	if out == "" {
		return "box"
	}
	return out
}
