# slog Migration Summary

## Overview
This document summarizes the migration from `log.Logger` to structured logging with `slog.Logger` throughout the gearbox codebase.

## Files Requiring Migration

### Completed
- ✅ `internal/framework/config/config.go` - Migrated `LogConfigDebug` function to use slog

### Already Using slog
- ✅ `internal/framework/events/hub.go` - Already uses `*slog.Logger`
- ✅ `internal/framework/collector/websocket_manager.go` - Already uses `*slog.Logger`
- ✅ `internal/framework/agent/websocket.go` - Already uses `*slog.Logger`

### Requires Migration (18 files total)
1. `cmd/server/main.go` - Main application (49 logger calls)
2. `internal/framework/handler/handler.go`
3. `internal/framework/database/database.go`
4. `internal/framework/auth/auth.go`
5. `internal/framework/collector/manager.go`
6. `internal/framework/collector/registry.go`
7. `internal/framework/services/email/email.go`
8. `internal/framework/services/alerts/evaluator.go`
9. `internal/framework/services/server_adapter.go`
10. `internal/framework/plugin/manager.go`
11. `internal/framework/plugin/helpers.go`
12. `internal/framework/plugin/dependencies.go`
13. `internal/framework/middleware/permissions.go`

## Migration Patterns

### 1. Struct Field Updates
```go
// OLD
type Handler struct {
    logger *log.Logger
}

// NEW
type Handler struct {
    logger *slog.Logger
}
```

### 2. Constructor Parameters
```go
// OLD
func NewHandler(logger *log.Logger) *Handler {
    return &Handler{logger: logger}
}

// NEW
func NewHandler(logger *slog.Logger) *Handler {
    return &Handler{logger: logger}
}
```

### 3. Logger Initialization (main.go)
```go
// OLD
logger := log.New(os.Stdout, "", log.LstdFlags)

// NEW
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
```

### 4. Logging Call Conversions

#### Simple Info Messages
```go
// OLD
logger.Println("Authentication manager initialized")

// NEW
logger.Info("authentication manager initialized")
```

#### Formatted Messages with Data
```go
// OLD
logger.Printf("Database initialized (path: %s, retention: %dh)", cfg.DatabasePath, cfg.DatabaseRetentionHours)

// NEW
logger.Info("database initialized",
    "path", cfg.DatabasePath,
    "retention_hours", cfg.DatabaseRetentionHours)
```

#### Error Messages
```go
// OLD
logger.Printf("Failed to cleanup old data: %v", err)

// NEW
logger.Error("failed to cleanup old data", "error", err)
```

#### Server-Specific Logging
```go
// OLD
logger.Printf("[%s] Starting data collection", m.serverID)

// NEW
logger.Info("starting data collection", "server_id", m.serverID)
```

#### Multi-Value Logging
```go
// OLD
logger.Printf("Collector added for server: %s (%s)", serverConfig.Name, serverConfig.ID)

// NEW
logger.Info("collector added",
    "server_name", serverConfig.Name,
    "server_id", serverConfig.ID)
```

#### Fatal Errors
```go
// OLD
log.Fatalf("Failed to initialize database: %v", err)

// NEW
logger.Error("failed to initialize database", "error", err)
os.Exit(1)
```

### 5. Import Updates
Add to imports:
```go
import (
    "log/slog"
    // ... other imports
)
```

## Special Cases

### Admin Password Display (main.go lines 106-111)
```go
// OLD
logger.Printf("========================================")
logger.Printf("ADMIN USER CREATED")
logger.Printf("Email: admin")
logger.Printf("Password: %s", adminPassword)
logger.Printf("Please change this password after first login!")
logger.Printf("========================================")

// NEW
logger.Warn("========================================")
logger.Warn("ADMIN USER CREATED")
logger.Warn("admin credentials", "email", "admin", "password", adminPassword)
logger.Warn("Please change this password after first login!")
logger.Warn("========================================")
```

### Debug Logging with Conditionals
```go
// OLD
if enabled {
    logger.Printf("DEBUG: Starting process for %s", name)
}

// NEW
if enabled {
    logger.Debug("starting process", "name", name)
}
```

### Long Cleanup Routines (main.go lines 137-193)
```go
// OLD
if err := db.CleanupOldData(retentionDuration); err != nil {
    logger.Printf("Failed to cleanup old data: %v", err)
} else {
    logger.Println("Database cleanup completed")
}

// NEW
if err := db.CleanupOldData(retentionDuration); err != nil {
    logger.Error("failed to cleanup old data", "error", err)
} else {
    logger.Info("database cleanup completed")
}
```

## Testing After Migration

After completing the migration, run:

```bash
cd gearbox
make templ-generate && make build
```

This will:
1. Generate templ templates
2. Build the binary with the new slog logging
3. Verify all changes compile successfully

## Benefits of slog

1. **Structured Logging**: Key-value pairs instead of formatted strings
2. **Better Parsing**: Logs can be easily parsed by log aggregation tools
3. **Performance**: slog is optimized for performance
4. **Flexibility**: Easy to change output format (JSON, text, etc.)
5. **Context Support**: Better integration with context.Context
6. **Type Safety**: Keys and values are strongly typed

## Example Output Comparison

### Old (log.Logger)
```
2026/01/27 10:30:15 Database initialized (path: /data/haproxy-monitor.db, retention: 168h)
2026/01/27 10:30:15 Collector added for server: prod-haproxy (haproxy-01)
```

### New (slog.Logger)
```
2026-01-27T10:30:15.123Z INFO database initialized path=/data/haproxy-monitor.db retention_hours=168
2026-01-27T10:30:15.124Z INFO collector added server_name=prod-haproxy server_id=haproxy-01
```

## Next Steps

1. Complete migration of all 18 files listed above
2. Update all `NewXxx()` constructors to accept `*slog.Logger`
3. Replace all `logger.Printf()` calls with appropriate slog methods
4. Test build: `make templ-generate && make build`
5. Test runtime to ensure logging works correctly
6. Consider adding JSON output option for production deployments

## Notes

- Keep `log` import for `log.Fatalf` at top level (before logger is created)
- Use `logger.Info()` for informational messages
- Use `logger.Warn()` for warnings
- Use `logger.Error()` for errors
- Use `logger.Debug()` for debug messages (requires setting log level)
- Always use key-value pairs for structured data
- Use descriptive keys that are easy to parse and search
