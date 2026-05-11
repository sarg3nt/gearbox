package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/services/haproxy"
)

// stubMetadataProvider is the minimal MetadataProvider needed for handler
// construction. Not actually exercised by the /health test.
type stubMetadataProvider struct{}

func (stubMetadataProvider) GetLastSyncTime() time.Time      { return time.Time{} }
func (stubMetadataProvider) GetLastError() error             { return nil }
func (stubMetadataProvider) GetMetadata() *haproxy.Metadata  { return nil }

// 2026-05 audit P2-5: the unauthenticated /health endpoint must not leak
// version or uptime. A remote scanner probing for known-vulnerable agent
// versions should get "ok" and nothing else.
func TestHealth_NoVersionLeak(t *testing.T) {
	h := NewHandlers(stubMetadataProvider{}, "1.2.3-leakytest")

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	body := rr.Body.String()
	for _, leak := range []string{"1.2.3-leakytest", "uptime", "timestamp"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
			t.Errorf("/health response contains %q (potential info leak): %s", leak, body)
		}
	}

	// Sanity: the actual response should be a small {"status":"ok"} JSON.
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v (%q)", err, body)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want \"ok\"", got["status"])
	}
	if len(got) != 1 {
		t.Errorf("response has %d fields; want exactly 1 (status): %v", len(got), got)
	}
}
