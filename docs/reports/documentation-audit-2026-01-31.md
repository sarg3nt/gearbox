# Documentation Audit Report

**Date:** 2026-01-31
**Scope:** Analysis of `/docs/claude-tasks`, `/docs/reports`, `/docs/research`

## Executive Summary

**Recommendation: DELETE claude-tasks directory entirely. It contains legacy Python/HAProxy docs not relevant to Gearbox.**

## Detailed Analysis

### 1. `/docs/claude-tasks` - **DELETE ENTIRELY** ❌

**Status:** 100% outdated - references non-existent Python project

**Files found:**
- `development-setup.md` - Python/pytest setup (Gearbox is Go)
- `github-actions-summary.md` - References HAProxy monitoring app (outdated)
- `pytest-migration-guide.md` - unittest → pytest migration (no Python in repo)
- `repository-setup-guide.md` - GitHub setup (useful but misplaced)
- `README.md` - References bash-to-python conversion (doesn't exist)

**Evidence this is legacy:**
- References `requirements-dev.txt` (doesn't exist)
- References Python testing frameworks (no Python code in repo)
- README mentions `bash-to-python-conversion.md` (file doesn't exist)
- All files reference "HAProxy monitoring application" instead of "Gearbox"

**What to keep:**
- Nothing - this is from a previous iteration of the project

### 2. `/docs/reports` - **KEEP** ✅

**Status:** Current, relevant, properly organized

**Purpose:** Consolidated location for generated reports and analysis

**Files (all relevant):**
- `README.md` - Directory documentation
- `security-scan-report.md` - Pre-commit security audit (2026-01-31)
- `security-migration-status.md` - Security feature tracking
- `plugin-work-summary.md` - Recent plugin work (2026-01-31)

**Recommendation:** Keep as-is. This is the correct place for reports.

### 3. `/docs/research` - **CONSOLIDATE** ⚠️

**Status:** Mix of useful and completed work

#### Keep (Core Design Docs):
- `plugin-system.md` (42 lines) - Initial plugin requirements ✅
- `plugin-system-analysis.md` (590 lines) - Detailed feasibility study ✅

#### Move to Reports (Completed Work):
- `refactoring-plan.md` (295 lines) - Completed refactoring plan
- `refactoring-summary.md` (314 lines) - Completed refactoring summary
- `slog-migration-summary.md` (244 lines) - Completed migration guide

**Reasoning:** These are reports of completed work, not ongoing research

#### Evaluate for Deletion (Debugging/Historical):
- `debug-websocket.md` (150 lines) - WebSocket debugging notes
- `traffic-bugs-context.md` (169 lines) - Traffic bug investigation

**Question:** Are these bugs fixed? If so, delete. If recurring issues, move to troubleshooting guide.

## Recommended Actions

### Immediate Actions

1. **Delete `/docs/claude-tasks` directory entirely**
   ```bash
   rm -rf /Users/dave/src/gearbox/docs/claude-tasks
   ```

2. **Move completed work from research to reports:**
   ```bash
   mv docs/research/refactoring-plan.md docs/reports/
   mv docs/research/refactoring-summary.md docs/reports/
   mv docs/research/slog-migration-summary.md docs/reports/
   ```

3. **Ask user about debugging docs:**
   - Is WebSocket debugging still needed?
   - Are traffic bugs fixed?

### After Cleanup

**Final structure:**

```
docs/
├── reports/                           # Generated reports & completed work
│   ├── README.md
│   ├── security-scan-report.md       # Security audit
│   ├── security-migration-status.md  # Feature tracking
│   ├── plugin-work-summary.md        # Recent work
│   ├── refactoring-plan.md          # Moved from research
│   ├── refactoring-summary.md       # Moved from research
│   └── slog-migration-summary.md    # Moved from research
│
└── research/                          # Active design docs only
    ├── README.md
    ├── plugin-system.md              # Core design
    └── plugin-system-analysis.md     # Feasibility study
    # Optional: debug-websocket.md (if still needed)
    # Optional: traffic-bugs-context.md (if bugs persist)
```

## Rationale

### Why Delete claude-tasks?

1. **Not relevant to Gearbox** - References Python, pytest, bash conversion
2. **Outdated** - References "HAProxy monitoring app" not "Gearbox platform"
3. **No Python in repo** - All docs reference Python tooling
4. **References missing files** - README mentions files that don't exist
5. **Confusing** - Will mislead AI and humans about tech stack

### Why Move refactoring docs to reports?

1. **Work is complete** - These document finished tasks, not ongoing research
2. **Reports directory purpose** - "Migration reports" fits perfectly
3. **Research is for design** - Active architectural decisions, not completed migrations
4. **Better organization** - Completed work separate from design docs

### Why Keep plugin-system docs in research?

1. **Core architecture** - Fundamental to understanding Gearbox design
2. **Reference material** - Needed when designing new plugins
3. **Design rationale** - Explains why plugin architecture was chosen
4. **Still relevant** - Plugin system is the foundation of Gearbox

## Impact Assessment

### Files to Delete: 5
- `docs/claude-tasks/` directory and all contents

### Files to Move: 3
- Research → Reports (completed work)

### Files to Keep: 5-7
- Reports: 4 files (current)
- Research: 2-4 files (design docs + optional debugging)

### Size Reduction
- Current: 17 markdown files across 3 directories
- After: 6-10 markdown files across 2 directories
- Reduction: ~40-60% fewer files, 100% relevant content

## Questions for User

1. **WebSocket debugging** - Are WebSocket issues resolved? Can we delete `debug-websocket.md`?
2. **Traffic bugs** - Are traffic visualization bugs fixed? Can we delete `traffic-bugs-context.md`?

If both are resolved, we can reduce research to just 2 core design documents.
