package agent

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestDriftHandler_FiresOnKIDMismatch(t *testing.T) {
	// Mock agent that always echoes a hard-coded kid in the response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(HeaderKID, "actual-kid")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := NewClientWithKID(srv.URL, "ignored", "expected-kid", false)
	var fired atomic.Bool
	var seenExpected, seenActual string
	c.SetDriftHandler(func(expected, actual string) {
		fired.Store(true)
		seenExpected, seenActual = expected, actual
	})

	if _, err := c.doRequest("GET", "/anything", url.Values{}); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if !fired.Load() {
		t.Errorf("drift handler not invoked")
	}
	if seenExpected != "expected-kid" || seenActual != "actual-kid" {
		t.Errorf("drift handler args: expected=%q actual=%q", seenExpected, seenActual)
	}
}

func TestDriftHandler_DoesNotFireOnMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(HeaderKID, "matched")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := NewClientWithKID(srv.URL, "ignored", "matched", false)
	var fired atomic.Bool
	c.SetDriftHandler(func(_, _ string) { fired.Store(true) })

	if _, err := c.doRequest("GET", "/anything", url.Values{}); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if fired.Load() {
		t.Errorf("drift handler incorrectly invoked on matching kid")
	}
}

func TestDriftHandler_DoesNotFireWhenAgentDoesNotEchoKID(t *testing.T) {
	// Older agents won't set X-Gearbox-Kid. The drift handler should
	// stay silent rather than flagging every response as "drift".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := NewClientWithKID(srv.URL, "ignored", "expected-kid", false)
	var fired atomic.Bool
	c.SetDriftHandler(func(_, _ string) { fired.Store(true) })

	if _, err := c.doRequest("GET", "/anything", url.Values{}); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if fired.Load() {
		t.Errorf("drift handler incorrectly invoked when agent sent no kid header")
	}
}

func TestDriftHandler_DoesNotFireWhenClientHasNoKID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(HeaderKID, "something")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "ignored", false) // no kid
	var fired atomic.Bool
	c.SetDriftHandler(func(_, _ string) { fired.Store(true) })

	if _, err := c.doRequest("GET", "/anything", url.Values{}); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if fired.Load() {
		t.Errorf("drift handler invoked when client has no kid; should no-op")
	}
}
