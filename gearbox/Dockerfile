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

# Install build dependencies
RUN apk add --no-cache git

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

# Build the application with CGO disabled (pure-Go sqlite via modernc.org/sqlite)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.CommitSHA=${COMMIT_SHA} -X main.BuildDate=${BUILD_DATE}" \
    -o gearbox \
    ./cmd/server

# Pre-create the default DatabasePath dir so distroless:nonroot (UID 65532) can
# write to it on first run without a pre-chowned bind mount. Override
# DATABASE_PATH if you mount the database elsewhere.
RUN mkdir -p /data

# Stage 3: Final runtime image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary from builder (static files are embedded in the binary)
COPY --from=go-builder /build/gearbox .
COPY --from=go-builder --chown=65532:65532 /data /data

# Expose port
EXPOSE 3000

# Run the application
ENTRYPOINT ["/app/gearbox"]
