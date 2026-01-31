# CSP-Compliant Local Development

This document explains how to run Gearbox locally with strict Content Security Policy (CSP) enabled.

## Overview

Gearbox supports two asset loading modes:

1. **Production Mode (Default)**: Uses CDN resources (Tailwind, HTMX, Chart.js, etc.)
   - Best for production deployments
   - Automatic updates from CDNs
   - Good performance via edge caching
   - CSP allows specific CDN domains

2. **Local Assets Mode**: Uses self-hosted assets downloaded to `/static/js/vendor` and `/static/css/vendor`
   - Ultra-strict CSP (only `'self'` allowed)
   - No external dependencies
   - Best for security-focused development
   - Works offline

## Quick Start for CSP-Compliant Development

### 1. Download CDN Assets Locally

```bash
cd gearbox
make dev-assets
```

This downloads:
- Tailwind CSS
- HTMX
- morphdom
- Chart.js
- Hammer.js
- Chart.js Zoom Plugin
- Tabulator (JS and CSS)

### 2. Enable Local Assets Mode

Add to your `.env` file:

```bash
USE_LOCAL_ASSETS=true
```

### 3. Run the Application

```bash
# Option 1: With air hot-reload
make dev

# Option 2: Direct run
make run

# Option 3: Build and run binary
make build
./bin/gearbox
```

## How It Works

### Asset Loading

The [base.templ](../internal/framework/templates/layouts/base.templ) template conditionally loads assets:

```go
if middleware.UseLocalAssets(ctx) {
    <!-- Local assets -->
    <script src="/static/js/vendor/tailwind.js"></script>
} else {
    <!-- CDN assets -->
    <script src="https://cdn.tailwindcss.com"></script>
}
```

### Content Security Policy

The CSP is configured in [security_headers.go](../internal/framework/middleware/security_headers.go):

- **Local Assets Mode** (`USE_LOCAL_ASSETS=true`):
  ```
  script-src 'self' 'unsafe-inline'
  ```

- **Production Mode** (default):
  ```
  script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com https://cdn.jsdelivr.net
  ```

## Updating Assets

To update the locally downloaded assets to the latest versions:

```bash
cd gearbox
make dev-assets
```

This re-downloads all assets from their CDNs.

## Production Deployment

For production, **do NOT set** `USE_LOCAL_ASSETS=true`. The default production mode:

- Uses CDN resources (always up-to-date)
- Better performance (edge caching, HTTP/2 multiplexing)
- Shared caching across websites
- CSP still enforces specific allowed CDN domains

## Security Comparison

### Local Assets Mode (Development)

**Pros:**
- Ultra-strict CSP (maximum security)
- No external dependencies
- Works offline
- Full control over asset versions

**Cons:**
- Manual updates required
- Larger Docker images if bundled
- No CDN edge caching benefits

### Production Mode (Default)

**Pros:**
- Automatic updates from CDNs
- Better performance (edge caching)
- Smaller Docker images
- Still secure with domain-specific CSP

**Cons:**
- Depends on external CDNs
- Requires network connectivity
- CDN domains must be in CSP whitelist

## Troubleshooting

### CSP Violations in Browser Console

If you see CSP errors like:

```
Content-Security-Policy: The page's settings blocked a script (script-src-elem) at https://cdn.tailwindcss.com/
```

**Solution**: Either:
1. Set `USE_LOCAL_ASSETS=true` and run `make dev-assets`
2. Remove `USE_LOCAL_ASSETS` from `.env` to use production CDN mode

### Missing Assets (404 Errors)

If you set `USE_LOCAL_ASSETS=true` but see 404 errors for `/static/js/vendor/*`:

**Solution**: Run `make dev-assets` to download the assets

### Session Expired Errors

This is unrelated to CSP/assets. Check your session configuration and cookies.

## Files Modified

- [`Makefile`](../Makefile) - Added `dev-assets` target
- [`internal/framework/models/server.go`](../internal/framework/models/server.go) - Added `UseLocalAssets` config field
- [`internal/framework/config/config.go`](../internal/framework/config/config.go) - Load `USE_LOCAL_ASSETS` env var
- [`internal/framework/middleware/assets.go`](../internal/framework/middleware/assets.go) - Context injection
- [`internal/framework/middleware/security_headers.go`](../internal/framework/middleware/security_headers.go) - Conditional CSP
- [`internal/framework/templates/layouts/base.templ`](../internal/framework/templates/layouts/base.templ) - Conditional asset loading
- [`cmd/server/main.go`](../cmd/server/main.go) - Inject asset config middleware

## Best Practices

1. **Development**: Use local assets mode for strict CSP compliance
2. **Production**: Use CDN mode (default) for best performance and auto-updates
3. **Testing**: Test both modes before deployment
4. **Updates**: Run `make dev-assets` periodically to update local assets
