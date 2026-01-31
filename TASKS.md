# Gearbox - Task Tracker

**Purpose:** Track active implementation tasks for both gearbox-agent and gearbox applications.

**Archive:** Completed tasks are moved to TASKS_ARCHIVE.md when created.

---

## Quick Context

**Project:** Gearbox - GitOps-focused server monitoring and management platform

**What is Gearbox?**

Gearbox is a plugin-based server monitoring and management platform designed for DevOps engineers who embrace GitOps principles. It provides real-time visibility into servers, services, containers, and infrastructure while keeping configuration in git repositories rather than web forms.

**IMPORTANT:** Gearbox is NOT an HAProxy-specific tool. It is a universal monitoring and management platform that supports ANY Linux system. HAProxy monitoring is just one plugin among many.

**Core Philosophy:**

- **GitOps First:** Configuration lives in git, not in UI forms
- **Plugin Architecture:** Extensible core framework with feature plugins
- **Universal Monitoring:** Works with any Linux system (HAProxy, TrueNAS Scale, Docker hosts, bare Linux servers, workstations)
- **Auto-Discovery:** gearbox-agent automatically detects installed services and enables relevant plugins
- **Security Hardened:** Built for production infrastructure
- **Real-time Monitoring:** WebSocket-based live updates

**Applications:**

- **gearbox-agent** - Go service running on monitored servers (port 8405)
  - Auto-discovers services on the host (HAProxy, Docker, system services)
  - Collects metrics, stats, and system information
  - Exposes REST API and WebSocket for real-time data
  - Plugin-based collectors for extensibility
  - Configurable via environment variables (auto-discovery enabled by default)

- **gearbox** - Web dashboard for monitoring and management (port 3000)
  - Widget-based dashboard system
  - Multi-server support
  - Plugin-based features
  - YAML-defined dashboards stored in `data/dashboards/`
  - GitOps-ready (dashboards can be synced from git)

**Architecture Overview:**

```text
Plugin = Collection of Widgets + Optional Predefined Dashboard(s)
Widget = Reusable UI component built with framework building blocks
Dashboard = Named page containing arranged widgets (YAML-defined)
Framework = Shared services and building blocks (graphs, tables, panels, cards, API client, WebSocket manager)
```

**Key Concepts:**

1. **Plugins provide widgets:** Each plugin exports one or more widgets that display data or provide controls
2. **Dashboards compose widgets:** Dashboards are pages that arrange widgets in a grid layout
3. **Every page is a dashboard:** Whether it's a plugin's default dashboard or a user-created one
4. **Framework provides building blocks:** Plugins use shared components (graphs, tables, panels) to build widgets
5. **YAML-based configuration:** Dashboards are stored as YAML files for GitOps workflows

**Current Plugins:**

1. **HAProxy** - HAProxy monitoring and statistics (5 widgets)
2. **Metrics** - System metrics visualization (7 widgets)
3. **Services** - Service management and monitoring (3 widgets)
4. **Certificates** - TLS certificate tracking (3 widgets)
5. **Logs** - Log aggregation and viewing (1 widget)
6. **Traffic** - Traffic analysis and visualization (4 widgets)
7. **Alerts** - Alert management (5 widgets)
8. **OS Updates** - OS package updates (3 widgets)

**Total:** 8 plugins, 31 widgets

---

## Active Tasks

### Widget Palette and Plugin Dashboard System

**Status:** In Progress (2026-01-31)

**Goal:** Complete the widget palette system for adding widgets from enabled plugins to custom dashboards.

**Completed Foundation Work:**

- ✅ HAProxy added to DefaultPlugins list
- ✅ All plugins disabled by default when creating new servers
- ✅ Auto-deployment of plugin dashboards when plugin is enabled
- ✅ Infrastructure changes (dashboard storage in Handler)

**Remaining Tasks:**

#### Task 1: Widget Palette API

- [ ] Create `/api/dashboards/widgets` endpoint in `gearbox/internal/framework/handler/dashboard.go`
- [ ] Return all available widgets from **enabled plugins only** for the selected server
- [ ] Support filtering by plugin name, category, search term
- [ ] Register route in router setup
- [ ] Test with curl/Postman

#### Task 2: Widget Palette Side Panel UI

- [ ] Add palette panel HTML to `gearbox/internal/framework/templates/pages/dashboard_editor.templ`
- [ ] Create `gearbox/static/css/dashboard/palette.css` for styling
- [ ] Create `gearbox/static/js/dashboard/palette.js` for interactions
- [ ] Display widgets as cards with icon, name, description, plugin badge
- [ ] Add filtering UI (search, plugin dropdown)
- [ ] Make panel scrollable with fixed header

#### Task 3: Drag-and-Drop from Palette

- [ ] Extend `gearbox/static/js/dashboard/editor.js` drag-drop functionality
- [ ] Use SortableJS `group` option to enable cross-container dragging
- [ ] On drop: generate widget ID, create instance, add to dashboard, save YAML
- [ ] Show loading state while widget content loads via HTMX

#### Task 4: Navigation Updates

- [ ] Create dynamic plugin navigation
- [ ] Show plugin menu items only when enabled for server
- [ ] Ensure plugin menu items update when plugins are enabled/disabled

**Testing Checklist:**

- [ ] Enable HAProxy plugin → verify dashboard auto-created
- [ ] Open dashboard editor → verify "Add Widget" button appears
- [ ] Click "Add Widget" → verify palette panel opens
- [ ] Search/filter widgets → verify functionality works
- [ ] Drag widget from palette → verify drop works and saves to YAML
- [ ] Disable HAProxy → verify widgets disappear from palette

---

## Completed Work

### Plugin-to-Widget Architecture Migration ✅

**Status:** COMPLETE - All Phases 1-14 Done (2026-01-30)

**Key Achievements:**

1. ✅ Core infrastructure (widget system, dashboard storage, building blocks)
2. ✅ HAProxy plugin conversion (proof-of-concept)
3. ✅ 7 additional plugins converted (Alerts, Metrics, Services, Certificates, Traffic, Logs, OS Updates)
4. ✅ Dashboard management system (CRUD, export/import, reset)
5. ✅ Auto-discovery system framework

**Progress:** 8 of 8 plugins converted (100%), 31 widgets created

**Build Status:**

- ✅ Gearbox: Builds successfully
- ✅ Gearbox-Agent: Builds successfully
- ✅ Templ: 147 templates generated

---

## Reference

- **Agent API Docs:** [gearbox-agent/docs/](gearbox-agent/docs/)
- **Plugin Architecture:** [docs/plugins.md](docs/plugins.md)
- **Development Guide:** [gearbox/docs/development.md](gearbox/docs/development.md)

---

## Fresh Start

Code to run for a fresh start (deletes local database and starts dev server):

```bash
# from the gearbox directory
cd ../gearbox-agent
make deploy
cd -
rm -f data/haproxy-monitor.db
rm -rf data/dashboards
make dev-local
```

