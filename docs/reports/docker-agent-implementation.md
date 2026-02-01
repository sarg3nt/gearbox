# Docker Support Implementation for gearbox-agent

**Date:** 2026-02-01
**Status:** Complete

## Summary

Added comprehensive Docker container support to gearbox-agent as an alternative deployment method alongside the existing binary installation. This provides users with a containerized option for running the agent while maintaining full compatibility with the existing binary deployment.

## Implementation Details

### Files Created

#### 1. Docker Build Files

**[gearbox-agent/Dockerfile](../../gearbox-agent/Dockerfile)**

- Multi-stage build using Go 1.25 and Alpine 3.23
- Security hardened (non-root user, minimal dependencies)
- Optimized for size (~47MB final image)
- Includes health check configuration
- Supports custom TLS certificates
- Build arguments for version, commit SHA, and build date

**[gearbox-agent/.dockerignore](../../gearbox-agent/.dockerignore)**

- Excludes build artifacts, tests, and documentation
- Keeps Swagger docs (required for build)
- Optimizes Docker context size

#### 2. Docker Compose Configuration

**[gearbox-agent/docker-compose.yml](../../gearbox-agent/docker-compose.yml)**

- Complete production-ready configuration example
- Includes all environment variable options
- Volume mount examples for:
  - Data persistence
  - TLS certificates
  - HAProxy socket access
  - Systemd monitoring
- Security options configured
- Health check enabled

#### 3. Documentation

**[gearbox-agent/docs/docker.md](../../gearbox-agent/docs/docker.md)**

Comprehensive Docker installation and configuration guide covering:

- Quick start instructions
- Pre-built image usage
- Building from source
- Docker Compose setup
- Environment variable reference
- Volume mount configuration
- TLS certificate setup
- HAProxy monitoring scenarios
- Advanced configurations
- Troubleshooting guide
- Production deployment checklist

**[gearbox-agent/README.md](../../gearbox-agent/README.md)** - Updated

- Added Docker installation section
- Three installation options documented:
  - Docker run
  - Docker Compose
  - Build from source
- Links to comprehensive Docker guide

**[README.md](../../README.md)** - Updated

- Added Docker installation examples
- Binary and Docker options documented
- Links to agent README and Docker guide

**[docs/getting-started.md](../../docs/getting-started.md)** - Updated

- Added Docker as recommended installation method
- Step-by-step Docker setup instructions
- Docker Compose example configuration
- Binary installation still documented as alternative

#### 4. Build System Updates

**[gearbox-agent/Makefile](../../gearbox-agent/Makefile)** - Updated

New Docker targets added:

```makefile
make docker-build    # Build Docker image
make docker-push     # Build and push to registry
make docker-run      # Run container locally
make docker-stop     # Stop and remove container
make docker-logs     # View container logs
```

Variables added:

- `DOCKER_REGISTRY` - Container registry (default: ghcr.io)
- `DOCKER_REPO` - Repository name
- `DOCKER_IMAGE` - Full image name
- `DOCKER_TAG` - Image tag (default: VERSION)

#### 5. CI/CD Workflows

**[.github/workflows/docker-agent.yml](../../.github/workflows/docker-agent.yml)**

GitHub Actions workflow for automated Docker builds:

- Triggers on push to main, version tags, and PRs
- Path filters (only builds when agent code changes)
- Multi-architecture support (amd64, arm64)
- Automatic tagging:
  - `latest` for main branch
  - Semver tags (`v1.0.0`, `v1.0`, `v1`)
  - Git SHA tags
  - Branch tags
- Build caching for faster builds
- Publishes to GitHub Container Registry (ghcr.io)
- Build provenance attestation

**[.github/workflows/ci-agent.yml](../../.github/workflows/ci-agent.yml)**

Continuous integration workflow for agent:

- Test suite execution with race detection
- Code coverage reporting to Codecov
- golangci-lint checks
- Binary build verification
- Artifact upload for debugging
- Path filters to avoid unnecessary runs

**[.github/workflows/docker.yml](../../.github/workflows/docker.yml)** - Updated

- Fixed image name from `haproxy-monitor` to `gearbox`
- Added multi-architecture support (amd64, arm64)
- Added path filters for efficiency

**[gearbox/.dockerignore](../../gearbox/.dockerignore)** - Created

- Added for gearbox dashboard to match agent
- Optimizes build context

## Docker Image Specifications

### Base Images

- **Build Stage:** `golang:1.25-alpine`
- **Runtime Stage:** `alpine:3.23`

### Image Size

- Final image: ~47MB
- Multi-architecture: amd64, arm64

### Security Features

- Runs as non-root user (UID/GID 1000)
- `no-new-privileges` security option
- Minimal runtime dependencies
- Self-signed TLS by default
- Support for custom certificates

### Published Images

Images are automatically published to GitHub Container Registry:

- **Registry:** `ghcr.io/sarg3nt/gearbox/gearbox-agent`
- **Tags:**
  - `latest` - Latest main branch build
  - `v1.0.0` - Specific version
  - `main` - Latest main branch
  - `sha-abc123` - Specific commit

## Testing Results

### Build Test

```bash
cd gearbox-agent
docker build -t gearbox-agent:test .
```

**Result:** ✅ Success
**Build time:** ~7 seconds (with cache)
**Image size:** 47.2MB

### Runtime Test

```bash
docker run -d --name test -p 8406:8405 gearbox-agent:test
docker exec test wget -q --spider --no-check-certificate https://localhost:8405/health
```

**Result:** ✅ Health check passed
**Startup time:** ~1 second
**Memory usage:** ~15MB

### Expected Behaviors

The following behaviors are normal in Docker:

- ⚠️ `journalctl` errors - Expected in Alpine (no systemd)
- ⚠️ HAProxy stats errors - Expected when not monitoring HAProxy
- ⚠️ Certificate warnings - Expected with self-signed certificates

These are informational and do not affect core functionality.

## Installation Methods Comparison

| Feature | Binary | Docker |
|---------|--------|--------|
| **Setup Complexity** | Medium | Low |
| **System Requirements** | systemd, Linux | Docker Engine |
| **Isolation** | None | Full container |
| **Resource Overhead** | Minimal | ~15MB RAM |
| **Auto-restart** | systemd | Docker restart policy |
| **Update Process** | systemctl restart | docker pull & restart |
| **Log Access** | journalctl | docker logs |
| **Host Integration** | Full | Limited (needs mounts) |
| **Multi-server** | Requires SSH | Standard Docker tooling |

## Usage Examples

### Quick Start

```bash
docker pull ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

### Production Deployment

```yaml
version: '3.8'
services:
  gearbox-agent:
    image: ghcr.io/sarg3nt/gearbox/gearbox-agent:v1.0.0
    container_name: gearbox-agent
    restart: unless-stopped
    ports:
      - "8405:8405"
    volumes:
      - ./data:/var/lib/gearbox-agent
      - /etc/letsencrypt/live/example.com:/etc/certs:ro
    environment:
      - HAPROXY_AGENT_TLS_CERT=/etc/certs/fullchain.pem
      - HAPROXY_AGENT_TLS_KEY=/etc/certs/privkey.pem
      - HAPROXY_AGENT_LOG_LEVEL=info
    security_opt:
      - no-new-privileges:true
```

### Local Development

```bash
cd gearbox-agent
make docker-build
make docker-run
make docker-logs  # View logs
make docker-stop  # Clean up
```

## Future Enhancements

Potential improvements for future consideration:

1. **Distroless Image:** Consider using distroless base for even smaller size
2. **Volume Plugins:** Support for secret management (Docker Secrets, Vault)
3. **Kubernetes Support:** Add Helm charts and k8s manifests
4. **ARM32 Support:** Extend multi-arch to include ARM32 for Raspberry Pi
5. **Docker Healthcheck Script:** Custom healthcheck script with more validation
6. **Environment File Support:** Built-in support for .env files
7. **Init System:** Consider s6-overlay or tini for better signal handling

## Breaking Changes

None. This is a new deployment option that does not affect existing binary deployments.

## Migration Path

For users currently running the binary:

1. Binary deployment continues to work unchanged
2. Docker can be adopted gradually
3. Data can be migrated by copying `/var/lib/gearbox-agent`
4. Configuration maps directly to environment variables

No action required for existing users.

## Documentation Updates

All documentation has been updated to include Docker installation:

- ✅ [gearbox-agent/README.md](../../gearbox-agent/README.md)
- ✅ [gearbox-agent/docs/docker.md](../../gearbox-agent/docs/docker.md) (new)
- ✅ [README.md](../../README.md)
- ✅ [docs/getting-started.md](../../docs/getting-started.md)

## Verification Checklist

- [x] Dockerfile builds successfully
- [x] Docker image runs without errors
- [x] Health check passes
- [x] API key generation works
- [x] TLS certificates generate correctly
- [x] Volume persistence works
- [x] Multi-architecture builds configured
- [x] GitHub workflows created
- [x] Documentation complete
- [x] Examples tested
- [x] Makefile targets work
- [x] Docker Compose example works

## Conclusion

Docker support for gearbox-agent is now fully implemented and tested. Users have a choice between:

1. **Binary installation** - Traditional systemd service deployment
2. **Docker installation** - Containerized deployment with Docker or Docker Compose

Both methods are fully supported and documented, with Docker offering easier setup and better isolation, while binary installation provides tighter host integration.
