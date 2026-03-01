# Gear System Feasibility Study - Comprehensive Analysis

**Date:** January 26, 2026
**Author:** Claude (AI Analysis)
**Status:** Research Complete - Awaiting Review

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Current Architecture Analysis](#current-architecture-analysis)
3. [Chosen Approach](#chosen-approach-compile-time-gears-caddy-pattern)
4. [Directory Structure](#directory-structure)
5. [Gear Interface Specification](#gear-interface-specification)
6. [Framework Components to Extract](#framework-components-to-extract)
7. [Migration Strategy](#migration-strategy)
8. [Pros and Cons Analysis](#pros-and-cons-analysis)
9. [Research Sources](#research-sources)

---

## Executive Summary

After thorough analysis of your codebase and extensive research into Go gear systems used by major projects (Caddy, Grafana, HashiCorp, Traefik), I've concluded that:

**Verdict: FEASIBLE and RECOMMENDED - but with the Compile-Time Gear approach, NOT Go's native runtime gear system.**

Your current "integrations" system is already 70% of the way to a gear architecture. The primary work involves:

1. Extracting shared framework components
2. Defining a formal Gear interface
3. Restructuring code into self-contained gear packages
4. Creating gear documentation for humans and AI agents

**Estimated Effort:** 4-6 weeks of focused development
**Risk Level:** Medium (significant refactoring, but patterns are proven)
**Long-term Value:** High (easier maintenance, extensibility, clear boundaries)

---

## Current Architecture Analysis

### What You Have (HAProxy Monitoring)

Your application already implements a **proto-gear system** called "Integrations":

| Feature                      | Current State          | Full Gear System   |
|------------------------------|------------------------|----------------------|
| Enable/disable per server    | ✅ Implemented          | ✅ Same               |
| Custom configuration (JSON)  | ✅ Implemented          | ✅ Same               |
| Custom display order         | ✅ Implemented          | ✅ Same               |
| Sidebar visibility control   | ✅ Implemented          | ✅ Same               |
| Permission-based access      | ✅ Implemented          | ✅ Same               |
| Self-contained code packages | ❌ Mixed throughout     | ✅ Isolated packages  |
| Formal gear interface      | ❌ Implicit patterns    | ✅ Explicit interface |
| Gear lifecycle management  | ❌ Manual wiring        | ✅ Automated          |
| Dynamic route registration   | ❌ Hardcoded in main.go | ✅ Gear-provided    |
| Gear-specific migrations   | ❌ Single migration set | ✅ Per-gear         |

### Current Integration Count (8 total)

1. **Dashboard** - Main overview page
2. **Metrics** - System resource monitoring (CPU, memory, disk, network)
3. **Logs** - View/search system and HAProxy logs
4. **Services** - Monitor systemd services status
5. **Certificates** - SSL/TLS certificate management
6. **Traffic** - Real-time traffic analysis
7. **Alerts** - Configurable alerting system
8. **OS Updates** - System package management

### Key Files Requiring Refactoring

| File                                    | Lines | Impact | Changes Needed                                            |
|-----------------------------------------|-------|--------|-----------------------------------------------------------|
| `cmd/server/main.go`                    | ~635  | High   | Extract gear initialization, dynamic route registration |
| `internal/handler/integrations.go`      | ~948  | High   | Becomes generic "gear manager"                          |
| `internal/handler/pages.go`             | ~500+ | High   | Page handlers move to gears                             |
| `internal/templates/layouts/base.templ` | ~1166 | Medium | Dynamic sidebar from gear registry                      |
| `internal/database/integrations.go`     | ~904  | Medium | Rename to gears, keep config storage generic            |
| `internal/models/permissions.go`        | ~366  | Medium | Dynamic component registration                            |

### What You Have (HAProxy Agent)

The agent is more monolithic but shows gear-like patterns:

| Pattern                | Location                         | Adaptability                  |
|------------------------|----------------------------------|-------------------------------|
| Collector pattern      | `internal/collector/`            | High - already interface-like |
| Event bus              | `internal/events/bus.go`         | High - pub/sub ready          |
| Handler registration   | `internal/api/server.go`         | Medium - needs abstraction    |
| Hardcoded service list | `main.go:355`, `handlers.go:114` | Low - needs consolidation     |

---

## Chosen Approach: Compile-Time Gears (Caddy Pattern)

**How it works:** Gears are Go packages that register themselves via `init()`. Single binary output.

**Used by:** Caddy, Go's database/sql drivers, many production systems

**Why this approach:**

- **Single binary deployment** - maintains current strength
- **Type-safe** - compiler catches interface mismatches
- **Easy debugging** - standard Go debugging works
- **All platforms** - works on Linux, macOS, Windows
- **Fast** - no RPC overhead, direct function calls
- **Simple builds** - standard `go build`

**Trade-offs accepted:**

- Requires recompilation to add/remove gears
- All gears bundled into binary (size grows slightly)

### Architecture Overview

```txt
┌─────────────────────────────────────────────────────────────────┐
│                        Single Binary                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                     Framework Core                        │    │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │    │
│  │  │   Auth   │ │  Events  │ │ Database │ │   HTTP   │    │    │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │    │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────────────────┐     │    │
│  │  │   UI     │ │ Gear   │ │ Shared Components    │     │    │
│  │  │ Layout   │ │ Registry │ │ (Toast, Table, etc.) │     │    │
│  │  └──────────┘ └──────────┘ └──────────────────────┘     │    │
│  └─────────────────────────────────────────────────────────┘    │
│                              │                                   │
│                    Gear Interface                              │
│                              │                                   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │Dashboard │ │   Logs   │ │ Services │ │  Certs   │ ...       │
│  │  Gear  │ │  Gear  │ │  Gear  │ │  Gear  │           │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

### Why This Approach?

1. **Preserves single binary** - Your current deployment strength
2. **Matches existing patterns** - Similar to your "integrations" system
3. **Industry proven** - Caddy, database/sql, many others
4. **Type-safe** - Go compiler validates gear implementations
5. **Simple builds** - Standard `go build`, no special tooling
6. **Easy testing** - Standard Go testing works
7. **Good debugging** - Standard Go debugging, stack traces work

### Customization via Build Tags (Optional Enhancement)

For users who want smaller binaries, you can use Go build tags:

```go
// internal/gears/logs/gear.go
//go:build gear_logs || gear_all

package logs

func init() {
    gear.Register(&LogsGear{})
}
```

Build commands:

```bash
# Full build with all gears
go build -tags gear_all ./cmd/server

# Minimal build with specific gears
go build -tags "gear_dashboard,gear_logs,gear_alerts" ./cmd/server
```

This is **optional** and can be added later if needed.

---

## Directory Structure

```txt
internal/
├── framework/                    # Framework core (all shared code)
│   ├── gear/                   # Gear system
│   │   ├── interface.go
│   │   ├── registry.go
│   │   └── manager.go
│   ├── handler/                  # Core framework handlers
│   │   ├── auth.go               # Login, logout, session management
│   │   ├── profile.go            # User profile, password change
│   │   ├── users.go              # User management (admin)
│   │   ├── gears.go            # Gear manager UI (was integrations.go)
│   │   └── health.go             # Health check endpoint
│   ├── ui/                       # Shared UI components
│   │   ├── toast.templ
│   │   ├── table.templ
│   │   ├── modal.templ
│   │   ├── toggle.templ
│   │   ├── icons.templ
│   │   └── badge.templ
│   ├── templates/                # Core page templates
│   │   ├── layouts/
│   │   │   └── base.templ        # Main layout with sidebar
│   │   └── pages/
│   │       ├── login.templ
│   │       ├── profile.templ
│   │       ├── users.templ
│   │       └── gears.templ     # Gear manager page
│   ├── auth/                     # Move from internal/auth
│   ├── events/                   # Move from internal/events
│   ├── database/                 # Core DB operations
│   ├── agent/                    # Agent client
│   ├── config/                   # Configuration loading
│   └── middleware/               # Shared middleware
│
└── gears/                      # Self-contained gears
    ├── dashboard/
    ├── logs/
    ├── services/
    ├── certificates/
    ├── metrics/
    ├── traffic/
    ├── alerts/
    └── os_updates/
```

Each gear follows this structure:

```txt
internal/gears/logs/
├── gear.go           # Gear implementation (interface methods)
├── handlers.go         # HTTP handlers
├── collectors.go       # Data collection logic (if any)
├── templates/
│   ├── page.templ      # Main logs page
│   └── settings.templ  # Logs settings page
├── models.go           # Gear-specific types
└── README.md           # Gear documentation
```

---

## Gear Interface Specification

### Full Interface Definition

```go
package gear

import (
    "context"
    "database/sql"
    "log/slog"
    "net/http"

    "github.com/a-h/templ"
    "github.com/go-chi/chi/v5"
)

// ============================================================================
// CORE INTERFACE
// ============================================================================

// Gear is the main interface all gears must implement
type Gear interface {
    // Metadata returns gear information
    Info() GearInfo

    // Lifecycle methods
    Initialize(ctx context.Context, deps Dependencies) error
    Start() error
    Stop() error

    // Health returns the current health status of the gear
    Health() HealthStatus

    // Routes registers HTTP routes for this gear
    RegisterRoutes(r chi.Router)

    // UI integration
    SidebarItem() SidebarConfig
    SettingsPage(config map[string]interface{}) templ.Component

    // Security
    Permissions() []PermissionDef

    // Database
    Migrations() []Migration
}

// ============================================================================
// SUPPORTING TYPES
// ============================================================================

// GearInfo contains metadata about a gear
type GearInfo struct {
    Name        string   // Internal identifier: "logs", "metrics"
    DisplayName string   // Shown in UI: "System Logs"
    Description string   // Detailed description
    Version     string   // Semantic version: "1.0.0"
    Icon        string   // Icon identifier
    Category    string   // "monitoring", "security", "system", "utility"
    Author      string   // Optional: "Dave"
    Website     string   // Optional: documentation URL
}

// Dependencies contains services provided by the framework
type Dependencies struct {
    DB          *sql.DB
    Logger      *slog.Logger
    EventHub    EventPublisher
    AuthManager AuthChecker
    AgentClient AgentClientInterface
    HTTPClient  *http.Client
    Config      map[string]interface{}
}

// EventPublisher allows gears to publish events
type EventPublisher interface {
    Publish(eventType string, payload interface{})
    Subscribe(eventType string, handler func(interface{}))
}

// AuthChecker allows gears to check permissions
type AuthChecker interface {
    HasPermission(r *http.Request, component, action string) bool
    CurrentUser(r *http.Request) *User
}

// AgentClientInterface for communicating with HAProxy Agent
type AgentClientInterface interface {
    Get(path string, result interface{}) error
    Post(path string, body, result interface{}) error
}

// SidebarConfig defines how the gear appears in navigation
type SidebarConfig struct {
    Path          string                // URL path: "/logs"
    Icon          templ.Component       // SVG icon component
    DefaultOrder  int                   // Default sort order (lower = higher)
    BadgeProvider func() int            // Optional: returns badge count (alerts, etc.)
    ShowAlways    bool                  // Show even when disabled (for core gears)
}

// PermissionDef defines a permission this gear uses
type PermissionDef struct {
    Component   string   // Permission component name
    Actions     []string // Available actions: "view", "configure", "manage", "action"
    Description string   // Human-readable description
}

// Migration defines a database migration
type Migration struct {
    Version     int    // Sequential version number
    Description string // What this migration does
    Up          string // SQL to apply migration
    Down        string // SQL to revert migration
}

// HealthStatus represents gear health
type HealthStatus struct {
    Status  string // "healthy", "degraded", "unhealthy"
    Message string // Optional message
}

// ============================================================================
// OPTIONAL INTERFACES (Gears can implement for additional features)
// ============================================================================

// Collector gears that collect data periodically
type CollectorGear interface {
    Gear
    Collectors() []Collector
}

// Collector defines a data collector
type Collector struct {
    Name     string
    Interval time.Duration
    Collect  func(ctx context.Context) (interface{}, error)
}

// WebSocketGear for gears that need WebSocket support
type WebSocketGear interface {
    Gear
    HandleWebSocket(conn *websocket.Conn)
}

// EventHandler for gears that react to events
type EventHandlerGear interface {
    Gear
    HandleEvent(eventType string, payload interface{})
}

// Searchable for gears that support global search
type SearchableGear interface {
    Gear
    Search(query string) []SearchResult
}

type SearchResult struct {
    Title       string
    Description string
    URL         string
    Relevance   float64
}
```

---

## Framework Components to Extract

### Shared UI Components

| Component           | Current Location           | New Location                  | Used By               |
|---------------------|----------------------------|-------------------------------|-----------------------|
| Toast notifications | `components/toast.templ`   | `framework/ui/toast.templ`    | All gears           |
| Data tables         | `components/table.templ`   | `framework/ui/table.templ`    | Logs, Alerts, Traffic |
| Modals/Dialogs      | Inline in pages            | `framework/ui/modal.templ`    | All gears           |
| Toggle switches     | Inline in forms            | `framework/ui/toggle.templ`   | Settings pages        |
| Icons (SVG)         | `base.templ`               | `framework/ui/icons.templ`    | All gears           |
| Badges              | `base.templ`               | `framework/ui/badge.templ`    | Sidebar, Alerts       |
| Loading spinners    | Inline                     | `framework/ui/spinner.templ`  | All gears           |
| Empty states        | Inline                     | `framework/ui/empty.templ`    | Tables, lists         |
| Error displays      | Inline                     | `framework/ui/error.templ`    | All gears           |
| Metrics cards       | `components/metrics.templ` | `framework/ui/metrics.templ`  | Dashboard, Metrics    |
| Charts (Chart.js)   | Inline JS                  | `framework/ui/charts.templ`   | Metrics, Traffic      |
| Progress bars       | Inline                     | `framework/ui/progress.templ` | OS Updates            |
| Terminal output     | OS Updates                 | `framework/ui/terminal.templ` | OS Updates, Logs      |

### Shared Services

| Service        | Current Location          | New Location          | Purpose               |
|----------------|---------------------------|-----------------------|-----------------------|
| Authentication | `internal/auth/`          | `framework/auth/`     | User auth, sessions   |
| Events         | `internal/events/`        | `framework/events/`   | Pub/sub event hub     |
| Database base  | `internal/database/`      | `framework/database/` | Core DB operations    |
| Agent client   | `internal/agent/`         | `framework/agent/`    | Agent communication   |
| SSE            | `internal/handler/sse.go` | `framework/sse/`      | Server-sent events    |
| Config         | `internal/config/`        | `framework/config/`   | Configuration loading |

### Middleware

| Middleware          | Purpose                    | Shared?         |
|---------------------|----------------------------|-----------------|
| RequireAuth         | Authentication check       | Yes - framework |
| InjectGearContext | Add gear info to context | Yes - framework |
| PermissionCheck     | Verify user permissions    | Yes - framework |
| Timeout             | Request timeout            | Yes - framework |
| CORS                | Cross-origin requests      | Yes - framework |
| RateLimiting        | Request rate limiting      | Gear-specific |

---

## Migration Strategy

### Backward Compatibility

**Database:**

- Keep `integrations` table name (or rename transparently with migration)
- Config JSON format remains compatible
- No data loss during migration

**Configuration:**

- Existing config files continue to work
- New gear-specific config nested under gear name

**URLs:**

- All existing URLs remain the same
- No breaking changes to API endpoints

### Migration Steps for Existing Deployments

```bash
# 1. Backup database
make backup

# 2. Pull new version
docker pull ghcr.io/sarg3nt/gearbox:latest

# 3. Run migration (automatic on startup)
# The new version detects old schema and migrates

# 4. Verify
curl http://localhost:3000/health
```

### Rollback Plan

If issues occur:

1. Stop new container
2. Start previous version container
3. Database remains compatible (forward-only migrations are optional)

---

## Pros and Cons Analysis

### Pros of Gear Architecture

| Benefit              | Description                                                                | Impact                                         |
|----------------------|----------------------------------------------------------------------------|------------------------------------------------|
| **Clear Separation** | Each gear is self-contained with its own handlers, templates, and logic  | High - easier to understand, modify, and debug |
| **Easy to Disable**  | Disable a gear = none of its code runs, not just hidden UI               | High - cleaner than current approach           |
| **Maintainability**  | Bug in one gear doesn't affect others; clear ownership boundaries        | High - faster debugging, safer changes         |
| **Extensibility**    | Adding new gears follows a clear pattern; AI agents can generate gears | High - future development is faster            |
| **Testing**          | Each gear can be tested in isolation                                     | Medium - better test coverage                  |
| **Documentation**    | Each gear has its own README; self-documenting interface                 | Medium - easier onboarding                     |
| **Customization**    | Users can build custom binaries with only needed gears                   | Low - niche use case                           |
| **Code Reuse**       | Framework components are clearly separated and reusable                    | Medium - less duplication                      |

### Cons of Gear Architecture

| Drawback                  | Description                                             | Mitigation                            |
|---------------------------|---------------------------------------------------------|---------------------------------------|
| **Upfront Effort**        | 4-6 weeks of development to implement                   | Plan carefully, migrate incrementally |
| **Learning Curve**        | Developers need to learn gear interface               | Good documentation, example gears   |
| **Compile-Time Only**     | Can't add gears without recompilation                 | Acceptable for your use case          |
| **Binary Size**           | All gears compiled into one binary                    | Use build tags if size matters        |
| **Interface Rigidity**    | Changing gear interface requires updating all gears | Design interface carefully upfront    |
| **Complexity**            | More abstraction layers than current approach           | Keep interface minimal                |
| **Cross-Gear Features** | Features spanning multiple gears need careful design  | Use events for loose coupling         |
| **Regression Risk**       | Large refactoring could introduce bugs                  | Thorough testing, staged rollout      |

### Trade-off Analysis

**Invest Now vs. Continue Current Approach:**

| Factor                     | Current Approach | Gear Architecture  |
|----------------------------|------------------|----------------------|
| Development speed (now)    | Faster           | Slower (refactoring) |
| Development speed (future) | Slowing down     | Faster               |
| Bug isolation              | Poor             | Good                 |
| Code organization          | Degrading        | Clean                |
| New developer onboarding   | Moderate         | Easy                 |
| AI-assisted development    | Moderate         | Excellent            |
| Technical debt             | Growing          | Reduced              |

**Recommendation:** The current codebase is at a good point for this refactoring - complex enough to benefit, not so complex that it's overwhelming. Waiting longer will make the migration harder.

---

## Research Sources

### Go Gear Architecture Patterns

- [Design patterns in Go's database/sql package](https://eli.thegreenplace.net/2019/design-patterns-in-gos-databasesql-package/) - Eli Bendersky's analysis of Go's compile-time gear pattern
- [Gears in Go](https://eli.thegreenplace.net/2021/gears-in-go/) - Comprehensive overview of gear approaches
- [RPC-based gears in Go](https://eli.thegreenplace.net/2023/rpc-based-gears-in-go/) - HashiCorp go-gear analysis
- [Building a Gear System in Go](https://skoredin.pro/blog/golang/go-gear-system) - Practical guide to Go gear systems
- [Clean Architecture and Gears in Go](https://cekrem.github.io/posts/clean-architecture-and-gears-in-go/) - Dependency inversion with gears

### Real-World Gear Systems

- [Caddy - Extending Caddy](https://caddyserver.com/docs/extending-caddy) - Caddy's module/gear documentation
- [Caddy Architecture](https://caddyserver.com/docs/architecture) - How Caddy structures its gear system
- [HashiCorp go-gear](https://github.com/hashicorp/go-gear) - gRPC-based gear system (Terraform, Vault)
- [Grafana Gear SDK for Go](https://grafana.com/developers/gear-tools/key-concepts/backend-gears/grafana-gear-sdk-for-go) - Grafana's approach to Go gears
- [Traefik Gear Development](https://gears.traefik.io/create) - Interpreted Go gears with Yaegi

### Go Project Structure

- [Go Project Structure: Practices & Patterns](https://www.glukhov.org/post/2025/12/go-project-structure/) - 2025 best practices
- [Go Modular Monolith](https://medium.com/@arkjuniork.yudistira/go-modular-monolith-part-i-f963da742e81) - Modular architecture in Go
- [Interface-based Gear Architecture](https://www.slingacademy.com/article/leveraging-interfaces-for-gear-based-architecture-in-go-applications/) - Using interfaces for gears
- [Registry Pattern in Go](https://github.com/Faheetah/registry-pattern) - Example implementation

### Go Web Development with Templ

- [Templ Documentation](https://templ.guide/) - Official templ guide
- [Templ Project Structure](https://templ.guide/project-structure/project-structure/) - Recommended architecture
- [Echo-Modarch](https://github.com/daluisgarcia/echo-modarch) - Modular Go SSR template
- [Go + Templ + HTMX](https://medium.com/@iamsiddharths/building-reactive-uis-with-go-templ-and-htmx-a-simpler-path-beyond-spas-17e7dad2c7a2) - Building reactive UIs

### Real-Time Updates

- [Live website updates with Go, SSE, and htmx](https://threedots.tech/post/live-website-updates-go-sse-htmx/) - Three Dots Labs tutorial
- [htmx SSE Extension](https://htmx.org/extensions/sse/) - Official SSE documentation
- [go-htmx Package](https://github.com/donseba/go-htmx) - Go library with SSE support

### Chi Router

- [go-chi/chi](https://github.com/go-chi/chi) - Router documentation
- [Chi Middleware](https://pkg.go.dev/github.com/go-chi/chi/middleware) - Built-in middleware
