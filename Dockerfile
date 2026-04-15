# Dockerfile — Multi-stage build for nssAAF
# Spec: TS 29.500 §5 (TLS requirements), Go 1.22+
#
# Stage 1: Build the binary with go build
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Download dependencies first (layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with symbol table and DWARF stripped for smaller size
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w" \
    -o nssAAF ./cmd/nssAAF/

# Stage 2: Runtime image
FROM alpine:3.19 AS runtime

# Install runtime dependencies (CA certs for TLS, tzdata for time zones)
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user for security
RUN adduser -D -g '' appuser

WORKDIR /app

# Copy binary and configs from builder
COPY --from=builder /build/nssAAF .
COPY --from=builder /build/configs/ ./configs/

# Set file permissions
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose the server port
EXPOSE 8080 9090 9091

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/nssAAF"]
CMD ["-config", "configs/production.yaml"]
