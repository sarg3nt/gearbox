# HAProxy API

The gearbox-agent provides endpoints for accessing HAProxy statistics and runtime information directly through the agent's API. This eliminates the need for the monitoring application to have direct access to the HAProxy stats socket or HTTP stats endpoint.

## Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/haproxy/stats` | GET | API key | Parsed HAProxy stats (JSON, or CSV with `?format=csv`) |
| `/api/v1/haproxy/info` | GET | API key | HAProxy runtime information |
| `/api/v1/haproxy/tables` | GET | API key | Stick table information |
| `/api/v1/haproxy/validate` | GET | API key | Validate HAProxy configuration |

## Stats (Parsed JSON)

Returns parsed HAProxy statistics in JSON format. This is the recommended endpoint for monitoring applications.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/haproxy/stats
```

### Response

```json
{
  "frontends": [
    {
      "name": "https_front",
      "status": "OPEN",
      "scur": 5,
      "smax": 100,
      "slim": 20000,
      "stot": 123456,
      "bin": 987654321,
      "bout": 123456789,
      "req_rate": 50,
      "req_tot": 123456,
      "hrsp_2xx": 12345,
      "hrsp_3xx": 678,
      "hrsp_4xx": 90,
      "hrsp_5xx": 12,
      "dreq": 10,
      "dresp": 5,
      "ereq": 2,
      "econ": 0
    }
  ],
  "backends": [
    {
      "name": "myapp_backend",
      "status": "UP",
      "scur": 2,
      "smax": 50,
      "stot": 5000,
      "bin": 123456,
      "bout": 654321,
      "req_rate": 10,
      "qtime": 5,
      "ctime": 10,
      "rtime": 50,
      "ttime": 100,
      "hrsp_2xx": 4500,
      "hrsp_3xx": 400,
      "hrsp_4xx": 80,
      "hrsp_5xx": 20,
      "servers": [
        {
          "name": "myapp_srv",
          "status": "UP",
          "weight": 100,
          "scur": 2,
          "smax": 50,
          "stot": 5000,
          "check_status": "L7OK",
          "check_duration": 5,
          "lastchg": 86400,
          "downtime": 0,
          "health_percent": 100,
          "req_rate": 10,
          "rtime": 50
        }
      ],
      "act": 1,
      "bck": 0,
      "down_servers": 0,
      "total_servers": 1,
      "health_percent": 100
    }
  ],
  "parsed_at": "2024-01-17T10:00:00Z"
}
```

### Frontend Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Frontend name |
| `status` | string | Status (OPEN, STOP, etc.) |
| `scur` | int64 | Current sessions |
| `smax` | int64 | Maximum sessions seen |
| `slim` | int64 | Session limit |
| `stot` | int64 | Total sessions |
| `bin` | int64 | Bytes in |
| `bout` | int64 | Bytes out |
| `req_rate` | int64 | Request rate (req/s) |
| `req_tot` | int64 | Total requests |
| `hrsp_2xx` | int64 | HTTP 2xx responses |
| `hrsp_3xx` | int64 | HTTP 3xx responses |
| `hrsp_4xx` | int64 | HTTP 4xx responses |
| `hrsp_5xx` | int64 | HTTP 5xx responses |
| `dreq` | int64 | Denied requests |
| `dresp` | int64 | Denied responses |
| `ereq` | int64 | Request errors |
| `econ` | int64 | Connection errors |

### Backend Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Backend name |
| `status` | string | Status (UP, DOWN, etc.) |
| `scur` | int64 | Current sessions |
| `smax` | int64 | Maximum sessions |
| `stot` | int64 | Total sessions |
| `bin` | int64 | Bytes in |
| `bout` | int64 | Bytes out |
| `req_rate` | int64 | Request rate |
| `qtime` | int64 | Queue time (ms) |
| `ctime` | int64 | Connect time (ms) |
| `rtime` | int64 | Response time (ms) |
| `ttime` | int64 | Total time (ms) |
| `servers` | array | Individual server stats |
| `act` | int64 | Active servers |
| `bck` | int64 | Backup servers |
| `down_servers` | int64 | Down servers (calculated) |
| `total_servers` | int64 | Total servers (calculated) |
| `health_percent` | float64 | Health percentage (calculated) |

### Server Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Server name |
| `status` | string | Status (UP, DOWN, MAINT, DRAIN) |
| `weight` | int64 | Server weight |
| `scur` | int64 | Current sessions |
| `smax` | int64 | Maximum sessions |
| `stot` | int64 | Total sessions |
| `check_status` | string | Health check status |
| `check_duration` | int64 | Last check duration (ms) |
| `lastchg` | int64 | Time since last status change (s) |
| `downtime` | int64 | Total downtime (s) |
| `health_percent` | float64 | Health percentage |
| `qcur` | int64 | Current queue size |
| `req_rate` | int64 | Request rate |
| `rtime` | int64 | Response time (ms) |

### CSV Format (Legacy)

For compatibility with existing tooling, you can request raw CSV format:

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  "https://your-server:8405/api/v1/haproxy/stats?format=csv"
```

This returns the same format as HAProxy's native `/stats;csv` endpoint.

## Runtime Information

Returns detailed HAProxy process information.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/haproxy/info
```

### Response

```json
{
  "version": "2.8.0",
  "release_date": "2023/05/31",
  "uptime": "5d 2h 30m 15s",
  "uptime_sec": 442215,
  "process_num": 1,
  "pid": 12345,
  "nbthread": 8,
  "maxconn": 20000,
  "curr_conn": 150,
  "conn_rate": 25,
  "max_conn_rate": 500,
  "cum_sess": 1234567,
  "max_sess_rate": 1000,
  "tasks": 42,
  "run_queue": 2,
  "mem_max_mb": 512,
  "pool_alloc_mb": 128,
  "pool_used_mb": 64,
  "idle_pct": 85,
  "node": "haproxy-1",
  "description": "Production HAProxy",
  "extra": {}
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | HAProxy version |
| `release_date` | string | HAProxy release date |
| `uptime` | string | Human-readable uptime |
| `uptime_sec` | integer | Uptime in seconds |
| `nbthread` | integer | Number of threads |
| `maxconn` | integer | Maximum connections |
| `curr_conn` | integer | Current connections |
| `conn_rate` | integer | Current connection rate |
| `cum_sess` | integer | Cumulative sessions |
| `idle_pct` | integer | Idle percentage (0-100) |
| `mem_max_mb` | integer | Maximum memory in MB |
| `pool_alloc_mb` | integer | Allocated pool memory in MB |
| `pool_used_mb` | integer | Used pool memory in MB |

## Stick Tables

Returns information about HAProxy stick tables (used for rate limiting, session persistence, etc.).

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/haproxy/tables
```

### Response

```json
{
  "tables": [
    {
      "name": "https_front",
      "type": "ip",
      "size": 102400,
      "used": 1234,
      "data_types": ["http_req_rate(10s)"]
    }
  ]
}
```

**Note:** Stick tables require Unix socket access. The HTTP stats endpoint does not expose this information.

## Configuration Validation

Validates the current HAProxy configuration file.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/haproxy/validate
```

### Response (Valid)

```json
{
  "valid": true,
  "output": "Configuration file is valid\n",
  "message": "Configuration is valid"
}
```

### Response (Invalid)

```json
{
  "valid": false,
  "output": "[ALERT] parsing error...",
  "message": "Configuration is invalid"
}
```

## Configuration

Configure HAProxy access via environment variables:

```bash
# Unix socket path (preferred - provides more data)
HAPROXY_STATS_SOCKET=/run/haproxy/admin.sock

# HTTP stats endpoint (fallback)
HAPROXY_STATS_URL=http://localhost:8404/stats
HAPROXY_STATS_USER=admin
HAPROXY_STATS_PASSWORD=secret

# HAProxy config file for validation
HAPROXY_CONFIG_FILE=/etc/haproxy/haproxy.cfg
```

## Data Source Priority

The agent attempts to fetch stats in this order:

1. **Unix socket** (preferred) - `/run/haproxy/admin.sock`
2. **HTTP endpoint** (fallback) - Configured via `HAPROXY_STATS_URL`

## JavaScript Example

```javascript
async function getHAProxyStats(baseUrl, apiKey) {
  const response = await fetch(`${baseUrl}/api/v1/haproxy/stats`, {
    headers: {
      'Authorization': `Bearer ${apiKey}`
    }
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch stats: ${response.statusText}`);
  }

  const stats = await response.json();

  // Access parsed data directly
  console.log(`Frontends: ${stats.frontends.length}`);
  console.log(`Backends: ${stats.backends.length}`);

  for (const backend of stats.backends) {
    console.log(`${backend.name}: ${backend.health_percent}% healthy`);
  }

  return stats;
}
```

## Benefits Over Direct Access

Using the agent's HAProxy API instead of direct access provides:

1. **JSON by default** - No CSV parsing needed on the client
2. **Single authentication** - Use the same API key for all agent endpoints
3. **TLS encryption** - All data encrypted in transit
4. **Unix socket access** - Get additional data only available via socket
5. **Firewall simplicity** - Only expose agent port, not HAProxy stats port
6. **Calculated fields** - Health percentages and server counts pre-calculated
7. **Rate limit info** - Access to stick table data for rate limiting visibility
