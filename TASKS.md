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

#### Task 1: Widget Palette API ✅

- [x] Create `/api/dashboards/widgets` endpoint in `gearbox/internal/framework/handler/dashboard.go`
- [x] Return all available widgets from **enabled plugins only** for the selected server
- [x] Support filtering by plugin name, category, search term
- [x] Register route in router setup
- [x] Added `PluginName` field to all 31 widget definitions across 8 plugins
- [x] Built successfully with `make templ-generate && make build`

**Implementation Details:**

- Route: `GET /api/dashboards/widgets`
- Query params: `?server_id=<id>&plugin=<name>&category=<cat>&search=<term>`
- Returns JSON array of widgets from enabled plugins
- Core widgets always included (PluginName = "core")

#### Task 2: Widget Palette Side Panel UI ✅

- [x] Add palette panel HTML to `gearbox/internal/framework/templates/pages/dashboard_editor.templ`
- [x] Create `gearbox/static/css/dashboard/palette.css` for styling
- [x] Create `gearbox/static/js/dashboard/palette.js` for interactions
- [x] Display widgets as cards with icon, name, description, plugin badge
- [x] Add filtering UI (search, plugin dropdown)
- [x] Make panel scrollable with fixed header
- [x] Built successfully with `make templ-generate && make build`

**Implementation Details:**

- Enhanced widget palette panel with fixed header and scrollable list
- Widget cards display icon, name, description, and color-coded plugin badges
- Real-time search across name, description, and plugin
- Plugin dropdown filter with all available plugins
- Fetches widgets dynamically from `/api/dashboards/widgets` endpoint
- Responsive design with dark mode support

#### Task 3: Drag-and-Drop from Palette ✅

- [x] Extend `gearbox/static/js/dashboard/editor.js` drag-drop functionality
- [x] Use SortableJS `group` option to enable cross-container dragging
- [x] On drop: generate widget ID, create instance, add to dashboard, save YAML
- [x] Show loading state while widget content loads via HTMX

**Implementation Details:**

- Configured SortableJS with `group` option for cross-container dragging between palette and grid
- Palette widgets use `pull: 'clone'` to allow dragging without removing from palette
- Grid accepts drops from palette via `put: ['widget-palette']`
- `onAdd` handler creates new widget instances with unique IDs when dropped
- Auto-saves dashboard after widget drop with silent save (no page reload)
- Displays loading state during widget creation, then shows placeholder content
- Widget position is automatically calculated based on drop index
- Edit controls (drag handle, delete button) are injected after widget creation
- Success toast notification confirms widget addition
- Built successfully with `make templ-generate && make build`

#### Task 4: Navigation Updates ✅

- [x] Create dynamic plugin navigation
- [x] Show plugin menu items only when enabled for server
- [x] Ensure plugin menu items update when plugins are enabled/disabled
- [x] Left hand nav items can be rearranged by clicking edit icon that is next to the toggle sidebar icon
- [x] When in edit mode user can drag a menu item up or down and other nav items flow around it
- [x] Edit icon becomes save icon while in edit mode
- [x] When a plugin is enabled it generates its default "dashboard" interface yaml file if it does not already exist.  Each plugin's default dashboard has a default name and icon as designed by plugin developer.  When plugins is enabled it is added to the nav bar
- [x] The rearrangement of left hand nav items will be removed from the plugin screen, all nav rearrangement is now in the nav bar only.

**Implementation Details:**

- Added edit mode toggle button next to sidebar collapse button in [base.templ](gearbox/internal/framework/templates/layouts/base.templ:1105-1129)
- Edit icon switches to save/checkmark icon when in edit mode
- Created `SidebarLinkDraggable` and `SidebarLinkDraggableWithBadge` templ components with data attributes and drag handles
- Implemented `toggleSidebarEditMode()` JavaScript function for managing edit state
- Used SortableJS for drag-and-drop reordering of navigation items
- Drag handles visible only in edit mode, links disabled during editing
- `saveSidebarOrder()` POSTs new order to `/api/integrations/sort-order` endpoint
- Removed drag handle from plugin cards in [plugins.templ](gearbox/internal/framework/templates/pages/plugins.templ)
- Disabled drag-and-drop in [plugins-page.js](gearbox/static/js/plugins/plugins-page.js) with explanatory comments
- Dynamic navigation already working via `OrderedIntegrationLinks()` and middleware
- Plugin dashboards auto-generate when enabled (completed in Task 1)
- Built successfully with `make templ-generate && make build`

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
