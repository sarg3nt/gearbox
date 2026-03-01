# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains **Gearbox**, a general-purpose monitoring and management platform with a gear-based architecture.

### What is Gearbox?

A gear-based server monitoring and management platform designed for DevOps. Provides real-time visibility into servers, services, and infrastructure through purpose-built gear pages.

**Architecture:**

- **gearbox-agent** - Go binary installed on monitored servers/workstations to gather data and expose secure API/WebSocket
- **gearbox** - Web dashboard (port 3000) for monitoring multiple servers
- **Gears** - Self-contained modules providing pages, API handlers, and functionality (HAProxy, Metrics, Logs, Services, Certificates, Traffic, Alerts, OS Updates)

**Key Principles:**

- Gear-based architecture with shared framework components
- gearbox-agent can run on ANY Linux system (servers, workstations, HAProxy hosts, TrueNAS, Docker hosts)
- Multi-server support: gearbox talks to many different gearbox-agent instances
- Configuration via web UI
- HAProxy monitoring is ONE gear, not the core purpose

## Dual-App Architecture

**CRITICAL**: This repository contains TWO interconnected Go applications:

1. **gearbox/** - Web dashboard that connects to multiple agents via WebSocket/REST (port 3000)
2. **gearbox-agent/** - Runs on monitored servers/workstations, collects data, exposes API (port 8405)

**Data Flow:** Agent collects → Agent API/WebSocket → Dashboard receives → Dashboard displays

**Multi-Server Support:** One gearbox dashboard can connect to many gearbox-agent instances running on different servers.

When troubleshooting, **ALWAYS check both codebases** and understand which server's agent is involved.

## Development Workflow

### Gearbox Dashboard (`gearbox/`)

Web application for monitoring multiple servers. Gear-based architecture.

**Post-Change Workflow:**

After ANY change to `gearbox/`, MUST run:

```bash
cd gearbox && make templ-generate && make build
```

**NEVER run** `make dev` - user typically has this running. Use `make build` to verify compilation only.

**Key directories:**

- `internal/framework/` - Shared services and building blocks
- `internal/gears/` - 8 gears
- `internal/framework/templates/` - Templ templates
- `static/` - JavaScript, CSS, assets

### Gearbox Agent (`gearbox-agent/`)

Runs on monitored servers and workstations. Gear-based collectors auto-discover services (HAProxy, Docker, systemd services).

**Building and Deploying:**

```bash
cd gearbox-agent && make deploy
```

**IMPORTANT**: Inform user if you run `make deploy` to avoid conflicts.

**Key directories:**

- `internal/api/` - REST API handlers
- `internal/gears/` - Agent-side gear collectors
- `internal/framework/` - Shared agent framework
- `cmd/gearbox-agent/` - Entry point

## Gear Architecture

8 gears: HAProxy, Metrics, Logs, Services, Certificates, Traffic, Alerts, OS Updates. Each gear is self-contained in `internal/gears/`. Framework provides shared services in `internal/framework/`.

See [docs/gears.md](docs/gears.md) for complete gear architecture documentation.

## GitHub-Driven Workflow

**CRITICAL**: All features and bugs go through a full GitHub workflow. Claude manages this end-to-end.

### Workflow Steps

When a new feature is planned or a bug is reported:

1. **Create a GitHub Issue** — Use `gh issue create` with a clear title, description, and appropriate labels (`enhancement`, `bug`, etc.)
2. **Add to Project Board** — Add the issue to the GitHub Project board using `gh project item-add`
3. **Create a Feature Branch** — Branch from `main` using the naming convention below
4. **Do the Work** — Implement the feature or fix on the branch
5. **Ask Before Creating PR** — Always ask the user for confirmation before creating a PR. Do not create PRs automatically.
6. **Create a PR** — Use `gh pr create` targeting `main`, linked to the issue (use `Closes #N` in the body)
7. **Track Progress** — Keep the project board and issues in sync
8. **Complete** — When user confirms done: merge PR, close issue, move project card to Done

### Branch Naming Convention

- **Features:** `feature/short-description` (e.g., `feature/dashboard-export`)
- **Bug fixes:** `fix/short-description` (e.g., `fix/websocket-reconnect`)
- **Always branch from `main`**

### PR Convention

- All PRs target `main`
- PR body must include `Closes #<issue-number>` to auto-close the issue on merge
- Use the standard PR template format (Summary, Test Plan)

### Issue Labels

Use these labels consistently:

- `enhancement` — New features
- `bug` — Bug fixes
- `documentation` — Docs-only changes
- `refactor` — Code improvements without behavior change

### Project Board

- **Project Number:** 3
- **Project ID:** `PVT_kwHOADN1xs4BOB1W`
- **Owner:** `sarg3nt`

Use `gh project` commands and the MCP GitHub Projects tool to:

- Add new issues to the board
- Move items between columns as work progresses
- Query board status when reporting progress

## TASKS.md as Scratch Pad

TASKS.md is a **scratch pad only** — not a tracking system. The GitHub Project board is the source of truth for all work items.

### How TASKS.md is used

- User writes rough ideas, feature descriptions, or bug notes in TASKS.md
- Claude reads TASKS.md, breaks the content into actionable GitHub issues, and adds them to the project board
- Once issues are created, the content in TASKS.md can be cleared
- TASKS.md is never the source of truth — the project board is

### Workflow Skills

- `/dowork` - Read TASKS.md, create issues from it, and start working (ask questions as needed)
- `/doallwork` - Read TASKS.md, create issues from it, and work autonomously

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
- Add business logic to framework (belongs in gears)
- Hardcode server-specific values (use multi-server config)

### ALWAYS

- Run `make templ-generate && make build` after template changes in gearbox
- Keep gears self-contained
- Use framework services (don't duplicate functionality)
- Follow gear architecture patterns
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

### Adding New Gears

1. Create directory in `internal/gears/`
2. Implement gear interface
3. Register in framework gear system
4. Add pages and templates
5. Update [docs/gears.md](docs/gears.md)

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
- **[docs/gears.md](docs/gears.md)**: Complete gear architecture documentation
- **[TASKS.md](TASKS.md)**: Scratch pad for describing upcoming work
- **[gearbox/docs/development.md](gearbox/docs/development.md)**: Local development guide

**Application Documentation:**

- **[gearbox/README.md](gearbox/README.md)**: Dashboard overview
- **[gearbox-agent/README.md](gearbox-agent/README.md)**: Agent overview and API
- **[gearbox-agent/docs/](gearbox-agent/docs/)**: API documentation

**Research & Historical:**

- **[docs/research/](docs/research/)**: Research and analysis documents

IMPORTANT: This context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.
