# Gearbox

A modern, self-contained monitoring dashboard for HAProxy reverse proxy servers. Built with Go backend and server-side rendered frontend, Gearbox provides real-time visibility into HAProxy health, configuration, and performance metrics through an intuitive web interface.

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
  - [Using Docker Compose](#using-docker-compose-recommended)
  - [Using Docker](#using-docker)
- [Configuration](#configuration)
  - [Required Environment Variables](#required-environment-variables)
  - [Optional Environment Variables](#optional-environment-variables)
  - [Server Configuration Format](#server-configuration-format)
- [Development](#development)
  - [Prerequisites](#prerequisites)
  - [Local Development](#local-development)
  - [Building](#building)
- [Architecture](#architecture)
- [Documentation](#documentation)
- [Contributing](#contributing)
  - [Coding Standards](#coding-standards)
- [License](#license)
- [Acknowledgments](#acknowledgments)
- [Support](#support)

## Features

- 📊 **Real-time Performance Metrics** - Live HAProxy statistics, throughput, and connection monitoring
- 🔍 **Configuration-Aware** - Understands custom haproxy-autoconfig labels and displays full backend configuration
- 🎯 **Multi-Server Support** - Monitor multiple HAProxy instances from a single dashboard
- 🏠 **Home Dashboard Gear** - Drag-and-drop start page with launcher tiles, live widgets (Sonarr / Radarr / Plex / UniFi / Pi-hole / qBittorrent / …), inline sparkline graphs, server-side reachability checks, and encrypted per-tile API keys. See [Home gear docs](docs/home-gear.md).
- 📱 **Mobile-Responsive UI** - Modern, beautiful interface that works on desktop, tablet, and mobile
- 🔐 **Secure Authentication** - Session-based authentication with encrypted cookies
- 📈 **System Metrics** - CPU, memory, disk, and network usage from HAProxy servers
- 📝 **Log Viewer** - Real-time log viewing with search, filtering, and export capabilities
- 🐳 **Fully Containerized** - Zero-state design, all configuration via environment variables
- ⚡ **Fast & Lightweight** - Built in Go, <50MB Docker image, minimal resource usage
- 🏷️ **Automatic Grouping** - Organizes backends by naming convention (no custom config needed)

## Quick Start

### Using Docker Compose (Recommended)

1. **Clone the repository:**

   ```bash
   git clone https://github.com/sarg3nt/gearbox.git
   cd gearbox
   ```

2. **Create environment file:**

   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

3. **Generate session secret:**

   ```bash
   openssl rand -hex 32
   # Add to .env as SESSION_SECRET
   ```

4. **Start the application:**

   ```bash
   docker-compose up -d
   ```

5. **Access the dashboard:**
   Open <http://localhost:3000> in your browser. On first run, an admin password will be generated and logged to the console. You can also set `ADMIN_PASSWORD` in your `.env` file.

### Using Docker

```bash
# SECURITY: Replace ALL placeholder values before using in production
docker run -d \
  -p 3000:3000 \
  -e HAPROXY_SERVERS='[{"id":"main","name":"Production","stats_url":"http://YOUR_HAPROXY_IP:8404/stats","stats_user":"YOUR_STATS_USERNAME","stats_password":"YOUR_STATS_PASSWORD","ssh_host":"YOUR_HAPROXY_IP:22","ssh_user":"YOUR_SSH_USERNAME","ssh_auth_method":"password","ssh_password":"YOUR_SSH_PASSWORD"}]' \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/sarg3nt/gearbox:latest
# Check logs for auto-generated admin password, or set ADMIN_PASSWORD env var
```

## Backend Naming Convention for Grouping

The monitor automatically groups backends based on a simple naming convention. **No additional configuration or metadata files are required.**

### Naming Pattern

Backend names should follow this pattern:

```text
{group}_{service}_{kind}
```

**Examples:**

- `hardware_thor_backend` → Group: "Hardware"
- `hardware_unifi_backend` → Group: "Hardware"
- `mediamanager_gluetun_backend` → Group: "Mediamanager"
- `qbittorrent_gluetun_backend` → Group: "Qbittorrent"

### How It Works

1. The monitor parses each backend name by splitting on underscores (`_`)
2. The **first part** becomes the group name (e.g., "hardware", "mediamanager")
3. Backends with the same group prefix are displayed together in a grouped container
4. Group names are automatically capitalized in the UI

### For Manual HAProxy Configurations

If you're manually writing HAProxy configs (not using haproxy-autoconfig), simply name your backends following the pattern above:

```haproxy
backend hardware_server1_backend
  mode http
  server srv1 192.168.1.10:80 check

backend hardware_server2_backend
  mode http
  server srv2 192.168.1.11:80 check

backend app_webserver_backend
  mode http
  server srv1 192.168.1.20:8080 check
```

This will create two groups in the UI: "Hardware" (with 2 backends) and "App" (with 1 backend).

### For Docker Compose with gearbox-agent

When using Docker Compose labels with gearbox-agent, you can override the default backend name:

```yaml
labels:
  haproxy.enable: "true"
  haproxy.hostname: "app.example.com"
  haproxy.backend.server: "10.40.0.20:8080"
  haproxy.backend.name: "apps_myapp_backend"  # Custom name for grouping
```

By default, the backend name is `{app_folder}_{service_name}_backend`.

## Configuration

All configuration is done via environment variables. See [SETUP.md](docs/SETUP.md) for detailed setup instructions.

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `HAPROXY_SERVERS` | JSON array of servers to monitor | See below |
| `SESSION_SECRET` | Secret key for session encryption (32+ chars) | Generate with `openssl rand -hex 32` |

### Authentication Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_PASSWORD` | Auto-generated | Initial admin password. If not set, a random password is generated and logged on first startup. |
| `BASE_URL` | `http://localhost:3000` | Base URL for email links (password reset, etc.) |

### Optional Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SESSION_TIMEOUT_MINUTES` | `60` | Session timeout in minutes |
| `DASHBOARD_REFRESH_SECONDS` | `30` | Dashboard auto-refresh interval |
| `CONFIG_REFRESH_SECONDS` | `60` | Config metadata refresh interval |
| `HTTP_PORT` | `3000` | HTTP server port |
| `LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |
| `TLS_CERT_PATH` | - | Path to TLS certificate (enables HTTPS) |
| `TLS_KEY_PATH` | - | Path to TLS key (enables HTTPS) |
| `SSH_HOST_KEY_VERIFICATION` | `strict` | SSH host key verification (strict/accept-new) |

### Server Configuration Format

```json
{
  "id": "light-hugger",
  "name": "Production Proxy",
  "stats_url": "http://10.0.0.3:8404/stats",
  "stats_user": "admin",
  "stats_password": "secret123",
  "ssh_host": "10.0.0.3:22",
  "ssh_user": "dave",
  "ssh_auth_method": "key",
  "ssh_private_key_path": "/secrets/id_ed25519",
  "metadata_json_path": "/var/lib/gearbox-agent/metadata.json",
  "additional_logs": [
    {"name": "fail2ban", "command": "journalctl -u fail2ban -n 1000"}
  ]
}
```

## Development

### Prerequisites

- Go 1.25+
- Docker (for containerized builds)
- make (optional, for convenience commands)

### Local Development

1. **Install development tools:**

   ```bash
   make install-tools
   ```

2. **Download dependencies:**

   ```bash
   make deps
   ```

3. **Generate Templ templates:**

   ```bash
   make templ-generate
   ```

4. **Run tests:**

   ```bash
   make test
   ```

5. **Run linter:**

   ```bash
   make lint
   ```

6. **Run locally:**

   ```bash
   # Set required environment variables first
   export HAPROXY_SERVERS='[...]'
   export SESSION_SECRET="dev-secret-at-least-32-chars-long"
   # Optional: set admin password (otherwise auto-generated)
   export ADMIN_PASSWORD="devpassword123"

   make run
   ```

### Building

```bash
# Build binary
make build

# Build Docker image
make docker-build

# Run Docker container
make docker-run
```

## Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for detailed architecture documentation.

**High-level overview:**

```text
┌─────────────────┐
│  Web Browser    │
└────────┬────────┘
         │ HTTPS
┌────────▼────────┐
│    Gearbox      │
│  (Go Server)    │
└────────┬────────┘
         │
    ┌────┴────┬──────────┐
    │         │          │
┌───▼───┐ ┌──▼──┐  ┌────▼─────┐
│ Stats │ │ SSH │  │ Metadata │
│  API  │ │     │  │   JSON   │
└───┬───┘ └──┬──┘  └────┬─────┘
    │        │          │
┌───▼────────▼──────────▼───┐
│   HAProxy Server(s)       │
│   (gearbox-agent)         │
└───────────────────────────┘
```

## Documentation

- [docs/home-gear.md](docs/home-gear.md) - Home dashboard gear: widgets, sparklines, security model
- [docs/development.md](docs/development.md) - Local development guide
- [docs/local-development-csp.md](docs/local-development-csp.md) - CSP notes for local dev
- [internal/gears/home/README.md](internal/gears/home/README.md) - Home gear: implementation reference (routes, schema, providers)

## Contributing

Contributions are welcome! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes using conventional commits (`git commit -m 'feat: add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Coding Standards

- Follow Go best practices and idiomatic Go code
- Run `make lint` before committing
- Ensure `make test` passes
- Add tests for new functionality
- Update documentation as needed

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Acknowledgments

Built with:

- [Go](https://golang.org/) - Programming language
- [Templ](https://templ.guide/) - Type-safe Go templating
- [Chi](https://github.com/go-chi/chi) - Lightweight router
- [Tailwind CSS](https://tailwindcss.com/) - CSS framework
- [Chart.js](https://www.chartjs.org/) - Charts and graphs
- [AG-Grid](https://www.ag-grid.com/) - Data grid

## Support

- 🐛 **Bug Reports:** [GitHub Issues](https://github.com/sarg3nt/gearbox/issues)
- 💬 **Discussions:** [GitHub Discussions](https://github.com/sarg3nt/gearbox/discussions)
- 📧 **Email:** <your-email@example.com>

---

**Version:** 1.0.0
**Built with ❤️ for the HAProxy community**
