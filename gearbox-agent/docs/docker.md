# Docker Installation and Configuration Guide

This guide covers installing and running gearbox-agent using Docker containers.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Installation Methods](#installation-methods)
  - [Using Pre-built Images](#using-pre-built-images)
  - [Building from Source](#building-from-source)
  - [Using Docker Compose](#using-docker-compose)
- [Configuration](#configuration)
  - [Environment Variables](#environment-variables)
  - [Volume Mounts](#volume-mounts)
  - [TLS Certificates](#tls-certificates)
- [Monitoring HAProxy with Docker](#monitoring-haproxy-with-docker)
- [Advanced Configurations](#advanced-configurations)
- [Troubleshooting](#troubleshooting)

## Overview

The gearbox-agent Docker image provides:

- Lightweight Alpine-based container (~50MB)
- Non-root user execution for security
- Multi-architecture support (amd64, arm64)
- Self-signed TLS certificates by default
- Automatic health checks
- Persistent data storage via volumes

**Image Repository:** `ghcr.io/sarg3nt/gearbox/gearbox-agent`

## Quick Start

Pull and run the latest image:

```bash
# Pull the image
docker pull ghcr.io/sarg3nt/gearbox/gearbox-agent:latest

# Run with minimal configuration
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest

# View logs
docker logs -f gearbox-agent

# Get API key
docker exec gearbox-agent cat /var/lib/gearbox-agent/api-key
```

Access the API at `https://localhost:8405`

## Installation Methods

### Using Pre-built Images

Pre-built images are automatically published to GitHub Container Registry on every release.

**Available tags:**

- `latest` - Latest stable release from main branch
- `v1.0.0` - Specific version tag
- `main` - Latest commit on main branch
- `sha-abc123` - Specific commit SHA

**Pull and run:**

```bash
# Latest version
docker pull ghcr.io/sarg3nt/gearbox/gearbox-agent:latest

# Specific version
docker pull ghcr.io/sarg3nt/gearbox/gearbox-agent:v1.0.0

# Run the container
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

### Building from Source

Build the Docker image locally:

```bash
cd gearbox-agent

# Build using Makefile
make docker-build

# Or build with docker directly
docker build -t gearbox-agent:local .

# Run the locally built image
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  gearbox-agent:local
```

### Using Docker Compose

Docker Compose provides the easiest way to manage gearbox-agent with all its configuration.

**1. Create docker-compose.yml:**

```yaml
version: '3.8'

services:
  gearbox-agent:
    image: ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
    container_name: gearbox-agent
    restart: unless-stopped
    ports:
      - "8405:8405"
    volumes:
      - ./data:/var/lib/gearbox-agent
    environment:
      - HAPROXY_AGENT_LISTEN=0.0.0.0:8405
      - HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent
      - HAPROXY_AGENT_LOG_LEVEL=info
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "--no-check-certificate", "https://localhost:8405/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
    security_opt:
      - no-new-privileges:true
    user: "1000:1000"
```

**2. Start the service:**

```bash
# Start in detached mode
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the service
docker-compose down

# Restart the service
docker-compose restart
```

A complete `docker-compose.yml` example with all available options is included in the repository.

## Configuration

### Environment Variables

Configure gearbox-agent using environment variables:

**Core Settings:**

```bash
# Server configuration
HAPROXY_AGENT_LISTEN=0.0.0.0:8405
HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent
HAPROXY_AGENT_LOG_LEVEL=info  # debug, info, warn, error
```

**TLS Configuration:**

```bash
# Use custom TLS certificates (recommended for production)
HAPROXY_AGENT_TLS_CERT=/etc/certs/fullchain.crt
HAPROXY_AGENT_TLS_KEY=/etc/certs/key.key
```

**Git Sync (for HAProxy auto-configuration):**

```bash
HAPROXY_GIT_REPO=https://github.com/your-org/your-repo
HAPROXY_GIT_PAT=ghp_your_personal_access_token
HAPROXY_GIT_BRANCH=main
HAPROXY_APPS_FOLDER=apps
HAPROXY_POLL_INTERVAL=5m
```

**Webhook Configuration:**

```bash
HAPROXY_WEBHOOK_ENABLED=true
HAPROXY_WEBHOOK_POLL_BACKUP=false
```

**HAProxy Stats (when monitoring HAProxy):**

```bash
HAPROXY_STATS_SOCKET=/run/haproxy/admin.sock
HAPROXY_STATS_URL=http://localhost:8404/stats
HAPROXY_STATS_USER=admin
HAPROXY_STATS_PASSWORD=secret
```

### Volume Mounts

**Required volumes:**

| Host Path | Container Path | Purpose |
|-----------|---------------|---------|
| `./data` or named volume | `/var/lib/gearbox-agent` | API keys, state, sync data |

**Optional volumes (depending on monitoring needs):**

| Host Path | Container Path | Purpose |
|-----------|---------------|---------|
| `/etc/certs` | `/etc/certs:ro` | Custom TLS certificates |
| `/run/haproxy` | `/run/haproxy:ro` | HAProxy admin socket access |
| `/run/systemd` | `/run/systemd:ro` | Systemd service monitoring |
| `/var/log` | `/var/log:ro` | Log file access |

**Example with all volumes:**

```bash
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  -v /etc/letsencrypt/live/example.com:/etc/certs:ro \
  -v /run/haproxy:/run/haproxy:ro \
  -v /run/systemd:/run/systemd:ro \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

### TLS Certificates

#### Option 1: Self-signed (Default)

The agent automatically generates self-signed certificates on first run. The cert covers loopback (`localhost`, `127.0.0.1`, `::1`) out of the box, which is enough when clients reach the agent over `localhost` (e.g. host-network containers, or the host itself).

If clients dial the agent by a static container IP, an FQDN, or a LAN hostname, add those as extra SANs via `HAPROXY_AGENT_TLS_HOSTS`:

```yaml
services:
  gearbox-agent:
    environment:
      # Comma-separated list of hostnames and/or IPs to include as SANs.
      - HAPROXY_AGENT_TLS_HOSTS=mjolnir,172.16.2.3,agent.example.com
```

The agent regenerates the self-signed cert automatically when this list changes (adding a SAN the existing cert does not cover).

#### Option 2: Custom Certificates

Mount your certificates and configure the paths:

```yaml
services:
  gearbox-agent:
    volumes:
      - /etc/letsencrypt/live/example.com:/etc/certs:ro
    environment:
      - HAPROXY_AGENT_TLS_CERT=/etc/certs/fullchain.pem
      - HAPROXY_AGENT_TLS_KEY=/etc/certs/privkey.pem
```

## Monitoring HAProxy with Docker

### Scenario 1: HAProxy on Host, Agent in Docker

Mount the HAProxy admin socket into the container:

```yaml
services:
  gearbox-agent:
    volumes:
      - /run/haproxy:/run/haproxy:ro
    environment:
      - HAPROXY_STATS_SOCKET=/run/haproxy/admin.sock
```

### Scenario 2: HAProxy and Agent Both in Docker

Use Docker networking to connect containers:

```yaml
version: '3.8'

services:
  haproxy:
    image: haproxy:latest
    volumes:
      - haproxy-socket:/run/haproxy
    # ... HAProxy config

  gearbox-agent:
    image: ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
    volumes:
      - haproxy-socket:/run/haproxy:ro
    environment:
      - HAPROXY_STATS_SOCKET=/run/haproxy/admin.sock
    depends_on:
      - haproxy

volumes:
  haproxy-socket:
```

### Scenario 3: Using HAProxy Stats URL

If the admin socket isn't accessible, use the stats URL:

```yaml
services:
  gearbox-agent:
    environment:
      - HAPROXY_STATS_URL=http://haproxy:8404/stats
      - HAPROXY_STATS_USER=admin
      - HAPROXY_STATS_PASSWORD=secret
```

## Advanced Configurations

### Running with Host Network Mode

For full system monitoring capabilities:

```bash
docker run -d \
  --name gearbox-agent \
  --network host \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

**Note:** Host network mode disables network isolation. Use with caution.

### Custom Entrypoint Arguments

Pass custom arguments to the gearbox-agent binary:

```bash
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest \
  --log-level debug
```

### Resource Limits

Constrain container resource usage:

```yaml
services:
  gearbox-agent:
    deploy:
      resources:
        limits:
          cpus: '0.50'
          memory: 256M
        reservations:
          cpus: '0.25'
          memory: 128M
```

### Multi-Architecture Support

The image supports both amd64 and arm64 architectures:

```bash
# Pull for specific architecture
docker pull --platform linux/amd64 ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
docker pull --platform linux/arm64 ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

## Troubleshooting

### View Logs

```bash
# Docker run
docker logs -f gearbox-agent

# Docker Compose
docker-compose logs -f gearbox-agent
```

### Get API Key

```bash
# Docker run
docker exec gearbox-agent cat /var/lib/gearbox-agent/api-key

# Docker Compose
docker-compose exec gearbox-agent cat /var/lib/gearbox-agent/api-key
```

### Check Health

```bash
# Check health status
docker inspect --format='{{.State.Health.Status}}' gearbox-agent

# Manual health check
docker exec gearbox-agent wget -q --spider --no-check-certificate https://localhost:8405/health && echo "OK"
```

### Access Container Shell

```bash
# Docker run
docker exec -it gearbox-agent sh

# Docker Compose
docker-compose exec gearbox-agent sh
```

### Common Issues

#### Issue: Permission denied on volumes

**Solution:** Ensure the data directory has correct permissions:

```bash
# For bind mount
mkdir -p ./data
chown -R 1000:1000 ./data

# Or use a named volume instead
docker volume create gearbox-agent-data
```

#### Issue: Cannot access HAProxy socket

**Solution:** Verify socket permissions and mount:

```bash
# Check socket exists
ls -la /run/haproxy/admin.sock

# Ensure readable by container user
sudo chmod 666 /run/haproxy/admin.sock

# Or run container as root (not recommended)
docker run --user root ...
```

#### Issue: TLS certificate errors

**Solution:** Check certificate paths and permissions:

```bash
# Verify certificates are mounted
docker exec gearbox-agent ls -la /etc/certs/

# Check certificate validity
docker exec gearbox-agent openssl x509 -in /etc/certs/fullchain.pem -text -noout
```

#### Issue: Container won't start

**Solution:** Check logs for errors:

```bash
docker logs gearbox-agent

# Check with more verbose logging
docker run --rm \
  -e HAPROXY_AGENT_LOG_LEVEL=debug \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

### Performance Tuning

Increase health check interval for resource-constrained systems:

```yaml
healthcheck:
  interval: 60s  # Reduced from 30s
  timeout: 15s
  retries: 3
```

Disable health checks entirely:

```bash
docker run --no-healthcheck ...
```

## Production Deployment Checklist

- [ ] Use specific version tags, not `latest`
- [ ] Configure custom TLS certificates
- [ ] Set up persistent volumes with backups
- [ ] Configure resource limits
- [ ] Enable restart policy (`unless-stopped` or `always`)
- [ ] Run as non-root user (default)
- [ ] Use `no-new-privileges` security option
- [ ] Set up log rotation for Docker logs
- [ ] Configure monitoring for container health
- [ ] Document API key location and backup procedures

## Next Steps

- [API Documentation](../README.md#api-endpoints) - Explore available API endpoints
- [WebSocket Events](websocket-events.md) - Set up real-time event streaming
- [Security Guide](security-api.md) - Configure security monitoring
- [HAProxy Monitoring](haproxy-api.md) - Monitor HAProxy instances
