# Gearbox Design

## What Is Gearbox?

Gearbox is a universal, plugin-based monitoring and management platform for DevOps. It provides real-time visibility into servers, services, and infrastructure through a plugin-based architecture.

Gearbox is **not** a single-purpose tool. HAProxy monitoring is one plugin among many. The platform monitors any Linux system: bare-metal servers, virtual machines, workstations, Docker hosts, NAS appliances, or anything else running Linux.

## Design Goals

- **Plugin-based extensibility** -- All monitoring capabilities live in self-contained plugins. The framework provides shared infrastructure; plugins implement features. Adding a new capability means adding a new plugin, not modifying the core.
- **Plugin-based pages** -- Each plugin provides its own monitoring pages with purpose-built UI. Plugins use shared framework components (charts, tables, cards) for consistent presentation.
- **Multi-server monitoring** -- A single Gearbox dashboard connects to many agents running on different servers. Every plugin page and data source is server-aware.
- **Configuration as code** -- Server configurations and plugin settings can be managed through the web UI.
- **Compile-time safety** -- Plugins are compiled into the binary (similar to Caddy). No runtime plugin loading, no reflection. Interfaces are checked at compile time. Templates use Templ for type-safe HTML generation.

## Architecture

### Dual-Application Model

Gearbox consists of two Go applications:

**gearbox** (Dashboard) -- Web application on port 3000. Connects to multiple agents, renders UI, and manages users. Contains 7 plugins.

**gearbox-agent** (Agent) -- Lightweight service on port 8405. Runs on each monitored server. Collects data via plugin-based collectors, exposes a REST API, and publishes real-time events over WebSocket. Contains 7 data collection plugins.

```text
┌────────────────────────────────────┐
│         Browser                    │
│   HTTP pages, SSE, WebSocket       │
└──────────────┬─────────────────────┘
               │
┌──────────────▼─────────────────────┐
│     Gearbox Dashboard (:3000)      │
│                                    │
│  Plugins → Pages → UI              │
│  Auth, Sessions, Permissions       │
│  SQLite database                   │
└──────┬────────────┬────────────────┘
       │            │
       ▼            ▼
┌─────────────┐  ┌─────────────┐
│  Agent :8405│  │  Agent :8405│  ...N agents
│  Server A   │  │  Server B   │
│             │  │             │
│  Collectors │  │  Collectors │
│  REST API   │  │  REST API   │
│  WebSocket  │  │  WebSocket  │
└─────────────┘  └─────────────┘
```

### Data Flow

1. **Agent collects** -- Plugin collectors run on intervals, gathering system metrics, service stats, log data, certificate info, and more from the host.
2. **Agent exposes** -- Collected data is available via REST API endpoints. Changes are broadcast as WebSocket events.
3. **Dashboard fetches** -- The agent client (80+ methods) calls agent REST APIs over HTTPS with API key authentication.
4. **Dashboard renders** -- Plugin handlers pass data to Templ components for display.
5. **Browser updates** -- Real-time events flow from agent → dashboard → browser via WebSocket/SSE for live updates without polling.

## Plugin System

### How Plugins Work

Plugins register themselves at compile time via `init()` functions. On startup, the framework initializes each plugin with a `Dependencies` struct containing database access, logger, event hub, authentication, agent client, and configuration.

Each plugin is self-contained: it defines its own routes, handlers, templates, and permissions. Plugins cannot call each other directly; they communicate through the event bus.

### Feature Flags

Plugins progress through a state machine: `disabled` → `alpha` → `beta` → `production`. Alpha and beta plugins must be explicitly enabled by the user. Production plugins are enabled by default. The `disabled` state excludes the plugin from the build entirely.

### Dashboard Plugins (7)

| Plugin       | Purpose                                      |
|--------------|----------------------------------------------|
| HAProxy      | HAProxy overview, status grid, and backend/frontend/server monitoring |
| Metrics      | Historical CPU, memory, disk, network charts |
| Services     | Systemd service monitoring and control       |
| Certificates | TLS certificate expiration tracking          |
| Logs         | Real-time log viewing and search             |
| Traffic      | Traffic analysis and GeoIP visualization     |
| Alerts       | Alert rules, notifications, and history      |

### Agent Plugins (7)

| Plugin   | Purpose                                                   |
|----------|-----------------------------------------------------------|
| HAProxy  | Stats socket and stats URL collection                     |
| Metrics  | System metrics (CPU, memory, disk, network, load, uptime) |
| Logs     | Log file access and streaming                             |
| Certs    | Certificate discovery and management                      |
| Traffic  | Traffic data collection                                   |
| Security | Fail2ban and firewall integration                         |
| Updates  | OS package management (APT)                               |

### Agent Facades

Each dashboard plugin defines a narrow interface for the agent methods it needs, rather than depending on the full agent client. This keeps plugins decoupled and testable.

```go
type AgentClient interface {
    GetCertificates() (*agent.CertificatesResponse, error)
}
var _ AgentClient = (*agent.Client)(nil)
```

## UI Architecture

### Plugin Pages

Each plugin provides its own pages with purpose-built UI. Plugins use shared framework components (charts, tables, cards, panels) for consistent presentation. Pages are server-side rendered with Templ and enhanced with HTMX for dynamic updates.

### Shared Components

The framework provides reusable UI components that plugins use to build their pages:

- **Charts** -- Chart.js-based components for time-series data (CPU, memory, network, etc.)
- **Cards** -- Status cards, metric cards for at-a-glance information
- **Tables** -- Data tables for detailed listings
- **Panels** -- Collapsible containers for organizing content

### Data Flow to UI

Plugins expose API endpoints that return HTML partials (for HTMX) or JSON (for JavaScript). Pages use HTMX polling or SSE for real-time updates without full page reloads.

## Technology Stack

### Backend

- **Go** -- Primary language for both applications
- **Chi** -- HTTP router
- **Templ** -- Type-safe HTML template engine (compiles to Go)
- **SQLite** -- Dashboard database (WAL mode)
- **Gorilla WebSocket** -- WebSocket connections
- **slog** -- Structured logging

### Frontend

- **Tailwind CSS** -- Utility-first styling
- **Alpine.js** -- Lightweight reactivity for interactive components
- **Chart.js** -- Data visualization
- Server-side rendered HTML with progressive enhancement (no SPA framework)

## Security Model

- **Agent authentication** -- API keys generated on first run, required for all endpoints except `/health`
- **WebSocket authentication** -- Two-step token exchange: API key → short-lived token → WebSocket connection
- **Dashboard authentication** -- Password-based with optional WebAuthn/FIDO2
- **Authorization** -- Component-action permission model (e.g., `certificates:view`, `alerts:manage`)
- **Transport** -- HTTPS with TLS 1.2+ between dashboard and agents. Optional CA cert pinning.
- **Rate limiting** -- Configurable per-endpoint rate limits

## Key Patterns

- **Compile-time plugin registration** via `init()` and a global registry
- **Dependency injection** through a `Dependencies` struct passed to each plugin
- **Interface segregation** with per-plugin agent facades
- **Event-driven communication** between plugins via a pub/sub event hub
- **Middleware chain** for HTTP concerns (auth, CSRF, logging, rate limiting, recovery)
- **Server-side rendering** with Templ components, enhanced with Alpine.js for interactivity
