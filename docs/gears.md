# Gear Architecture

**Version:** 0.2.0 | **Last Updated:** 2026-01-31

> **Terminology:** In Gearbox, we call modular components **"gears"** — our term for what are traditionally called plugins.

Gearbox uses a compile-time gear architecture for both the **gearbox** (client) and **gearbox-agent** (server agent) applications. This document describes the gear system, how to create gears, and how feature flags control gear availability.

---

## Table of Contents

- [Overview](#overview)
- [Terminology](#terminology)
- [Architecture Principles](#architecture-principles)
- [Gear Types](#gear-types)
- [Gearbox Client Gears](#gearbox-client-gears)
- [Gearbox Agent Gears](#gearbox-agent-gears)
- [Agent Client Communication](#agent-client-communication)
- [Feature Flags](#feature-flags)
- [Creating a New Gear](#creating-a-new-gear)
- [Testing Gears](#testing-gears)
- [Examples](#examples)

---

## Overview

### What is a Gear?

A **gear** is a self-contained module that provides specific functionality to the Gearbox system. Gears:

- Register themselves at compile time via `init()` functions
- Implement a common `Gear` interface
- Can define HTTP routes, permissions, UI pages, and background tasks
- Are isolated from other gears but share framework services

### Why Gears?

- **Modularity:** Features are cleanly separated into independent modules
- **Extensibility:** New functionality can be added without modifying the core
- **Maintainability:** Each gear owns its domain logic
- **Testability:** Gears can be tested in isolation
- **Feature Management:** Gears can be enabled/disabled per server

---

## Terminology

To avoid confusion, this document uses the following terms consistently:

| Term | Meaning | Location |
|------|---------|----------|
| **Gearbox** | The entire monitoring system (both client and agent) | - |
| **Gearbox Client** | The web application (`gearbox`) that admins use | Runs on admin's machine or server |
| **Gearbox Agent** | The server agent (`gearbox-agent`) that collects data | Runs on monitored servers |
| **Client Gear** | A gear for the web application | `gearbox/internal/gears/` |
| **Agent Gear** | A gear for the server agent | `gearbox-agent/internal/gears/` |
| **Dashboard Gear** | The specific gear named "dashboard" (main overview page) | `gearbox/internal/gears/dashboard/` |

**Why "Client" instead of "Dashboard"?**

The web application is the **client** that connects to agents. Using "client gear" avoids confusion with the `dashboard` gear (one specific gear that shows the overview page).

---

## Architecture Principles

### Compile-Time Gears

Gearbox uses **compile-time gears** (similar to Caddy) rather than runtime dynamic loading:

- Gears are compiled into the binary
- No runtime gear loading or shared libraries
- Type safety and performance of static compilation
- Easier to reason about and debug

### Framework vs Gears

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
│   │   ├── gear/          # Gear system
│   │   ├── services/      # Shared services
│   │   └── templates/     # Shared templates
│   │
│   └── gears/             # Feature implementations
│       ├── alerts/
│       ├── certificates/
│       ├── dashboard/
│       ├── logs/
│       ├── metrics/
│       ├── services/
│       └── traffic/
```

**Key principle:** The `framework` provides infrastructure and services. The `gears` implement features.

### Gear Isolation

Gears are isolated from each other but can:

- Access framework services via `Dependencies`
- Publish/subscribe to events via the event bus
- Register HTTP routes under their namespace
- Define permissions and UI components

Gears **cannot** directly call methods on other gears.

---

## Gear Types

### Gearbox Client Gears

Client gears run in the **gearbox** web application (the monitoring client) and provide UI pages and API endpoints for monitoring and managing servers.

**Location:** `gearbox/internal/gears/`

**Purpose:** Render UI pages, handle user interactions, display data from agents

**Terminology Note:** These are called "client gears" to distinguish them from the `dashboard` gear (which provides the main overview page).

**Examples:**

- `dashboard` - Main monitoring overview page (the homepage)
- `certificates` - SSL/TLS certificate monitoring page
- `logs` - Log viewing and analysis page
- `traffic` - Traffic analysis and visualization page
- `alerts` - Alert management page
- `services` - Service status and control page
- `metrics` - System metrics and history page

### Gearbox Agent Gears

Agent gears run on monitored servers (in the **gearbox-agent** application) and collect data, expose APIs, and respond to management commands.

**Location:** `gearbox-agent/internal/gears/`

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

## Gearbox Client Gears

Client gears run in the gearbox web application and provide full-page UIs for specific features.

### Gear Interface

Client gears implement the `gear.Gear` interface:

```go
type Gear interface {
    Info() Info                          // Metadata
    Initialize(ctx, deps) error          // Setup
    RegisterRoutes(r chi.Router)         // HTTP routes
    SidebarItem() *SidebarConfig         // Navigation
    SettingsPage(config) templ.Component // Settings UI
    Permissions() []PermissionDef        // Required permissions
}
```

### Directory Structure

```txt
internal/gears/mygear/
├── gear.go        # Gear definition and interface implementation
├── handlers.go    # HTTP request handlers
├── icons.go       # SVG icons for UI
├── agent.go       # Agent client interface (if needed)
├── pages.templ    # Templ templates for pages
└── README.md      # Gear documentation
```

### Registration

Gears register themselves in `gear.go` via `init()`:

```go
package mygear

import "github.com/sarg3nt/gearbox/internal/framework/gear"

func init() {
    gear.Register(&Gear{})
}

type Gear struct {
    gear.BaseGear
    handlers *Handlers
}

func (g *Gear) Info() gear.Info {
    return gear.Info{
        Name:        "mygear",
        DisplayName: "My Gear",
        Description: "What this gear does",
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
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
    if err := g.BaseGear.Initialize(ctx, deps); err != nil {
        return err
    }

    g.handlers = NewHandlers(deps)
    return nil
}
```

### Dependencies Available to Gears

```go
type Dependencies struct {
    DB         *sql.DB           // Database connection
    Logger     *slog.Logger      // Structured logger
    EventHub   EventPublisher    // Event bus
    Auth       AuthChecker       // Auth and permissions
    Agent      *agent.Client     // Agent API client
    Servers    ServerRegistry    // Server configurations
    HTTPClient *http.Client      // HTTP client for external requests
    Config     map[string]any    // Gear configuration from DB
}
```

### Route Registration

Gears register HTTP routes that are mounted under their path:

```go
func (g *Gear) RegisterRoutes(r chi.Router) {
    // Mounted at /mygear/
    r.Get("/", g.handlers.IndexPage)
    r.Get("/details/{id}", g.handlers.DetailsPage)
}
```

**Note:** API routes typically remain in `framework/handler` for now and access collectors directly. This is a transitional state.

### Sidebar Configuration

Gears define their sidebar navigation item:

```go
func (g *Gear) SidebarItem() *gear.SidebarConfig {
    return &gear.SidebarConfig{
        Path:               "/mygear",
        Icon:               MyGearIcon(),       // templ component
        DefaultOrder:       50,                 // Sort order
        RequiresPermission: "mygear:view",     // Optional permission check
        ShowAlways:         false,              // Show even when disabled
    }
}
```

### Permissions

Gears declare the permissions they use:

```go
func (g *Gear) Permissions() []gear.PermissionDef {
    return []gear.PermissionDef{
        {
            Component:   "mygear",
            Actions:     []string{"view", "manage", "delete"},
            Description: "View and manage gear features",
        },
    }
}
```

### Settings Page

Gears can provide a settings UI:

```go
func (g *Gear) SettingsPage(config map[string]any) templ.Component {
    return MyGearSettings(config)  // templ component
}
```

Return `nil` if the gear has no configurable settings.

---

## Gearbox Agent Gears

### Gear Interface

Agent gears implement the `gear.Gear` interface:

```go
type Gear interface {
    Info() Info                      // Metadata
    Initialize(ctx, deps) error      // Setup
    Start(ctx) error                 // Start background tasks
    Stop(ctx) error                  // Cleanup
    Health() HealthStatus            // Health check
    RegisterRoutes(r chi.Router)     // HTTP API routes
    EventTypes() []EventType         // Events published
}
```

### Collector Gear

Gears that collect data periodically also implement `CollectorGear`:

```go
type CollectorGear interface {
    Gear
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

```txt
internal/gears/mygear/
├── gear.go        # Gear definition
├── handlers.go    # HTTP API handlers
├── collector.go   # Data collection logic (if CollectorGear)
└── README.md      # Gear documentation
```

### Registration

```go
package mygear

import "github.com/sarg3nt/gearbox-agent/internal/framework/gear"

func init() {
    gear.Register(&Gear{})
}

type Gear struct {
    gear.BaseGear
}

func (g *Gear) Info() gear.Info {
    return gear.Info{
        Name:        "mygear",
        DisplayName: "My Gear",
        Description: "Collects data about X",
        Version:     "1.0.0",
        Category:    "monitoring",
        Core:        true,
    }
}
```

### Initialization and Lifecycle

```go
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
    if err := g.BaseGear.Initialize(ctx, deps); err != nil {
        return err
    }
    // Setup gear state
    return nil
}

func (g *Gear) Start(ctx context.Context) error {
    // Start background goroutines if needed
    return nil
}

func (g *Gear) Stop(ctx context.Context) error {
    // Cleanup resources
    return nil
}
```

### Dependencies Available to Gears

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
func (g *Gear) Collectors() []gear.Collector {
    return []gear.Collector{
        {
            Name:     "mygear-data",
            Interval: g.Deps().StatsInterval,
            Collect: func(ctx context.Context) (any, error) {
                return g.collectData()
            },
            OnData: func(data any) error {
                return g.publishData(data)
            },
        },
    }
}
```

### Event Publishing

Gears publish events to notify the dashboard:

```go
func (g *Gear) publishData(data any) error {
    g.EventBus().Publish(events.Event{
        Type:      "mygear.updated",
        Timestamp: time.Now(),
        Data: map[string]any{
            "collected_at": time.Now().UTC().Format(time.RFC3339),
            "data":         data,
        },
    })
    return nil
}

func (g *Gear) EventTypes() []gear.EventType {
    return []gear.EventType{
        {
            Name:        "mygear.updated",
            Description: "Published when data is collected",
            Payload:     "Data object with collected information",
        },
    }
}
```

### HTTP API Routes

Gears expose REST APIs for the dashboard to query:

```go
func (g *Gear) RegisterRoutes(r chi.Router) {
    r.Get("/api/v1/mygear/data", g.handleGetData)
    r.Post("/api/v1/mygear/action", g.handleAction)
}

func (g *Gear) handleGetData(w http.ResponseWriter, r *http.Request) {
    data, err := g.collectData()
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

The `framework/agent` package provides an HTTP client with 80+ methods for calling the gearbox-agent API (stats, certificates, logs, metrics, etc.). This is **shared infrastructure**, not gear code.

**Challenge:** How do gears use the agent client without tight coupling?

### Solution: Gear-Specific Agent Facades

Each client gear defines an **interface** for the agent methods it needs. This provides:

- **Self-documentation:** Shows exactly what agent APIs the gear uses
- **Testability:** Easy to mock in tests
- **Isolation:** Changes to the agent client don't affect gears if their interface doesn't change

### Creating an Agent Facade

**Step 1:** Define your gear's agent interface in `agent.go`:

```go
// internal/gears/certificates/agent.go
package certificates

import "github.com/sarg3nt/gearbox/internal/framework/agent"

// AgentClient defines the agent operations needed by the certificates gear.
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
// internal/gears/certificates/handlers.go
type Handlers struct {
    agent AgentClient  // Your interface, not *agent.Client
    deps  gear.Dependencies
}

func NewHandlers(deps gear.Dependencies) *Handlers {
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

### Why Not Move Agent Code Into Gears?

The agent client is **infrastructure**, like `database/sql` or `http.Client`. It:

- Provides connectivity to gearbox-agent
- Handles authentication, TLS, retries, WebSocket management
- Is used by multiple gears (stats, metrics, certificates all need it)

Moving it into individual gears would cause massive code duplication.

---

## Feature Flags

### Overview

Feature flags control which gears are included in the compiled binary and their default state. This allows gradual rollout of experimental features without affecting production stability.

### Flag States

| State | Build | Default | Visible | Label | Description |
|-------|-------|---------|---------|-------|-------------|
| `disabled` | ❌ Not built | N/A | ❌ No | - | Excluded from binary entirely |
| `alpha` | ✅ Built | ❌ Off | ✅ Yes | 🔬 Alpha | Early development, may change significantly |
| `beta` | ✅ Built | ❌ Off | ✅ Yes | 🧪 Beta | Feature-complete, testing phase |
| `production` | ✅ Built | Varies | ✅ Yes | - | Stable, ready for use |

### Alpha and Beta Labels

Gears in `alpha` or `beta` state automatically display labels in the UI:

- **Alpha:** 🔬 "This feature is in early development and may change significantly"
- **Beta:** 🧪 "This feature is being tested and may have bugs"

These labels appear as tooltips/popovers when hovering over the gear name.

### Configuration

**For Gearbox Client:** `gearbox/internal/gears/<gear>/gear.go`

**For Gearbox Agent:** `gearbox-agent/internal/gears/<gear>/gear.go`

Add a feature flag constant at the top of `gear.go`:

```go
package mygear

import "github.com/sarg3nt/gearbox/internal/framework/gear"

// Feature flag: disabled | alpha | beta | production
const featureFlag = "beta"

func init() {
    gear.RegisterWithFlag(&Gear{}, featureFlag)
}
```

### Build Tags for Disabled Gears

Gears with `featureFlag = "disabled"` use Go build tags to exclude them:

**In `gear.go`:**

```go
//go:build mygear

package mygear

const featureFlag = "disabled"

func init() {
    gear.RegisterWithFlag(&Gear{}, featureFlag)
}
```

**Building:**

```bash
# Standard build (excludes mygear)
go build -o server cmd/server/main.go

# Explicitly enable mygear
go build -tags mygear -o server cmd/server/main.go
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

Gears in `alpha` and `beta` are **disabled by default** on first run. Admins must explicitly enable them in settings.

Gears in `production` can be:

- **Enabled by default:** Set `Core: true` in `Info()` (cannot be disabled)
- **Disabled by default:** Set `Core: false` and handle default state in database migrations

### Migration Path

```txt
disabled  →  alpha  →  beta  →  production
   ↓          ↓        ↓          ↓
Not built   Built    Built     Built
            Off      Off       On/Off
            🔬       🧪        Stable
```

---

## Creating a New Gear

### Step 1: Choose Gear Type

- **Client gear?** Add to `gearbox/internal/gears/` (runs in web application)
- **Agent gear?** Add to `gearbox-agent/internal/gears/` (runs on servers)

### Step 2: Create Directory Structure

```bash
# Client gear (web application)
mkdir -p gearbox/internal/gears/mygear
cd gearbox/internal/gears/mygear

# Agent gear (server-side)
mkdir -p gearbox-agent/internal/gears/mygear
cd gearbox-agent/internal/gears/mygear
```

### Step 3: Create Gear Files

**For Client Gear:**

```go
// gear.go
package mygear

import (
    "context"
    "github.com/a-h/templ"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox/internal/framework/gear"
)

const featureFlag = "alpha"  // disabled | alpha | beta | production

func init() {
    gear.RegisterWithFlag(&Gear{}, featureFlag)
}

type Gear struct {
    gear.BaseGear
    handlers *Handlers
}

func (g *Gear) Info() gear.Info {
    return gear.Info{
        Name:        "mygear",
        DisplayName: "My Gear",
        Description: "What this gear does",
        Version:     "1.0.0",
        Icon:        "icon-name",
        Category:    "monitoring",
        Core:        false,
    }
}

func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
    if err := g.BaseGear.Initialize(ctx, deps); err != nil {
        return err
    }
    g.handlers = NewHandlers(deps)
    return nil
}

func (g *Gear) RegisterRoutes(r chi.Router) {
    r.Get("/", g.handlers.IndexPage)
}

func (g *Gear) SidebarItem() *gear.SidebarConfig {
    return &gear.SidebarConfig{
        Path:               "/mygear",
        Icon:               MyGearIcon(),
        DefaultOrder:       100,
        RequiresPermission: "mygear:view",
    }
}

func (g *Gear) SettingsPage(config map[string]any) templ.Component {
    return nil  // No settings page
}

func (g *Gear) Permissions() []gear.PermissionDef {
    return []gear.PermissionDef{
        {
            Component:   "mygear",
            Actions:     []string{"view"},
            Description: "View gear data",
        },
    }
}
```

```go
// handlers.go
package mygear

import (
    "net/http"
    "github.com/sarg3nt/gearbox/internal/framework/gear"
)

type Handlers struct {
    deps gear.Dependencies
}

func NewHandlers(deps gear.Dependencies) *Handlers {
    return &Handlers{deps: deps}
}

func (h *Handlers) IndexPage(w http.ResponseWriter, r *http.Request) {
    // Check permission
    if !h.deps.Auth.HasPermission(r, "mygear", "view") {
        http.Error(w, "Forbidden", http.StatusForbidden)
        return
    }

    // Render page
    w.Write([]byte("My Gear Page"))
}
```

**For Agent Gear:**

```go
// gear.go
package mygear

import (
    "context"
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

const featureFlag = "production"

func init() {
    gear.RegisterWithFlag(&Gear{}, featureFlag)
}

type Gear struct {
    gear.BaseGear
}

func (g *Gear) Info() gear.Info {
    return gear.Info{
        Name:        "mygear",
        DisplayName: "My Gear",
        Description: "Collects data about X",
        Version:     "1.0.0",
        Category:    "monitoring",
        Core:        true,
    }
}

func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
    return g.BaseGear.Initialize(ctx, deps)
}

func (g *Gear) Start(ctx context.Context) error {
    return nil
}

func (g *Gear) Stop(ctx context.Context) error {
    return nil
}

func (g *Gear) Health() gear.HealthStatus {
    return gear.NewHealthyStatus("operational")
}

func (g *Gear) RegisterRoutes(r chi.Router) {
    r.Get("/api/v1/mygear/data", g.handleGetData)
}

func (g *Gear) EventTypes() []gear.EventType {
    return nil
}

func (g *Gear) handleGetData(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"status":"ok"}`))
}
```

### Step 4: Import Gear in Main

The gear auto-registers via `init()`, but you must import it:

```go
// cmd/server/main.go (gearbox)
package main

import (
    _ "github.com/sarg3nt/gearbox/internal/gears/mygear"
    // ... other imports
)
```

```go
// cmd/gearbox-agent/main.go (gearbox-agent)
package main

import (
    _ "github.com/sarg3nt/gearbox-agent/internal/gears/mygear"
    // ... other imports
)
```

### Step 5: Build and Test

```bash
# Build
make templ-generate && make build

# Run locally
make dev-local

# Visit http://localhost:3000/mygear
```

---

## Testing Gears

### Unit Testing Handlers

```go
// handlers_test.go
package mygear

import (
    "net/http"
    "net/http/httptest"
    "testing"
    "github.com/sarg3nt/gearbox/internal/framework/gear"
)

type mockAuth struct{}

func (m *mockAuth) HasPermission(r *http.Request, component, action string) bool {
    return true  // Always allow for testing
}

func TestIndexPage(t *testing.T) {
    deps := gear.Dependencies{
        Auth: &mockAuth{},
    }

    handlers := NewHandlers(deps)

    req := httptest.NewRequest("GET", "/mygear", nil)
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
package mygear

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
// handlers := NewHandlers(gear.Dependencies{Agent: &mockAgentClient{}})
```

---

## Examples

### Example: Simple Client Gear

```go
// internal/gears/status/gear.go
package status

import (
    "context"
    "net/http"
    "github.com/a-h/templ"
    "github.com/go-chi/chi/v5"
    "github.com/sarg3nt/gearbox/internal/framework/gear"
)

const featureFlag = "production"

func init() {
    gear.RegisterWithFlag(&Gear{}, featureFlag)
}

type Gear struct {
    gear.BaseGear
}

func (g *Gear) Info() gear.Info {
    return gear.Info{
        Name:        "status",
        DisplayName: "Status",
        Description: "System status overview",
        Version:     "1.0.0",
        Category:    "core",
        Core:        true,
    }
}

func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
    return g.BaseGear.Initialize(ctx, deps)
}

func (g *Gear) RegisterRoutes(r chi.Router) {
    r.Get("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Status: OK"))
    })
}

func (g *Gear) SidebarItem() *gear.SidebarConfig {
    return &gear.SidebarConfig{
        Path:         "/status",
        DefaultOrder: 5,
        ShowAlways:   true,
    }
}

func (g *Gear) SettingsPage(config map[string]any) templ.Component {
    return nil
}

func (g *Gear) Permissions() []gear.PermissionDef {
    return nil  // Public
}
```

### Example: Agent Collector Gear

```go
// internal/gears/uptime/gear.go (gearbox-agent)
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
    "github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

const featureFlag = "production"

func init() {
    gear.RegisterWithFlag(&Gear{}, featureFlag)
}

type Gear struct {
    gear.BaseGear
}

func (g *Gear) Info() gear.Info {
    return gear.Info{
        Name:        "uptime",
        DisplayName: "System Uptime",
        Description: "Tracks system uptime",
        Version:     "1.0.0",
        Category:    "monitoring",
        Core:        false,
    }
}

func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
    return g.BaseGear.Initialize(ctx, deps)
}

func (g *Gear) Start(ctx context.Context) error {
    return nil
}

func (g *Gear) Stop(ctx context.Context) error {
    return nil
}

func (g *Gear) Health() gear.HealthStatus {
    return gear.NewHealthyStatus("operational")
}

func (g *Gear) Collectors() []gear.Collector {
    return []gear.Collector{
        {
            Name:     "uptime",
            Interval: 60 * time.Second,
            Collect:  g.collectUptime,
            OnData:   g.publishUptime,
        },
    }
}

func (g *Gear) collectUptime() (any, error) {
    out, err := exec.Command("uptime", "-p").Output()
    if err != nil {
        return nil, err
    }
    return strings.TrimSpace(string(out)), nil
}

func (g *Gear) publishUptime(data any) error {
    g.EventBus().Publish(events.Event{
        Type:      "uptime.updated",
        Timestamp: time.Now(),
        Data: map[string]any{
            "uptime": data,
        },
    })
    return nil
}

func (g *Gear) RegisterRoutes(r chi.Router) {
    r.Get("/api/v1/uptime", func(w http.ResponseWriter, r *http.Request) {
        uptime, _ := g.collectUptime()
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{"uptime": uptime})
    })
}

func (g *Gear) EventTypes() []gear.EventType {
    return []gear.EventType{
        {
            Name:        "uptime.updated",
            Description: "System uptime collected",
            Payload:     "uptime string",
        },
    }
}

var (
    _ gear.Gear          = (*Gear)(nil)
    _ gear.CollectorGear = (*Gear)(nil)
)
```

---

## Summary

### Key Takeaways

1. **Gears are compiled into the binary** - No runtime loading, full type safety
2. **Framework provides infrastructure** - Gears implement features
3. **Agent facades prevent coupling** - Gears define their own agent interfaces
4. **Feature flags control rollout** - Disabled, alpha, beta, production states
5. **Both apps use the same pattern** - Gearbox and gearbox-agent have parallel gear systems

### When to Create a Gear

Create a gear when you want to:

- Add a new monitoring page to the web application (client gear)
- Collect new types of data on servers (agent gear)
- Expose new agent APIs (agent gear)
- Add a feature that can be enabled/disabled per server

### When NOT to Create a Gear

Don't create a gear for:

- Shared infrastructure (auth, database, HTTP client)
- Cross-cutting concerns (logging, metrics)
- Framework modifications

### Next Steps

- See [examples/](#examples) for complete gear implementations
- Check existing gears in `internal/gears/` for patterns
- Read [TASKS.md](../TASKS.md) for planned gear features

---

**Last Updated:** 2026-01-31 | **Document Version:** 0.2.0
