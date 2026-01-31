# Plugin System Work - Session Summary

## Work Completed (2026-01-31)

### 1. HAProxy Plugin Integration ✅
- **Added HAProxy to DefaultPlugins list** in [gearbox/internal/framework/database/plugins.go](gearbox/internal/framework/database/plugins.go)
- Added `PluginHAProxy` constant
- HAProxy now appears in the Settings > Plugins page alongside other plugins
- Plugin positioned first in the list

### 2. Plugin Default State ✅
- **All plugins now disabled by default** when a new server is created
- This includes: HAProxy, Metrics, Logs, Services, Certificates, Traffic, Alerts, OS Updates
- Users must manually enable plugins for each server
- Prevents auto-enabling features the user may not want

### 3. Dashboard Auto-Deployment ✅
- **Created `deployPluginDashboards()` method** in [gearbox/internal/framework/handler/plugins.go](gearbox/internal/framework/handler/plugins.go:958-1007)
- When a plugin is enabled via toggle, its predefined dashboards are automatically deployed
- Dashboards are parsed from plugin's YAML definitions
- Dashboards are marked as non-editable and plugin-owned
- Skips deployment if dashboard already exists
- Added dashboard storage to Handler struct
- Modified [cmd/server/main.go](cmd/server/main.go) to call `h.SetDashboardStorage(dashboardStorage)`

### 4. Infrastructure Changes ✅
- Added `dashboardStorage *dashboard.Storage` field to Handler struct
- Created `SetDashboardStorage()` setter method
- Added imports: `fmt`, `dashboard`, `plugin`, `gopkg.in/yaml.v3` to plugins.go
- Build verified successful - all code compiles without errors

## What Still Needs to Be Done

### 1. Widget Palette API (High Priority)
**Location:** New endpoint needed in dashboard handler

**Requirements:**
- Create `/api/dashboards/widgets` endpoint
- Return all available widgets from enabled plugins only
- Filter by plugin, category, or search term
- Include widget metadata: type, name, description, icon, category
- Response format:
```json
{
  "widgets": [
    {
      "id": "haproxy-status-summary",
      "name": "HAProxy Status Summary",
      "description": "...",
      "plugin": "haproxy",
      "category": "monitoring",
      "icon": "activity",
      "defaultSize": {"width": 12, "height": "auto"}
    }
  ]
}
```

**Files to create/modify:**
- `gearbox/internal/framework/handler/dashboard.go` - add `WidgetPaletteAPI()` method
- Register route in router setup

### 2. Widget Palette Side Panel UI (High Priority)
**Location:** Dashboard editor template

**Requirements:**
- Side panel overlays left navigation when "Add Widget" is clicked
- Width: same as nav bar (~256px)
- Widgets displayed as:
  - Thumbnail showing widget preview/screenshot
  - Widget name and description
  - Plugin badge
- Filtering:
  - Search box (filters by name/description)
  - Plugin dropdown (filter by plugin)
  - Category tabs/pills
- Scrollable list of widgets
- Drag handle on each widget thumbnail

**Files to create/modify:**
- `gearbox/internal/framework/templates/pages/dashboard_editor.templ` - add palette panel HTML
- `gearbox/static/css/dashboard/palette.css` - styling
- `gearbox/static/js/dashboard/palette.js` - palette interactions

**Design notes:**
- Thumbnails: 240px wide x auto height
- Could use widget icon + category color as fallback for thumbnail
- "Creative solution" needed for generating widget previews

### 3. Drag-and-Drop from Palette (Medium Priority)
**Location:** Dashboard editor JavaScript

**Requirements:**
- Drag widget from palette to dashboard grid
- Drop creates new widget instance
- Auto-assigns unique ID
- Places at drop location in grid
- Saves to dashboard YAML immediately
- Updates UI to show new widget

**Files to modify:**
- `gearbox/static/js/dashboard/editor.js` - extend drag-drop to work from palette
- May need to use SortableJS's `group` option to allow cross-container dragging

### 4. Navigation Updates (Complex - Lower Priority)
**Location:** Sidebar navigation template

**Current state:**
- Hardcoded "Dashboard" and "Status Grid" links at top of nav
- These are HAProxy-specific but shown always

**Desired state:**
- When HAProxy plugin is enabled: Show "HAProxy" menu item (links to /haproxy)
- When HAProxy plugin is disabled: Don't show it
- Remove hardcoded "Dashboard" and "Status Grid" items
- Plugin menu items appear based on enabled state

**Challenges:**
- Templates need access to per-server plugin state
- Base layout template needs to query database or receive plugin data
- May require passing additional context to all page renders
- OR: Use HTMX to load nav items dynamically based on selected server

**Files to modify:**
- `gearbox/internal/framework/templates/layouts/base.templ:832-833` - remove hardcoded items
- Add logic to fetch and display plugin menu items dynamically
- Possibly create `@PluginLinks(serverID, currentPath)` templ component

## Testing Checklist

When implementing the above:

- [ ] Enable HAProxy plugin for a server
- [ ] Verify "HAProxy Overview" dashboard is auto-created
- [ ] Verify dashboard appears in Settings > Dashboards
- [ ] Verify dashboard is marked as non-editable
- [ ] Open dashboard editor
- [ ] Click "Add Widget" to open palette
- [ ] Search and filter widgets
- [ ] Drag widget from palette to dashboard
- [ ] Verify widget appears and saves
- [ ] Verify only enabled plugin widgets appear in palette
- [ ] Disable HAProxy plugin
- [ ] Verify HAProxy widgets no longer appear in palette
- [ ] Verify navigation updates (once implemented)

## Code Quality Notes

- ✅ All code compiles successfully
- ✅ No linting errors introduced
- ✅ Follows existing code patterns
- ✅ Error handling in place for dashboard deployment
- ⚠️ Some diagnostic warnings in other files (pre-existing, not related to this work)

## Remaining Effort Estimate

- Widget Palette API: ~30-45 minutes
- Palette UI: ~1-2 hours (depends on thumbnail generation approach)
- Drag-and-drop: ~30-60 minutes
- Navigation updates: ~1-2 hours (complex due to template context requirements)

**Total:** ~3-5 hours of additional work

## Nice-to-Haves (Not Required)

- Widget preview thumbnails (could screenshot widgets in test environment)
- Widget categories grouping in palette
- Recently used widgets section
- Widget favorites/bookmarks
- Widget search history
- Keyboard shortcuts for palette (Cmd+K to open, Esc to close)

## Files Modified This Session

1. `gearbox/internal/framework/database/plugins.go` - Added HAProxy plugin, set all to disabled by default
2. `gearbox/internal/framework/handler/handler.go` - Added dashboardStorage field and setter
3. `gearbox/internal/framework/handler/plugins.go` - Added deployPluginDashboards(), imports
4. `gearbox/cmd/server/main.go` - Set dashboard storage on handler

## Files That Need Creation/Modification

1. `gearbox/internal/framework/handler/dashboard.go` - Widget palette API
2. `gearbox/internal/framework/templates/pages/dashboard_editor.templ` - Palette panel UI
3. `gearbox/static/js/dashboard/palette.js` - Palette interactions (new file)
4. `gearbox/static/css/dashboard/palette.css` - Palette styling (new file)
5. `gearbox/static/js/dashboard/editor.js` - Extend drag-drop
6. `gearbox/internal/framework/templates/layouts/base.templ` - Navigation (complex)

---

Good night! The foundation is solid. The remaining work is primarily UI/UX implementation.
