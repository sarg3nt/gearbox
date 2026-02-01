# Gearbox - Completed Tasks Archive

**Purpose:** Archive of completed tasks moved from TASKS.md to keep the active task file lean.

---

## Server → Box Rebrand ✅

**Status:** COMPLETE (2026-01-31)

**Summary:** Rebranded "server" terminology to "box" throughout the codebase (~80 files, 14 phases). Database schema, Go types, handlers, templates, JavaScript, routes, permissions, audit logs, and documentation all updated. Application builds successfully.

---

## Plugin-to-Widget Architecture Migration ✅

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
