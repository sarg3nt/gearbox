# Stage 1: Build Go binary
FROM golang:1.26-alpine AS go-builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

# Build the application with CGO disabled for static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.CommitSHA=${COMMIT_SHA} -X main.BuildDate=${BUILD_DATE}" \
    -o gearbox-agent \
    ./cmd/gearbox-agent

# Pre-create the default HAPROXY_AGENT_DATA_DIR so distroless:nonroot (UID 65532)
# can write the API key on first run without a pre-chowned bind mount.
RUN mkdir -p /var/lib/gearbox-agent

# Stage 2: Final runtime image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary from builder
COPY --from=go-builder /build/gearbox-agent .
COPY --from=go-builder --chown=65532:65532 /var/lib/gearbox-agent /var/lib/gearbox-agent

# busybox (statically linked, musl) supplies the `nsenter` applet for the
# console gear's host-exec mode (#142). When the agent runs with
# pid:host + privileged on a Docker host, sessions need to setns into
# host PID 1's namespaces — but the host's own nsenter binary can't be
# exec'd from the container because its ELF interpreter
# (/lib64/ld-linux-x86-64.so.2 + libc) isn't visible in the distroless
# mount namespace. busybox is statically linked, ~1MB, and dispatches
# applets by argv[0], so dropping it in as /usr/bin/nsenter is enough.
# Operators not running host-exec mode pay only the 1MB image cost.
#
# Pinned by index digest (not just tag) so the build is reproducible
# across busybox tag rebuilds. The buildx multi-arch resolver picks
# the right platform manifest from this index automatically. busybox
# 1.37.0's nsenter applet supports -t/--target, -m/--mount,
# -u/--uts, -i/--ipc, -n/--net, -p/--pid (which is what SpawnNsenter
# emits); verified against the upstream applet list before pinning.
COPY --from=busybox:1.37.0-musl@sha256:19b646668802469d968a05342a601e78da4322a414a7c09b1c9ee25165042138 /bin/busybox /usr/bin/nsenter

# Expose API port
EXPOSE 8405

# Environment defaults (can be overridden)
ENV HAPROXY_AGENT_LISTEN=0.0.0.0:8405 \
    HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent \
    HAPROXY_AGENT_LOG_LEVEL=info

# Run the application
ENTRYPOINT ["/app/gearbox-agent"]
