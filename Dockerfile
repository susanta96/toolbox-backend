# syntax=docker/dockerfile:1

# ---------- Build stage ----------
FROM golang:1.23-alpine AS builder

# git + ca-certs needed for `go mod download` over HTTPS/VCS
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Download deps first so this layer is cached unless go.mod/go.sum change.
# BuildKit cache mounts keep the module + build caches warm across builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Static, stripped, reproducible binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /app/server ./cmd/server

# ---------- Runtime stage ----------
FROM alpine:3.20

# Runtime deps:
#   qpdf + ghostscript -> PDF lock/unlock/merge/split/compress
#   ca-certificates    -> outbound HTTPS (FX provider, Neon)
#   tzdata             -> correct timezone handling for schedulers
#   wget (busybox)     -> container HEALTHCHECK
RUN apk add --no-cache ca-certificates tzdata qpdf ghostscript

# Run as an unprivileged user.
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /app/server .

# Writable storage dirs owned by the runtime user.
RUN mkdir -p /app/uploads /app/generated && chown -R appuser:appgroup /app

USER appuser

# App listens on 8088 (override with PORT). Keep in sync with docker-compose.yml.
ENV PORT=8088
EXPOSE 8088

# Lightweight liveness probe against the /hello endpoint.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/hello" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["./server"]
