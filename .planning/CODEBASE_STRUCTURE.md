# NSSAAF Codebase Structure

**Analysis Date:** 2026-04-20

## 1. Project Structure Overview

```
naf3/
├── cmd/nssAAF/
│   └── main.go                        # Application entry point
├── configs/
│   ├── example.yaml                   # Annotated config reference
│   ├── development.yaml, staging.yaml, production.yaml
├── internal/
│   ├── api/
│   │   ├── aiw/                      # Nnssaaf_AIW service (N60 interface)
│   │   ├── common/                   # Shared HTTP utilities, middleware
│   │   └── nssaa/                   # Nnssaaf_NSSAA service (N58 interface)
│   ├── aaa/                          # AAA proxy (AAA-P) — routes to RADIUS/Diameter
│   ├── amf/                          # AMF SBI client stub
│   ├── auth/                         # Authentication utilities
│   ├── cache/redis/                  # Redis client pool and session cache
│   ├── config/                      # YAML configuration loading
│   ├── crypto/                       # Cryptographic utilities
│   ├── diameter/                    # Diameter AAA client (go-diameter/v4)
│   ├── eap/                         # EAP engine (RFC 3748)
│   ├── nrf/                         # NRF service discovery client
│   ├── resilience/                  # Circuit breaker, retry policies
│   ├── storage/postgres/            # PostgreSQL pool and DAOs
│   ├── types/                       # 3GPP data types (GPSI, SUPI, Snssai, NssaaStatus)
│   └── udm/                         # UDM SBI client stub
├── oapi-gen/
│   └── gen/                         # Code-generated from 3GPP OpenAPI specs
│       ├── aiw/
│       ├── nssaa/
│       └── specs/
├── pkg/                             # Public helper packages (empty/placeholder)
├── test/
│   ├── e2e/                        # End-to-end test runner
│   └── integration/                # Integration test runner
├── scripts/
│   ├── migrate/                    # Database migration tooling
│   └── tools/                      # Build/dev helper scripts
├── deployments/
│   ├── argocd/, helm/, kustomize/  # Kubernetes deployment manifests
└── docs/                           # 3GPP specs, design docs, roadmap
```

## 2. Module Inventory

### `internal/types/`

Core 3GPP data types with validation.

| Public Type / Function | Purpose |
|---|---|
| `type Gpsi string` | GPSI identifier; validates `^5-?[0-9]{8,14}$` (TS 29.571 §5.4.4.3) |
| `func (Gpsi) Validate() error` | Validates GPSI pattern and required presence |
| `type Snssai struct { SST uint8; SD string }` | S-NSSAI; SST 0–255, SD 6 hex chars (TS 23.003 §3.2) |
| `func (Snssai) Validate() error` | Validates SST range and SD hex format |
| `func (Snssai) Key() string` | Returns `"sst:sd"` normalized key for maps/cache |
| `type Supi string` | SUPI; validates `^imu-[0-9]{15}$` (TS 29.571 §5.4.4.2) |
| `type EapMessage []byte` | EAP payload wrapper with JSON marshal support |
| `const AuthResultNotExecuted/Success/Failure/Pending` | NssaaStatus authResult enum |
| `type ValidationError struct { HTTPStatus, Field, Reason, Cause }` | Input validation failure |
| `type NssaaError struct { HTTPStatus, Err, Cause, Detail }` | Domain-level error |
| `const Cause*` | 3GPP cause codes (24 constants) |
| `var Err*` | Sentinel errors (ErrAuthContextNotFound, ErrAaaTimeout, etc.) |

**Spec traces in every type:** All types include `// Spec: TS XX.XXX §Y.Z` comments.

### `internal/config/`

YAML configuration loading with environment variable expansion.

| Public Type / Function | Purpose |
|---|---|
| `type Config struct { Server, Database, Redis, EAP, AAA, RateLimit, Logging, Metrics, NRF, UDM }` | Root config struct |
| `type ServerConfig / DatabaseConfig / RedisConfig / EAPConfig / AAAConfig / ...` | Per-subsystem config structs |
| `func Load(path string) (*Config, error)` | Reads YAML, expands `${VAR}` env placeholders, applies defaults |

Defaults are applied for: server timeouts, EAP rounds/timeout, AAA retries/timeouts, Redis pool size, metrics path.

### `internal/api/common/`

Shared HTTP layer: ProblemDetails (RFC 7807), headers, middleware.

| Public Type / Function | Purpose |
|---|---|
| `type ProblemDetails struct { Type, Title, Detail, Instance, Cause, Status }` | RFC 7807 error response |
| `func NewProblem / ValidationProblem / ForbiddenProblem / NotFoundProblem / ...` | Factory functions for every HTTP error type |
| `func WriteProblem(w, *ProblemDetails)` | Writes problem response with correct Content-Type |
| `func WriteJSON(w, status, v)` | Writes JSON response |
| `const HeaderXRequestID / HeaderLocation / Header3GPPSessionID / ...` | HTTP header constants (TS 29.500 §5) |
| `const MediaTypeJSONVersion / MediaTypeProblemJSON / MediaType3GPPNSSAA / ...` | Content-Type constants |
| `func RequestIDMiddleware(http.Handler) http.Handler` | Injects X-Request-ID; preserves client-supplied ID |
| `func LoggingMiddleware(http.Handler) http.Handler` | Structured slog log for every request (method, path, status, duration_ms) |
| `func RecoveryMiddleware(http.Handler) http.Handler` | Panic recovery → 500 ProblemDetails + stack trace |
| `func CORSMiddleware(http.Handler) http.Handler` | Adds CORS headers for `/oam/` paths only |
| `type responseWriter struct` | Wraps ResponseWriter to capture status code for logging |
| `func GetRequestID / WithRequestID ctx helpers` | Context value accessors for request correlation |

### `internal/api/nssaa/`

N58 interface: Nnssaaf_NSSAA service (TS 29.526 §7.2, TS 23.502 §4.2.9).

| Public Type / Function | Purpose |
|---|---|
| `type AuthCtx struct { AuthCtxID, GPSI, SnssaiSST, SnssaiSD, AmfInstance, ReauthURI, RevocURI, EapPayload }` | Slice auth context stored in NSSAAF |
| `type AuthCtxStore interface { Load/Save/Delete/Close }` | Storage abstraction (Phase 3 replaces with Redis) |
| `type InMemoryStore struct` | Phase 1 in-memory impl of AuthCtxStore |
| `var ErrNotFound` | Sentinel for missing context |
| `type Handler struct { store, aaa, apiRoot }` | Implements `nssaanats.ServerInterface` |
| `type HandlerOption func(*Handler)` | Functional options (`WithAAA`, `WithAPIRoot`) |
| `func NewHandler(AuthCtxStore, ...HandlerOption) *Handler` | Constructor |
| `func (Handler) CreateSliceAuthenticationContext` | `POST /slice-authentications` — creates auth context, echoes EAP |
| `func (Handler) ConfirmSliceAuthentication` | `PUT /slice-authentications/{authCtxId}` — confirms auth |
| `func (h *Handler) ServeHTTP(w, r)` | Satisfies `http.Handler`; delegates to oapi-codegen |
| `var _ nssaanats.ServerInterface = (*Handler)(nil)` | Compile-time interface check |

**Phase marker:** AAA forwarding is commented out with `// Phase 2:`; Phase 1 echoes the EAP message back.

### `internal/api/aiw/`

N60 interface: Nnssaaf_AIW service (TS 29.526 §7.3).

Identical structure to `nssaa/`, but for SUPI-based authentication (vs GPSI/S-NSSAI):

| Public Type | Difference from NSSAA |
|---|---|
| `type AuthContext { AuthCtxID, Supi, EapPayload, TtlsInner }` | Uses SUPI, not GPSI/Snssai |
| `func CreateAuthenticationContext` | `POST /authentications` |
| `func ConfirmAuthentication` | `PUT /authentications/{authCtxId}` |

Both handlers use `aiwnats.ServerInterface` (generated from AIW OpenAPI YAML).

### `internal/radius/`

RADIUS client for AAA-S interworking (TS 29.561 Ch.16, RFC 2865, RFC 3579).

| Public Type / Function | Purpose |
|---|---|
| `type Config struct { ServerAddress, ServerPort, SharedSecret, Timeout, MaxRetries, Transport, LocalBindAddr }` | Client config |
| `type Client struct { config, transport, packetID, mu, logger }` | Main RADIUS client |
| `func NewRadiusClient(Config, *slog.Logger) (*Client, error)` | Constructor; creates UDP transport |
| `func (Client) SendAccessRequest(ctx, attrs) ([]byte, error)` | Sends Access-Request; retry loop with backoff |
| `func (Client) SendEAP(ctx, gpsi, eapPayload, snssaiSst, snssaiSd) ([]byte, error)` | Wraps EAP in RADIUS attributes, sends Access-Request |
| `func (Client) Stats() ClientStats` | Returns operational stats |
| `func FragmentEAPMessage(payload, maxSize) [][]byte` | Fragments EAP into ≤253-byte chunks (RFC 3579) |
| `func AssembleEAPMessage(attrs) []byte` | Reassembles fragments from RADIUS attributes |
| `var ErrTimeout / ErrInvalidResponse / ErrIDMismatch` | Sentinel errors |
| `const DefaultServerPort = 1812, DefaultResponseWindow = 10s, DefaultMaxRetries = 3` | Defaults |

**Transport layer:** `NewUDPClient` with `LocalBindAddr`. DTLS transport also defined in `dtls.go`.

### `internal/diameter/`

Diameter client using `go-diameter/v4` state machine (TS 29.561 Ch.17, RFC 4072, RFC 6733).

| Public Type / Function | Purpose |
|---|---|
| `type Config struct { OriginHost, OriginRealm, DestinationHost, DestinationRealm, ServerAddress, Network, TLS* }` | Client config |
| `type Client struct { cfg, settings, machine, smClient, conn, mu, pending map, hopByHopSeq }` | Wraps go-diameter state machine |
| `func NewClient(Config, *slog.Logger) (*Client, error)` | Constructor; registers DEA/STA handlers |
| `func (Client) Connect() error` | Establishes TCP/SCTP/TLS connection |
| `func (Client) SendDER(ctx, sessionID, userName, eapPayload, sst, sd) ([]byte, error)` | Sends Diameter-EAP-Request; waits on channel for DEA response |
| `func (Client) PeerMetadata() (*smpeer.Metadata, error)` | Returns peer info from CER/CEA handshake |
| `const AppIDAAP = 5, CmdDER/CmdDEA = 268` | Diameter application/command constants |
| `func EncodeSnssaiAVP(sst, sd) (*diam.AVP, error)` | Encodes 3GPP S-NSSAI AVP |

**Pending request map:** `map[uint32]chan *diam.Message` keyed by hop-by-hop ID; uses atomic counter for ID generation.

### `internal/eap/`

EAP engine (RFC 3748) with session state machine and TLS support.

| Public Type / Function | Purpose |
|---|---|
| `type Engine struct { cfg, sessionManager, fragmentMgr, aaaClient, tlsConfig, logger }` | Main EAP engine |
| `func NewEngine(Config, AAAClient, *slog.Logger) *Engine` | Constructor |
| `func (Engine) StartSession(authCtxID, gpsi) (*Session, error)` | Creates new EAP session |
| `func (Engine) Process(ctx, authCtxID, eapPayload) (*EapMessage, AuthResult, error)` | Processes EAP message; drives state machine |
| `func (Engine) GetSession(authCtxID) (*Session, error)` | Retrieves existing session |
| `func (Engine) DeleteSession(authCtxID)` | Removes session |
| `func (Engine) Stats() EngineStats` | Returns active sessions, fragment buffers, etc. |
| `type Session struct { AuthCtxID, GPSI, State, Rounds, ExpectedId, MaxRounds, Timeout, ... }` | Per-auth session state |
| `type SessionState int` | `SessionStateInit/Exchange/Completing/Done/Failed/Timeout` |
| `type EapMethod int` | `EapMethodIdentity/TLS/AKAprime/TTLS` |
| `const DefaultMaxRounds = 20, DefaultRoundTimeout = 30s, DefaultSessionTTL = 5m` | Engine defaults |

**State machine:** `Init → EapExchange → Completing → Done/Failed/Terminal`. Retry detection via `sha256Hash` of incoming payload cached against `LastNonce`. `AAAClient` interface: `SendEAP(ctx, authCtxID, payload) ([]byte, error)`.

### `internal/storage/postgres/`

PostgreSQL persistence layer via `jackc/pgx/v5`.

| Public Type / Function | Purpose |
|---|---|
| `type Pool struct { pool *pgxpool.Pool }` | Wraps pgx connection pool |
| `func NewPool(ctx, Config) (*Pool, error)` | Constructor; parses DSN, sets pool params, pings |
| `func (Pool) Acquire / Exec / ExecResult / Query / QueryRow / BeginTx / Stats / Close` | Pool operations |
| `func RunMigrations(ctx, *Pool, migrations) error` | Applies SQL migrations |
| `type SessionDAO struct { pool }` | Data access for auth sessions |
| `type AaaConfigDAO struct { pool }` | Data access for AAA server configs |
| `type AuditDAO struct { pool }` | Audit log writes |

### `internal/cache/redis/`

Redis caching layer via `go-redis/v9`.

| Public Type / Function | Purpose |
|---|---|
| `type Pool struct { client redis.Cmdable }` | Single or cluster Redis client |
| `func NewPool(ctx, Config) (*Pool, error)` | Single-node constructor |
| `func NewClusterPool(ctx, Config) (*Pool, error)` | Redis Cluster constructor |
| `func (Pool) Client() redis.Cmdable` | Returns underlying client |
| `func (Pool) Close() error` | Closes client (handles both types) |
| `type SessionCache struct` | EAP session caching with TTL |
| `type IdempotencyKey struct` | Idempotency key dedup |
| `type RateLimiter struct` | Per-GPSI, per-AMF, global rate limiting |
| `type DistributedLock struct` | Redis-based distributed lock |

### `internal/aaa/`

AAA Proxy (AAA-P) that routes EAP to either RADIUS or Diameter backend.

| Public Type / Function | Purpose |
|---|---|
| `type Router interface { SendEAP(ctx, authCtxID, eapPayload) ([]byte, error) }` | Interface for AAA routing |
| `type Config struct` | Per-server config (address, protocol, TLS, etc.) |
| `type CircuitBreaker struct` | Tracks consecutive failures, opens after threshold |
| `func (Router) SelectBackend(snssaiSnssai) (*Backend, error)` | Looks up AAA config for S-NSSAI |
| `func (Router) SendEAP(ctx, authCtxID, eapPayload) ([]byte, error)` | Routes to RADIUS or Diameter based on config |

### `internal/nrf/`, `internal/udm/`, `internal/amf/`

SBI client stubs for 3GPP service discovery and peer NF communication.

| Package | Purpose |
|---|---|
| `internal/nrf/` | NF profile registration + NRF service discovery |
| `internal/udm/` | UDM client for subscription data retrieval (N59 interface) |
| `internal/amf/` | AMF client for notifications (e.g., slice auth result) |

These are stubs in Phase 1 — log at `slog.Debug` or return empty responses.

### `internal/resilience/`

Circuit breaker pattern and retry policies for AAA calls.

| Public Type | Purpose |
|---|---|
| `type CircuitBreaker struct { failures, threshold, recoveryTimeout, state, mu }` | Tracks AAA server health |

### `internal/crypto/`, `internal/auth/`

Cryptographic utilities and auth helpers (placeholders/stubs).

## 3. Dependency Graph

### Go module dependencies (`go.mod`)

```
github.com/operator/nssAAF
├── github.com/google/uuid v1.6.0         # Request ID generation
├── github.com/stretchr/testify v1.11.1    # Testing assertions
├── github.com/go-chi/chi/v5 v5.1.0       # HTTP routing (in tests)
├── github.com/jackc/pgx/v5 v5.9.1        # PostgreSQL driver
├── github.com/redis/go-redis/v9 v9.18.0  # Redis client
├── github.com/fiorix/go-diameter/v4 v4.1.0 # Diameter state machine + dict
├── gopkg.in/yaml.v3 v3.0.1               # Config file parsing
└── oapi-gen/gen/{nssaa,aiw,specs}        # Generated from 3GPP OpenAPI YAMLs
```

### Internal package import graph

```
cmd/nssAAF/main.go
├── internal/config
├── internal/api/nssaa  (→ creates InMemoryStore)
├── internal/api/aiw    (→ creates InMemoryStore)
└── internal/api/common (→ middleware wrappers)

internal/api/nssaa/handler.go
├── internal/api/common    (ProblemDetails, headers, validators)
├── oapi-gen/gen/nssaa    (generated ServerInterface + types)
└── oapi-gen/gen/specs    (shared specs like AuthStatus)

internal/api/aiw/handler.go
├── internal/api/common
└── oapi-gen/gen/aiw

internal/api/common/
├── (self-contained — no internal deps)
└── (used by all api/* packages)

internal/eap/engine.go
├── internal/types
└── internal/radius OR internal/diameter (via AAAClient interface)

internal/radius/client.go
└── (self-contained — no internal deps)

internal/diameter/client.go
└── github.com/fiorix/go-diameter/v4

internal/storage/postgres/pool.go
└── github.com/jackc/pgx/v5

internal/cache/redis/pool.go
└── github.com/redis/go-redis/v9

internal/types/
├── internal/api/common (ValidationError.ToProblemDetails)
└── (no other internal deps — pure types)

internal/aaa/router.go
├── internal/radius  (RADIUSBackend)
└── internal/diameter (DiameterBackend)
```

### Call flow (runtime)

```
HTTP Request
    ↓
common.RecoveryMiddleware → common.RequestIDMiddleware → common.LoggingMiddleware → common.CORSMiddleware
    ↓
http.ServeMux
    ↓
nssaa.Router or aiw.Router (oapi-codegen chi-based)
    ↓
Handler.ServeHTTP → oapi-codegen Handler (validates request) → Handler.CreateSliceAuthenticationContext / ConfirmSliceAuthentication
    ↓
AuthCtxStore (InMemoryStore → Phase 3: Redis-backed)
    ↓
// Phase 2: AAA Client → RADIUS or Diameter → NSS-AAA Server
```

## 4. Entry Points

### `cmd/nssAAF/main.go`

Single binary entry point.

**Startup sequence:**
1. Parse `-config` flag (default: `configs/staging.yaml`)
2. Initialize `slog.NewJSONHandler` logger
3. `config.Load(*configPath)` — read YAML, expand env vars, apply defaults
4. Create `nssaa.NewInMemoryStore()` and `aiw.NewInMemoryStore()`
5. Create `nssaa.NewHandler(store)` and `aiw.NewHandler(store)` with `WithAPIRoot`
6. Create routers via `nssaa.NewRouter` / `aiw.NewRouter`
7. Register `mux.Handle("/nnssaaf-nssaa/", ...)` and `mux.Handle("/nnssaaf-aiw/", ...)`
8. Register OAM: `/health`, `/ready` with CORS
9. Wrap mux with global middleware (Recovery → RequestID → Logging → CORS, outermost-to-innermost)
10. `http.Server` with configured timeouts
11. Goroutine: `srv.ListenAndServe()` → error channel
12. Signal handler: `SIGINT/SIGTERM` → graceful shutdown with 10s timeout

**Build artifact:** Binary at `./nssAAF` (compiled, 9.7 MB).

## 5. Test Coverage

### Test files (13 total)

| Test File | Package | Coverage |
|---|---|---|
| `internal/types/types_test.go` | `internal/types/` | GPSI, SUPI, Snssai validation |
| `internal/types/gpsi_test.go` | `internal/types/` | GPSI regex and normalization |
| `internal/types/snssai_test.go` | `internal/types/` | S-NSSAI validation |
| `internal/config/config_test.go` | `internal/config/` | YAML loading, env expansion, defaults |
| `internal/api/common/common_test.go` | `internal/api/common/` | ProblemDetails factories |
| `internal/api/nssaa/handler_test.go` | `internal/api/nssaa/` | POST/PUT handlers, GPSI/Snssai validation, store errors |
| `internal/api/aiw/handler_test.go` | `internal/api/aiw/` | POST/PUT handlers, SUPI validation |
| `internal/eap/engine_test.go` | `internal/eap/` | EAP engine state machine |
| `internal/eap/eap_test.go` | `internal/eap/` | EAP packet parsing |
| `internal/radius/radius_test.go` | `internal/radius/` | RADIUS message encoding |
| `internal/radius/client_test.go` | `internal/radius/` | RADIUS client retries, validation |
| `internal/diameter/diameter_test.go` | `internal/diameter/` | Diameter AVP encoding |
| `internal/storage/postgres/session_test.go` | `internal/storage/postgres/` | Session DAO |
| `internal/cache/redis/cache_test.go` | `internal/cache/redis/` | Redis cache operations |
| `internal/aaa/aaa_test.go` | `internal/aaa/` | AAA routing logic |

### Test patterns observed

**Mock store pattern** (used in both `nssaa/handler_test.go` and `aiw/handler_test.go`):

```go
type mockStore struct {
    data      map[string]*AuthCtx
    loadErr   error
    saveErr   error
    deleteErr error
}
func (m *mockStore) Load(id string) (*AuthCtx, error) { ... }
func (m *mockStore) Save(ctx *AuthCtx) error { ... }
```

**httptest helpers:**

```go
func doRequest(handler *Handler, method, path string, body interface{}) *httptest.ResponseRecorder
func makeRouter(handler *Handler) http.Handler  // chi router + RequestIDMiddleware
```

**Framework:** `github.com/stretchr/testify` — `assert` (failures continue) and `require` (fatal on failure).

**Coverage:** `coverage.out` file present (98 KB), indicating `go test -coverprofile` was run.

### Packages without tests

The following packages have no `*_test.go` files:
- `internal/aaa/router.go` (partial — `aaa_test.go` exists)
- `internal/amf/`, `internal/udm/`, `internal/nrf/` (stub implementations)
- `internal/crypto/`, `internal/auth/`, `internal/resilience/`
- `internal/storage/postgres/pool.go`, `migrate.go`, `aaa_config.go`, `audit.go`
- `internal/cache/redis/session_cache.go`, `ratelimit.go`, `lock.go`, `idempotency.go`

## 6. Configuration

### Config file format

YAML files in `configs/`: `example.yaml`, `development.yaml`, `staging.yaml`, `production.yaml`.

### Configuration structure

All config lives in a single nested `Config` struct. Environment variable placeholders use `${VAR}` syntax (simple `os.Expand`), including defaults: `${VAR:-default}`.

| Section | Key Fields | Defaults |
|---|---|---|
| `server` | `addr`, `readTimeout`, `writeTimeout`, `idleTimeout` | `":8080"`, `10s`, `30s`, `120s` |
| `database` | `host`, `port`, `name`, `user`, `password` (env), `maxConns`, `minConns`, `connMaxLifetime`, `sslMode` | Pool: 100/20 conns |
| `redis` | `addrs[]`, `password` (env), `db`, `poolSize` | `poolSize: 50` |
| `eap` | `maxRounds`, `roundTimeout`, `sessionTtl` | `20`, `30s`, `5m` |
| `aaa` | `responseTimeout`, `maxRetries`, `failureThreshold`, `recoveryTimeout` | `10s`, `3`, `5`, `30s` |
| `rateLimit` | `perGpsiPerMin`, `perAmfPerSec`, `globalPerSec` | (no defaults) |
| `logging` | `level`, `format` | (no defaults) |
| `metrics` | `enabled`, `path` | `path: "/metrics"` |
| `nrf` | `baseURL`, `discoverTimeout` | (no defaults) |
| `udm` | `baseURL`, `timeout` | (no defaults) |

### How config is loaded

`cmd/nssAAF/main.go` line 33:
```go
cfg, err := config.Load(*configPath)
```

`internal/config/config.go` line 103: reads file → `expandEnv` → `yaml.Unmarshal` → `applyDefaults`.

## 7. Notable Patterns

### Functional options for handler construction

Both `nssaa.Handler` and `aiw.Handler` use the functional options pattern:

```go
type HandlerOption func(*Handler)

func WithAAA(aaa AAARouter) HandlerOption { ... }
func WithAPIRoot(apiRoot string) HandlerOption { ... }

func NewHandler(store AuthCtxStore, opts ...HandlerOption) *Handler {
    h := &Handler{store: store}
    for _, opt := range opts { opt(h) }
    return h
}
```

### Store interface abstraction

`AuthCtxStore` interface defined in both `nssaa/` and `aiw/`. Phase 1 uses `InMemoryStore`; Phase 3 will replace with Redis-backed implementation without changing handler code.

### Compile-time interface verification

```go
var _ nssaanats.ServerInterface = (*Handler)(nil)
```

Both handlers assert at compile time that they implement the oapi-codegen `ServerInterface`.

### Oapi-codegen router integration

`Handler.ServeHTTP` delegates to the generated oapi-codegen chi router:

```go
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    reqID := common.GetRequestID(r.Context())
    if reqID == "" { reqID = uuid.NewString() }
    r = r.WithContext(common.WithRequestID(r.Context(), reqID))
    nssaanats.Handler(h).ServeHTTP(w, r)
}
```

### AAA client interface

`internal/eap/engine.go` defines an `AAAClient` interface:
```go
type AAAClient interface {
    SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error)
}
```

Both `radius.Client` and `diameter.Client` satisfy this interface, allowing swap via configuration.

### EAP retry detection

`internal/eap/engine.go` uses SHA-256 hash of incoming EAP payload as `LastNonce` to detect and replay cached responses for idempotent retries:

```go
msgHash := sha256Hash(eapPayload)
if bytesEqual(session.LastNonce, msgHash) && session.CachedResponse != nil {
    // Replay cached response without calling AAA
    return &respMsg, types.AuthResultPending, nil
}
```

### ProblemDetails factory functions

`internal/api/common/problem.go` provides named constructors for every HTTP error type:
- `ValidationProblem`, `UnauthorizedProblem`, `ForbiddenProblem`, `NotFoundProblem`
- `ConflictProblem`, `GoneProblem`
- `BadGatewayProblem` (502), `ServiceUnavailableProblem` (503), `GatewayTimeoutProblem` (504)
- `InternalServerProblem` (500)

### Structured logging with slog

All packages use `log/slog` with structured key-value pairs:

```go
slog.Info("starting NSSAAF", "config", *configPath, "server_addr", cfg.Server.Addr)
slog.Error("radius_send_error", "id", id, "attempt", attempt, "error", err)
```

No `fmt.Printf` or `log.Printf` observed in production code.

### HTTP middleware composition

`cmd/nssAAF/main.go` line 76-82 — middleware applied outermost-to-innermost:

```go
handler = common.RecoveryMiddleware(handler)   // innermost: catches panics
handler = common.RequestIDMiddleware(handler)  // injects/correlates request ID
handler = common.LoggingMiddleware(handler)    // logs request/response
handler = common.CORSMiddleware(handler)       // outermost: CORS headers
```

### RADIUS retry with exponential backoff

```go
for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
    if attempt > 0 {
        backoff := time.Duration(1<<(attempt-1)) * time.Second
        select { case <-ctx.Done(): return nil, ctx.Err(); case <-time.After(backoff): }
    }
    response, err := c.transport.Send(sendCtx, rawPacket, serverAddr)
    ...
}
```

### Diameter hop-by-hop ID tracking

`internal/diameter/client.go` uses atomic counter for hop-by-hop ID and a `map[uint32]chan *diam.Message` pending requests registry.

## 8. Key Design Decisions

### OpenAPI code generation

API types and routers are generated by `oapi-codegen` from 3GPP OpenAPI YAML specs stored in `oapi-gen/specs/`. Generated code lives in `oapi-gen/gen/nssaa/`, `oapi-gen/gen/aiw/`, and `oapi-gen/gen/specs/`. Local `replace` directives in `go.mod` point to these local paths.

**Spec files** (stored in `oapi-gen/5GC_APIs-Rel-18/` and `oapi-gen/specs/`):
- `TS29526_Nnssaaf_NSSAA.yaml` → N58 interface
- `TS29526_Nnssaaf_AIW.yaml` → N60 interface
- `TS29571_CommonData.yaml` → shared data types (Snssai, Gpsi, Supi, ProblemDetails)

### Phase-based implementation roadmap

Code is explicitly marked with `// Phase 1:`, `// Phase 2:`, `// Phase 3:` comments indicating what is stubbed vs. implemented:

- **Phase 1** (current): In-memory stores, EAP engine (stub), RADIUS/Diameter framing, no actual AAA forwarding
- **Phase 2**: Connect EAP engine to AAA-S via RADIUS/Diameter
- **Phase 3**: Replace `InMemoryStore` with Redis-backed `AuthCtxStore`

### Dual AAA protocol support

`internal/radius/` and `internal/diameter/` are separate packages, both implement a common `AAAClient` interface used by `internal/eap/engine.go`. Selection is likely configured via `aaa.protocol` in config (not yet wired in `main.go`).

### 3GPP spec traceability

Every type, function, and key decision includes `// Spec: TS XX.XXX §Y.Z` comments. Cause codes, data types, error codes, and API fields all trace back to specific 3GPP specification sections.

### No external secrets manager

Configuration uses simple `${VAR}` env var expansion in YAML config files. No HashiCorp Vault, AWS Secrets Manager, or Kubernetes secrets integration — passwords are read from env vars directly.

### No authentication on HTTP endpoints

The SBI API (`/nnssaaf-nssaa/`, `/nnssaaf-aiw/`) has no TLS/client certificate auth in Phase 1. CORS middleware only applies to `/oam/` paths. Production config would need TLS termination at load balancer.

### Redis used for both caching and distributed coordination

`internal/cache/redis/` covers session caching, rate limiting, idempotency keys, and distributed locks — a single Redis instance serves multiple purposes per the Phase 3 design.

### go-chi/chi used only in generated routers

The `chi` router (`github.com/go-chi/chi/v5`) appears only in the oapi-codegen generated router (not in `main.go`). `main.go` uses the standard library `http.ServeMux` for top-level routing.

---

*Codebase analysis: 2026-04-20*
