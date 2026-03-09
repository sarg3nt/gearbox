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

# Expose API port
EXPOSE 8405

# Environment defaults (can be overridden)
ENV HAPROXY_AGENT_LISTEN=0.0.0.0:8405 \
    HAPROXY_AGENT_DATA_DIR=/var/lib/gearbox-agent \
    HAPROXY_AGENT_LOG_LEVEL=info

# Run the application
ENTRYPOINT ["/app/gearbox-agent"]
