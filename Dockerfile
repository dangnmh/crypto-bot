# ==========================================
# Stage 1: Build static Go binary
# ==========================================
FROM golang:1.27-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Cache Go modules dependency layer
COPY go.mod go.sum ./
RUN go mod download

# Copy source repository code
COPY . .

# Build args for versioning
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown

# Build the executable targeting cmd/funding/main.go
# Bitwarden SDK requires CGO to be enabled (CGO_ENABLED=1) to link its C-bindings
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -X crypto-bot/pkg/version.Version=${VERSION} -X crypto-bot/pkg/version.Commit=${COMMIT} -X crypto-bot/pkg/version.BuildTime=${BUILD_TIME}" -o bin/funding-bot ./cmd/funding

# ==========================================
# Stage 2: Hardened Runtime Container
# ==========================================
FROM alpine:3.24

# Install security root CA-certificates, timezone database files, and libgcc (required for CGO-enabled binary)
RUN apk add --no-cache ca-certificates tzdata libgcc

# Run as non-privileged app user for container sandbox security
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app

# Copy the compiled binary from the builder environment
COPY --from=builder /app/bin/funding-bot /usr/local/bin/funding-bot

# Set correct read/write permissions
RUN chown -R appuser:appgroup /app

# Execute as non-root user
USER appuser

# Define startup entrypoint
ENTRYPOINT ["/usr/local/bin/funding-bot"]
