# Gearbox - Completed Tasks Archive

**Purpose:** Archive of completed tasks moved from TASKS.md to keep the active task file lean.

---

## Server → Box Rebrand ✅

**Status:** COMPLETE (2026-01-31)

**Summary:** Rebranded "server" terminology to "box" throughout the codebase (~80 files, 14 phases). Database schema, Go types, handlers, templates, JavaScript, routes, permissions, audit logs, and documentation all updated. Application builds successfully.

---

## Plugin-to-Widget Architecture Migration (REVERTED)

**Status:** REVERTED (2026-02-28)

**Summary:** Widget-based dashboard architecture was implemented but later reverted in favor of simpler plugin-based pages. The approach was over-engineered for the project's needs. Reusable components (chart partials, metrics partials, services partials, API endpoints) were retained as standalone pieces.
