# HAProxy Monitoring - Code Refactoring Plan

## Executive Summary

This document outlines a comprehensive refactoring plan for the gearbox codebase based on a thorough code review. The codebase is functional but requires improvements in security, code quality, maintainability, and testing.

**Codebase Size:**
- 151 Go files, 54,381 lines of Go code
- 48 templ template files
- ~200 total source files

**Overall Code Quality Score: 5.5/10**

## Critical Security Issues (Priority 1)

### 1. SQL Injection Risk
**File:** `internal/framework/database/database.go:671`
**Issue:** Direct table name interpolation using `fmt.Sprintf`
```go
err := d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
```
**Risk Level:** Medium (table names are internally controlled, but sets bad precedent)
**Fix:** Use whitelist validation for table names

### 2. Error Information Exposure
**Affected Files:** 70+ instances across handler package
**Issue:** Raw error messages exposed to clients
```go
http.Error(w, "Failed to fetch logs: "+err.Error(), http.StatusInternalServerError)
```
**Risk Level:** Medium (could leak internal paths, database structure)
**Fix:** Create sanitized error responses, log full errors server-side

### 3. Ignored Error from Decryption
**File:** `cmd/server/main.go:210`
```go
apiKey, _ := encryptor.DecryptString(dbServer.APIKeyEncrypted)
```
**Risk Level:** High (silently uses empty/corrupted API key)
**Fix:** Handle error explicitly, log and skip server if decryption fails

### 4. Input Validation Gaps
**Multiple Files:** Form handlers lack server-side validation
**Risk Level:** Medium
**Fix:** Add validation layer for all user inputs

## Code Quality Issues (Priority 2)

### 1. Handler Package Bloat
**File:** `internal/framework/handler/api.go` - 1,596 lines
**Issue:** Single file handles too many responsibilities
**Fix:** Split into:
- `api_stats.go` - Stats endpoints
- `api_logs.go` - Log endpoints
- `api_certificates.go` - Certificate endpoints
- `api_services.go` - Service control endpoints
- `api_traffic.go` - Traffic data endpoints

### 2. Type Safety Issues
**Issue:** Excessive use of `interface{}` and `map[string]interface{}`
- 92 occurrences of bare `interface{}`
- 50+ instances of `map[string]interface{}` for JSON responses
**Fix:** Create typed response structs:
```go
type StatsResponse struct {
    Stats     *models.Stats  `json:"stats"`
    UpdatedAt time.Time      `json:"updated_at"`
    Fresh     bool           `json:"fresh"`
}
```

### 3. Error Handling Inconsistency
**Issue:** Mixed patterns for error handling
- 8 instances of `_ =` (silently ignoring errors)
- Inconsistent `sql.ErrNoRows` handling
**Fix:** Standardize error handling patterns, never ignore errors without explicit comment

### 4. Logging Inconsistency
**Issue:** Mixed logging libraries (`log.Logger` vs `slog`)
**Fix:** Migrate entirely to `slog` with structured logging

## Template/Frontend Issues (Priority 2)

### 1. Large Monolithic Templates
**Files:**
- `traffic.templ` - 2,669 lines
- `integrations.templ` - 2,392 lines
- `haproxy_config.templ` - 2,226 lines

**Fix:** Decompose into smaller, reusable components

### 2. Inline JavaScript (222 instances)
**Issue:** Event handlers scattered throughout templates
```html
onclick="switchServer(this.value)"
onchange="changeTimeRange(this.value)"
```
**Fix:** Use event delegation with data attributes

### 3. Inline CSS (126 `<script>` blocks with styles)
**Fix:** Extract to separate stylesheets or Tailwind config

### 4. Generated Files in Git
**Issue:** 48 `*_templ.go` files committed (22K+ lines)
**Fix:** Add to `.gitignore`, document build process

## Database Issues (Priority 3)

### 1. No Migration Version Tracking
**Issue:** Migrations run on every startup without version control
**Fix:** Implement migration versioning system

### 2. Inconsistent ErrNoRows Handling
**Fix:** Standardize: "not found" vs "database error" responses

### 3. Missing Query Optimization
**Fix:** Add EXPLAIN PLAN comments for complex queries

## Testing Gaps (Priority 1)

### Current State
- Only 5 test files in 150+ Go files (3.3% coverage)
- `config_redaction_test.go` is the only substantial test
- No tests for:
  - API handlers (1,596 lines untested)
  - Database operations (1,079 lines untested)
  - Auth system
  - Plugin system

### Required Tests
1. **Unit Tests**
   - Database layer (all CRUD operations)
   - Auth system (password hashing, session management)
   - Configuration redaction
   - Permission checks

2. **Integration Tests**
   - API endpoints (all routes)
   - Plugin system
   - WebSocket connections

3. **Security Tests**
   - SQL injection attempts
   - XSS prevention
   - CSRF token validation
   - Permission bypass attempts

**Target:** 70%+ code coverage

## Documentation Gaps (Priority 3)

### Missing Documentation
1. No OpenAPI/Swagger spec for REST API
2. Handler functions lack godoc comments
3. Complex algorithms undocumented:
   - Traffic delta calculation
   - Alert evaluation logic
   - Metrics retention policies
4. No Architecture Decision Records (ADRs)

### Required Documentation
1. Add godoc comments to all exported functions
2. Create OpenAPI 3.0 specification
3. Document business logic with examples
4. Create ADR directory with key decisions

## Refactoring Phases

### Phase 1: Critical Security Fixes (4-6 hours)
1. Fix SQL injection risk in database.go
2. Create error sanitization layer
3. Fix ignored decryption error
4. Add input validation framework

**Deliverables:**
- `internal/framework/errors/` package for sanitized errors
- `internal/framework/validation/` package for input validation
- Fixed database.go with table name whitelist
- Updated main.go with proper error handling

### Phase 2: Handler Package Refactoring (6-8 hours)
1. Split `api.go` into separate files
2. Create typed response structs
3. Standardize error handling
4. Migrate to structured logging (slog)

**Deliverables:**
- `internal/framework/handler/api/` directory with split files
- `internal/framework/models/responses.go` with typed structs
- Updated handlers with consistent error patterns
- Removed all `interface{}` usage

### Phase 3: Template Refactoring (6-8 hours)
1. Decompose large templ files
2. Extract inline JavaScript to modules
3. Extract inline CSS
4. Update `.gitignore` for generated files

**Deliverables:**
- `internal/templates/components/traffic/` with decomposed components
- `static/js/` with extracted JavaScript modules
- Updated `.gitignore`

### Phase 4: Testing (12-16 hours)
1. Add database layer tests
2. Add API integration tests
3. Add auth system tests
4. Add security tests

**Deliverables:**
- 70%+ code coverage
- CI integration for tests
- Test documentation

### Phase 5: Database Improvements (4-6 hours)
1. Add migration version tracking
2. Standardize error handling
3. Add query optimization comments

**Deliverables:**
- `internal/framework/database/migrations/` with versioned migrations
- Consistent ErrNoRows handling
- EXPLAIN PLAN comments on complex queries

### Phase 6: Documentation (4-6 hours)
1. Add godoc comments
2. Create OpenAPI spec
3. Document complex algorithms
4. Create ADRs

**Deliverables:**
- Complete godoc coverage
- `docs/api/openapi.yaml`
- `docs/architecture/adr/` directory

## Total Estimated Effort

- **Phase 1 (Critical):** 4-6 hours
- **Phase 2 (High Priority):** 6-8 hours
- **Phase 3 (Medium Priority):** 6-8 hours
- **Phase 4 (High Priority):** 12-16 hours
- **Phase 5 (Medium Priority):** 4-6 hours
- **Phase 6 (Low Priority):** 4-6 hours

**Total:** 36-50 hours of development work

## Risk Assessment

### Low Risk (Can proceed immediately)
- Adding tests
- Adding documentation
- Extracting inline CSS/JS
- Logging migration

### Medium Risk (Requires careful testing)
- Splitting handler files
- Adding type safety
- Error handling changes

### High Risk (Requires staging environment validation)
- Database query changes
- Input validation (could break forms)
- Template decomposition (UI changes)

## Success Criteria

1. **Security:** All critical security issues resolved
2. **Code Quality:**
   - No files > 500 lines
   - No `interface{}` usage without explicit justification
   - Consistent error handling patterns
3. **Testing:** 70%+ code coverage
4. **Documentation:** All exported functions documented
5. **Maintainability:** New developers can understand code structure in < 1 hour

## Rollout Strategy

1. **Week 1:** Phase 1 (Security) + Phase 4 start (Tests)
2. **Week 2:** Phase 2 (Handlers) + Phase 4 continue
3. **Week 3:** Phase 3 (Templates) + Phase 5 (Database)
4. **Week 4:** Phase 4 complete + Phase 6 (Docs)

## Approval Required

Before proceeding, please review this plan and confirm:
1. Which phases should be prioritized?
2. Any specific concerns about breaking changes?
3. Preferred approach for template refactoring?
4. Testing strategy (unit vs integration focus)?

---

**Document Version:** 1.0
**Date:** 2026-01-27
**Author:** Claude Code (Sonnet 4.5)
