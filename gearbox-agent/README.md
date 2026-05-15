# Gearbox Agent

A Go service that runs on monitored servers and workstations to provide:

1. **Gear-based data collection** - Gathers system metrics, service stats, logs, and security data
2. **Secure REST API** - Exposes collected data over HTTPS with API key authentication
3. **Real-time events** - WebSocket endpoint for live updates to Gearbox dashboard
4. **Capability-driven gear loading** - At startup the agent probes the host for each gear's prerequisites and loads only the gears that apply

**Universal Monitoring:** Works on ANY Linux system - HAProxy hosts, Docker hosts, TrueNAS Scale, workstations, bare servers. Gears that don't apply to the current host (e.g. `haproxy` on a TrueNAS box) are skipped at startup — no Initialize, no background goroutines, no failing collectors. See [docs/gear-probes.md](docs/gear-probes.md) for the probe lifecycle, status enum, and per-gear contracts.

**HAProxy-Specific Features (when HAProxy is detected):**

- Auto-configuration from Docker Compose labels in GitHub
- HAProxy stats and runtime info collection
- Replaces the original Python `haproxy-autoconfig.py` script

## Table of Contents

- [Quick Start](#quick-start)
- [Installation Methods](#installation-methods)
  - [Binary Installation](#binary-installation)
  - [Docker Installation](#docker-installation)
- [API Endpoints](#api-endpoints)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Deployment](#deployment)
- [Development](#development)
- [Documentation](#documentation)

## Quick Start

The fastest way to get started depends on your environment:

- **Binary Installation**: Direct systemd service on Linux servers
- **Docker Installation**: Containerized deployment with Docker or Docker Compose

See [Installation Methods](#installation-methods) below for detailed instructions.

## Installation Methods

### Binary Installation

For traditional Linux deployments with systemd:

```bash
# Build for Linux
make build-linux

# First-time deployment (creates systemd service)
make deploy-first

# Subsequent deployments
make deploy
```

See the [Deployment](#deployment) section for full binary installation details.

### Docker Installation

For containerized deployments:

#### Option 1: Docker Run

```bash
# Pull the image
docker pull ghcr.io/sarg3nt/gearbox/gearbox-agent:latest

# Run the container
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v /var/lib/gearbox-agent:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

#### Option 2: Docker Compose

```bash
# Create docker-compose.yml (see docker-compose.yml in this directory for full example)
docker-compose up -d
```

#### Option 3: Build from source

```bash
# Build Docker image locally
make docker-build

# Run locally built image
make docker-run
```

See [docs/docker.md](docs/docker.md) for complete Docker installation and configuration guide.

### Verify Installation

```bash
# Check service status
make status

# Get API key
make show-api-key

# Test the API
curl -sk -H "Authorization: Bearer YOUR_API_KEY" https://10.0.0.3:8405/health
```

## API Endpoints

### Health & Monitoring

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | No | Health check (for load balancers) |
| `/api/v1/metrics` | GET | Yes | System metrics (CPU, memory, disk, network) |
| `/api/v1/services` | GET | Yes | Systemd service statuses |

### HAProxy Data

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/haproxy/stats` | GET | Yes | Parsed HAProxy stats (JSON or CSV) |
| `/api/v1/haproxy/info` | GET | Yes | HAProxy runtime info |
| `/api/v1/haproxy/tables` | GET | Yes | Stick table info |
| `/api/v1/haproxy/validate` | GET | Yes | Validate HAProxy config |
| `/api/v1/metadata` | GET | Yes | Backend/frontend metadata from sync |

### Logs

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/logs` | GET | Yes | List available log sources |
| `/api/v1/logs/{name}` | GET | Yes | Fetch logs from a source |

### Security

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/security/summary` | GET | Yes | Quick security status overview |
| `/api/v1/security/fail2ban` | GET | Yes | Fail2ban jail stats and bans |
| `/api/v1/security/firewall` | GET | Yes | Firewall stats and blocks |

### Sync

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/sync/status` | GET | Yes | Git sync status |
| `/api/v1/webhook/github` | POST | Signature | GitHub webhook receiver |
| `/api/v1/webhook/info` | GET | Yes | Webhook configuration info |

### WebSocket

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/events/token` | POST | Yes | Exchange API key for WebSocket token |
| `/api/v1/events` | GET | Token | Real-time event stream |
| `/api/v1/events/info` | GET | Yes | WebSocket endpoint info |

### Documentation

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/swagger/` | GET | No | Swagger UI |

## Configuration

All configuration is via environment variables. Set them in `/etc/default/gearbox-agent`.

### Core Settings

```bash
# Server
HAPROXY_AGENT_LISTEN=0.0.0.0:8405
HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent
HAPROXY_AGENT_LOG_LEVEL=info  # debug, info, warn, error

# TLS (optional - uses self-signed if not set)
HAPROXY_AGENT_TLS_CERT=/etc/haproxy/certs/sarg3.net.fullchain.crt
HAPROXY_AGENT_TLS_KEY=/etc/haproxy/certs/sarg3.net.key
```

### Git Sync

```bash
HAPROXY_GIT_REPO=https://github.com/your-org/your-repo
HAPROXY_GIT_PAT=ghp_your_personal_access_token
HAPROXY_GIT_BRANCH=main
HAPROXY_APPS_FOLDER=apps
HAPROXY_POLL_INTERVAL=5m  # Supports: 60, 60s, 5m, 1h
```

### Webhook

```bash
HAPROXY_WEBHOOK_ENABLED=true
HAPROXY_WEBHOOK_POLL_BACKUP=false  # Keep polling as backup
```

### HAProxy Stats

```bash
HAPROXY_STATS_SOCKET=/run/haproxy/admin.sock  # Preferred
HAPROXY_STATS_URL=http://localhost:8404/stats  # Fallback
HAPROXY_STATS_USER=admin
HAPROXY_STATS_PASSWORD=secret
```

### Metric-source overrides

Most hosts have one obvious producer per metric category. Where two coexist — for example HAProxy fronting nginx, with both genuinely serving HTTP — the agent auto-picks a primary using a built-in preference list and surfaces it in the capability manifest. Operators can override that pick when auto-detection chooses wrong on their host.

Auto-detection is the default. The override env vars below exist for the rare edge cases.

```bash
# Force a specific gear as the primary for HTTP-request metrics
# (request volume, response codes, response times, vhost breakdowns).
# Valid values match gear identifiers in /api/v1/system/capabilities:
# haproxy, nginx, apache, caddy, traefik.
GEARBOX_AGENT_HTTP_SOURCE=nginx
```

Override behaviour:

- Names are case-insensitive and trimmed (`HAProxy` and `haproxy` both work).
- An override pointing at a gear that didn't probe Available, or doesn't produce data for the category, logs a warning at startup and **falls back to auto-detect** — locking out HTTP metrics because the override's target isn't installed on this box would be worse than serving auto-picked data.
- The selected primary plus the chosen reason and the alternatives that were also available appear in `/api/v1/system/capabilities` under `primary_sources` so dashboards and humans can confirm the resolution.

### Source detection

The agent probes the host at startup for every supported source — nginx, Apache, Caddy, Traefik, Docker, plus the always-present `host` entry — and reports each one's status in the capability manifest as `available`, `not_installed`, or `inaccessible`. **Auto-detection is the default; no configuration required for the common case.**

The override env vars below exist for the rare cases where auto-detection misses or the operator wants to point the agent at a non-default surface:

| Env var               | Purpose                                                          |
|-----------------------|------------------------------------------------------------------|
| `NGINX_STATUS_URL`    | Force a specific `stub_status` URL (skips the default probe).    |
| `NGINX_CONFIG_FILE`   | Force a specific `nginx.conf` path.                              |
| `APACHE_STATUS_URL`   | Force a specific `mod_status` URL (e.g. `?auto` variant).        |
| `APACHE_CONFIG_FILE`  | Force a specific `httpd.conf` / `apache2.conf` path.             |
| `CADDY_ADMIN_URL`     | Force the admin / Prometheus URL (default `:2019/metrics`).      |
| `TRAEFIK_METRICS_URL` | Force the Prometheus endpoint URL.                               |
| `DOCKER_SOCKET`       | Force a specific Docker socket path (e.g. rootless installs).    |

When an override is set the agent trusts the operator and skips the synchronous detection probe for that source — a misconfigured value surfaces later when the metrics gear (Phase 4+) tries to read from it, not at startup.

See [docs/source-detection.md](docs/source-detection.md) for the full probe precedence flow, per-source troubleshooting recipes, and the "multiple instances of one source" limitation.

## Authentication

### API Key

All endpoints (except `/health` and `/swagger/`) require API key authentication:

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://server:8405/api/v1/metrics
```

The API key is auto-generated on first run and stored at `/var/lib/gearbox-agent/api-key`.

### CLI Commands

```bash
# Show current API key
gearbox-agent --show-api-key

# Rotate API key
gearbox-agent --rotate-api-key

# Show webhook secret
gearbox-agent --show-webhook-secret

# Generate webhook secret
gearbox-agent --generate-webhook-secret
```

## Deployment

### Prerequisites

- Target server with HAProxy installed
- SSH access to the server
- Go 1.25+ for building

### Passwordless Sudo Setup (Required for make deploy)

The `make deploy` command requires passwordless sudo for specific commands. Ubuntu 24.04+ uses **sudo-rs** (Rust rewrite of sudo) which is stricter about command matching than traditional sudo.

1. Copy the sudoers file to the server:

   ```bash
   scp deploy/sudoers-gearbox-agent-deploy dave@10.0.0.3:/tmp/
   ```

1. Install it on the server (requires password once):

   ```bash
   ssh dave@10.0.0.3 'sudo cp /tmp/sudoers-gearbox-agent-deploy /etc/sudoers.d/gearbox-agent-deploy && sudo chmod 0440 /etc/sudoers.d/gearbox-agent-deploy'
   ```

1. Verify it works:

   ```bash
   ssh dave@10.0.0.3 'sudo /usr/bin/systemctl status gearbox-agent'
   ```

The sudoers file grants passwordless access to:

- `systemctl stop/start/restart/status gearbox-agent`
- `systemctl daemon-reload`
- `cp /tmp/gearbox-agent /usr/local/bin/gearbox-agent`
- `chmod +x /usr/local/bin/gearbox-agent`
- `cat /var/lib/gearbox-agent/api-key`
- `journalctl -u gearbox-agent` (with various flags)

**Important:** sudo-rs requires exact command matching. The Makefile uses full paths (`/usr/bin/systemctl`) and avoids extra flags not in the sudoers file.

### Deploy

```bash
# Build and deploy
make deploy

# View logs
make logs

# Check status
make status
```

### First-Time Installation

```bash
# First deployment (creates systemd service, directories, etc.)
make deploy-first
```

### Manual Installation

```bash
# Copy binary
scp bin/gearbox-agent-linux-amd64 user@server:/tmp/gearbox-agent
ssh user@server 'sudo cp /tmp/gearbox-agent /usr/local/bin/'

# Copy service file
scp deploy/gearbox-agent.service user@server:/tmp/
ssh user@server 'sudo cp /tmp/gearbox-agent.service /etc/systemd/system/'

# Enable and start
ssh user@server 'sudo systemctl daemon-reload && sudo systemctl enable gearbox-agent && sudo systemctl start gearbox-agent'
```

### Troubleshooting Deployment

If `make deploy` fails with "Authentication failed":

1. **Check sudo-rs compatibility**: Ubuntu 24.04+ uses sudo-rs which requires exact command matching
2. **Verify sudoers file is installed**: `ssh dave@10.0.0.3 'sudo -l'` should list the allowed commands
3. **Check file permissions**: Sudoers file must be owned by root:root with mode 0440
4. **No extra flags**: Commands must match exactly (e.g., no `--no-pager` unless in sudoers)

## Development

### Commands

```bash
make build        # Build for current platform
make build-linux  # Build for Linux amd64
make test         # Run tests
make lint         # Run linter
make swagger      # Generate Swagger docs
make fmt          # Format code
make tidy         # Tidy dependencies
```

### Running Locally

The agent uses Linux-specific syscalls and won't run on macOS. Use a Linux VM or deploy to a test server.

### Code Structure

```text
gearbox-agent/
├── cmd/gearbox-agent/    # Entry point
├── internal/
│   ├── api/              # HTTP handlers, middleware
│   ├── compose/          # Docker Compose parser
│   ├── config/           # Configuration loading
│   ├── crypto/           # TLS, API keys, secrets
│   ├── events/           # Event bus for WebSocket
│   ├── github/           # GitHub API client
│   ├── haproxy/          # Stats, config generation
│   ├── logs/             # Log collection
│   ├── metrics/          # System metrics
│   ├── security/         # Fail2ban, firewall stats
│   ├── state/            # Sync state persistence
│   └── sync/             # Git sync service
├── deploy/               # Systemd service file
└── docs/                 # API documentation
```

## Documentation

- [Gear Probes](docs/gear-probes.md) - Capability-driven gear loading, status enum, per-gear contracts
- [HAProxy API](docs/haproxy-api.md) - Stats, runtime info, validation
- [Logs API](docs/logs-api.md) - Log streaming
- [Security API](docs/security-api.md) - Fail2ban and firewall stats
- [WebSocket Events](docs/websocket-events.md) - Real-time event streaming
- [Webhook Setup](docs/webhook-setup.md) - GitHub webhook configuration
