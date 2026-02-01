# Stage 1: Build Templ templates
FROM golang:1.25-alpine AS templ-builder

WORKDIR /build

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Copy all source files (includes all .templ files in framework/plugins)
COPY . .

# Generate templates
RUN templ generate

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS go-builder

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
FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

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
