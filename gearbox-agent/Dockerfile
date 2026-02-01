# Stage 1: Build Go binary
FROM golang:1.25-alpine AS go-builder

# Install build dependencies (git for go modules, ca-certificates, tzdata for runtime)
RUN apk add --no-cache git ca-certificates tzdata

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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.CommitSHA=${COMMIT_SHA} -X main.BuildDate=${BUILD_DATE}" \
    -o gearbox-agent \
    ./cmd/gearbox-agent

# Stage 2: Final runtime image
FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Create data directory with proper permissions
RUN mkdir -p /var/lib/gearbox-agent && \
    chown -R appuser:appuser /var/lib/gearbox-agent

# Copy binary from builder
COPY --from=go-builder /build/gearbox-agent .

# Switch to non-root user
USER appuser

# Expose API port
EXPOSE 8405

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q --spider --no-check-certificate https://localhost:8405/health || exit 1

# Environment defaults (can be overridden)
ENV HAPROXY_AGENT_LISTEN=0.0.0.0:8405 \
    HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent \
    HAPROXY_AGENT_LOG_LEVEL=info

# Run the application
ENTRYPOINT ["/app/gearbox-agent"]
