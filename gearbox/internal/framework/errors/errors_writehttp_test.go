package errors

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteHTTPError_JSONEnvelope guards the wire format consumers of
// WriteHTTPError rely on — Logs page, Services page, and any other JS that
// calls `response.json()` on both success and error responses. Historically
// WriteHTTPError emitted `text/plain` ("Failed to ...") which caused
// `JSON.parse` to throw `Unexpected token 'F'` in the browser; the fix in
// issue #112 standardizes on a JSON envelope.
func TestWriteHTTPError_JSONEnvelope(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := httptest.NewRecorder()

	WriteHTTPError(w, logger, Internal("fetch logs", io.EOF))

	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := w.Code; got != http.StatusInternalServerError {
		t.Fatalf("Status = %d, want 500", got)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v\nbody=%q", err, w.Body.String())
	}
	if body["success"] != false {
		t.Errorf("body.success = %v, want false", body["success"])
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "fetch logs") {
		t.Errorf("body.message = %q, want it to contain 'fetch logs'", msg)
	}
}

// TestWriteHTTPError_UnknownErrorWraps verifies that a plain error (not an
// AppError) still serializes as a JSON envelope with the generic
// "process request" wrapping that Internal() produces.
func TestWriteHTTPError_UnknownErrorWraps(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := httptest.NewRecorder()

	WriteHTTPError(w, logger, io.ErrUnexpectedEOF)

	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v\nbody=%q", err, w.Body.String())
	}
	if body["success"] != false {
		t.Errorf("body.success = %v, want false", body["success"])
	}
}
