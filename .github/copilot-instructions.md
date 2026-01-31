# Copilot Instructions for Gearbox

## Project Overview

**Gearbox** is a general-purpose monitoring and management platform with a plugin-based architecture and composable widgets. It consists of two Go applications:

1. **gearbox** - Web dashboard (port 3000)
2. **gearbox-agent** - Agent running on monitored servers (port 8405)

**Key Principle**: This is a plugin-based architecture. The framework provides shared services, and plugins provide widgets and functionality.

## Architecture

### Dual Application Structure

```
gearbox/          - Web dashboard application
gearbox-agent/    - Agent binary for monitored servers
```

**Data Flow:**
```
Monitored Server → gearbox-agent (collects) → API/WebSocket → gearbox (displays) → Browser
```

### Plugin Architecture

**Framework** (gearbox/internal/framework/):
- Agent client (WebSocket/REST communication)
- Database models and access
- Authentication and authorization
- Event bus for real-time updates
- Shared UI components (graphs, tables, panels)
- Template system (Templ)

**Plugins** (gearbox/internal/plugins/):
- Self-contained feature modules
- Provide widgets (reusable UI components)
- Optional predefined dashboards
- Domain-specific logic

Current plugins: HAProxy, Metrics, Services, Certificates, Logs, Traffic, Alerts, OS Updates

## Development Workflow

### Gearbox Dashboard (gearbox/)

**CRITICAL**: After ANY change to template files:
```bash
cd gearbox && make templ-generate && make build
```

**Local development with hot reload:**
```bash
cd gearbox && make dev
```

**NEVER run `make dev` when assisting user** - they typically have it running already.

**Key directories:**
- `internal/framework/` - Core framework services
- `internal/plugins/` - Feature plugins
- `internal/framework/templates/` - Templ templates
- `static/` - JavaScript, CSS, assets
- `data/dashboards/` - YAML dashboard definitions

### Gearbox Agent (gearbox-agent/)

**Build and deploy:**
```bash
cd gearbox-agent && make deploy
```

**IMPORTANT**: Always inform user when running `make deploy` to avoid conflicts.

**Key directories:**
- `internal/api/` - REST API handlers
- `internal/plugins/` - Agent-side plugin collectors
- `internal/framework/` - Shared agent framework
- `cmd/gearbox-agent/` - Entry point

## Code Guidelines

### Adding New Widgets

1. Create widget in appropriate plugin directory
2. Register in plugin's widget registry
3. Add Templ template for rendering
4. Update plugin documentation

### Adding New Plugins

1. Create directory in `internal/plugins/`
2. Implement plugin interface
3. Register in framework plugin system
4. Add widgets and optional dashboards
5. Update docs/plugins.md

### Template System (Templ)

- Use Templ for all HTML rendering
- Templates are type-safe Go code
- Run `make templ-generate` after changes
- Templates live in `internal/framework/templates/` and plugin directories

### Multi-Server Support

- One dashboard connects to multiple agents
- Server configuration stored in database
- Plugins enabled per-server
- WebSocket connections managed by framework

## Testing

**Run tests:**
```bash
cd gearbox && go test ./...
cd gearbox-agent && go test ./...
```

**Run with coverage:**
```bash
make test
```

## Common Tasks

### Adding a Dashboard Widget

1. Create widget component in plugin
2. Add Templ template
3. Register in widget palette
4. Add to dashboard YAML or via UI

### Modifying API Endpoints

**Agent side:**
- Add handler in `gearbox-agent/internal/api/`
- Update Swagger docs
- Regenerate with `swag init`

**Dashboard side:**
- Update agent client in `gearbox/internal/framework/agent/`
- Handle responses in appropriate handler

### Database Changes

1. Update models in `internal/framework/models/`
2. Add migration in `internal/framework/database/`
3. Update queries as needed
4. Test migration path

## Key Constraints

### NEVER

- Run `make dev` when user already has it running
- Edit generated `*_templ.go` files directly
- Skip running `make templ-generate` after template changes
- Add business logic to framework (belongs in plugins)
- Hardcode server-specific values (use multi-server config)

### ALWAYS

- Run `make templ-generate && make build` after template changes
- Keep plugins self-contained
- Use framework services (don't duplicate functionality)
- Follow plugin architecture patterns
- Document new widgets in docs/plugins.md

## Documentation

**Primary sources:**
- [README.md](../README.md) - Project overview
- [CLAUDE.md](../CLAUDE.md) - Development guidance
- [docs/plugins.md](../docs/plugins.md) - Plugin architecture
- [gearbox/docs/development.md](../gearbox/docs/development.md) - Local setup
- [gearbox-agent/README.md](../gearbox-agent/README.md) - Agent overview

## Build System

**Gearbox Makefile targets:**
- `make build` - Build binary
- `make dev` - Run with hot reload and .env loaded
- `make run` - Run with .env loaded
- `make templ-generate` - Generate Templ templates
- `make test` - Run tests
- `make clean-data` - Clear development database

**Agent Makefile targets:**
- `make build` - Build agent binary
- `make deploy` - Build and deploy to configured server
- `make test` - Run tests

## Security

- Use framework authentication/authorization
- Validate all user inputs
- Use prepared statements for database queries
- TLS required for agent connections
- API keys for agent authentication
- Session management via framework

## Performance

- WebSocket for real-time updates
- Event bus for decoupled communication
- Database connection pooling
- Efficient template rendering
- Widget-level updates (not full page reloads)
