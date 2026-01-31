# Local Development Guide

Fast local development workflow for the Gearbox application on macOS.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Development Workflow Options](#development-workflow-options)
  - [Option 1: Hot Reload with Air (Recommended)](#option-1-hot-reload-with-air-recommended)
  - [Option 2: Manual Rebuild](#option-2-manual-rebuild)
  - [Option 3: VS Code Launch Configuration](#option-3-vs-code-launch-configuration)
- [VS Code Integration](#vs-code-integration)
  - [Required Extensions](#required-extensions)
  - [Launch Configuration](#launch-configuration)
  - [Templ Support](#templ-support)
- [Understanding the Build Process](#understanding-the-build-process)
- [Connecting to HAProxy Servers](#connecting-to-haproxy-servers)
- [Troubleshooting](#troubleshooting)
- [Comparison: Local vs Container Development](#comparison-local-vs-container-development)

## Overview

Running locally eliminates the container build/push/deploy cycle:

| Workflow | Time per Change |
|----------|-----------------|
| Container (old) | 60-90 seconds |
| Local with Air | 2-5 seconds |
| Local manual | 5-10 seconds |

## Prerequisites

1. **Go 1.25+**

   ```bash
   brew install go
   ```

2. **Xcode Command Line Tools** (required for SQLite CGO)

   ```bash
   xcode-select --install
   ```

3. **Development tools**

   ```bash
   cd gearbox
   make install-tools
   ```

   This installs:
   - `templ` - Template compiler
   - `air` - Hot reload tool
   - `golangci-lint` - Linter

## Quick Start

1. **Navigate to the project**

   ```bash
   cd /Users/dave/src/ubuntu-ha-proxy-install/gearbox
   ```

2. **Ensure .env file exists** (should already be configured)

   ```bash
   cat .env
   ```

3. **Run with hot reload**

   ```bash
   make dev-local
   ```

4. **Open browser**

   Navigate to <http://localhost:3000>

   Login: `admin` / `be_like_bob_123` (from .env)

## Development Workflow Options

### Option 1: Hot Reload with Air (Recommended)

Air watches for file changes and automatically rebuilds/restarts the application.

```bash
make dev-local
```

**What happens:**

1. Air watches all `.go` and `.templ` files
2. On change: runs `templ generate`, then rebuilds the binary
3. Restarts the server automatically
4. Browser refresh shows changes in 2-5 seconds

**Configuration:** The `.air.toml` file in the project root controls Air's behavior.

### Option 2: Manual Rebuild

For more control over when rebuilds happen:

```bash
# Terminal 1: Build and run
make run-local

# After making changes, Ctrl+C and re-run
make run-local
```

Or build separately:

```bash
make build
./bin/gearbox
```

### Option 3: VS Code Launch Configuration

See [VS Code Integration](#vs-code-integration) below for debugger support.

## VS Code Integration

### Required Extensions

Install these extensions for the best development experience:

1. **Go** (`golang.go`) - Essential Go support

   ```text
   ext install golang.go
   ```

2. **templ-vscode** (`a-h.templ`) - Templ template support

   ```text
   ext install a-h.templ
   ```

3. **Error Lens** (`usernamehw.errorlens`) - Inline error display (optional but helpful)

   ```text
   ext install usernamehw.errorlens
   ```

### Launch Configuration

VS Code launch and task configurations are already set up in `.vscode/`:

- `.vscode/launch.json` - Debug configurations
- `.vscode/tasks.json` - Build tasks
- `.vscode/settings.json` - Editor settings for Go and Templ
- `.vscode/extensions.json` - Recommended extensions

**Available launch configurations:**

| Configuration | Description |
|--------------|-------------|
| Run Gearbox | Runs with debugger, generates templates first |
| Run (Skip Templ Generate) | Runs with debugger, skips template generation |
| Debug Current Test | Debug the test file currently open |

**Usage:**

- Press `F5` to run with debugger (generates templates first)
- Set breakpoints in Go code
- Use the Debug Console for inspection

### Templ Support

The `templ-vscode` extension provides:

- Syntax highlighting for `.templ` files
- Go to definition
- Auto-completion
- Error highlighting

Format-on-save is already configured in `.vscode/settings.json`.

## Understanding the Build Process

The application uses Templ for type-safe HTML templates:

```text
*.templ files → templ generate → *_templ.go files → go build → binary
```

**Key points:**

1. **Templ files** (`internal/templates/**/*.templ`) are the source
2. **Generated files** (`*_templ.go`) are Go code - don't edit these
3. Must run `templ generate` before building when templates change
4. Air handles this automatically; manual workflow requires explicit generation

**Example change workflow:**

```bash
# Edit a template
vim internal/templates/pages/overview.templ

# If using Air, it auto-rebuilds
# If manual, run:
make templ-generate
make run-local
```

## Connecting to HAProxy Servers

The app connects to HAProxy servers via the Agent API. For local development:

### Using Real Servers

Configure servers in the UI at `/settings/servers`:

- **Agent URL:** `https://light-hugger:8405` (or your HAProxy server)
- **API Key:** The Bearer token from your gearbox-agent

### Mock Data (No Server)

The app will start without any configured servers. You can:

1. Browse the UI and see the empty state
2. Add a server configuration through the settings page
3. Test UI changes without a real HAProxy connection

## Troubleshooting

### "undefined: component" errors

Templates need regeneration:

```bash
make templ-generate
```

### CGO/SQLite errors

Ensure Xcode tools are installed:

```bash
xcode-select --install
```

### Port 3000 already in use

Find and kill the process:

```bash
lsof -i :3000
kill -9 <PID>
```

Or change the port in `.env`:

```bash
HTTP_PORT=3001
```

### Air not watching files

Restart Air or check `.air.toml` configuration:

```bash
# Kill any running air processes
pkill air
make dev-local
```

### Templates not updating

1. Check that `templ generate` runs (visible in Air output)
2. Hard refresh browser (Cmd+Shift+R)
3. Clear browser cache

## Comparison: Local vs Container Development

| Aspect | Local (make dev-local) | Container (make docker-build-dev) |
|--------|------------------------|-----------------------------------|
| **Startup time** | 1-2 seconds | 60-90 seconds |
| **Change cycle** | 2-5 seconds | 60-90 seconds |
| **Debugging** | Full VS Code debugger | Container logs only |
| **Hot reload** | Yes (Air) | No |
| **Environment** | macOS native | Linux container |
| **Dependencies** | Local Go install | Docker only |

**Recommendation:** Use local development for active coding, container builds for final verification before commit.

## Workflow Summary

### Daily Development

```bash
# Start hot reload server
cd gearbox
make dev-local

# Make changes, browser auto-refreshes
# Ctrl+C when done
```

### Before Committing

```bash
# Run tests
make test

# Run linter
make lint

# Verify container build works
make docker-build
```

### Deploy to Production

```bash
# Build and push container, trigger Portainer
make docker-build-dev
```
