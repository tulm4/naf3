# Architecture Improvement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve codebase architecture through 5 targeted improvements: wire real metrics, create domain package, delete deprecated code, standardize error handling, and extract factory functions.

**Architecture:** Incremental approach — each task is independent and verified in isolation. Uses TDD where applicable (tests first, then implementation).

**Tech Stack:** Go 1.22+, Prometheus client_golang, standard library `net/http`

---

## File Structure Overview

```
Files to CREATE:
- internal/domain/nssaa_status.go
- internal/domain/nssaa_status_test.go
- cmd/biz/factory.go

Files to MODIFY:
- internal/biz/router.go

Files to DELETE:
- internal/aaa/router.go

Files to AUDIT:
- internal/api/nssaa/handler.go
- internal/api/aiw/handler.go
- internal/nrm/*.go
- cmd/biz/main.go
```

---

## Task 1: Wire Real Prometheus Metrics into biz/router.go

**Files:**
- Modify: `internal/biz/router.go`

- [ ] **Step 1: Read current biz/router.go to understand the no-op Metrics struct**

```go
// internal/biz/router.go — find this no-op Metrics struct
type Metrics struct{}

func (m *Metrics) RecordAAARequest(protocol, host, result string) {}

func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {}
```

- [ ] **Step 2: Add import for metrics package**

```go
import (
    "github.com/operator/nssAAF/internal/metrics"
    // ... existing imports
)
```

- [ ] **Step 3: Replace empty Metrics struct with real implementation**

Replace lines 100-116 (approximately):

```go
// Metrics holds Biz Pod AAA metrics backed by Prometheus collectors.
// Spec: REQ-14
type Metrics struct {
    requestsTotal *prometheus.CounterVec
    latencyHist   *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance backed by the global metrics registry.
func NewMetrics() *Metrics {
    return &Metrics{
        requestsTotal: metrics.AaaRequestsTotal,
        latencyHist:   metrics.AaaRequestDuration,
    }
}

// RecordAAARequest records an AAA request metric.
func (m *Metrics) RecordAAARequest(protocol, host, result string) {
    m.requestsTotal.WithLabelValues(protocol, host, result).Inc()
}

// RecordAAALatency records AAA request latency.
func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {
    m.latencyHist.WithLabelValues(protocol, host).Observe(d.Seconds())
}
```

- [ ] **Step 4: Update WithMetrics option to accept *Metrics**

Replace the WithMetrics function:

```go
// WithMetrics sets the metrics collector.
func WithMetrics(m *Metrics) RouterOption {
    return func(r *Router) { r.metrics = m }
}
```

- [ ] **Step 5: Update NewRouter to initialize default metrics if nil**

In `NewRouter`, after `for _, opt := range opts { opt(r) }`, add:

```go
// Default to real metrics if not provided
if r.metrics == nil {
    r.metrics = NewMetrics()
}
```

- [ ] **Step 6: Verify build**

Run: `go build ./internal/biz/...`
Expected: PASS (no errors)

- [ ] **Step 7: Verify existing tests still pass**

Run: `go test ./internal/biz/... -short`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/biz/router.go
git commit -m "feat(biz): wire real Prometheus metrics into Router.Metrics

- Replace no-op Metrics struct with real prometheus collectors
- NewMetrics() wraps metrics.AaaRequestsTotal and metrics.AaaRequestDuration
- RecordAAARequest calls requestsTotal.WithLabelValues().Inc()
- RecordAAALatency calls latencyHist.WithLabelValues().Observe()
- Default to NewMetrics() in NewRouter if not provided"
```

---

## Task 2: Create Domain Package for NssaaStatus State Machine

**Files:**
- Create: `internal/domain/nssaa_status.go`
- Create: `internal/domain/nssaa_status_test.go`

- [ ] **Step 1: Create the domain directory**

```bash
mkdir -p internal/domain
```

- [ ] **Step 2: Create internal/domain/nssaa_status.go**

```go
// Package domain provides domain-level business logic for NSSAAF.
// Spec: TS 29.571 §5.4.4.60
package domain

import (
    "fmt"

    "github.com/operator/nssAAF/internal/types"
)

// NssaaStatus represents the NSSAA authentication status.
// Type alias for the existing types.NssaaStatus.
type NssaaStatus = types.NssaaStatus

// Re-export status constants for domain package convenience.
const (
    // StatusNotExecuted means NSSAA has not been executed for this S-NSSAI yet.
    StatusNotExecuted = types.NssaaStatusNotExecuted
    // StatusPending means NSSAA is in progress (EAP exchange ongoing).
    StatusPending = types.NssaaStatusPending
    // StatusSuccess means EAP authentication completed successfully.
    StatusSuccess = types.NssaaStatusEapSuccess
    // StatusFailure means EAP authentication failed.
    StatusFailure = types.NssaaStatusEapFailure
)

// AuthEvent represents events that trigger NssaaStatus state transitions.
type AuthEvent int

const (
    // EventAuthStarted indicates the NSSAA procedure has been initiated.
    EventAuthStarted AuthEvent = iota
    // EventEAPRound indicates an intermediate EAP exchange round.
    EventEAPRound
    // EventAAAComplete indicates the AAA server responded with success.
    EventAAAComplete
    // EventAAAFailed indicates the AAA server responded with failure.
    EventAAAFailed
)

// TransitionError represents an invalid state transition attempt.
type TransitionError struct {
    From NssaaStatus
    To   string // "event_name" for invalid events
}

// Error implements the error interface.
func (e *TransitionError) Error() string {
    return fmt.Sprintf("invalid NSSAA status transition from %s with event: %s", e.From, e.To)
}

// TransitionTo validates and returns the next NssaaStatus based on current state and event.
// Spec: TS 29.571 §5.4.4.60, TS 23.502 §4.2.9
//
// State machine:
//   NOT_EXECUTED + EventAuthStarted → PENDING
//   PENDING + EventAAAComplete → EAP_SUCCESS
//   PENDING + EventAAAFailed → EAP_FAILURE
//   PENDING + EventEAPRound → PENDING (intermediate round, no error)
//   EAP_SUCCESS / EAP_FAILURE (terminal states) absorb all events (return current, nil)
func TransitionTo(current NssaaStatus, event AuthEvent) (NssaaStatus, error) {
    switch current {
    case StatusNotExecuted:
        if event == EventAuthStarted {
            return StatusPending, nil
        }
        return current, &TransitionError{From: current, To: event.String()}

    case StatusPending:
        switch event {
        case EventAAAComplete:
            return StatusSuccess, nil
        case EventAAAFailed:
            return StatusFailure, nil
        case EventEAPRound:
            return StatusPending, nil // intermediate round
        case EventAuthStarted:
            return current, &TransitionError{From: current, To: event.String()}
        default:
            return current, &TransitionError{From: current, To: event.String()}
        }

    case StatusSuccess, StatusFailure:
        // Terminal states absorb all events
        return current, nil

    default:
        return current, &TransitionError{From: current, To: event.String()}
    }
}

// String implements fmt.Stringer for AuthEvent.
func (e AuthEvent) String() string {
    switch e {
    case EventAuthStarted:
        return "EventAuthStarted"
    case EventEAPRound:
        return "EventEAPRound"
    case EventAAAComplete:
        return "EventAAAComplete"
    case EventAAAFailed:
        return "EventAAAFailed"
    default:
        return fmt.Sprintf("AuthEvent(%d)", int(e))
    }
}
```

- [ ] **Step 3: Verify domain package builds**

Run: `go build ./internal/domain/...`
Expected: PASS

- [ ] **Step 4: Create internal/domain/nssaa_status_test.go**

```go
package domain

import (
    "testing"
)

func TestTransitionTo(t *testing.T) {
    cases := []struct {
        name     string
        from     NssaaStatus
        event    AuthEvent
        expected NssaaStatus
        wantErr  bool
    }{
        // NOT_EXECUTED transitions
        {
            name:     "not_executed_to_pending_on_auth_started",
            from:     StatusNotExecuted,
            event:    EventAuthStarted,
            expected: StatusPending,
            wantErr:  false,
        },
        {
            name:     "not_executed_ignores_eap_round",
            from:     StatusNotExecuted,
            event:    EventEAPRound,
            expected: StatusNotExecuted,
            wantErr:  true,
        },
        {
            name:     "not_executed_ignores_aaa_complete",
            from:     StatusNotExecuted,
            event:    EventAAAComplete,
            expected: StatusNotExecuted,
            wantErr:  true,
        },
        {
            name:     "not_executed_ignores_aaa_failed",
            from:     StatusNotExecuted,
            event:    EventAAAFailed,
            expected: StatusNotExecuted,
            wantErr:  true,
        },

        // PENDING transitions
        {
            name:     "pending_to_success_on_aaa_complete",
            from:     StatusPending,
            event:    EventAAAComplete,
            expected: StatusSuccess,
            wantErr:  false,
        },
        {
            name:     "pending_to_failure_on_aaa_failed",
            from:     StatusPending,
            event:    EventAAAFailed,
            expected: StatusFailure,
            wantErr:  false,
        },
        {
            name:     "pending_remains_pending_on_eap_round",
            from:     StatusPending,
            event:    EventEAPRound,
            expected: StatusPending,
            wantErr:  false,
        },
        {
            name:     "pending_ignores_auth_started",
            from:     StatusPending,
            event:    EventAuthStarted,
            expected: StatusPending,
            wantErr:  true,
        },

        // Terminal state transitions (terminal states absorb all events)
        {
            name:     "success_absorbs_auth_started",
            from:     StatusSuccess,
            event:    EventAuthStarted,
            expected: StatusSuccess,
            wantErr:  false,
        },
        {
            name:     "success_absorbs_eap_round",
            from:     StatusSuccess,
            event:    EventEAPRound,
            expected: StatusSuccess,
            wantErr:  false,
        },
        {
            name:     "success_absorbs_aaa_complete",
            from:     StatusSuccess,
            event:    EventAAAComplete,
            expected: StatusSuccess,
            wantErr:  false,
        },
        {
            name:     "success_absorbs_aaa_failed",
            from:     StatusSuccess,
            event:    EventAAAFailed,
            expected: StatusSuccess,
            wantErr:  false,
        },
        {
            name:     "failure_absorbs_auth_started",
            from:     StatusFailure,
            event:    EventAuthStarted,
            expected: StatusFailure,
            wantErr:  false,
        },
        {
            name:     "failure_absorbs_eap_round",
            from:     StatusFailure,
            event:    EventEAPRound,
            expected: StatusFailure,
            wantErr:  false,
        },
        {
            name:     "failure_absorbs_aaa_complete",
            from:     StatusFailure,
            event:    EventAAAComplete,
            expected: StatusFailure,
            wantErr:  false,
        },
        {
            name:     "failure_absorbs_aaa_failed",
            from:     StatusFailure,
            event:    EventAAAFailed,
            expected: StatusFailure,
            wantErr:  false,
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got, err := TransitionTo(tc.from, tc.event)

            if tc.wantErr {
                if err == nil {
                    t.Errorf("TransitionTo(%s, %s) = %s, want error",
                        tc.from, tc.event, got)
                }
                return
            }

            if err != nil {
                t.Errorf("TransitionTo(%s, %s) returned unexpected error: %v",
                    tc.from, tc.event, err)
                return
            }

            if got != tc.expected {
                t.Errorf("TransitionTo(%s, %s) = %s, want %s",
                    tc.from, tc.event, got, tc.expected)
            }
        })
    }
}

func TestTransitionError(t *testing.T) {
    err := &TransitionError{From: StatusNotExecuted, To: "unknown"}

    if err.Error() == "" {
        t.Error("TransitionError.Error() should not be empty")
    }

    // Verify it implements error interface
    var _ error = err
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/domain/... -v`
Expected: PASS with 17 test cases (16 transitions + 1 error test)

- [ ] **Step 6: Commit**

```bash
git add internal/domain/nssaa_status.go internal/domain/nssaa_status_test.go
git commit -m "feat(domain): extract NssaaStatus state machine

- Create internal/domain/ package with NssaaStatus type alias
- Add AuthEvent enum: EventAuthStarted, EventEAPRound, EventAAAComplete, EventAAAFailed
- Add TransitionTo() function implementing state machine per TS 29.571 §5.4.4.60
- Add TransitionError for invalid transition attempts
- Add 16 table-driven tests covering all state × event combinations"
```

---

## Task 3: Delete Deprecated internal/aaa/router.go

**Files:**
- Delete: `internal/aaa/router.go`

- [ ] **Step 1: Verify no imports exist**

Run: `grep -r "internal/aaa" --include="*.go" | grep -v "_test.go" | grep -v "^internal/aaa/"`
Expected: No output (no external imports)

- [ ] **Step 2: Delete the file**

```bash
rm internal/aaa/router.go
```

- [ ] **Step 3: Verify build still passes**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git rm internal/aaa/router.go
git commit -m "refactor: delete deprecated internal/aaa/router.go

This file was marked DEPRECATED and is no longer used.
The same types (ProxyMode, Protocol, RouteDecision, ServerConfig, SnssaiConfig)
are defined in internal/biz/router.go which is the active implementation."
```

---

## Task 4: Audit and Standardize Error Handling in API Handlers

**Files to audit:**
- `internal/api/nssaa/handler.go`
- `internal/api/aiw/handler.go`
- `internal/nrm/*.go`

- [ ] **Step 1: Search for raw http.Error usage**

Run: `grep -n "http\.Error" internal/api/nssaa/handler.go internal/api/aiw/handler.go`
Expected: List of raw http.Error calls to replace

- [ ] **Step 2: Search for raw http.Error in nrm**

Run: `grep -n "http\.Error" internal/nrm/*.go 2>/dev/null || echo "No nrm handlers found or no matches"`
Expected: List of raw http.Error calls to replace

- [ ] **Step 3: For each http.Error found, replace with common.WriteProblem**

Pattern:
```go
// Before
http.Error(w, "invalid GPSI", http.StatusBadRequest)

// After
common.WriteProblem(w, common.ValidationProblem("gpsi", "invalid format"))
```

Available helpers:
- `ValidationProblem(field, reason)` → HTTP 400
- `ForbiddenProblem(detail)` → HTTP 403
- `NotFoundProblem(detail)` → HTTP 404
- `ConflictProblem(detail)` → HTTP 409
- `BadGatewayProblem(detail)` → HTTP 502
- `ServiceUnavailableProblem(detail)` → HTTP 503
- `GatewayTimeoutProblem(detail)` → HTTP 504
- `InternalServerProblem(detail)` → HTTP 500

- [ ] **Step 4: Verify vet passes**

Run: `go vet ./internal/api/... ./internal/nrm/... 2>/dev/null || echo "nrm package may not exist"`
Expected: PASS (no issues)

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix(api): standardize error handling with ProblemDetails

Replace raw http.Error calls with common.WriteProblem and
proper ProblemDetails constructors per TS 29.526 §7 error codes."
```

---

## Task 5: Extract Factory Functions from cmd/biz/main.go

**Files:**
- Create: `cmd/biz/factory.go`
- Modify: `cmd/biz/main.go`

- [ ] **Step 1: Read cmd/biz/main.go**

Extract these initialization blocks into factory:
1. PostgreSQL pool + session stores (lines ~85-144)
2. Redis pool + DLQ (lines ~146-171)
3. NRF client + UDM client + AUSF client (lines ~173-192)
4. HTTP AAA client (lines ~199-220)
5. Handler construction (lines ~222-239)

- [ ] **Step 2: Create cmd/biz/factory.go**

```go
// Package main provides the Biz Pod factory for dependency injection.
// Spec: Architecture Improvement
package main

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "time"

    "github.com/operator/nssAAF/internal/amf"
    "github.com/operator/nssAAF/internal/api/aiw"
    "github.com/operator/nssAAF/internal/api/common"
    "github.com/operator/nssAAF/internal/api/nssaa"
    "github.com/operator/nssAAF/internal/ausf"
    "github.com/operator/nssAAF/internal/cache/redis"
    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/crypto"
    "github.com/operator/nssAAF/internal/metrics"
    "github.com/operator/nssAAF/internal/nrf"
    "github.com/operator/nssAAF/internal/resilience"
    "github.com/operator/nssAAF/internal/storage/postgres"
    "github.com/operator/nssAAF/internal/tracing"
    "github.com/operator/nssAAF/internal/udm"
)

// BizPod holds all dependencies for the Biz Pod.
type BizPod struct {
    Server       *http.Server
    NRFClient    *nrf.Client
    SessionStore *postgres.SessionStore
    AIWSessionStore *postgres.SessionStore
    RedisPool    *redis.Pool
    DLQ          *redis.DLQ
    Logger       *slog.Logger
    Shutdown     func()
}

// BizPodOption configures a BizPod.
type BizPodOption func(*BizPod)

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) BizPodOption {
    return func(bp *BizPod) { bp.Logger = logger }
}

// WithPodID sets the pod ID for service discovery.
func WithPodID(podID string) BizPodOption {
    return func(bp *BizPod) { bp.podID = podID }
}

// BizPodFactory creates BizPod instances with dependency injection.
type BizPodFactory struct {
    cfg    *config.Config
    logger *slog.Logger
    podID  string
}

// NewBizPodFactory creates a new factory.
func NewBizPodFactory(cfg *config.Config, opts ...BizPodOption) *BizPodFactory {
    f := &BizPodFactory{cfg: cfg, logger: slog.Default(), podID: "unknown"}

    for _, opt := range opts {
        opt(f)
    }

    return f
}

// Build creates a fully-wired BizPod.
func (f *BizPodFactory) Build(ctx context.Context) (*BizPod, error) {
    bp := &BizPod{Logger: f.logger}

    // Initialize OpenTelemetry tracing
    tracingShutdown := tracing.Init("nssAAF-biz", f.cfg.Version, f.podID)
    bp.Shutdown = func() {
        tracingShutdown()
    }

    // Build API root URL
    apiRoot := f.cfg.Server.Addr
    if !hasScheme(apiRoot) {
        apiRoot = "http://" + apiRoot
    }

    // PostgreSQL pool
    pgPool, err := postgres.NewPool(ctx, postgres.Config{
        DSN: fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
            f.cfg.Database.User, f.cfg.Database.Password, f.cfg.Database.Host,
            f.cfg.Database.Port, f.cfg.Database.Name, f.cfg.Database.SSLMode),
        MaxConns:          int32(f.cfg.Database.MaxConns),
        MinConns:          int32(f.cfg.Database.MinConns),
        MaxConnLifetime:   f.cfg.Database.ConnMaxLifetime,
        MaxConnIdleTime:   10 * time.Minute,
        HealthCheckPeriod: 30 * time.Second,
    })
    if err != nil {
        return nil, fmt.Errorf("postgres pool: %w", err)
    }

    // Run migrations
    migrator := postgres.NewMigrator(pgPool)
    if err := migrator.Migrate(ctx); err != nil {
        pgPool.Close()
        return nil, fmt.Errorf("database migration: %w", err)
    }

    // Initialize crypto
    var vaultCfg *crypto.VaultConfig
    if f.cfg.Crypto.VaultConfig != nil {
        vaultCfg = &crypto.VaultConfig{
            Address:    f.cfg.Crypto.VaultConfig.Address,
            KeyName:    f.cfg.Crypto.VaultConfig.KeyName,
            AuthMethod: f.cfg.Crypto.VaultConfig.AuthMethod,
            K8sRole:    f.cfg.Crypto.VaultConfig.K8sRole,
            Token:      f.cfg.Crypto.VaultConfig.Token,
            TokenFile:  f.cfg.Crypto.VaultConfig.TokenFile,
        }
    }
    if err := crypto.Init(&crypto.Config{
        KeyManager:     f.cfg.Crypto.KeyManager,
        MasterKeyHex:   f.cfg.Crypto.MasterKeyHex,
        KEKOverlapDays: f.cfg.Crypto.KEKOverlapDays,
        Vault:          vaultCfg,
    }); err != nil {
        pgPool.Close()
        return nil, fmt.Errorf("crypto.Init: %w", err)
    }

    // Session stores
    encryptor, err := postgres.NewEncryptorFromKeyManager(crypto.KM())
    if err != nil {
        pgPool.Close()
        return nil, fmt.Errorf("session encryptor: %w", err)
    }

    bp.SessionStore = postgres.NewSessionStore(pgPool, encryptor)
    bp.AIWSessionStore = postgres.NewAIWSessionStore(pgPool, encryptor)

    // Redis pool
    redisPool, err := redis.NewPool(ctx, redis.Config{
        Addrs:        []string{f.cfg.Redis.Addr},
        Password:     f.cfg.Redis.Password,
        DB:           f.cfg.Redis.DB,
        PoolSize:     f.cfg.Redis.PoolSize,
        MinIdleConns: 10,
        DialTimeout:  100 * time.Millisecond,
        ReadTimeout:  100 * time.Millisecond,
        WriteTimeout: 100 * time.Millisecond,
    })
    if err != nil {
        pgPool.Close()
        return nil, fmt.Errorf("redis pool: %w", err)
    }

    bp.RedisPool = redisPool

    // DLQ
    bp.DLQ = redis.NewDLQ(redisPool)
    go bp.DLQ.Process(ctx)

    // Resilience: circuit breaker registry
    cbRegistry := resilience.NewRegistry(
        f.cfg.AAA.FailureThreshold,
        f.cfg.AAA.RecoveryTimeout,
        3*time.Second,
    )

    // NRF client
    nrfClient := nrf.NewClient(f.cfg.NRF)
    go nrfClient.RegisterAsync(ctx)
    go nrfClient.StartHeartbeat(ctx)
    bp.NRFClient = nrfClient

    // UDM and AUSF clients
    udmClient := udm.NewClient(f.cfg.UDM, nrfClient)
    ausfClient := ausf.NewClient(f.cfg.AUSF)

    // AMF notifier
    _ = amf.NewClient(30*time.Second, cbRegistry, bp.DLQ)

    // HTTP AAA client
    tlsCfg := &tls.Config{}
    if f.cfg.Biz.UseMTLS {
        tlsCfg.RootCAs = mustLoadCertPool(f.cfg.Biz.TLSCA)
        tlsCfg.Certificates = []tls.Certificate{mustLoadCert(f.cfg.Biz.TLSCert, f.cfg.Biz.TLSKey)}
        tlsCfg.ServerName = "aaa-gateway"
    }
    aaaClient := newHTTPAAAClient(
        f.cfg.Biz.AAAGatewayURL,
        f.cfg.Redis.Addr,
        f.podID,
        f.cfg.Version,
        &http.Client{
            Transport: &http.Transport{TLSClientConfig: tlsCfg},
            Timeout:   30 * time.Second,
        },
    )

    // Handlers
    nssaaHandler := nssaa.NewHandler(bp.SessionStore,
        nssaa.WithAPIRoot(apiRoot),
        nssaa.WithAAA(aaaClient),
        nssaa.WithNRFClient(nrfClient),
        nssaa.WithUDMClient(udmClient),
    )
    nssaaRouter := nssaa.NewRouter(nssaaHandler, apiRoot)

    aiwwHandler := aiw.NewHandler(bp.AIWSessionStore,
        aiw.WithAPIRoot(apiRoot),
        aiw.WithAUSFClient(ausfClient),
    )
    aiwwRouter := aiw.NewRouter(aiwwHandler, apiRoot)

    // HTTP server
    mux := http.NewServeMux()
    mux.HandleFunc("/aaa/forward", handleAaaForward)
    mux.HandleFunc("/aaa/server-initiated", handleServerInitiated)
    mux.Handle("/nnssaaf-nssaa/", nssaaRouter)
    mux.Handle("/nnssaaf-aiw/", aiwwRouter)
    mux.HandleFunc("/healthz/live", handleLiveness)
    mux.HandleFunc("/healthz/ready", handleReadiness)
    mux.Handle("/metrics", metrics.Handler())

    // Middleware
    var handler http.Handler = mux
    handler = common.RecoveryMiddleware(handler)
    handler = common.RequestIDMiddleware(handler)
    handler = common.MetricsMiddleware(handler)
    handler = common.LoggingMiddleware(handler)
    handler = common.CORSMiddleware(handler)

    bp.Server = &http.Server{
        Addr:         f.cfg.Server.Addr,
        Handler:      handler,
        ReadTimeout:  f.cfg.Server.ReadTimeout,
        WriteTimeout: f.cfg.Server.WriteTimeout,
        IdleTimeout:  f.cfg.Server.IdleTimeout,
    }

    return bp, nil
}

// hasScheme returns true if s already contains a URL scheme prefix.
func hasScheme(s string) bool {
    return len(s) >= 7 && (s[:7] == "http://" || s[:8] == "https://")
}

// mustLoadCertPool loads and parses a CA certificate file into an x509.CertPool.
func mustLoadCertPool(caPath string) *x509.CertPool {
    data, err := os.ReadFile(caPath)
    if err != nil {
        panic("failed to read TLS CA cert: " + err.Error())
    }
    pool := x509.NewCertPool()
    if !pool.AppendCertsFromPEM(data) {
        panic("failed to parse TLS CA cert from: " + caPath)
    }
    return pool
}

// mustLoadCert loads a client certificate and key for mTLS.
func mustLoadCert(certPath, keyPath string) tls.Certificate {
    cert, err := tls.LoadX509KeyPair(certPath, keyPath)
    if err != nil {
        panic("failed to load TLS cert/key pair: " + err.Error())
    }
    return cert
}
```

- [ ] **Step 3: Simplify cmd/biz/main.go**

Replace the 512-line main.go with a streamlined version:

```go
// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
    "context"
    "flag"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/operator/nssAAF/internal/config"
)

var configPath = flag.String("config", "configs/biz.yaml", "path to YAML configuration file")

// Health check closure variables (set during initialization)
var (
    pgHealth    func(ctx context.Context) error
    redisHealth func(ctx context.Context) error
    nrfHealth   interface{ IsRegistered() bool }
)

func main() {
    flag.Parse()

    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    slog.SetDefault(logger)

    cfg, err := config.Load(*configPath)
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }
    if cfg.Component != config.ComponentBiz {
        slog.Error("config.component must be 'biz'", "got", cfg.Component)
        os.Exit(1)
    }

    podID, _ := os.Hostname()
    slog.Info("starting NSSAAF Biz Pod",
        "pod_id", podID,
        "version", cfg.Version,
        "use_mtls", cfg.Biz.UseMTLS,
    )

    // Build BizPod using factory
    factory := NewBizPodFactory(cfg,
        WithLogger(logger),
        WithPodID(podID),
    )

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    pod, err := factory.Build(ctx)
    if err != nil {
        slog.Error("failed to build BizPod", "error", err)
        os.Exit(1)
    }
    defer pod.Shutdown()

    // Wire health check closures
    pgHealth = pod.SessionStore.Ping
    redisHealth = func(ctx context.Context) error {
        return pod.RedisPool.Client().Ping(ctx).Err()
    }
    nrfHealth = pod.NRFClient

    // Start HTTP server
    errCh := make(chan error, 1)
    go func() {
        slog.Info("biz HTTP server listening", "addr", pod.Server.Addr)
        if err := pod.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            errCh <- err
        }
    }()

    // Biz Pod heartbeat
    go podHeartbeat(context.Background(), cfg.Redis.Addr, podID)

    select {
    case err := <-errCh:
        slog.Error("server error", "error", err)
        os.Exit(1)
    case <-signalReceived():
        slog.Info("shutdown signal received")
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        if pod.NRFClient != nil {
            nrfCtx, nrfCancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer nrfCancel()
            _ = pod.NRFClient.Deregister(nrfCtx)
        }

        _ = pod.Server.Shutdown(ctx)
    }
}
```

- [ ] **Step 4: Verify build**

Run: `go build ./cmd/biz/...`
Expected: PASS

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/biz/... -short` (if tests exist)
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/biz/factory.go cmd/biz/main.go
git commit -m "refactor(biz): extract factory from main.go

Extract initialization logic into cmd/biz/factory.go:
- BizPod struct holds all dependencies
- BizPodFactory with functional options (WithLogger, WithPodID)
- Build() method wires PostgreSQL, Redis, crypto, NRF, handlers
- main.go reduced from ~512 lines to ~120 lines

Improves testability: factory accepts interfaces for mock injection."
```

---

## Final Verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 2: Full test**

Run: `go test ./... -short`
Expected: PASS

- [ ] **Step 3: Full vet**

Run: `go vet ./...`
Expected: PASS (no issues)

---

## Success Criteria Summary

| Task | Criteria |
|------|----------|
| 1. Metrics | `biz/router.go` calls `metrics.AaaRequestsTotal.WithLabelValues` |
| 2. Domain | `internal/domain/nssaa_status.go` exists with `TransitionTo`, 16 tests pass |
| 3. Delete deprecated | `internal/aaa/router.go` deleted, build passes |
| 4. Error handling | All `http.Error` replaced with ProblemDetails |
| 5. Factory | `cmd/biz/main.go` reduced by >50%, `cmd/biz/factory.go` created |

---

## Rollback Instructions

If issues arise, revert in reverse order:
```bash
git revert HEAD~4  # undo all
git revert HEAD~3  # undo Task 5
git revert HEAD~2  # undo Task 4
git revert HEAD~1  # undo Task 3 (may need manual: git checkout HEAD~2 -- internal/aaa/router.go)
```
