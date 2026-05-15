package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotFoundHandlerStatusAndBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything-not-registered", nil)
	w := httptest.NewRecorder()

	NotFoundHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want HTML", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	body := w.Body.String()
	for _, want := range []string{"<!doctype html>", "404", "Page not found", "Back to dashboard"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q", want)
		}
	}
}

// TestNotFoundHandlerDoesNotEchoRequestData guards against accidentally
// turning the 404 page into a reflection vector. The handler should
// never embed any part of the request URL, headers, cookies, or body
// into the response — the point of the static page is that a typo'd
// URL produces a fully deterministic response.
func TestNotFoundHandlerDoesNotEchoRequestData(t *testing.T) {
	const probe = "GEARBOX-PROBE-MARKER-39df09a1"
	req := httptest.NewRequest(http.MethodGet, "/"+probe, nil)
	req.Header.Set("X-Probe", probe)
	req.Header.Set("Cookie", "session="+probe)
	req.Header.Set("Referer", "https://example.com/"+probe)
	w := httptest.NewRecorder()

	NotFoundHandler(w, req)

	if strings.Contains(w.Body.String(), probe) {
		t.Errorf("response body contains request-supplied marker %q — handler is reflecting request data", probe)
	}
}

func TestNotFoundHandlerStableAcrossMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/missing", nil)
			w := httptest.NewRecorder()
			NotFoundHandler(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404", method, w.Code)
			}
		})
	}
}
