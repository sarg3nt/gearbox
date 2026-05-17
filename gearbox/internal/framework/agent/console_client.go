package agent

import (
	"encoding/json"
	"fmt"
)

// ConsoleCapabilitiesResponse mirrors the agent's CapabilitiesResponse —
// kept in sync by hand because the two repos are sibling Go modules
// without a shared types package. If they drift, the dashboard logs
// a JSON-unmarshal warning and shows the console as unavailable for
// that box, which is the safer failure mode than guessing.
type ConsoleCapabilitiesResponse struct {
	Enabled     bool     `json:"enabled"`
	Mode        string   `json:"mode"`
	HostConsole bool     `json:"host_console"`
	DefaultUID  int      `json:"default_uid"`
	OS          string   `json:"os"`
	Shell       []string `json:"shell,omitempty"`
}

// ConsoleTokenResponse mirrors the agent's TokenResponse.
type ConsoleTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// GetConsoleCapabilities asks the agent what its console surface can
// do. Returns an APIError with status 404 when the operator hasn't
// enabled console on this agent — the caller should treat that as
// "console not available for this box" rather than a hard error.
func (c *Client) GetConsoleCapabilities() (*ConsoleCapabilitiesResponse, error) {
	body, err := c.doRequest("GET", "/api/v1/console/capabilities", nil)
	if err != nil {
		return nil, err
	}
	var resp ConsoleCapabilitiesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse console capabilities: %w", err)
	}
	return &resp, nil
}

// GetConsoleToken exchanges the API key for a 60-second single-use
// token suitable for opening /api/v1/console/ws. The token namespace
// is separate from the events token — they cannot be cross-replayed.
func (c *Client) GetConsoleToken() (*ConsoleTokenResponse, error) {
	body, err := c.doRequest("POST", "/api/v1/console/token", nil)
	if err != nil {
		return nil, err
	}
	var resp ConsoleTokenResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse console token: %w", err)
	}
	return &resp, nil
}

// BaseURL exposes the agent's base URL so the dashboard's WebSocket
// proxy can build the agent-side `wss://.../api/v1/console/ws` URL.
// Kept on the Client (not duplicated in the proxy) so URL parsing
// stays in one place; any change to how URLs are normalized lives
// here too.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// APIKey exposes the API key for code paths that need to authenticate
// directly against the agent (the WebSocket proxy uses it to fetch a
// fresh console token before establishing the upstream WS). Treat as
// a credential — never log, never include in error messages.
func (c *Client) APIKey() string {
	return c.apiKey
}
