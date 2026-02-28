# Gearbox Design

## What Is Gearbox?

Gearbox is a universal, plugin-based monitoring and management platform for DevOps. It provides real-time visibility into servers, services, and infrastructure through composable widgets arranged on customizable dashboards.

Gearbox is **not** a single-purpose tool. HAProxy monitoring is one plugin among many. The platform monitors any Linux system: bare-metal servers, virtual machines, workstations, Docker hosts, NAS appliances, or anything else running Linux.

## Design Goals

- **Plugin-based extensibility** -- All monitoring capabilities live in self-contained plugins. The framework provides shared infrastructure; plugins implement features. Adding a new capability means adding a new plugin, not modifying the core.
- **Composable widget dashboards** -- Users build custom monitoring views by arranging widgets on a grid. Widgets are provided by plugins and consume data through standardized data sources. Dashboards are stored as YAML files, making them version-controllable and GitOps-ready.
- **Multi-server monitoring** -- A single Gearbox dashboard connects to many agents running on different servers. Every widget and data source is server-aware.
- **Configuration as code** -- Dashboards, server configurations, and plugin settings can all be managed through YAML files or the web UI. Either path produces the same result.
- **Compile-time safety** -- Plugins are compiled into the binary (similar to Caddy). No runtime plugin loading, no reflection. Interfaces are checked at compile time. Templates use Templ for type-safe HTML generation.

## Architecture

### Dual-Application Model

Gearbox consists of two Go applications:

**gearbox** (Dashboard) -- Web application on port 3000. Connects to multiple agents, renders UI, manages dashboards and users. Contains 8 plugins providing 31 widgets.

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
│  Plugins → Widgets → Dashboards    │
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
4. **Dashboard renders** -- Plugin handlers pass data to Templ components. Widgets render inside dashboard grids.
5. **Browser updates** -- Real-time events flow from agent → dashboard → browser via WebSocket/SSE for live updates without polling.

## Plugin System

### How Plugins Work

Plugins register themselves at compile time via `init()` functions. On startup, the framework initializes each plugin with a `Dependencies` struct containing database access, logger, event hub, authentication, agent client, and configuration.

Each plugin is self-contained: it defines its own routes, handlers, templates, permissions, and widgets. Plugins cannot call each other directly; they communicate through the event bus.

### Feature Flags

Plugins progress through a state machine: `disabled` → `alpha` → `beta` → `production`. Alpha and beta plugins must be explicitly enabled by the user. Production plugins are enabled by default. The `disabled` state excludes the plugin from the build entirely.

### Dashboard Plugins (8)

| Plugin       | Purpose                                      |
|--------------|----------------------------------------------|
| Dashboard    | Main overview and status grid                |
| HAProxy      | HAProxy backend/frontend/server monitoring   |
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

## Widget and Dashboard System

### Widgets

Widgets are reusable UI components provided by plugins. Each widget has:

- A **type** identifier (e.g., `line-chart`, `status-card`, `data-table`)
- A **renderer** function that takes configuration and data, returns a Templ component
- A **configuration schema** describing the widget's settings
- An optional **data source** binding

The framework provides a library of standard widget types: charts (line, bar, doughnut, pie), cards (status, metric), tables (data table, simple table), streaming widgets (log viewer, terminal), action widgets (buttons, toolbars), and layout widgets (tabs, grids, containers).

### Dashboards

Dashboards are YAML files defining a page layout with positioned widgets on a 12-column grid:

```yaml
version: "1.0"
name: "Server Overview"
layout:
    columns: 12
    gap: 4
widgets:
    - id: "cpu-chart"
      type: "line-chart"
      position: { row: 1, column: 1, width: 6, height: 300 }
      config:
        data_source: "metrics.cpu"
        title: "CPU Usage"
```

Dashboards come in two forms:

- **User dashboards** -- Created through the web UI, stored as `{slug}.yaml`
- **Plugin dashboards** -- Predefined templates from plugins, stored as `plugin-{name}-{slug}.yaml`

Dashboards can define top-bar controls (filters, server selectors, refresh buttons) that affect all widgets on the page.

### Data Sources

Plugins expose data through standardized `DataSource` interfaces with `Fetch` (on-demand) and `Subscribe` (real-time streaming) methods. Widgets bind to data sources by name, decoupling data collection from presentation.

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
- **SortableJS** -- Drag-and-drop widget arrangement
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
