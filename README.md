# Gearbox - Universal Monitoring and Management Platform

**Version:** 0.2.0 (Plugin Architecture) | **Last Updated:** 2026-01-31

## Overview

Gearbox is a general-purpose monitoring and management platform with a plugin-based architecture and composable widgets. It uses gearbox-agent (a separate Go binary) installed on servers and workstations to gather data and act as a secure agent and controller.

**Key Capabilities:**

- Monitor multiple servers/workstations from a single dashboard
- Plugin-based feature system with composable widgets
- Real-time updates via WebSocket
- YAML-defined dashboards (GitOps-ready)
- Web UI or YAML file configuration
- Auto-discovery of services on monitored hosts

**IMPORTANT:** Gearbox is NOT an HAProxy-specific tool. It is a universal monitoring platform. HAProxy monitoring is just one plugin among many.

## 🚀 Getting Started

**New to Gearbox?** Follow our comprehensive [Getting Started Guide](docs/getting-started.md) for step-by-step instructions on:

- Installing the Gearbox dashboard and agent
- Generating and configuring API keys
- Adding your first monitored server
- Enabling plugins and creating dashboards

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

```bash
cd gearbox-agent
make build
./bin/gearbox-agent
```

Agent runs on `https://localhost:8405`

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
- Plugin-based features with widget system
- YAML-defined dashboards

### Data Flow

```text
Monitored Server → gearbox-agent (collects data) → API/WebSocket → gearbox dashboard → Browser
```

### Plugin Architecture

```text
Plugin = Collection of Widgets + Optional Predefined Dashboard(s)
Widget = Reusable UI component built with framework building blocks
Dashboard = Named page containing arranged widgets (YAML-defined)
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

- Widgets (reusable UI components)
- Optional predefined dashboards
- Domain-specific logic
- Self-contained functionality

### Current Plugins

| Plugin | Widgets | Purpose |
|--------|---------|---------|
| HAProxy | 5 | HAProxy monitoring and stats |
| Metrics | 7 | System metrics (CPU, memory, disk, network, load, uptime) |
| Services | 3 | Systemd service monitoring |
| Certificates | 3 | TLS certificate tracking |
| Logs | 1 | Log aggregation and viewing |
| Traffic | 4 | Traffic analysis and visualization |
| Alerts | 5 | Alert management and rules |
| OS Updates | 3 | Package update monitoring |

**Total:** 8 plugins, 31 widgets

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
- `data/dashboards/` - YAML dashboard definitions

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

### Dashboard Configuration

Dashboards are YAML files in `gearbox/data/dashboards/`:

```yaml
name: "System Overview"
description: "System metrics and status"
editable: true
plugin: ""
widgets:
  - id: "cpu-usage"
    type: "metrics-cpu-graph"
    title: "CPU Usage"
    position: { x: 0, y: 0 }
    size: { width: 6, height: "auto" }
```

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
4. Dashboards display data from selected server

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
- Dashboard export/import and git sync
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
Please contact **dave@sarg3.net** for more information.

This license is designed to be friendly to internal enterprise use while
protecting the project from being resold or rebranded as a competing service.
