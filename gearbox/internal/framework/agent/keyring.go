package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// KeyRingMetadataEntry is the dashboard-side mirror of the agent's
// crypto.KeyRingMetadata. Kept in this package so the dashboard
// doesn't need to import the agent's internal crypto package.
type KeyRingMetadataEntry struct {
	KID         string    `json:"kid"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	Fingerprint string    `json:"fingerprint"`
}

// KeyRingResponse is the body shape of GET /api/v1/system/keyring and
// the response to the mutation endpoints. Secrets are never present.
type KeyRingResponse struct {
	Version int                    `json:"version"`
	Entries []KeyRingMetadataEntry `json:"entries"`
}

// KeyRingGet returns the agent's keyring metadata. Used by the
// dashboard rotator to verify the post-rotation state matches its
// expectation, and by drift detection (Phase 5).
func (c *Client) KeyRingGet() (*KeyRingResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/system/keyring", url.Values{})
	if err != nil {
		return nil, err
	}
	var resp KeyRingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse keyring response: %w", err)
	}
	return &resp, nil
}

// KeyRingInstall adds a new entry to the agent's keyring. Idempotent
// when (kid, secret) match an existing entry; returns ConflictError
// (HTTP 409) when kid clashes with a different secret.
//
// secret must be exactly 32 random bytes — caller's responsibility.
func (c *Client) KeyRingInstall(kid string, secret []byte, role string) (*KeyRingResponse, error) {
	if role == "" {
		role = "secondary"
	}
	reqBody := map[string]string{
		"kid":        kid,
		"secret_b64": base64.RawURLEncoding.EncodeToString(secret),
		"role":       role,
	}
	body, err := c.doRequestWithBody("POST", "/api/v1/system/keyring/install", reqBody)
	if err != nil {
		return nil, err
	}
	var resp KeyRingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse install response: %w", err)
	}
	return &resp, nil
}

// KeyRingUse promotes the named keyring entry to primary. The previous
// primary becomes secondary (still accepted for inbound auth).
func (c *Client) KeyRingUse(kid string) (*KeyRingResponse, error) {
	body, err := c.doRequestWithBody("POST", "/api/v1/system/keyring/use", map[string]string{"kid": kid})
	if err != nil {
		return nil, err
	}
	var resp KeyRingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse use response: %w", err)
	}
	return &resp, nil
}

// KeyRingDelete removes the named keyring entry. Returns an APIError
// with status 409 when called on the only remaining entry — the agent
// refuses to brick itself.
func (c *Client) KeyRingDelete(kid string) (*KeyRingResponse, error) {
	body, err := c.doRequest("DELETE", "/api/v1/system/keyring/"+url.PathEscape(kid), url.Values{})
	if err != nil {
		return nil, err
	}
	var resp KeyRingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse delete response: %w", err)
	}
	return &resp, nil
}
