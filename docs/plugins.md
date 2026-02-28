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

**Why "Client" instead of "Dashboard"?**

The web application is the **client** that connects to agents. Using "client plugin" avoids confusion with the `dashboard` plugin (one specific plugin that shows the overview page).

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

**Terminology Note:** These are called "client plugins" to distinguish them from the `dashboard` plugin (which provides the main overview page).

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
