# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Model Preference

**Default Model:** Use Claude Sonnet (not Opus) for all tasks unless specifically requested otherwise by the user. Sonnet provides the optimal balance of speed, cost, and capability for typical development tasks.

## Project Overview

This repository contains **Gearbox**, a general-purpose monitoring and management platform with a plugin-based architecture and composable widgets.

### What is Gearbox?

A plugin-based server monitoring and management platform designed for DevOps. Provides real-time visibility into servers, services, and infrastructure through composable widgets.

**Architecture:**

- **gearbox-agent** - Go binary installed on monitored servers/workstations to gather data and expose secure API/WebSocket
- **gearbox** - Web dashboard (port 3000) for monitoring multiple servers with widget-based dashboards
- **Plugins** - Self-contained modules providing widgets and functionality (HAProxy, Metrics, Logs, Services, Certificates, Traffic, Alerts, OS Updates)
- **Widgets** - Reusable UI components that display data or provide controls
- **Dashboards** - YAML-defined pages arranging widgets in grid layouts

**Key Principles:**

- Plugin-based with composable widgets and components
- gearbox-agent can run on ANY Linux system (servers, workstations, HAProxy hosts, TrueNAS, Docker hosts)
- Multi-server support: gearbox talks to many different gearbox-agent instances
- Configuration via web UI or YAML files (GitOps-ready)
- HAProxy monitoring is ONE plugin, not the core purpose

## Dual-App Architecture

**CRITICAL**: This repository contains TWO interconnected Go applications:

1. **gearbox/** - Web dashboard that connects to multiple agents via WebSocket/REST (port 3000)
2. **gearbox-agent/** - Runs on monitored servers/workstations, collects data, exposes API (port 8405)

**Data Flow:** Agent collects → Agent API/WebSocket → Dashboard receives → Dashboard displays

**Multi-Server Support:** One gearbox dashboard can connect to many gearbox-agent instances running on different servers.

When troubleshooting, **ALWAYS check both codebases** and understand which server's agent is involved.

## Development Workflow

### Gearbox Dashboard (`gearbox/`)

Web application for monitoring multiple servers. Plugin-based with widget architecture.

**Post-Change Workflow:**

After ANY change to `gearbox/`, MUST run:

```bash
cd gearbox && make templ-generate && make build
```

**NEVER run** `make dev` - user typically has this running. Use `make build` to verify compilation only.

**Key directories:**

- `internal/framework/` - Shared services and building blocks
- `internal/plugins/` - 8 plugins providing 31 widgets total
- `internal/framework/templates/` - Templ templates
- `static/` - JavaScript, CSS, assets
- `data/dashboards/` - YAML dashboard definitions

### Gearbox Agent (`gearbox-agent/`)

Runs on monitored servers and workstations. Plugin-based collectors auto-discover services (HAProxy, Docker, systemd services).

**Building and Deploying:**

```bash
cd gearbox-agent && make deploy
```

**IMPORTANT**: Inform user if you run `make deploy` to avoid conflicts.

**Key directories:**

- `internal/api/` - REST API handlers
- `internal/plugins/` - Agent-side plugin collectors
- `internal/framework/` - Shared agent framework
- `cmd/gearbox-agent/` - Entry point

## Plugin Architecture

8 plugins provide 31 widgets total: HAProxy, Metrics, Logs, Services, Certificates, Traffic, Alerts, OS Updates. Each plugin is self-contained in `internal/plugins/`. Framework provides shared services in `internal/framework/`.

See [docs/plugins.md](docs/plugins.md) for complete plugin architecture documentation.

## Task Management (TASKS.md)

**CRITICAL**: This repository uses TASKS.md to track work items.

### Adding Tasks

When creating tasks in TASKS.md:

- Add them under the `## Active Tasks` section
- Use markdown checkbox syntax: `- [ ] Task description`
- Tasks should be clear, actionable, and specific

### Completing Tasks

- Check off tasks as you complete them: `- [x] Task description`
- Mark tasks complete immediately when work is done
- Don't batch completions - check them off right away

### Workflow Skills

Use these skills to work with TASKS.md:

- `/dowork` - Start the first unchecked task, ask questions as needed
- `/doallwork` - Complete the first unchecked task autonomously without questions

## User Preferences

### Code Block Formatting

- **Always use fenced code blocks** (triple backticks) for commands
- Fenced blocks provide a copy button in the IDE
- Never use inline code for commands the user should run

## Key Constraints

### NEVER

- Run `make dev` in gearbox (user typically has this running already)
- Edit generated `*_templ.go` files directly
- Skip running `make templ-generate` after template changes
- Add business logic to framework (belongs in plugins)
- Hardcode server-specific values (use multi-server config)

### ALWAYS

- Run `make templ-generate && make build` after template changes in gearbox
- Keep plugins self-contained
- Use framework services (don't duplicate functionality)
- Follow plugin architecture patterns
- Inform user before running `make deploy` for agent
- Validate HAProxy config with `haproxy -c` before reload (if applicable)

### Markdown Linting

Run `npx markdownlint-cli '**/*.md' --config .markdownlint.json` to validate. Key rules: blank lines around lists/code blocks, specify language for code blocks, proper headings, single newline at EOF.

### Creating New Documentation

Store in `docs/` directory using kebab-case naming. Include TOC after main heading. Reference in README.md.

### Creating Reports and Scan Results

**ALWAYS** store generated reports, scan results, and analysis documents in `docs/reports/`:

- Security scan reports
- Dependency audits
- Performance analysis
- Code quality reports
- Migration reports
- Any automated or manual analysis output

**Never** create these files in the repository root. This keeps the root clean and reports organized.

## Common Development Tasks

### Adding New Widgets

1. Create widget in appropriate plugin directory
2. Register in plugin's widget registry
3. Add Templ template for rendering
4. Update [docs/plugins.md](docs/plugins.md)

### Adding New Plugins

1. Create directory in `internal/plugins/`
2. Implement plugin interface
3. Register in framework plugin system
4. Add widgets and optional dashboards
5. Update [docs/plugins.md](docs/plugins.md)

### Modifying Templates (Templ)

1. Edit `.templ` files
2. Run `cd gearbox && make templ-generate`
3. Run `make build` to verify
4. DO NOT edit generated `*_templ.go` files

### Database Changes

1. Update models in `internal/framework/models/`
2. Add migration in `internal/framework/database/`
3. Update queries as needed
4. Test migration path

### API Changes

**Agent side:**
- Add handler in `gearbox-agent/internal/api/`
- Update Swagger docs if needed

**Dashboard side:**
- Update agent client in `gearbox/internal/framework/agent/`
- Handle responses in appropriate handler

## Documentation References

**Primary Sources:**

- **[README.md](README.md)**: Project overview and quick start - SOURCE OF TRUTH
- **[CLAUDE.md](CLAUDE.md)**: This file (development guidance)
- **[docs/plugins.md](docs/plugins.md)**: Complete plugin architecture documentation
- **[TASKS.md](TASKS.md)**: Active development tasks
- **[gearbox/docs/development.md](gearbox/docs/development.md)**: Local development guide

**Application Documentation:**

- **[gearbox/README.md](gearbox/README.md)**: Dashboard overview
- **[gearbox-agent/README.md](gearbox-agent/README.md)**: Agent overview and API
- **[gearbox-agent/docs/](gearbox-agent/docs/)**: API documentation

**Research & Historical:**

- **[docs/research/](docs/research/)**: Research and analysis documents

IMPORTANT: This context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.
