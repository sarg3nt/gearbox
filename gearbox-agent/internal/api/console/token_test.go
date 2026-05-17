package console

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTokenManager_CreateProducesUniqueHexOfExpectedLength(t *testing.T) {
	// Tokens are the only thing standing between an API-key-bearing
	// caller and a shell session — uniqueness is non-negotiable. The
	// hex-length check pins the wire format (64 chars) so a future
	// "let's shorten this" change has to update the test deliberately.
	m := NewTokenManager()
	defer m.Close()

	const N = 100
	seen := make(map[string]struct{}, N)
	for i := 0; i < N; i++ {
		v, err := m.Create()
		if err != nil {
			t.Fatalf("Create() err = %v", err)
		}
		if got, want := len(v), tokenLength*2; got != want {
			t.Errorf("token hex length = %d, want %d", got, want)
		}
		if _, err := hex.DecodeString(v); err != nil {
			t.Errorf("token not valid hex: %v", err)
		}
		if _, dup := seen[v]; dup {
			t.Errorf("duplicate token after %d iterations: %s", i, v)
		}
		seen[v] = struct{}{}
	}
}

func TestTokenManager_ValidateSucceedsOnceThenFails(t *testing.T) {
	// Single-use is the entire reason these tokens exist — a leaked
	// token must be useless after the first redemption.
	m := NewTokenManager()
	defer m.Close()

	v, err := m.Create()
	if err != nil {
		t.Fatalf("Create() err = %v", err)
	}
	if !m.Validate(v) {
		t.Fatal("first Validate() = false, want true")
	}
	if m.Validate(v) {
		t.Fatal("second Validate() = true, want false (replay)")
	}
}

func TestTokenManager_ValidateFailsForUnknownToken(t *testing.T) {
	m := NewTokenManager()
	defer m.Close()
	if m.Validate("not-a-real-token") {
		t.Fatal("Validate(unknown) = true, want false")
	}
}

func TestTokenManager_ValidateFailsForExpired(t *testing.T) {
	// Forge an entry directly so we don't have to wait the real 60s
	// TTL in tests. The cleanupLoop's eviction is incidental — what
	// matters is that Validate refuses a token whose expiresAt is in
	// the past, even though the entry physically exists in the map.
	m := NewTokenManager()
	defer m.Close()
	m.mu.Lock()
	v := "deadbeef"
	m.tokens[v] = token{value: v, expiresAt: time.Now().Add(-time.Second)}
	m.mu.Unlock()
	if m.Validate(v) {
		t.Fatal("Validate(expired) = true, want false")
	}
}

func TestTokenManager_ConcurrentCreateAndValidate(t *testing.T) {
	// Race detector test — `go test -race` will catch the bug if the
	// map mutex is dropped. The mutex protects the entire map; a fast
	// concurrent caller minting and validating shouldn't see races.
	m := NewTokenManager()
	defer m.Close()

	var wg sync.WaitGroup
	wg.Add(20)
	for i := 0; i < 20; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				v, err := m.Create()
				if err != nil {
					t.Errorf("Create() err = %v", err)
					return
				}
				if !m.Validate(v) {
					t.Errorf("Validate(just-created) = false")
				}
			}
		}()
	}
	wg.Wait()
}

func TestHandleTokenExchange_ReturnsJSONWithExpiry(t *testing.T) {
	m := NewTokenManager()
	defer m.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/token", nil)
	rr := httptest.NewRecorder()

	m.HandleTokenExchange(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var resp TokenResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Error("token field is empty")
	}
	if resp.ExpiresIn != int(tokenExpiry.Seconds()) {
		t.Errorf("expires_in = %d, want %d", resp.ExpiresIn, int(tokenExpiry.Seconds()))
	}
	// The returned token should be redeemable exactly once.
	if !m.Validate(resp.Token) {
		t.Error("returned token does not validate")
	}
	if m.Validate(resp.Token) {
		t.Error("returned token validated twice")
	}
}

func TestHandleTokenExchange_RejectsNonPost(t *testing.T) {
	m := NewTokenManager()
	defer m.Close()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/console/token", nil)
		rr := httptest.NewRecorder()
		m.HandleTokenExchange(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Method not allowed") {
			t.Errorf("%s: body = %q, want 'Method not allowed'", method, rr.Body.String())
		}
	}
}
