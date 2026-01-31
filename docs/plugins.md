# Plugin Architecture

**Version:** 0.2.0 | **Last Updated:** 2026-01-31

Gearbox uses a compile-time plugin architecture for both the **gearbox** (client) and **gearbox-agent** (server agent) applications. This document describes the plugin system, how to create plugins, and how feature flags control plugin availability.

---

## Table of Contents

- [Overview](#overview)
- [Terminology](#terminology)
- [Architecture Principles](#architecture-principles)
- [Plugin Types](#plugin-types)
- [Gearbox Client Plugins](#gearbox-client-plugins)
- [Gearbox Agent Plugins](#gearbox-agent-plugins)
- [Agent Client Communication](#agent-client-communication)
- [Feature Flags](#feature-flags)
- [Creating a New Plugin](#creating-a-new-plugin)
- [Testing Plugins](#testing-plugins)
- [Dashboards and Widgets](#dashboards-and-widgets)
- [Examples](#examples)

---

## Overview

### What is a Plugin?

A **plugin** is a self-contained module that provides specific functionality to the Gearbox system. Plugins:

- Register themselves at compile time via `init()` functions
- Implement a common `Plugin` interface
- Can define HTTP routes, permissions, UI pages, and background tasks
- Are isolated from other plugins but share framework services

### Why Plugins?

- **Modularity:** Features are cleanly separated into independent modules
- **Extensibility:** New functionality can be added without modifying the core
- **Maintainability:** Each plugin owns its domain logic
- **Testability:** Plugins can be tested in isolation
- **Feature Management:** Plugins can be enabled/disabled per server

---

## Terminology

To avoid confusion, this document uses the following terms consistently:

| Term | Meaning | Location |
|------|---------|----------|
| **Gearbox** | The entire monitoring system (both client and agent) | - |
| **Gearbox Client** | The web application (`gearbox`) that admins use | Runs on admin's machine or server |
| **Gearbox Agent** | The server agent (`gearbox-agent`) that collects data | Runs on monitored servers |
| **Client Plugin** | A plugin for the web application | `gearbox/internal/plugins/` |
| **Agent Plugin** | A plugin for the server agent | `gearbox-agent/internal/plugins/` |
| **Dashboard Plugin** | The specific plugin named "dashboard" (main overview page) | `gearbox/internal/plugins/dashboard/` |
| **Dashboard Widgets** | Future feature: embeddable components for custom dashboards | Not yet implemented |

**Why "Client" instead of "Dashboard"?**

The web application is the **client** that connects to agents. Using "client plugin" avoids confusion with:

- The `dashboard` plugin (one specific plugin that shows the overview page)
- Future dashboard widgets (embeddable components)

---

## Architecture Principles

### Compile-Time Plugins

Gearbox uses **compile-time plugins** (similar to Caddy) rather than runtime dynamic loading:

- Plugins are compiled into the binary
- No runtime plugin loading or shared libraries
- Type safety and performance of static compilation
- Easier to reason about and debug

### Framework vs Plugins

```txt
gearbox/
├── internal/
│   ├── framework/         # Shared infrastructure
│   │   ├── agent/         # Agent client (infrastructure)
│   │   ├── auth/          # Authentication/authorization
│   │   ├── collector/     # Data collection
│   │   ├── database/      # Database access
│   │   ├── events/        # Event bus
│   │   ├── handler/       # HTTP handlers (legacy)
│   │   ├── models/        # Data models
│   │   ├── plugin/        # Plugin system
│   │   ├── services/      # Shared services
│   │   └── templates/     # Shared templates
│   │
│   └── plugins/           # Feature implementations
│       ├── alerts/
│       ├── certificates/
│       ├── dashboard/
│       ├── logs/
│       ├── metrics/
│       ├── services/
│       └── traffic/
```

**Key principle:** The `framework` provides infrastructure and services. The `plugins` implement features.

### Plugin Isolation

Plugins are isolated from each other but can:

- Access framework services via `Dependencies`
- Publish/subscribe to events via the event bus
- Register HTTP routes under their namespace
- Define permissions and UI components

Plugins **cannot** directly call methods on other plugins.

---

## Plugin Types

### Gearbox Client Plugins

Client plugins run in the **gearbox** web application (the monitoring client) and provide UI pages and API endpoints for monitoring and managing servers.

**Location:** `gearbox/internal/plugins/`

**Purpose:** Render UI pages, handle user interactions, display data from agents

**Terminology Note:** These are called "client plugins" to distinguish them from:
- The `dashboard` plugin (which provides the main overview page)
- Future "dashboard widgets" (embeddable components for custom dashboards)

**Examples:**

- `dashboard` - Main monitoring overview page (the homepage)
- `certificates` - SSL/TLS certificate monitoring page
- `logs` - Log viewing and analysis page
- `traffic` - Traffic analysis and visualization page
- `alerts` - Alert management page
- `services` - Service status and control page
- `metrics` - System metrics and history page

### Gearbox Agent Plugins

Agent plugins run on monitored servers (in the **gearbox-agent** application) and collect data, expose APIs, and respond to management commands.

**Location:** `gearbox-agent/internal/plugins/`

**Purpose:** Collect data, expose REST APIs, publish events

**Examples:**

- `haproxy` - HAProxy stats collection
- `metrics` - System metrics (CPU, memory, disk)
- `logs` - Log access and streaming
- `certs` - Certificate management
- `security` - Firewall and fail2ban integration
- `traffic` - Traffic analysis data collection
- `updates` - OS package management

---

## Gearbox Client Plugins

Client plugins run in the gearbox web application and provide full-page UIs for specific features.

### Plugin Interface

Client plugins implement the `plugin.Plugin` interface:

```go
type Plugin interface {
    Info() Info                          // Metadata
    Initialize(ctx, deps) error          // Setup
    RegisterRoutes(r chi.Router)         // HTTP routes
    SidebarItem() *SidebarConfig         // Navigation
    SettingsPage(config) templ.Component // Settings UI
    Permissions() []PermissionDef        // Required permissions
}
```

### Directory Structure

```
internal/plugins/myplugin/
├── plugin.go      # Plugin definition and interface implementation
├── handlers.go    # HTTP request handlers
├── icons.go       # SVG icons for UI
├── agent.go       # Agent client interface (if needed)
├── pages.templ    # Templ templates for pages
└── README.md      # Plugin documentation
```

### Registration

Plugins register themselves in `plugin.go` via `init()`:

```go
package myplugin

import "github.com/sarg3nt/gearbox/internal/framework/plugin"

func init() {
    plugin.Register(&Plugin{})
}

type Plugin struct {
    plugin.BasePlugin
    handlers *Handlers
}

func (p *Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "myplugin",
        DisplayName: "My Plugin",
        Description: "What this plugin does",
        Version:     "1.0.0",
        Icon:        "icon-name",
        Category:    "monitoring",
        Core:        false,  // true = cannot be disabled
    }
}
```

### Initialization

The `Initialize` method receives dependencies from the framework:

```go
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
        return err
    }

    p.handlers = NewHandlers(deps)
    return nil
}
```

### Dependencies Available to Plugins

```go
type Dependencies struct {
    DB         *sql.DB           // Database connection
    Logger     *slog.Logger      // Structured logger
    EventHub   EventPublisher    // Event bus
    Auth       AuthChecker       // Auth and permissions
    Agent      *agent.Client     // Agent API client
    Servers    ServerRegistry    // Server configurations
    HTTPClient *http.Client      // HTTP client for external requests
    Config     map[string]any    // Plugin configuration from DB
}
```

### Route Registration

Plugins register HTTP routes that are mounted under their path:

```go
func (p *Plugin) RegisterRoutes(r chi.Router) {
    // Mounted at /myplugin/
    r.Get("/", p.handlers.IndexPage)
    r.Get("/details/{id}", p.handlers.DetailsPage)
}
```

**Note:** API routes typically remain in `framework/handler` for now and access collectors directly. This is a transitional state.

### Sidebar Configuration

Plugins define their sidebar navigation item:

```go
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
    return &plugin.SidebarConfig{
        Path:               "/myplugin",
        Icon:               MyPluginIcon(),     // templ component
        DefaultOrder:       50,                 // Sort order
        RequiresPermission: "myplugin:view",   // Optional permission check
        ShowAlways:         false,              // Show even when disabled
    }
}
```

### Permissions

Plugins declare the permissions they use:

```go
func (p *Plugin) Permissions() []plugin.PermissionDef {
    return []plugin.PermissionDef{
        {
            Component:   "myplugin",
            Actions:     []string{"view", "manage", "delete"},
            Description: "View and manage plugin features",
        },
    }
}
```

### Settings Page

Plugins can provide a settings UI:

```go
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
    return MyPluginSettings(config)  // templ component
}
```

Return `nil` if the plugin has no configurable settings.

---

## Gearbox Agent Plugins

### Plugin Interface

Agent plugins implement the `plugin.Plugin` interface:

```go
type Plugin interface {
    Info() Info                      // Metadata
    Initialize(ctx, deps) error      // Setup
    Start(ctx) error                 // Start background tasks
    Stop(ctx) error                  // Cleanup
    Health() HealthStatus            // Health check
    RegisterRoutes(r chi.Router)     // HTTP API routes
    EventTypes() []EventType         // Events published
}
```

### Collector Plugin

Plugins that collect data periodically also implement `CollectorPlugin`:

```go
type CollectorPlugin interface {
    Plugin
    Collectors() []Collector  // Periodic data collection tasks
}

type Collector struct {
    Name     string                                 // Collector name
    Interval time.Duration                          // Collection interval
    Collect  func(ctx context.Context) (any, error) // Collection function
    OnData   func(data any) error                   // Data handler (publish events)
}
```

### Directory Structure

```
internal/plugins/myplugin/
├── plugin.go      # Plugin definition
├── handlers.go    # HTTP API handlers
├── collector.go   # Data collection logic (if CollectorPlugin)
└── README.md      # Plugin documentation
```

### Registration

```go
package myplugin

import "github.com/sarg3nt/gearbox-agent/internal/framework/plugin"

func init() {
    plugin.Register(&Plugin{})
}

type Plugin struct {
    plugin.BasePlugin
}

func (p *Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "myplugin",
        DisplayName: "My Plugin",
        Description: "Collects data about X",
        Version:     "1.0.0",
        Category:    "monitoring",
        Core:        true,
    }
}
```

### Initialization and Lifecycle

```go
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
        return err
    }
    // Setup plugin state
    return nil
}

func (p *Plugin) Start(ctx context.Context) error {
    // Start background goroutines if needed
    return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
    // Cleanup resources
    return nil
}
```

### Dependencies Available to Plugins

```go
type Dependencies struct {
    Logger             *slog.Logger  // Structured logger
    EventBus           *events.Bus   // Event bus for publishing
    Config             Config        // Agent configuration
    HAProxyStatsSocket string        // HAProxy socket path
    HAProxyStatsURL    string        // HAProxy stats URL
    HAProxyConfigPath  string        // HAProxy config path
    StatsInterval      time.Duration // Stats collection interval
    MetricsInterval    time.Duration // Metrics collection interval
}
```

### Periodic Data Collection

Implement `Collectors()` to define periodic data collection:

```go
func (p *Plugin) Collectors() []plugin.Collector {
    return []plugin.Collector{
        {
            Name:     "myplugin-data",
            Interval: p.Deps().StatsInterval,
            Collect: func(ctx context.Context) (any, error) {
                return p.collectData()
            },
            OnData: func(data any) error {
                return p.publishData(data)
            },
        },
    }
}
```

### Event Publishing

Plugins publish events to notify the dashboard:

```go
func (p *Plugin) publishData(data any) error {
    p.EventBus().Publish(events.Event{
        Type:      "myplugin.updated",
        Timestamp: time.Now(),
        Data: map[string]any{
            "collected_at": time.Now().UTC().Format(time.RFC3339),
            "data":         data,
        },
    })
    return nil
}

func (p *Plugin) EventTypes() []plugin.EventType {
    return []plugin.EventType{
        {
            Name:        "myplugin.updated",
            Description: "Published when data is collected",
            Payload:     "Data object with collected information",
        },
    }
}
```

### HTTP API Routes

Plugins expose REST APIs for the dashboard to query:

```go
func (p *Plugin) RegisterRoutes(r chi.Router) {
    r.Get("/api/v1/myplugin/data", p.handleGetData)
    r.Post("/api/v1/myplugin/action", p.handleAction)
}

func (p *Plugin) handleGetData(w http.ResponseWriter, r *http.Request) {
    data, err := p.collectData()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}
```

---

## Agent Client Communication

### The Agent Client Problem

The `framework/agent` package provides an HTTP client with 80+ methods for calling the gearbox-agent API (stats, certificates, logs, metrics, etc.). This is **shared infrastructure**, not plugin code.

**Challenge:** How do plugins use the agent client without tight coupling?

### Solution: Plugin-Specific Agent Facades

Each client plugin defines an **interface** for the agent methods it needs. This provides:

- **Self-documentation:** Shows exactly what agent APIs the plugin uses
- **Testability:** Easy to mock in tests
- **Isolation:** Changes to the agent client don't affect plugins if their interface doesn't change

### Creating an Agent Facade

**Step 1:** Define your plugin's agent interface in `agent.go`:

```go
// internal/plugins/certificates/agent.go
package certificates

import "github.com/sarg3nt/gearbox/internal/framework/agent"

// AgentClient defines the agent operations needed by the certificates plugin.
type AgentClient interface {
    GetCertificates() (*agent.CertificatesResponse, error)
    RefreshCertificate(domain string) (*agent.RefreshCertificateResponse, error)
    DownloadCertificate(domain string) ([]byte, string, error)
}

// Compile-time check that framework client implements our interface
var _ AgentClient = (*agent.Client)(nil)
```

**Step 2:** Use the interface in your handlers:

```go
// internal/plugins/certificates/handlers.go
type Handlers struct {
    agent AgentClient  // Your interface, not *agent.Client
    deps  plugin.Dependencies
}

func NewHandlers(deps plugin.Dependencies) *Handlers {
    return &Handlers{
        agent: deps.Agent,  // Satisfies AgentClient interface
        deps:  deps,
    }
}
```

**Step 3:** Use it in your code:

```go
func (h *Handlers) GetCertificates(serverID string) {
    certs, err := h.agent.GetCertificates()
    // ...
}
```

### Available Agent Methods

See `internal/framework/agent/client.go` for the complete API. Common categories:

- **HAProxy:** `GetStats()`, `GetInfo()`, `GetMetadata()`, `GetStickTables()`
- **Certificates:** `GetCertificates()`, `RefreshCertificate()`, `DownloadCertificate()`
- **Logs:** `GetLogSources()`, `GetLogs(name, lines)`
- **Metrics:** `GetMetrics()`
- **Services:** `GetServices()`, `ServiceControl()`, `GetAvailableServices()`
- **Security:** `GetSecuritySummary()`, `GetFail2BanStats()`, `GetFirewallStats()`
- **Firewall:** `GetBlockedIPs()`, `BlockIP()`, `UnblockIP()`, `CheckIPBlocked()`
- **Traffic:** `GetTraffic()`, `GetTrafficSummary()`
- **Config Management:** `GetHAProxyConfig()`, `UpdateHAProxyConfig()`, `GetFirewallConfig()`
- **OS Updates:** `GetUpdateStatus()`, `InstallUpdates()`, `GetUpdateHistory()`

### Why Not Move Agent Code Into Plugins?

The agent client is **infrastructure**, like `database/sql` or `http.Client`. It:

- Provides connectivity to gearbox-agent
- Handles authentication, TLS, retries, WebSocket management
- Is used by multiple plugins (stats, metrics, certificates all need it)

Moving it into individual plugins would cause massive code duplication.

---

## Feature Flags

### Overview

Feature flags control which plugins are included in the compiled binary and their default state. This allows gradual rollout of experimental features without affecting production stability.

### Flag States

| State | Build | Default | Visible | Label | Description |
|-------|-------|---------|---------|-------|-------------|
| `disabled` | ❌ Not built | N/A | ❌ No | - | Excluded from binary entirely |
| `alpha` | ✅ Built | ❌ Off | ✅ Yes | 🔬 Alpha | Early development, may change significantly |
| `beta` | ✅ Built | ❌ Off | ✅ Yes | 🧪 Beta | Feature-complete, testing phase |
| `production` | ✅ Built | Varies | ✅ Yes | - | Stable, ready for use |

### Alpha and Beta Labels

Plugins in `alpha` or `beta` state automatically display labels in the UI:

- **Alpha:** 🔬 "This feature is in early development and may change significantly"
- **Beta:** 🧪 "This feature is being tested and may have bugs"

These labels appear as tooltips/popovers when hovering over the plugin name.

### Configuration

**For Gearbox Client:** `gearbox/internal/plugins/<plugin>/plugin.go`

**For Gearbox Agent:** `gearbox-agent/internal/plugins/<plugin>/plugin.go`

Add a feature flag constant at the top of `plugin.go`:

```go
package myplugin

import "github.com/sarg3nt/gearbox/internal/framework/plugin"

// Feature flag: disabled | alpha | beta | production
const featureFlag = "beta"

func init() {
    plugin.RegisterWithFlag(&Plugin{}, featureFlag)
}
```

### Build Tags for Disabled Plugins

Plugins with `featureFlag = "disabled"` use Go build tags to exclude them:

**In `plugin.go`:**

```go
//go:build myplugin

package myplugin

const featureFlag = "disabled"

func init() {
    plugin.RegisterWithFlag(&Plugin{}, featureFlag)
}
```

**Building:**

```bash
# Standard build (excludes myplugin)
go build -o server cmd/server/main.go

# Explicitly enable myplugin
go build -tags myplugin -o server cmd/server/main.go
```

### Docker Builds

**Standard Build (production features only):**

```dockerfile
FROM golang:1.25 AS builder
WORKDIR /app
COPY . .
RUN go build -o gearbox cmd/server/main.go
```

**Build with Alpha/Beta Features:**

```dockerfile
FROM golang:1.25 AS builder
ARG ENABLE_ALPHA=false
ARG ENABLE_BETA=false
WORKDIR /app
COPY . .
RUN if [ "$ENABLE_ALPHA" = "true" ]; then \
        BUILD_TAGS="alpha beta"; \
    elif [ "$ENABLE_BETA" = "true" ]; then \
        BUILD_TAGS="beta"; \
    fi && \
    go build -tags "$BUILD_TAGS" -o gearbox cmd/server/main.go
```

**Using:**

```bash
# Production build
docker build -t gearbox:latest .

# Beta build
docker build --build-arg ENABLE_BETA=true -t gearbox:beta .

# Alpha build (includes alpha + beta)
docker build --build-arg ENABLE_ALPHA=true -t gearbox:alpha .
```

### Default State

Plugins in `alpha` and `beta` are **disabled by default** on first run. Admins must explicitly enable them in settings.

Plugins in `production` can be:

- **Enabled by default:** Set `Core: true` in `Info()` (cannot be disabled)
- **Disabled by default:** Set `Core: false` and handle default state in database migrations

### Migration Path

```
disabled  →  alpha  →  beta  →  production
   ↓          ↓        ↓          ↓
Not built   Built    Built     Built
            Off      Off       On/Off
            🔬       🧪        Stable
```

---

## Creating a New Plugin

### Step 1: Choose Plugin Type

- **Client plugin?** Add to `gearbox/internal/plugins/` (runs in web application)
- **Agent plugin?** Add to `gearbox-agent/internal/plugins/` (runs on servers)

### Step 2: Create Directory Structure

```bash
# Client plugin (web application)
mkdir -p gearbox/internal/plugins/myplugin
cd gearbox/internal/plugins/myplugin

# Agent plugin (server-side)
mkdir -p gearbox-agent/internal/plugins/myplugin
cd gearbox-agent/internal/plugins/myplugin
```

### Step 3: Create Plugin Files

**For Client Plugin:**

```go
// plugin.go
package myplugin

import (
    "context"
    "github.com/a-h/templ"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox/internal/framework/plugin"
)

const featureFlag = "alpha"  // disabled | alpha | beta | production

func init() {
    plugin.RegisterWithFlag(&Plugin{}, featureFlag)
}

type Plugin struct {
    plugin.BasePlugin
    handlers *Handlers
}

func (p *Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "myplugin",
        DisplayName: "My Plugin",
        Description: "What this plugin does",
        Version:     "1.0.0",
        Icon:        "icon-name",
        Category:    "monitoring",
        Core:        false,
    }
}

func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
        return err
    }
    p.handlers = NewHandlers(deps)
    return nil
}

func (p *Plugin) RegisterRoutes(r chi.Router) {
    r.Get("/", p.handlers.IndexPage)
}

func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
    return &plugin.SidebarConfig{
        Path:               "/myplugin",
        Icon:               MyPluginIcon(),
        DefaultOrder:       100,
        RequiresPermission: "myplugin:view",
    }
}

func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
    return nil  // No settings page
}

func (p *Plugin) Permissions() []plugin.PermissionDef {
    return []plugin.PermissionDef{
        {
            Component:   "myplugin",
            Actions:     []string{"view"},
            Description: "View plugin data",
        },
    }
}
```

```go
// handlers.go
package myplugin

import (
    "net/http"
    "github.com/sarg3nt/gearbox/internal/framework/plugin"
)

type Handlers struct {
    deps plugin.Dependencies
}

func NewHandlers(deps plugin.Dependencies) *Handlers {
    return &Handlers{deps: deps}
}

func (h *Handlers) IndexPage(w http.ResponseWriter, r *http.Request) {
    // Check permission
    if !h.deps.Auth.HasPermission(r, "myplugin", "view") {
        http.Error(w, "Forbidden", http.StatusForbidden)
        return
    }

    // Render page
    w.Write([]byte("My Plugin Page"))
}
```

**For Agent Plugin:**

```go
// plugin.go
package myplugin

import (
    "context"
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
)

const featureFlag = "production"

func init() {
    plugin.RegisterWithFlag(&Plugin{}, featureFlag)
}

type Plugin struct {
    plugin.BasePlugin
}

func (p *Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "myplugin",
        DisplayName: "My Plugin",
        Description: "Collects data about X",
        Version:     "1.0.0",
        Category:    "monitoring",
        Core:        true,
    }
}

func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    return p.BasePlugin.Initialize(ctx, deps)
}

func (p *Plugin) Start(ctx context.Context) error {
    return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
    return nil
}

func (p *Plugin) Health() plugin.HealthStatus {
    return plugin.NewHealthyStatus("operational")
}

func (p *Plugin) RegisterRoutes(r chi.Router) {
    r.Get("/api/v1/myplugin/data", p.handleGetData)
}

func (p *Plugin) EventTypes() []plugin.EventType {
    return nil
}

func (p *Plugin) handleGetData(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"status":"ok"}`))
}
```

### Step 4: Import Plugin in Main

The plugin auto-registers via `init()`, but you must import it:

```go
// cmd/server/main.go (gearbox)
package main

import (
    _ "github.com/sarg3nt/gearbox/internal/plugins/myplugin"
    // ... other imports
)
```

```go
// cmd/gearbox-agent/main.go (gearbox-agent)
package main

import (
    _ "github.com/sarg3nt/gearbox-agent/internal/plugins/myplugin"
    // ... other imports
)
```

### Step 5: Build and Test

```bash
# Build
make templ-generate && make build

# Run locally
make dev-local

# Visit http://localhost:3000/myplugin
```

---

## Testing Plugins

### Unit Testing Handlers

```go
// handlers_test.go
package myplugin

import (
    "net/http"
    "net/http/httptest"
    "testing"
    "github.com/sarg3nt/gearbox/internal/framework/plugin"
)

type mockAuth struct{}

func (m *mockAuth) HasPermission(r *http.Request, component, action string) bool {
    return true  // Always allow for testing
}

func TestIndexPage(t *testing.T) {
    deps := plugin.Dependencies{
        Auth: &mockAuth{},
    }

    handlers := NewHandlers(deps)

    req := httptest.NewRequest("GET", "/myplugin", nil)
    w := httptest.NewRecorder()

    handlers.IndexPage(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected status 200, got %d", w.Code)
    }
}
```

### Mocking the Agent Client

```go
// agent_test.go
package myplugin

import "github.com/sarg3nt/gearbox/internal/framework/agent"

type mockAgentClient struct{}

func (m *mockAgentClient) GetCertificates() (*agent.CertificatesResponse, error) {
    return &agent.CertificatesResponse{
        Certificates: []agent.CertificateInfo{
            {Domain: "example.com", DaysUntilExpiry: 30},
        },
    }, nil
}

// Use in tests:
// handlers := NewHandlers(plugin.Dependencies{Agent: &mockAgentClient{}})
```

---

---

## Dashboards and Widgets

### Overview

Gearbox supports **custom dashboards** where users can arrange reusable widgets to create personalized monitoring views. This system allows for maximum flexibility while maintaining consistency and reusability.

**Key Concepts:**

- **Dashboard:** A named, customizable page with a grid layout
- **Widget:** A reusable UI component that displays data (cards, charts, tables, etc.)
- **Widget Library:** Framework-provided widgets that plugins can use
- **Predefined Dashboards:** Plugin-provided dashboard templates users can enable

### Dashboard System Architecture

#### Dashboard Management

**Users can:**

1. Create custom dashboards with unique names
2. Add/remove pre-configured widgets (created by plugin developers) to/from dashboards
3. Rearrange widgets via drag-and-drop
4. Enable predefined plugin dashboards
5. Delete custom dashboards (predefined dashboards can only be disabled)

#### Dashboard Editor Widget Palette

The dashboard editor's widget palette does NOT expose raw building-block widgets. Instead, it shows pre-configured, domain-specific widgets created by plugin developers.

Examples:

- ❌ **NOT in palette**: Raw "status-card" or "collapsible-container" widgets
- ✅ **IN palette**: "HAProxy Status Summary Doughnuts" widget (uses status doughnuts to show frontend/backend/server health)
- ✅ **IN palette**: "Backend Status Grid" widget (uses collapsible containers and status cards to show backend details)
- ✅ **IN palette**: "System Metrics Dashboard" widget (uses metric cards to show CPU, memory, disk)
- ✅ **IN palette**: "Service Status List" widget (uses status badges to show service health)

Why this distinction?

- **Building blocks** (`placement-box`, `status-card`, `metric-card`, etc.) are low-level framework components that widget authors use to BUILD widgets
- **Pre-configured widgets** are complete, ready-to-use components that users can add to dashboards via the editor
- This prevents users from needing to manually configure data sources, field mappings, and styling—plugin developers do this work once, and users simply drag-and-drop the final product

**Default Behavior:**

- First dashboard created is named "Dashboard" (homepage)
- Additional dashboards require custom names
- All dashboards are accessible from the sidebar/navigation

#### Dashboard Storage

Dashboards are stored as **YAML files** in the `data/dashboards/` directory:

```txt
data/
└── dashboards/
    ├── dashboard.yaml           # Default dashboard (homepage)
    ├── my-custom-view.yaml      # User-created dashboard
    ├── plugin-haproxy-overview.yaml  # Predefined from plugin
    └── plugin-security-summary.yaml  # Predefined from plugin
```

**Why YAML?**

- Human-readable and git-friendly
- Easy to edit manually if needed
- Simple to version control
- Easy to ship predefined dashboards in the binary
- Clear structure for widget configuration

#### Dashboard YAML Specification

```yaml
# data/dashboards/my-monitoring-view.yaml
version: "1.0"
name: "My Monitoring View"
description: "Custom dashboard for monitoring critical services"
created_by: "user"  # "user" or "plugin:<plugin-name>"
plugin_name: null   # null for user dashboards, "haproxy" for plugin dashboards
editable: true      # false for plugin predefined dashboards
layout:
  columns: 12       # Grid system (1-12 columns)
  gap: 4            # Gap between widgets (Tailwind spacing units)
widgets:
  - id: "cert-warnings-1"
    type: "alert-banner"
    position:
      row: 1
      column: 1
      width: 12     # full width (spans all 12 columns)
      height: auto
    config:
      data_source: "certificates.warnings"
      severity: "warning"
      dismissible: true

  - id: "backend-status-1"
    type: "card-grid"
    position:
      row: 2
      column: 1
      width: 12
      height: auto
    config:
      data_source: "haproxy.backends"
      card_type: "status-card"
      columns: 4    # 4 cards per row
      collapsible: true
      default_collapsed: false
      filters:
        - type: "health"
          label: "Health Status"
        - type: "text"
          label: "Filter backends..."

  - id: "cpu-chart-1"
    type: "line-chart"
    position:
      row: 3
      column: 1
      width: 6      # half width (2 widgets per row)
      height: 300
    config:
      data_source: "metrics.cpu"
      title: "CPU Usage"
      refresh_interval: 5  # seconds
      time_range: 3600     # last hour in seconds
      y_axis:
        label: "Percentage"
        min: 0
        max: 100

  - id: "memory-chart-1"
    type: "line-chart"
    position:
      row: 3
      column: 7
      width: 6
      height: 300
    config:
      data_source: "metrics.memory"
      title: "Memory Usage"
      refresh_interval: 5
      time_range: 3600
      y_axis:
        label: "Percentage"
        min: 0
        max: 100

  - id: "alerts-table-1"
    type: "data-table"
    position:
      row: 4
      column: 1
      width: 12
      height: auto
    config:
      data_source: "alerts.active"
      title: "Active Alerts"
      columns:
        - field: "timestamp"
          label: "Time"
          sortable: true
        - field: "severity"
          label: "Severity"
          sortable: true
        - field: "message"
          label: "Message"
      filters:
        - type: "select"
          field: "severity"
          label: "Severity"
          options: ["critical", "warning", "info"]
      pagination: true
      page_size: 10
      actions:
        - type: "button"
          label: "Acknowledge"
          action: "acknowledge_alert"
          permission: "alerts:action"
```

#### Dashboard Naming Convention

**User-Created Dashboards:**

- Filename: `{slug}.yaml` where slug is kebab-case version of name
- Examples:
  - "My Dashboard" → `my-dashboard.yaml`
  - "Production Overview" → `production-overview.yaml`
  - "Database Monitoring" → `database-monitoring.yaml`

**Plugin Predefined Dashboards:**

- Filename: `plugin-{plugin-name}-{dashboard-slug}.yaml`
- Examples:
  - HAProxy Overview → `plugin-haproxy-overview.yaml`
  - HAProxy Status Grid → `plugin-haproxy-status-grid.yaml`
  - Security Dashboard → `plugin-security-dashboard.yaml`
  - Traffic Analysis → `plugin-traffic-analysis.yaml`

#### Top Bar Controls

Dashboards can define controls that appear in the page header (top bar). These controls provide filtering, refresh, and other actions that affect the entire dashboard or specific widgets.

##### YAML Schema

```yaml
version: "1.0"
name: "My Dashboard"
# ... other dashboard fields ...

top_bar_controls:
  # Global health filter (affects all widgets)
  - id: "global-health-filter"
    type: "select"
    label: "Status"
    position: "left"  # left, center, or right
    options:
      - value: "all"
        label: "All Status"
        default: true
      - value: "healthy"
        label: "Healthy Only"
      - value: "critical"
        label: "Critical Only"
    action:
      type: "filter"
      target: "all-widgets"  # Apply to all widgets
      filter_key: "health"

  # Disabled items filter
  - id: "global-disabled-filter"
    type: "select"
    label: "Items"
    position: "left"
    options:
      - value: "all"
        label: "All Items"
        default: true
      - value: "active"
        label: "Active Only"
      - value: "disabled"
        label: "Disabled Only"
    action:
      type: "filter"
      target: "all-widgets"
      filter_key: "disabled"

  # Text filter
  - id: "global-text-filter"
    type: "text"
    placeholder: "Filter backends..."
    position: "left"
    width: 160  # px
    action:
      type: "filter"
      target: "all-widgets"
      filter_key: "name"
      match_mode: "contains"  # contains, exact, starts_with, ends_with

  # Manual refresh button
  - id: "refresh-btn"
    type: "button"
    icon: "refresh"
    position: "right"
    tooltip: "Refresh data"
    action:
      type: "reload"
      target: "all-widgets"

  # Server selector (for multi-server dashboards)
  - id: "server-selector"
    type: "select"
    label: "Server"
    position: "left"
    options_source: "servers.list"  # Dynamic options from data source
    action:
      type: "config_update"
      target: "all-widgets"
      update_key: "server_id"

  # Edit dashboard button (only shown if editable: true)
  - id: "edit-dashboard"
    type: "button"
    icon: "edit"
    position: "right"
    tooltip: "Edit Dashboard"
    action:
      type: "navigate"
      url: "/dashboards/{slug}/edit"
    visible_when:
      editable: true
```

##### Control Types

| Type | Description | Fields |
|------|-------------|--------|
| `select` | Dropdown selector | `options`, `options_source` |
| `text` | Text input field | `placeholder`, `width` |
| `button` | Action button | `icon`, `label` |
| `toggle` | On/off switch | `default_state` |
| `date_range` | Date range picker | `default_range` |

##### Action Types

| Type | Description | Target |
|------|-------------|--------|
| `filter` | Filter widget data | `all-widgets`, `widget-id`, `widget-type` |
| `reload` | Reload widget data | `all-widgets`, `widget-id` |
| `config_update` | Update widget config | `all-widgets`, `widget-id` |
| `navigate` | Navigate to URL | N/A |
| `custom` | Custom JavaScript function | N/A |

##### Example: HAProxy Dashboard with Top Bar Controls

```yaml
version: "1.0"
name: "HAProxy Status"
description: "HAProxy monitoring dashboard with filtering"
editable: true

top_bar_controls:
  - id: "health-filter"
    type: "select"
    position: "left"
    options:
      - {value: "all", label: "All Status", default: true}
      - {value: "healthy", label: "Healthy Only"}
      - {value: "critical", label: "Critical Only"}
    action:
      type: "filter"
      target: "all-widgets"
      filter_key: "health"

  - id: "backend-filter"
    type: "text"
    placeholder: "Filter backends..."
    position: "left"
    width: 160
    action:
      type: "filter"
      target: "widget-type:haproxy-backend-grid"
      filter_key: "name"

  - id: "refresh"
    type: "button"
    icon: "refresh"
    position: "right"
    action:
      type: "reload"
      target: "all-widgets"

widgets:
  - id: "backend-grid-1"
    type: "haproxy-backend-grid"
    position: {row: 1, column: 1, width: 12, height: auto}
    config:
      server_id: ""
      default_collapsed: false
```

##### Implementation Notes

- Top bar controls are rendered by the dashboard page template
- JavaScript functions handle filter/reload actions
- Widget components must support the filter keys defined in controls
- Data attributes on widget elements enable filtering (e.g., `data-health`, `data-name`)

**Slug Generation Rules:**

1. Convert to lowercase
2. Replace spaces with hyphens
3. Remove special characters (keep letters, numbers, hyphens)
4. Collapse multiple hyphens to single hyphen
5. Trim leading/trailing hyphens

### Widget Library

The framework provides a comprehensive widget library that plugins can use. All widgets are **data-source agnostic** - they receive data via standard interfaces.

#### Widget Categories

| Category | Purpose | Examples |
|----------|---------|----------|
| **Data Visualization** | Charts and graphs | Line charts, bar charts, doughnuts, pie charts |
| **Data Display** | Tables and lists | Data tables, simple tables, list views |
| **Status & Metrics** | Metrics and health | Status cards, metric cards, health indicators |
| **Containers** | Layout and grouping | Collapsible sections, card containers, tabs |
| **Controls** | User input | Filters, dropdowns, buttons, search boxes |
| **Streaming** | Real-time data | Log viewers, terminal output, live feeds |
| **Actions** | User operations | Action buttons, toolbars, context menus |

#### Core Widget Types

##### 1. Chart Widgets

**Line Chart Widget** (`line-chart`)

```yaml
type: "line-chart"
config:
  data_source: "metrics.cpu"
  title: "CPU Usage Over Time"
  refresh_interval: 5       # Auto-refresh every 5 seconds
  time_range: 3600          # Show last hour
  series:
    - name: "CPU %"
      color: "#3b82f6"
  y_axis:
    label: "Percentage"
    min: 0
    max: 100
  x_axis:
    label: "Time"
    format: "time"          # time, date, datetime, number
  legend: true
  grid: true
  animation: true
```

**Bar Chart Widget** (`bar-chart`)

```yaml
type: "bar-chart"
config:
  data_source: "traffic.requests_by_backend"
  title: "Requests by Backend"
  orientation: "vertical"   # vertical or horizontal
  stacked: false
  colors: ["#3b82f6", "#10b981", "#f59e0b"]
```

**Doughnut Chart Widget** (`doughnut-chart`)

```yaml
type: "doughnut-chart"
config:
  data_source: "haproxy.backend_health"
  title: "Backend Health Distribution"
  center_text: "Backends"
  colors:
    healthy: "#10b981"
    warning: "#f59e0b"
    critical: "#ef4444"
  show_legend: true
  show_labels: true
```

**Pie Chart Widget** (`pie-chart`)

```yaml
type: "pie-chart"
config:
  data_source: "traffic.status_codes"
  title: "HTTP Status Codes"
  colors:
    "2xx": "#10b981"
    "3xx": "#3b82f6"
    "4xx": "#f59e0b"
    "5xx": "#ef4444"
```

##### 2. Card Widgets

**Status Card Widget** (`status-card`)

```yaml
type: "status-card"
config:
  data_source: "haproxy.frontend.{id}"
  title: "{{name}}"                    # Templated from data
  subtitle: "{{hostname}}"
  status:
    field: "health"                    # healthy, warning, critical
    show_indicator: true
  metrics:
    - label: "Sessions"
      field: "current_sessions"
      format: "number"
    - label: "Rate"
      field: "session_rate"
      format: "number"
      suffix: "/s"
  actions:
    - label: "Details"
      link: "/server/{{server_id}}/frontend/{{name}}"
```

**Metric Card Widget** (`metric-card`)

```yaml
type: "metric-card"
config:
  title: "Active Sessions"
  value_source: "haproxy.total_sessions"
  format: "number"              # number, percentage, bytes, duration
  trend:
    enabled: true
    field: "session_change"     # Positive = green up, negative = red down
  threshold:
    warning: 80                 # Show warning color at 80%
    critical: 95                # Show critical color at 95%
  icon: "users"                 # Icon name
  color: "blue"                 # blue, green, yellow, red, gray
```

**Collapsible Card Container** (`collapsible-container`)

```yaml
type: "collapsible-container"
config:
  title: "Backend Servers"
  subtitle: "{{count}} backends"
  default_collapsed: false
  persist_state: true           # Remember collapsed state
  badge:
    text: "{{count}}"
    color: "blue"
  widgets:
    # Nested widgets go here
    - type: "status-card"
      # ... config
```

##### 3. Table Widgets

**Data Table Widget** (`data-table`)

```yaml
type: "data-table"
config:
  data_source: "alerts.all"
  title: "Alert History"
  columns:
    - field: "timestamp"
      label: "Time"
      sortable: true
      format: "datetime"
      width: "180px"
    - field: "severity"
      label: "Severity"
      sortable: true
      format: "badge"           # Renders as colored badge
      width: "100px"
    - field: "message"
      label: "Message"
      sortable: false
    - field: "actions"
      label: "Actions"
      format: "actions"         # Renders action buttons
      width: "120px"
  sorting:
    default_field: "timestamp"
    default_order: "desc"       # asc or desc
  filtering:
    enabled: true
    fields: ["severity", "message"]
  pagination:
    enabled: true
    page_size: 25
    page_size_options: [10, 25, 50, 100]
  search:
    enabled: true
    placeholder: "Search alerts..."
    fields: ["message"]         # Fields to search
  row_actions:
    - label: "View"
      action: "view_alert"
      icon: "eye"
    - label: "Acknowledge"
      action: "acknowledge_alert"
      icon: "check"
      permission: "alerts:action"
  selection:
    enabled: true               # Allow row selection
    mode: "multiple"            # single or multiple
  export:
    enabled: true
    formats: ["csv", "json"]
```

**Simple Table Widget** (`simple-table`)

```yaml
type: "simple-table"
config:
  data_source: "certificates.list"
  columns:
    - field: "domain"
      label: "Domain"
    - field: "expiry"
      label: "Expires"
      format: "date"
    - field: "days_remaining"
      label: "Days Left"
      format: "number"
  striped: true                 # Alternating row colors
  hover: true                   # Highlight on hover
  compact: false                # Compact spacing
```

##### 4. Control Widgets

**IMPORTANT: Control widgets (filters, dropdowns, search boxes, refresh controls) are NOT exposed directly to dashboard designers.**

Control widgets are embedded within data-display widgets that subscribe to streaming data. Widget authors decide where controls belong:

- **In the widget panel** - For widget-specific controls (filtering, searching within that widget's data)
- **In the page header** - For page-level controls (refresh, global filters)

Dashboard designers should never need to manually add a refresh control or filter widget. These are built into widgets that need them.

**Example: A traffic widget that needs filtering might include its own filter controls in its panel header:**

```yaml
type: "traffic-table"
config:
  data_source: "traffic.requests"
  # Widget internally renders its own filter controls
  filters:
    - type: "select"
      label: "Status Code"
      options: ["all", "2xx", "3xx", "4xx", "5xx"]
```

**Example: A widget that subscribes to streaming data includes refresh controls in the page header:**

```yaml
type: "log-viewer"
config:
  data_source: "logs.haproxy"
  # Widget author decides to show refresh control in page header
  # Dashboard designer doesn't need to add a separate refresh-control widget
```

##### 5. Streaming Widgets

**Log Viewer Widget** (`log-viewer`)

```yaml
type: "log-viewer"
config:
  data_source: "logs.haproxy"
  title: "HAProxy Logs"
  height: 500                   # pixels
  auto_scroll: true
  line_numbers: true
  syntax_highlight: false
  filters:
    - type: "select"
      label: "Log Level"
      field: "level"
      options: ["all", "error", "warning", "info", "debug"]
  controls:
    - type: "button"
      label: "Clear"
      action: "clear_logs"
    - type: "button"
      label: "Download"
      action: "download_logs"
    - type: "button"
      label: "Copy"
      action: "copy_logs"
  max_lines: 1000               # Buffer limit
  font_family: "monospace"
```

**Terminal Output Widget** (`terminal-output`)

```yaml
type: "terminal-output"
config:
  title: "APT Update Output"
  data_source: "apt.stream"
  height: 400
  auto_scroll: true
  show_timestamp: true
  controls:
    - type: "button"
      label: "Maximize"
      action: "maximize"
    - type: "button"
      label: "Copy"
      action: "copy_output"
```

##### 6. Action Widgets

**Button Widget** (`button`)

```yaml
type: "button"
config:
  label: "Refresh Certificate"
  action: "refresh_certificate"
  style: "primary"              # primary, secondary, success, warning, danger
  size: "medium"                # small, medium, large
  icon: "refresh"
  confirmation:
    enabled: true
    message: "Are you sure you want to refresh this certificate?"
  permission: "certificates:action"
```

**Button Group Widget** (`button-group`)

```yaml
type: "button-group"
config:
  buttons:
    - label: "Start"
      action: "start_service"
      style: "success"
      icon: "play"
    - label: "Stop"
      action: "stop_service"
      style: "danger"
      icon: "stop"
    - label: "Restart"
      action: "restart_service"
      style: "warning"
      icon: "refresh"
  orientation: "horizontal"     # horizontal or vertical
  permission: "services:manage"
```

**Toolbar Widget** (`toolbar`)

```yaml
type: "toolbar"
config:
  position: "top-right"
  items:
    - type: "button"
      label: "Export"
      icon: "download"
      action: "export_data"
    - type: "separator"
    - type: "dropdown"
      label: "Actions"
      items:
        - label: "Acknowledge All"
          action: "ack_all"
        - label: "Clear Resolved"
          action: "clear_resolved"
```

##### 7. Layout Widgets

**Placement Box** (`placement-box`)

The fundamental building block for dashboard layouts. All widgets are wrapped in placement boxes.

```yaml
type: "placement-box"
config:
  width: "full"                 # small (3 cols), medium (6 cols), full (12 cols)
  collapsible: false            # Cannot collapse (use collapsible-container instead)
  border: true
  shadow: true
  padding: "normal"             # none, small, normal, large
  background: "white"           # white, gray, transparent
  widgets:
    # Nested widgets (typically just one per box)
    - type: "status-card"
      # ... config
```

**Tab Container Widget** (`tab-container`)

```yaml
type: "tab-container"
config:
  tabs:
    - id: "overview"
      label: "Overview"
      icon: "dashboard"
      widgets:
        # Widgets for this tab
    - id: "details"
      label: "Details"
      icon: "list"
      widgets:
        # Widgets for this tab
  default_tab: "overview"
  persist_active_tab: true
```

**Grid Container Widget** (`grid-container`)

```yaml
type: "grid-container"
config:
  columns: 3                    # Number of columns
  gap: 4                        # Tailwind spacing units
  auto_flow: "row"              # row or column
  widgets:
    # Child widgets auto-arranged in grid
```

##### 8. Specialized Widgets

**Alert Banner Widget** (`alert-banner`)

```yaml
type: "alert-banner"
config:
  data_source: "certificates.expiring_soon"
  severity: "warning"           # info, warning, error, success
  dismissible: true
  icon: "alert-triangle"
  message_template: "{{count}} certificates expire within 7 days"
  show_when: "has_data"         # always, has_data, custom_condition
```

**Progress Bar Widget** (`progress-bar`)

```yaml
type: "progress-bar"
config:
  data_source: "updates.progress"
  label: "Installing Updates"
  show_percentage: true
  show_status_text: true
  color: "blue"
  animated: true
```

**Container Diagram Widget** (`container-diagram`)

```yaml
type: "container-diagram"
config:
  data_source: "haproxy.containers"
  layout: "tree"                # tree, list, grid
  show_dependencies: true
  show_network_mode: true
  interactive: true
```

### Plugin Widget Registration

Plugins can register both **widgets** and **predefined dashboards**.

#### Registering Widgets

```go
// internal/plugins/myplugin/plugin.go

func (p *Plugin) Widgets() []plugin.WidgetDefinition {
    return []plugin.WidgetDefinition{
        {
            Type:        "my-custom-widget",
            Name:        "Custom Metric Display",
            Description: "Displays custom metrics in a unique way",
            Category:    "data-visualization",
            ConfigSchema: plugin.WidgetConfigSchema{
                // JSON schema for widget configuration
                Properties: map[string]any{
                    "metric_type": map[string]any{
                        "type": "string",
                        "enum": []string{"cpu", "memory", "disk"},
                    },
                    "color": map[string]any{
                        "type": "string",
                        "default": "blue",
                    },
                },
                Required: []string{"metric_type"},
            },
            Template:     MyCustomWidgetTemplate, // templ.Component
            JavaScript:   "my-custom-widget.js",  // Optional JS file
        },
    }
}
```

#### Registering Predefined Dashboards

```go
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
    return []plugin.DashboardDefinition{
        {
            Name:        "HAProxy Overview",
            Description: "Comprehensive HAProxy monitoring dashboard",
            Slug:        "haproxy-overview",  // Becomes plugin-haproxy-overview.yaml
            Icon:        "dashboard",
            DefaultEnabled: true,
            YAML:        haproxyOverviewDashboardYAML,  // Embedded YAML string
        },
        {
            Name:        "HAProxy Status Grid",
            Description: "Grid view of all backend statuses",
            Slug:        "haproxy-status-grid",
            Icon:        "grid",
            DefaultEnabled: false,
            YAML:        haproxyStatusGridDashboardYAML,
        },
    }
}
```

### Dashboard Compilation and Deployment

**Development Workflow:**

1. Create dashboard YAML file
2. Test in development environment
3. Commit YAML to `data/dashboards/` (gitignored for user dashboards)
4. For plugin dashboards: embed YAML in plugin code

**Build Process:**

```go
// Predefined dashboards are embedded in the binary
//go:embed dashboards/*.yaml
var predefinedDashboards embed.FS

// On first run or plugin enable, write to data/dashboards/
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    for _, dashboard := range p.PredefinedDashboards() {
        path := filepath.Join("data/dashboards",
            fmt.Sprintf("plugin-%s-%s.yaml", p.Info().Name, dashboard.Slug))

        // Only write if doesn't exist or plugin version is newer
        if shouldWriteDashboard(path, p.Info().Version) {
            os.WriteFile(path, []byte(dashboard.YAML), 0644)
        }
    }
    return nil
}
```

### Widget Data Sources

Widgets receive data through **standardized data sources**. Plugins expose data sources that widgets can consume.

#### Data Source Interface

```go
type DataSource interface {
    // Identifier (e.g., "haproxy.backends", "metrics.cpu")
    Name() string

    // Fetch data for the widget
    Fetch(ctx context.Context, params map[string]any) (any, error)

    // Subscribe to real-time updates (optional)
    Subscribe(ctx context.Context, handler func(data any)) error

    // Data schema for validation and documentation
    Schema() DataSourceSchema
}
```

#### Registering Data Sources

```go
func (p *Plugin) DataSources() []plugin.DataSource {
    return []plugin.DataSource{
        {
            Name:        "haproxy.backends",
            Description: "List of HAProxy backends with current status",
            Schema: plugin.DataSourceSchema{
                Type: "array",
                Items: map[string]any{
                    "name": "string",
                    "status": "string",
                    "sessions": "number",
                },
            },
            FetchFunc: p.fetchBackends,
            StreamFunc: p.streamBackends,  // Optional
        },
    }
}
```

### Dashboard UI

**Dashboard Selector:**

- Sidebar shows all enabled dashboards
- First dashboard is the homepage (/)
- Additional dashboards at /dashboards/{slug}

**Dashboard Editor:**

- Drag widgets from sidebar palette
- Resize widgets by dragging edges
- Rearrange via drag-and-drop
- Configure widget settings via modal
- Save/cancel changes

**Dashboard Management UI:**

The Dashboard Manager provides a comprehensive interface for managing both user-created and plugin-provided dashboards. Access it via Settings > Dashboards (route: `/settings/dashboards`).

**Features:**

- **Quick Actions:**
  - "Create New Dashboard" - Start building a custom dashboard from scratch
  - "Browse Templates" - View predefined dashboard templates from plugins

- **My Dashboards Tab:**
  - Lists all user-created custom dashboards
  - Drag-to-reorder functionality to set dashboard priority
  - Quick action buttons for each dashboard:
    - View - Opens the dashboard
    - Edit - Opens the visual editor
    - Delete - Removes the dashboard (editable dashboards only)
  - Empty state message when no dashboards exist

- **Plugin Dashboards Tab:**
  - Lists all predefined dashboards provided by plugins
  - Enable/disable toggle for each dashboard
  - Plugin badge shows which plugin provides each dashboard
  - Cannot delete (can only disable/enable)
  - Empty state message when no plugin dashboards available

**Implementation:**

- Location: [dashboard_manager.templ](../gearbox/internal/framework/templates/pages/dashboard_manager.templ)
- Handler: [dashboard_manager.go](../gearbox/internal/framework/handler/dashboard_manager.go)
- Data types: `dashboard.DashboardManagerData`, `dashboard.PluginDashboardInfo`
- Uses SortableJS for drag-and-drop reordering
- Responsive design with Tailwind CSS
- Dark mode support

**API Endpoints:**

- `GET /settings/dashboards` - Dashboard manager page
- `POST /api/dashboards/order` - Save dashboard order
- `POST /api/dashboards/{slug}/toggle` - Enable/disable plugin dashboard

### Implementation Phases

**Phase 1: Foundation (v1.0)**

- Dashboard YAML storage
- Widget library (core widgets)
- Dashboard renderer
- Basic drag-and-drop editor
- Plugin registration API

**Phase 2: Advanced Widgets (v1.1)**

- Chart widgets with Chart.js
- Advanced tables with sorting/filtering
- Streaming widgets
- Container diagrams

**Phase 3: Dashboard Sharing (v1.2)**

- Export/import dashboards
- Dashboard templates
- Community dashboard gallery
- Version control integration

**Phase 4: Advanced Features (v2.0)**

- Dashboard variables (e.g., $server, $timeRange)
- Cross-widget interactions
- Dashboard-level permissions
- Mobile-responsive layouts


## UI Components

The framework provides reusable UI components that plugins should use for consistency.

### Dialog Components

**Location:** `gearbox/internal/framework/templates/components/dialog.templ`

Always use these custom dialog components instead of browser `alert()`, `confirm()`, or `prompt()`.

**AlertDialog** - Simple message with OK button:

```templ
@components.AlertDialog("my-alert", "Success", "Operation completed successfully")
```

**ConfirmDialog** - Confirmation with Cancel/Confirm buttons:

```templ
@components.ConfirmDialog(
    "delete-confirm",
    "Confirm Delete",
    "Are you sure you want to delete this item?",
    "Delete",      // Confirm button text
    "Cancel",      // Cancel button text
    true          // true = danger style (red), false = primary style (blue)
)
```

**Dialog** - Custom content with optional Cancel button:

```templ
@components.Dialog("my-dialog", "Edit Settings", true) {
    // Your custom content here
    <p>Dialog body content</p>
}
```

**Usage in JavaScript:**

```javascript
// Show dialog
document.getElementById('my-dialog').querySelector('[x-data]').__x.$data.show = true;

// Hide dialog
document.getElementById('my-dialog').querySelector('[x-data]').__x.$data.show = false;

// Or use Alpine.js magic:
Alpine.store('myDialog', { show: false });
// Then in templ: x-show="$store.myDialog.show"
```

**Why use custom dialogs?**

- Consistent styling with dark mode support
- Non-blocking (don't freeze the page)
- Better UX with animations and transitions
- Keyboard navigation (ESC to close)
- Mobile-friendly
- Customizable

### Other Components

**Toast Notifications** - `components/toast.templ`

```templ
@components.Toast("Success!", "success")
@components.ToastOnLoad("Page loaded successfully", "success")
```

**Settings Components** - `components/settings.templ`

```templ
@components.SettingsSection("Section Title", "Description") {
    @components.SettingsTextInput("field-id", "Label", "Placeholder", "Help text", "value")
    @components.SettingsCheckbox("enabled", "Enable feature", "Help text", true)
}
```

---

## Examples

### Example: Simple Client Plugin

```go
// internal/plugins/status/plugin.go
package status

import (
    "context"
    "net/http"
    "github.com/a-h/templ"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox/internal/framework/plugin"
)

const featureFlag = "production"

func init() {
    plugin.RegisterWithFlag(&Plugin{}, featureFlag)
}

type Plugin struct {
    plugin.BasePlugin
}

func (p *Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "status",
        DisplayName: "Status",
        Description: "System status overview",
        Version:     "1.0.0",
        Category:    "core",
        Core:        true,
    }
}

func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    return p.BasePlugin.Initialize(ctx, deps)
}

func (p *Plugin) RegisterRoutes(r chi.Router) {
    r.Get("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Status: OK"))
    })
}

func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
    return &plugin.SidebarConfig{
        Path:         "/status",
        DefaultOrder: 5,
        ShowAlways:   true,
    }
}

func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
    return nil
}

func (p *Plugin) Permissions() []plugin.PermissionDef {
    return nil  // Public
}
```

### Example: Agent Collector Plugin

```go
// internal/plugins/uptime/plugin.go (gearbox-agent)
package uptime

import (
    "context"
    "encoding/json"
    "net/http"
    "os/exec"
    "strings"
    "time"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox-agent/internal/framework/events"
    "github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
)

const featureFlag = "production"

func init() {
    plugin.RegisterWithFlag(&Plugin{}, featureFlag)
}

type Plugin struct {
    plugin.BasePlugin
}

func (p *Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "uptime",
        DisplayName: "System Uptime",
        Description: "Tracks system uptime",
        Version:     "1.0.0",
        Category:    "monitoring",
        Core:        false,
    }
}

func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
    return p.BasePlugin.Initialize(ctx, deps)
}

func (p *Plugin) Start(ctx context.Context) error {
    return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
    return nil
}

func (p *Plugin) Health() plugin.HealthStatus {
    return plugin.NewHealthyStatus("operational")
}

func (p *Plugin) Collectors() []plugin.Collector {
    return []plugin.Collector{
        {
            Name:     "uptime",
            Interval: 60 * time.Second,
            Collect:  p.collectUptime,
            OnData:   p.publishUptime,
        },
    }
}

func (p *Plugin) collectUptime() (any, error) {
    out, err := exec.Command("uptime", "-p").Output()
    if err != nil {
        return nil, err
    }
    return strings.TrimSpace(string(out)), nil
}

func (p *Plugin) publishUptime(data any) error {
    p.EventBus().Publish(events.Event{
        Type:      "uptime.updated",
        Timestamp: time.Now(),
        Data: map[string]any{
            "uptime": data,
        },
    })
    return nil
}

func (p *Plugin) RegisterRoutes(r chi.Router) {
    r.Get("/api/v1/uptime", func(w http.ResponseWriter, r *http.Request) {
        uptime, _ := p.collectUptime()
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{"uptime": uptime})
    })
}

func (p *Plugin) EventTypes() []plugin.EventType {
    return []plugin.EventType{
        {
            Name:        "uptime.updated",
            Description: "System uptime collected",
            Payload:     "uptime string",
        },
    }
}

var (
    _ plugin.Plugin          = (*Plugin)(nil)
    _ plugin.CollectorPlugin = (*Plugin)(nil)
)
```

---

## Summary

### Key Takeaways

1. **Plugins are compiled into the binary** - No runtime loading, full type safety
2. **Framework provides infrastructure** - Plugins implement features
3. **Agent facades prevent coupling** - Plugins define their own agent interfaces
4. **Feature flags control rollout** - Disabled, alpha, beta, production states
5. **Both apps use the same pattern** - Gearbox and gearbox-agent have parallel plugin systems

### When to Create a Plugin

Create a plugin when you want to:

- Add a new monitoring page to the web application (client plugin)
- Collect new types of data on servers (agent plugin)
- Expose new agent APIs (agent plugin)
- Add a feature that can be enabled/disabled per server

### When NOT to Create a Plugin

Don't create a plugin for:

- Shared infrastructure (auth, database, HTTP client)
- Cross-cutting concerns (logging, metrics)
- Framework modifications

### Next Steps

- See [examples/](#examples) for complete plugin implementations
- Check existing plugins in `internal/plugins/` for patterns
- Read [TASKS.md](../TASKS.md) for planned plugin features

---

**Last Updated:** 2026-01-31 | **Document Version:** 0.2.0
