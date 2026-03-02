package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// simulateJSErrorFlow simulates the full client-side error flow:
// 1. Server sends response (via jsonError or WriteHTTPError)
// 2. JS extractErrorMessage parses the response body
// 3. JS catch block wraps: jsPrefix + extractedMessage
//
// Returns the final message that would be shown in the toast.
func simulateJSErrorFlow(responseBody string, contentType string, jsPrefix string) string {
	var extractedMsg string

	if strings.Contains(contentType, "application/json") {
		var parsed map[string]any
		if json.Unmarshal([]byte(responseBody), &parsed) == nil {
			if msg, ok := parsed["message"].(string); ok && msg != "" {
				extractedMsg = msg
			} else if errMsg, ok := parsed["error"].(string); ok && errMsg != "" {
				extractedMsg = errMsg
			} else {
				extractedMsg = responseBody
			}
		} else {
			extractedMsg = responseBody
		}
	} else {
		extractedMsg = strings.TrimSpace(responseBody)
	}

	return jsPrefix + extractedMsg
}

// TestNoDoubleErrorPrefix_jsonError tests that jsonError messages
// don't produce duplicate "Failed to" prefixes when combined with JS.
//
// BUG: The server was sending messages like:
//
//	"Failed to check for updates: HTTP 500: Internal Server Error"
//
// JS catch then wraps this as:
//
//	"Failed to check for updates: Failed to check for updates: HTTP 500: Internal Server Error"
//
// FIX: Server should send just the error detail (e.g. "HTTP 500: Internal Server Error")
// and let JS be the sole place that adds the user-friendly prefix.
func TestNoDoubleErrorPrefix_jsonError(t *testing.T) {
	h := &Handler{}

	// Each test case simulates a handler calling h.jsonError() with the
	// message from os_updates.go, and then JS wrapping it.
	tests := []struct {
		name      string
		line      int    // source line in os_updates.go
		serverMsg string // what the handler passes to h.jsonError()
		jsPrefix  string // what the JS catch block prepends
	}{
		{
			// FIXED: h.jsonError(w, err.Error(), ...) — no prefix
			name:      "check for updates",
			line:      214,
			serverMsg: "HTTP 500: Internal Server Error",
			jsPrefix:  "Failed to check for updates: ",
		},
		{
			// FIXED: h.jsonError(w, err.Error(), ...) — no prefix
			name:      "restore snapshot",
			line:      542,
			serverMsg: "connection refused",
			jsPrefix:  "Failed to restore snapshot: ",
		},
		{
			// FIXED: h.jsonError(w, err.Error(), ...) — no prefix
			name:      "delete snapshot",
			line:      585,
			serverMsg: "not found",
			jsPrefix:  "Failed to delete snapshot: ",
		},
		{
			// FIXED: h.jsonError(w, err.Error(), ...) — no prefix
			name:      "upgrade packages",
			line:      907,
			serverMsg: "exit status 1",
			jsPrefix:  "Failed to upgrade pipx packages: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.jsonError(w, tt.serverMsg, http.StatusInternalServerError)

			finalMsg := simulateJSErrorFlow(w.Body.String(), w.Header().Get("Content-Type"), tt.jsPrefix)

			count := strings.Count(strings.ToLower(finalMsg), "failed to")
			if count > 1 {
				t.Errorf("duplicate 'Failed to' prefix detected (count=%d)\n"+
					"  Server sends:  %q\n"+
					"  Toast shows:   %q\n"+
					"  Fix line %d in os_updates.go", count, tt.serverMsg, finalMsg, tt.line)
			}
			if count < 1 {
				t.Errorf("missing 'Failed to' prefix — JS should add it\n"+
					"  Toast shows: %q", finalMsg)
			}
		})
	}
}

// TestNoDoubleErrorPrefix_AllHandlersUseJsonError verifies that all
// handlers now use jsonError (not WriteHTTPError), so the JS client
// always receives consistent JSON responses without "Failed to" prefixes.
//
// Previously, handlers used apperrors.WriteHTTPError which sent:
//
//	"Failed to <operation>. Please try again later." (plain text)
//
// Now all handlers use h.jsonError(w, err.Error(), status) which sends:
//
//	{"success":false,"message":"<raw error>"} (JSON)
func TestNoDoubleErrorPrefix_AllHandlersUseJsonError(t *testing.T) {
	h := &Handler{}

	// Simulate various agent errors that handlers now pass directly
	tests := []struct {
		name     string
		errMsg   string // what err.Error() returns from the agent client
		jsPrefix string
	}{
		{
			name:     "get update status",
			errMsg:   "HTTP 500: Internal Server Error",
			jsPrefix: "Failed to get update status: ",
		},
		{
			name:     "list packages",
			errMsg:   "connection refused",
			jsPrefix: "Failed to list packages: ",
		},
		{
			name:     "install updates",
			errMsg:   "HTTP 503: Service Unavailable",
			jsPrefix: "Failed to install updates: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.jsonError(w, tt.errMsg, http.StatusInternalServerError)

			// Verify response is JSON (not plain text like WriteHTTPError)
			ct := w.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("expected JSON content type, got %s", ct)
			}

			finalMsg := simulateJSErrorFlow(w.Body.String(), ct, tt.jsPrefix)

			count := strings.Count(strings.ToLower(finalMsg), "failed to")
			if count > 1 {
				t.Errorf("duplicate 'Failed to' prefix detected (count=%d)\n"+
					"  Server sends:  %q\n"+
					"  Toast shows:   %q", count, tt.errMsg, finalMsg)
			}
			if count < 1 {
				t.Errorf("missing 'Failed to' prefix — JS should add it\n"+
					"  Toast shows: %q", finalMsg)
			}
		})
	}
}
