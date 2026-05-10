# Stage 1: Build Templ templates
FROM golang:1.26-alpine AS templ-builder

# Pinned to match CI (.github/workflows/ci.yml TEMPL_VERSION).
# Bump in lockstep when upgrading.
ARG TEMPL_VERSION=v0.3.977

WORKDIR /build

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

# Copy all source files (includes all .templ files in framework/plugins)
COPY . .

# Generate templates
RUN templ generate

# Stage 2: Build Go binary
FROM golang:1.26-alpine AS go-builder

# Install build dependencies (gcc and musl-dev required for CGO/sqlite3)
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev

WORKDIR /build

# Copy go mod files first for better layer caching
COPY --from=templ-builder /build/go.mod /build/go.sum ./
RUN go mod download

# Copy all source code from templ-builder (includes generated _templ.go files)
COPY --from=templ-builder /build/ .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

# Build the application with CGO enabled for sqlite3
RUN CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.CommitSHA=${COMMIT_SHA} -X main.BuildDate=${BUILD_DATE}" \
    -o gearbox \
    ./cmd/server

# Stage 3: Final runtime image
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# Install runtime dependencies (sqlite-libs for CGO-built binary)
RUN apk add --no-cache ca-certificates tzdata wget sqlite-libs

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Copy binary from builder (static files are embedded in the binary)
COPY --from=go-builder /build/gearbox .

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q --spider http://localhost:3000/health || exit 1

# Run the application
ENTRYPOINT ["/app/gearbox"]
