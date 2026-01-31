# WebSocket Real-Time Events

The gearbox-agent provides a WebSocket endpoint for real-time event streaming. This allows clients to receive immediate notifications when sync operations occur or configuration changes.

## Authentication

WebSocket connections use a **two-step token exchange** for enhanced security:

1. **Exchange API key for a short-lived token** via `POST /api/v1/events/token`
2. **Connect to WebSocket** with the token as a query parameter

This approach prevents long-lived API keys from being exposed in WebSocket URLs or logs.

### Token Properties

- **Lifetime**: 60 seconds
- **Single-use**: Token is consumed on first use
- **Secure**: API key never appears in WebSocket connection URL

## Connecting

### Step 1: Get a WebSocket Token

```bash
curl -sk -X POST -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/events/token
```

Response:

```json
{
  "token": "a1b2c3d4e5f6...",
  "expires_in": 60
}
```

### Step 2: Connect with Token

```text
wss://your-server:8405/api/v1/events?token=TOKEN_FROM_STEP_1
```

### JavaScript Example

```javascript
const apiKey = 'your-api-key-here';
const baseUrl = 'https://light-hugger.sarg3.net:8405';

async function connectWebSocket() {
  // Step 1: Exchange API key for token
  const tokenResponse = await fetch(`${baseUrl}/api/v1/events/token`, {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${apiKey}` }
  });
  const { token } = await tokenResponse.json();

  // Step 2: Connect with token
  const ws = new WebSocket(`wss://light-hugger.sarg3.net:8405/api/v1/events?token=${token}`);

  ws.onopen = () => {
    console.log('Connected to gearbox-agent events');
  };

  ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log(`Event: ${data.type}`, data.timestamp, data.data);
  };

  ws.onerror = (error) => {
    console.error('WebSocket error:', error);
  };

  ws.onclose = () => {
    console.log('Disconnected from gearbox-agent events');
  };

  return ws;
}
```

### Go Example

```go
import (
    "encoding/json"
    "net/http"

    "github.com/gorilla/websocket"
)

type TokenResponse struct {
    Token     string `json:"token"`
    ExpiresIn int    `json:"expires_in"`
}

func connectWebSocket(baseURL, apiKey string) (*websocket.Conn, error) {
    // Step 1: Exchange API key for token
    req, _ := http.NewRequest("POST", baseURL+"/api/v1/events/token", nil)
    req.Header.Set("Authorization", "Bearer "+apiKey)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var tokenResp TokenResponse
    json.NewDecoder(resp.Body).Decode(&tokenResp)

    // Step 2: Connect with token
    dialer := websocket.Dialer{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // For self-signed certs
    }

    wsURL := strings.Replace(baseURL, "https://", "wss://", 1) + "/api/v1/events?token=" + tokenResp.Token
    conn, _, err := dialer.Dial(wsURL, nil)
    if err != nil {
        return nil, err
    }

    return conn, nil
}

// Usage
conn, err := connectWebSocket("https://light-hugger.sarg3.net:8405", "YOUR_API_KEY")
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

for {
    _, message, err := conn.ReadMessage()
    if err != nil {
        log.Println("Read error:", err)
        break
    }
    log.Printf("Received: %s", message)
}
```

## Event Types

All events are JSON objects with the following structure:

```json
{
  "type": "event.type",
  "timestamp": "2026-01-17T12:34:56.789Z",
  "data": { ... }
}
```

### sync.started

Emitted when a sync cycle begins.

```json
{
  "type": "sync.started",
  "timestamp": "2026-01-17T12:34:56.789Z",
  "data": {
    "force_update": false
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `force_update` | boolean | Whether this was a forced update (first run or missing markers) |

### sync.completed

Emitted when a sync cycle completes successfully.

```json
{
  "type": "sync.completed",
  "timestamp": "2026-01-17T12:34:57.123Z",
  "data": {
    "commit_sha": "abc1234",
    "backend_count": 15,
    "config_changed": true
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `commit_sha` | string | Git commit SHA that was synced |
| `backend_count` | integer | Number of HAProxy backends configured |
| `config_changed` | boolean | Whether HAProxy config was updated |

### sync.failed

Emitted when a sync cycle fails.

```json
{
  "type": "sync.failed",
  "timestamp": "2026-01-17T12:34:57.456Z",
  "data": {
    "error": "failed to fetch latest commit: network timeout"
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `error` | string | Error message describing the failure |

### config.changed

Emitted when HAProxy configuration is updated (always follows `sync.completed` with `config_changed: true`).

```json
{
  "type": "config.changed",
  "timestamp": "2026-01-17T12:34:57.789Z",
  "data": {
    "commit_sha": "abc1234",
    "backend_count": 15
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `commit_sha` | string | Git commit SHA that triggered the change |
| `backend_count` | integer | Number of HAProxy backends after update |

## API Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/events/token` | POST | API key (header) | Exchange API key for WebSocket token |
| `/api/v1/events` | GET (WebSocket) | Token (query param) | Real-time event stream |
| `/api/v1/events/info` | GET | API key (header) | WebSocket endpoint info |

### Token Exchange Response

```json
{
  "token": "a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456",
  "expires_in": 60
}
```

### Events Info Response

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://light-hugger.sarg3.net:8405/api/v1/events/info
```

```json
{
  "enabled": true,
  "endpoint": "/api/v1/events",
  "subscribers": 2,
  "event_types": [
    "sync.started",
    "sync.completed",
    "sync.failed",
    "config.changed",
    "webhook.received"
  ]
}
```

## Connection Management

- **Ping/Pong**: The server sends ping frames every 54 seconds to keep the connection alive
- **Timeout**: Connections are closed if no pong is received within 60 seconds
- **Reconnection**: Clients should implement automatic reconnection with exponential backoff

### Recommended Reconnection Pattern

```javascript
class EventClient {
  constructor(url) {
    this.url = url;
    this.reconnectDelay = 1000;
    this.maxReconnectDelay = 30000;
    this.connect();
  }

  connect() {
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      console.log('Connected');
      this.reconnectDelay = 1000; // Reset delay on successful connection
    };

    this.ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      this.handleEvent(data);
    };

    this.ws.onclose = () => {
      console.log(`Reconnecting in ${this.reconnectDelay}ms...`);
      setTimeout(() => this.connect(), this.reconnectDelay);
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
    };
  }

  handleEvent(event) {
    console.log(event.type, event.data);
  }
}
```

## Security Considerations

- **Token-based auth**: API keys are never exposed in WebSocket URLs
- **Short-lived tokens**: Tokens expire in 60 seconds and are single-use
- **Token exchange requires HTTPS**: API key is sent in Authorization header
- WebSocket connections require a valid token from the token exchange endpoint
- Failed authentication attempts are logged
- Tokens cannot be reused - each WebSocket connection requires a new token
