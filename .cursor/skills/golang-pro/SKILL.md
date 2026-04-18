---
name: golang-pro
description: Senior-level Go (Golang) engineering guidance for 5G telecom, distributed systems, and high-throughput services. Use when writing Go code, reviewing Go pull requests, designing Go packages, or implementing 3GPP/SBA protocols. Trigger terms: Go, Golang, .go files, gRPC, concurrency, goroutine, channel, slice, database, Redis, PostgreSQL.
---

# Go Professional Engineering

## Core Principles

1. **Explicit over implicit** — make I/O, errors, and concurrency visible in types.
2. **Interfaces as boundaries** — define interfaces at the consumer side, not the producer.
3. **Zero overhead abstractions** — if a pattern adds indirection without benefit, remove it.
4. **Composability** — small, focused packages that compose cleanly.
5. **Defensive input validation** — trust external input never.

## Project Layout (Standard)

```
cmd/nssAAF-server/main.go
internal/
  api/           # HTTP/gRPC handlers, middleware, server
  service/       # Business logic, orchestration
  domain/        # Entities, value objects, domain errors, interfaces
  repository/    # DB implementations (PostgreSQL, Redis)
  infrastructure/# AAA adapters, NRF client, observability
  config/        # Viper/EnvConfig structs
pkg/
  errors/        # Typed error definitions
  logger/        # Structured logger wrapper
  metrics/       # Prometheus/OTEL helpers
  middleware/    # Auth, rate-limit, tracing middleware
  validator/     # go-playground/validator helpers
scripts/
  migrate.sh
  gen.go
Makefile
go.mod
go.sum
```

## 3GPP / 5G Context

When implementing 5G SBA (Service-Based Architecture) functions:

- **SBI calls**: HTTP/2 + JSON over TLS. Use `net/http` with Go's native HTTP/2 support.
- **OpenAPI generation**: Use `deepmap/oapi-codegen` to generate server stubs from 3GPP YAML specs.
- **Inter-NF messaging**: HTTP POST/GET with OAuth2 Bearer tokens (JWT validated per-call).
- **NRF integration**: Register with NRF on startup; heartbeat every 70s; cache discovery results.
- **NRF client**: Implement exponential backoff on discovery failures; use stale-while-revalidate.
- **AAA protocol adapters**: Implement RADIUS and Diameter as pluggable backends; connection-pooled.
- **EAP engine**: Plugin-based architecture; each EAP method implements a common `EAPMethod` interface.
- **State management**: Hot cache (Redis) → warm store (PostgreSQL) → cold archive (Object Storage).

## Common Patterns

### Domain-Driven Structure (5G Service Example)

```go
// internal/domain/auth.go
type AuthContext struct {
    ID        AuthCtxID
    Supi      Supi
    GPSI      GPSI
    Snssai    Snssai
    State     AuthState
    EAPMethod EAPMethodType
    AmfID     string
    CreatedAt time.Time
    ExpiresAt time.Time
}

type AuthState int // CREATED=0, PENDING=1, SUCCESS=2, FAILURE=3, EXPIRED=4

// internal/domain/interfaces.go
type AuthRepository interface {
    Insert(context.Context, *AuthContext) error
    FindByID(context.Context, AuthCtxID) (*AuthContext, error)
    UpdateState(context.Context, AuthCtxID, AuthState) error
}

type AAAClient interface {
    Authenticate(context.Context, *EAPMessage, Snssai) (*EAPResult, error)
}
```

### Service Layer

```go
// internal/service/auth.go
type AuthService struct {
    repo      AuthRepository
    aaa       AAAClient
    cache     *redis.Client
    log       *slog.Logger
    cfg       *Config
}

func (s *AuthService) CreateContext(ctx context.Context, req *CreateAuthReq) (*AuthContext, error) {
    ctx, span := tracer.Start(ctx, "service.create_context")
    defer span.End()

    authCtx := &AuthContext{
        ID:        NewAuthCtxID(),
        Supi:      req.Supi,
        Snssai:    req.Snssai,
        State:     StateCreated,
        EAPMethod: req.EAPMethod,
        CreatedAt: time.Now(),
        ExpiresAt: time.Now().Add(s.cfg.AuthCtxTTL),
    }
    if err := s.repo.Insert(ctx, authCtx); err != nil {
        return nil, fmt.Errorf("insert auth context: %w", err)
    }
    s.log.InfoContext(ctx, "auth context created",
        "auth_ctx_id", authCtx.ID,
        "snssai", authCtx.Snssai,
    )
    return authCtx, nil
}
```

### RADIUS/Diameter Adapter

```go
// internal/infrastructure/aaa/radius.go
type RadiusClient struct {
    pool      *Pool  // connection pool per backend
    secret    []byte
    timeout   time.Duration
    log       *slog.Logger
    cb        *gobreaker.CircuitBreaker
}

func (c *RadiusClient) SendAccessRequest(ctx context.Context, attrs Attributes) (*RadiusPacket, error) {
    span := trace.SpanFromContext(ctx)
    span.SetAttributes(attribute.String("aaa.backend", "RADIUS"))

    result, err := c.cb.Execute(func() (any, error) {
        conn, err := c.pool.Get(ctx)
        if err != nil {
            return nil, fmt.Errorf("get connection: %w", err)
        }
        defer c.pool.Put(conn)

        pkt := NewAccessRequest(attrs)
        pkt.AddMessageAuthenticator(c.secret)
        if err := conn.WritePacket(ctx, pkt); err != nil {
            return nil, fmt.Errorf("write packet: %w", err)
        }
        return conn.ReadPacket(ctx, c.timeout)
    })
    if err != nil {
        span.RecordError(err)
        return nil, err
    }
    return result.(*RadiusPacket), nil
}
```

## Code Generation

Use Go's built-in tooling:

```bash
# OpenAPI 3.0 spec → Go server/client (3GPP YAML files)
oapi-codegen --generate=server,types,gin spec.yaml > internal/api.gen.go

# Protobuf → gRPC
protoc --go_out=. --go-grpc_out=. api.proto

# Wire dependency injection
wire gen ./...
```

## Key Libraries

| Purpose | Library | Notes |
|---------|---------|-------|
| HTTP router | `gin-gonic/gin` or `labstack/echo` | Echo has better middleware chain |
| DB | `jackc/pgx/v5` or `jmoiron/sqlx` | pgx for performance-critical; sqlx for ergonomics |
| Redis | `redis/go-redis/v9` | Supports Redis Cluster mode |
| Validation | `go-playground/validator` | Struct tag-based |
| Metrics | `prometheus/client_golang` | Auto HTTP handler at `/metrics` |
| Tracing | `go.opentelemetry.io/otel` | W3C Trace Context |
| Circuit breaker | `sony/gobreaker` | Per-backend isolation |
| Retry | `avast/retry-go/v4` | Exponential backoff |
| DI | `google/wire` | Compile-time DI |
| Config | `spf13/viper` or `knadh/envconfig` | YAML + ENV |
| Logging | `go.uber.org/zap` or stdlib `log/slog` | slog is stdlib since 1.21 |
| Testing | `stretchr/testify` | assert/require/mock |

## Reference

- For concurrent patterns (worker pools, fan-out, context cancellation), see [reference/concurrency.md](reference/concurrency.md)
- For 5G-specific patterns (EAP engine, SBI middleware, AAA adapters), see [reference/5g-patterns.md](reference/5g-patterns.md)
- For testing patterns (unit, integration, mocking), see [examples/testing.md](examples/testing.md)
