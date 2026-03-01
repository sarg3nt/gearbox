# Copilot Instructions for Gearbox

## Project Overview

**Gearbox** is a general-purpose monitoring and management platform with a gear-based architecture. It consists of two Go applications:

1. **gearbox** - Web dashboard (port 3000)
2. **gearbox-agent** - Agent running on monitored servers (port 8405)

**Key Principle**: This is a gear-based architecture. The framework provides shared services, and plugins provide functionality.

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

### Gear Architecture

**Framework** (gearbox/internal/framework/):
- Agent client (WebSocket/REST communication)
- Database models and access
- Authentication and authorization
- Event bus for real-time updates
- Shared UI components (graphs, tables, panels)
- Template system (Templ)

**Plugins** (gearbox/internal/gears/):
- Self-contained feature modules
- Provide pages and API handlers
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
- `internal/gears/` - Feature plugins
- `internal/framework/templates/` - Templ templates
- `static/` - JavaScript, CSS, assets

### Gearbox Agent (gearbox-agent/)

**Build and deploy:**
```bash
cd gearbox-agent && make deploy
```

**IMPORTANT**: Always inform user when running `make deploy` to avoid conflicts.

**Key directories:**
- `internal/api/` - REST API handlers
- `internal/gears/` - Agent-side plugin collectors
- `internal/framework/` - Shared agent framework
- `cmd/gearbox-agent/` - Entry point

## Code Guidelines

### Adding New Gears

1. Create directory in `internal/gears/`
2. Implement gear interface
3. Register in framework gear system
4. Add pages and templates
5. Update docs/gears.md

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
- Add business logic to framework (belongs in gears)
- Hardcode server-specific values (use multi-server config)

### ALWAYS

- Run `make templ-generate && make build` after template changes
- Keep gears self-contained
- Use framework services (don't duplicate functionality)
- Follow gear architecture patterns
- Document new plugins in docs/gears.md

## Documentation

**Primary sources:**
- [README.md](../README.md) - Project overview
- [CLAUDE.md](../CLAUDE.md) - Development guidance
- [docs/gears.md](../docs/gears.md) - Plugin architecture
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
- Partial updates via HTMX (not full page reloads)
