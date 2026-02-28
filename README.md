# Gearbox - Universal Monitoring and Management Platform

**Version:** 0.2.0

> [!WARNING]
> This project is not ready for public consumption and is under heavy development.

## Overview

Gearbox is a general-purpose monitoring and management platform with a plugin-based architecture. It uses gearbox-agent (a separate Go binary) installed on servers and workstations to gather data and act as a secure agent and controller.

**Key Capabilities:**

- Monitor multiple servers/workstations from a single dashboard
- Plugin-based feature system
- Real-time updates via WebSocket
- Web UI configuration
- Auto-discovery of services on monitored hosts

**IMPORTANT:** Gearbox is NOT an HAProxy-specific tool. It is a universal monitoring platform. HAProxy monitoring is just one plugin among many.

## 🚀 Getting Started

**New to Gearbox?** Follow our comprehensive [Getting Started Guide](docs/getting-started.md) for step-by-step instructions on:

- Installing the Gearbox dashboard and agent
- Generating and configuring API keys
- Adding your first monitored server
- Enabling plugins

The guide includes detailed examples, troubleshooting tips, and best practices for production deployments.

## Quick Start

### Prerequisites

- Go 1.25 or later
- Node.js (for frontend assets)
- [Templ](https://github.com/a-h/templ) for template generation

### Running the Dashboard

```bash
cd gearbox
make dev
```

Visit `http://localhost:3000`

### Running the Agent

#### Binary Installation

```bash
cd gearbox-agent
make build
./bin/gearbox-agent
```

#### Docker Installation

```bash
# Using Docker
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest

# Using Docker Compose
cd gearbox-agent
docker-compose up -d
```

Agent runs on <https://localhost:8405>

See [gearbox-agent/README.md](gearbox-agent/README.md) for installation options and [gearbox-agent/docs/docker.md](gearbox-agent/docs/docker.md) for Docker-specific configuration.

## Architecture

### Applications

**gearbox-agent** ([gearbox-agent/](gearbox-agent/))

- Go binary installed on monitored servers/workstations
- Runs on port 8405 (HTTPS)
- Plugin-based collectors auto-discover services
- Exposes REST API and WebSocket
- Works on ANY Linux system

**gearbox** ([gearbox/](gearbox/))

- Web dashboard application
- Runs on port 3000
- Connects to multiple gearbox-agent instances
- Plugin-based features

### Data Flow

```text
Monitored Server → gearbox-agent (collects data) → API/WebSocket → gearbox dashboard → Browser
```

### Plugin Architecture

```text
Plugin = Self-contained feature module with pages, API handlers, and templates
Framework = Shared services and building blocks
```

**Framework provides:**

- Agent client for API/WebSocket communication
- Database access and models
- Authentication/authorization
- Event bus for real-time updates
- Shared UI components (graphs, tables, panels, cards)
- Template system (Templ)

**Plugins provide:**

- Pages and routes
- API handlers
- Domain-specific logic
- Self-contained functionality

### Current Plugins

| Plugin       | Purpose                                                   |
|--------------|-----------------------------------------------------------|
| HAProxy      | HAProxy monitoring and stats                              |
| Metrics      | System metrics (CPU, memory, disk, network, load, uptime) |
| Services     | Systemd service monitoring                                |
| Certificates | TLS certificate tracking                                  |
| Logs         | Log aggregation and viewing                               |
| Traffic      | Traffic analysis and visualization                        |
| Alerts       | Alert management and rules                                |
| OS Updates   | Package update monitoring                                 |

**Total:** 8 plugins

## Development Workflow

### Gearbox Dashboard

Located in [gearbox/](gearbox/) directory.

**After ANY change, MUST run:**

```bash
cd gearbox && make templ-generate && make build
```

**For local development with hot reload:**

```bash
cd gearbox && make dev
```

**Key directories:**

- `internal/framework/` - Shared infrastructure
- `internal/plugins/` - Feature plugins
- `internal/templates/` - Templ templates
- `static/` - JavaScript, CSS, assets

### Gearbox Agent

Located in [gearbox-agent/](gearbox-agent/) directory.

**Build:**

```bash
cd gearbox-agent && make build
```

**Key directories:**

- `internal/api/` - REST API handlers
- `internal/collector/` - Data collection
- `internal/plugins/` - Agent-side plugins
- `cmd/gearbox-agent/` - Entry point

## Configuration

### Agent Configuration

Environment variables in `/etc/default/gearbox-agent`:

```bash
# Server
HAPROXY_AGENT_LISTEN=0.0.0.0:8405
HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent
HAPROXY_AGENT_LOG_LEVEL=info

# TLS
HAPROXY_AGENT_TLS_CERT=/path/to/cert.crt
HAPROXY_AGENT_TLS_KEY=/path/to/key.key
```

## Multi-Server Support

One gearbox dashboard can monitor many servers:

1. Install gearbox-agent on each server to monitor
2. Configure server in gearbox via Settings > Servers
3. Enable desired plugins for each server
4. Plugin pages display data from selected server

## Documentation

**Primary Sources:**

- [README.md](README.md) - This file (overview and quick start)
- [CLAUDE.md](CLAUDE.md) - Development guidance for Claude Code
- [docs/plugins.md](docs/plugins.md) - Complete plugin architecture documentation
- [TASKS.md](TASKS.md) - Active development tasks
- [gearbox/docs/development.md](gearbox/docs/development.md) - Local development guide

**Agent Documentation:**

- [gearbox-agent/README.md](gearbox-agent/README.md) - Agent overview
- [gearbox-agent/docs/](gearbox-agent/docs/) - API documentation

**Research & Historical:**

- [docs/research/](docs/research/) - Research and analysis documents

## Future Direction

- Container and stack management (GitOps-focused Portainer alternative)
- Multi-server orchestration
- Infrastructure-as-code integration
- Additional plugins for databases, web servers, etc.

## Contributing

This is a private repository. Development guidelines are in [CLAUDE.md](CLAUDE.md).

## Licensing

This project is licensed under the **Elastic License v2 (ELv2)**.

See [LICENSE](LICENSE) file for details.

### What you can do

You are free to:

- Use the software for personal, academic, or commercial purposes
- Deploy it internally within your organization
- Run it on servers, workstations, or embedded systems
- Modify it for internal use
- Embed it into internal tooling or workflows
- Host and use an **unmodified version** of the software for free

### What you cannot do

You may not:

- Offer the software as a hosted or managed service where the software itself is
  the primary value being sold
- Rebrand, resell, or offer it as a competing commercial product or service

### Commercial use

If you wish to offer this software (or a modified version of it) as a managed or
hosted service, commercial licensing is available.
Please contact <dave@sarg3.net> for more information.

This license is designed to be friendly to internal enterprise use while
protecting the project from being resold or rebranded as a competing service.
