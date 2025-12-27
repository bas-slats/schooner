# Homelab CD - Multi-stage Dockerfile

#=====================================
# Stage 1: Build Go binary
#=====================================
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION}" \
    -o /homelab-cd \
    ./cmd/schooner

#=====================================
# Stage 2: Final runtime image
#=====================================
FROM alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    git \
    docker-cli \
    docker-cli-compose \
    tzdata

# Create non-root user
RUN addgroup -g 1000 homelab-cd && \
    adduser -u 1000 -G homelab-cd -s /bin/sh -D homelab-cd

WORKDIR /app

# Copy binary from builder
COPY --from=builder /homelab-cd /usr/local/bin/homelab-cd

# Copy static assets
COPY ui/static /app/ui/static

# Copy default config
COPY config/config.example.yaml /app/config/config.yaml

# Create data directories
RUN mkdir -p /data/repos /data/ssh && \
    chown -R homelab-cd:homelab-cd /app /data

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

# Switch to non-root user
# Note: Commented out to allow Docker socket access - run with specific user if needed
# USER homelab-cd

ENTRYPOINT ["/usr/local/bin/homelab-cd"]
