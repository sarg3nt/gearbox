// Package updates provides functional tests for the OS Updates gear HTTP API.
//
// These tests exercise every API endpoint the gear registers, using
// httptest to call the handlers directly. They verify:
//   - Correct HTTP status codes
//   - Proper JSON response format (not plain text)
//   - No "Failed to" prefix leakage from the agent (the JS client adds these)
//   - Consistent error response structure
//
// This is important because the dashboard JS adds its own "Failed to X: "
// prefix to error messages. If the agent ALSO includes "Failed to X: ",
// the user sees: "Failed to X: Failed to X: actual error" — a double prefix.
package updates

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

// setupTestGear creates a Gear with a real collector (but no apt access on
// macOS / CI) so we can exercise the HTTP handlers. Errors are expected for
// most operations since apt is unavailable, but the handler response format
// and error messages must still be correct.
func setupTestGear(t *testing.T) *Gear {
	t.Helper()

	g := &Gear{}
	logger := slog.Default()
	bus := events.NewBus()

	deps := gear.Dependencies{
		Logger:   logger,
		EventBus: bus,
	}

	if err := g.Initialize(context.Background(), deps); err != nil {
		t.Fatalf("failed to initialize gear: %v", err)
	}

	return g
}

// setupTestRouter creates a chi router with all gear routes registered.
func setupTestRouter(t *testing.T) (*Gear, *chi.Mux) {
	t.Helper()
	g := setupTestGear(t)
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	return g, r
}

// assertJSONResponse verifies the response is valid JSON with correct
// Content-Type header.
func assertJSONResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var raw json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Errorf("response body is not valid JSON: %s\nbody: %s", err, w.Body.String())
	}
}

// assertNoFailedToPrefix checks that the response body doesn't contain
// "Failed to" prefixes. The JS client adds its own prefix; if the agent
// also adds one, users see a double prefix.
func assertNoFailedToPrefix(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	body := w.Body.String()
	if strings.Contains(body, "Failed to") {
		t.Errorf("response contains 'Failed to' prefix — agent must not add these"+
			" (JS adds its own)\nbody: %s", body)
	}
}

// assertErrorResponse checks that an error response:
//  1. Is valid JSON with {"error":"<message>"} structure
//  2. Does NOT contain "Failed to" prefix
//  3. Contains meaningful error text (not empty)
func assertErrorResponse(t *testing.T, w *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()

	if w.Code != wantStatus {
		t.Errorf("expected status %d, got %d", wantStatus, w.Code)
	}

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Errorf("failed to parse error response: %v\nbody: %s", err, w.Body.String())
	}
	if errResp.Error == "" {
		t.Error("error response has empty error message")
	}
}

// --- GET endpoints ---

func TestHandleStatus(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/updates", nil)
	router.ServeHTTP(w, req)

	// On macOS/CI, apt isn't available so this will error.
	// Either way, the response must be valid JSON without "Failed to" prefix.
	if w.Code == http.StatusOK {
		assertJSONResponse(t, w)
	} else {
		assertErrorResponse(t, w, w.Code)
	}
}

func TestHandleListUpgradable(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/updates/list", nil)
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		assertJSONResponse(t, w)
	} else {
		assertErrorResponse(t, w, w.Code)
	}
}

func TestHandleHistory(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("default limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/history", nil)
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
			var resp UpdateHistoryResponse
			json.Unmarshal(w.Body.Bytes(), &resp)
			// Count should be non-negative (history may be empty on macOS)
			if resp.Count < 0 {
				t.Error("expected non-negative count")
			}
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})

	t.Run("custom limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/history?limit=5", nil)
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})

	t.Run("invalid limit ignored", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/history?limit=abc", nil)
		router.ServeHTTP(w, req)

		// Should not fail - falls back to default limit
		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleRebootRequired(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/updates/reboot-required", nil)
	router.ServeHTTP(w, req)

	// This always succeeds (returns empty packages on non-Linux)
	assertJSONResponse(t, w)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleListSnapshots(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/updates/snapshots", nil)
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		assertJSONResponse(t, w)
	} else {
		assertErrorResponse(t, w, w.Code)
	}
}

func TestHandleUnattendedConfig(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/updates/unattended", nil)
	router.ServeHTTP(w, req)

	// This always succeeds (returns defaults when config not found)
	assertJSONResponse(t, w)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlePipxStatus(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/pipx", nil)
	router.ServeHTTP(w, req)

	// This always succeeds (returns available=false when pipx not found)
	assertJSONResponse(t, w)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleListOperations(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/updates/operations", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp OperationsListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Operations == nil {
		t.Error("expected non-nil operations slice")
	}
}

func TestHandleListUpdateLogs(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("default", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/logs", nil)
		router.ServeHTTP(w, req)

		assertJSONResponse(t, w)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("with limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/logs?limit=10", nil)
		router.ServeHTTP(w, req)

		assertJSONResponse(t, w)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

// --- POST endpoints ---

func TestHandleTriggerCheck(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("sync mode", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/system/updates/check", nil)
		router.ServeHTTP(w, req)

		// On macOS/CI this will fail because apt is unavailable
		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})

	t.Run("streaming mode", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/system/updates/check?stream=true", nil)
		router.ServeHTTP(w, req)

		// Streaming should return 202 Accepted or an error
		if w.Code == http.StatusAccepted {
			assertJSONResponse(t, w)
			var resp StreamingInstallResponse
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.OperationID == "" {
				t.Error("expected operation_id in streaming response")
			}
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleInstallUpdates(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("sync mode", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"packages":[],"security_only":false}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		// This always returns JSON (success or failure is in the response body)
		assertJSONResponse(t, w)
		assertNoFailedToPrefix(t, w)
	})

	t.Run("streaming mode", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"packages":[],"security_only":false}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/install?stream=true", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusAccepted {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})
}

func TestHandleScheduleReboot(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/reboot", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("schedule reboot", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"when":"22:00"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/reboot", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		// On macOS/CI this will fail
		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleCancelReboot(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/system/updates/reboot", nil)
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		assertJSONResponse(t, w)
	} else {
		assertErrorResponse(t, w, w.Code)
	}
}

// --- Snapshot endpoints ---

func TestHandleCreateSnapshot(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("with reason", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"reason":"test"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/snapshots", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})

	t.Run("empty body defaults to manual", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/snapshots", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleRestoreSnapshot(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing snapshot_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/snapshots/restore", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/snapshots/restore", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("nonexistent snapshot", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"snapshot_id":"nonexistent"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/snapshots/restore", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		// Should fail but with proper JSON error, no "Failed to" prefix
		if w.Code != http.StatusOK {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleDeleteSnapshot(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/api/v1/system/updates/snapshots/", nil)
		router.ServeHTTP(w, req)

		// chi won't match the route without {id}, so 405 or 404
		if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
			t.Errorf("expected 404 or 405 for missing id, got %d", w.Code)
		}
	})

	t.Run("nonexistent snapshot", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/api/v1/system/updates/snapshots/nonexistent", nil)
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusNotFound)
	})
}

// --- Package management endpoints ---

func TestHandleSearchPackages(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing query", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/packages/search", nil)
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("with query", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/packages/search?query=nginx", nil)
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})

	t.Run("with query and limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/packages/search?query=nginx&limit=10", nil)
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleInstallPackage(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing name", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/packages/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/packages/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("install package", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"name":"curl"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/packages/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandleRemovePackage(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing name", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/packages/remove", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/packages/remove", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("remove package", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"name":"vim","purge":false}`)
		req := httptest.NewRequest("POST", "/api/v1/system/packages/remove", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

// --- Pipx endpoints ---

func TestHandlePipxInstall(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing name", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("install pipx package", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"name":"black"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/install", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandlePipxUninstall(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing name", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/uninstall", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("uninstall pipx package", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"name":"black"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/uninstall", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandlePipxUpgrade(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("missing name", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/upgrade", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("upgrade pipx package", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"name":"black"}`)
		req := httptest.NewRequest("POST", "/api/v1/system/pipx/upgrade", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

func TestHandlePipxUpgradeAll(t *testing.T) {
	_, router := setupTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/system/pipx/upgrade-all", nil)
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		assertJSONResponse(t, w)
	} else {
		assertErrorResponse(t, w, w.Code)
	}
}

// --- Unattended config POST ---

func TestHandleConfigureUnattended(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("invalid body", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`not json`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/unattended", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, http.StatusBadRequest)
	})

	t.Run("configure", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := strings.NewReader(`{"enabled":true,"auto_reboot":false}`)
		req := httptest.NewRequest("POST", "/api/v1/system/updates/unattended", body)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			assertJSONResponse(t, w)
		} else {
			assertErrorResponse(t, w, w.Code)
		}
	})
}

// --- Operations endpoints ---

func TestHandleGetOperation(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("nonexistent operation", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/operation/nonexistent", nil)
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, w.Code)
	})
}

func TestHandleCancelOperation(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("nonexistent operation", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/api/v1/system/updates/operation/nonexistent", nil)
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, w.Code)
	})
}

// --- Update logs ---

func TestHandleGetUpdateLog(t *testing.T) {
	_, router := setupTestRouter(t)

	t.Run("nonexistent log", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/system/updates/logs/nonexistent", nil)
		router.ServeHTTP(w, req)

		assertErrorResponse(t, w, w.Code)
	})
}

// --- Error message consistency ---

// TestAllEndpointsNoFailedToPrefix is a comprehensive test that exercises
// every error path and verifies no response contains "Failed to" prefixes.
// This catches the exact bug where:
//   - Agent sends: "Failed to trigger update check: exit status 100"
//   - Dashboard passes through: "Failed to trigger update check: exit status 100"
//   - JS wraps as: "Failed to check for updates: Failed to trigger update check: exit status 100"
func TestAllEndpointsNoFailedToPrefix(t *testing.T) {
	_, router := setupTestRouter(t)

	// Every endpoint that might fail on non-Linux systems
	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"GET", "/api/v1/system/updates", ""},
		{"GET", "/api/v1/system/updates/list", ""},
		{"POST", "/api/v1/system/updates/check", ""},
		{"POST", "/api/v1/system/updates/check?stream=true", ""},
		{"POST", "/api/v1/system/updates/install", `{"packages":[]}`},
		{"POST", "/api/v1/system/updates/install?stream=true", `{"packages":[]}`},
		{"GET", "/api/v1/system/updates/history", ""},
		{"POST", "/api/v1/system/updates/reboot", `{"when":"22:00"}`},
		{"DELETE", "/api/v1/system/updates/reboot", ""},
		{"GET", "/api/v1/system/updates/snapshots", ""},
		{"POST", "/api/v1/system/updates/snapshots", `{"reason":"test"}`},
		{"POST", "/api/v1/system/updates/snapshots/restore", `{"snapshot_id":"test"}`},
		{"DELETE", "/api/v1/system/updates/snapshots/test", ""},
		{"GET", "/api/v1/system/packages/search?query=test", ""},
		{"POST", "/api/v1/system/packages/install", `{"name":"test-pkg"}`},
		{"POST", "/api/v1/system/packages/remove", `{"name":"test-pkg"}`},
		{"POST", "/api/v1/system/pipx/install", `{"name":"test"}`},
		{"POST", "/api/v1/system/pipx/uninstall", `{"name":"test"}`},
		{"POST", "/api/v1/system/pipx/upgrade", `{"name":"test"}`},
		{"POST", "/api/v1/system/pipx/upgrade-all", ""},
		{"POST", "/api/v1/system/updates/unattended", `{"enabled":true}`},
	}

	for _, ep := range endpoints {
		name := ep.method + " " + ep.path
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()

			var req *http.Request
			if ep.body != "" {
				req = httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(ep.method, ep.path, nil)
			}

			router.ServeHTTP(w, req)

			// Check that error responses don't contain "Failed to" prefix
			assertNoFailedToPrefix(t, w)

			// If it's an error response, verify it's proper JSON
			if w.Code >= 400 {
				assertJSONResponse(t, w)
			}
		})
	}
}
