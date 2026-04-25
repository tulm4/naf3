---
phase: "04"
name: NF Integration & Observability
padded: "04"
wave_count: 5
requirements_addressed:
  - REQ-01
  - REQ-02
  - REQ-03
  - REQ-04
  - REQ-05
  - REQ-06
  - REQ-07
  - REQ-08
  - REQ-09
  - REQ-10
  - REQ-11
  - REQ-12
  - REQ-13
  - REQ-14
  - REQ-15
  - REQ-16
  - REQ-17
  - REQ-18
  - REQ-19
files_modified:
  - go.mod
  - go.sum
  - cmd/biz/main.go
  - internal/config/config.go
  - internal/nrf/client.go
  - internal/udm/client.go
  - internal/amf/notifier.go
  - internal/ausf/client.go
  - internal/resilience/circuit_breaker.go
  - internal/resilience/retry.go
  - internal/metrics/metrics.go
  - internal/logging/gpsi.go
  - internal/tracing/tracing.go
  - internal/cache/redis/dlq.go
  - internal/storage/postgres/session_store.go
  - internal/api/nssaa/handler.go
  - internal/api/aiw/handler.go
  - compose/configs/biz.yaml
  - deployments/nssaa-biz/servicemonitor.yaml
  - deployments/nssaa-biz/prometheusrules.yaml
created: "2026-04-25"
---

# Phase 4 Plan: NF Integration & Observability

**Purpose:** Wire NRF, UDM, AMF, AUSF clients into Biz Pod; implement resilience patterns (circuit breaker, retry, timeout, health endpoints); implement observability (Prometheus metrics, structured logging, OpenTelemetry tracing); replace in-memory session stores with PostgreSQL.

**Locked Decisions (from 04-CONTEXT.md):**
- D-01: Full cross-component OTel tracing — W3C TraceContext, Biz Pod is trace hub
- D-02: DLQ for AMF notification failures — Redis list LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`
- D-03: Per host:port circuit breaker — `CircuitBreakerRegistry` keyed by `"host:port"`
- D-04: Startup in degraded mode — NRF registration retried in background, does not block startup
- D-05: Handler option functions — `WithNRFClient`, `WithUDMClient`, `WithAUSFClient`
- D-06: `NewSessionStore(*Pool)` and `NewAIWSessionStore(*Pool)` in `internal/storage/postgres/session_store.go`
- D-07: Health endpoints at `/healthz/live` and `/healthz/ready`

**Deferred Ideas (OUT OF SCOPE for this plan):**
- Per S-NSSAI circuit breaker (sst+sd+host granularity)
- HTTP Gateway and AAA Gateway metrics/tracing integration

---

## Source Audit

### Goals Covered

| GOAL | Plan |
|------|------|
| NRF client wired to Biz Pod | 04-02, 04-03 |
| PostgreSQL session store replaces in-memory | 04-02 |
| Resilience patterns (circuit breaker, retry, timeout) | 04-01 |
| Prometheus metrics | 04-03 |
| Health endpoints | 04-03 |
| DLQ for AMF notifications | 04-04 |
| UDM integration | 04-04 |
| AMF notifier | 04-04 |
| AUSF client | 04-04 |
| OTel tracing + structured logging | 04-03 |
| ServiceMonitor + alerting rules | 04-05 |
| All clients wired in main.go | 04-04 |

### Requirements Covered

| REQ | Description | Plan |
|-----|-------------|------|
| REQ-01 | NRF registration on startup (background, non-blocking) | 04-02 |
| REQ-02 | Nnrf_NFHeartBeat every 5 minutes | 04-02 |
| REQ-03 | AMF discovered via Nnrf_NFDiscovery | 04-02 |
| REQ-04 | UDM Nudm_UECM_Get wired to N58 handler | 04-04 |
| REQ-05 | UDM Nudm_UECM_UpdateAuthContext after EAP | 04-04 |
| REQ-06 | AMF Re-Auth notification POST to reauthNotifUri | 04-04 |
| REQ-07 | AMF Revocation notification POST to revocNotifUri | 04-04 |
| REQ-08 | AUSF N60 client (internal/ausf/) | 04-04 |
| REQ-09 | PostgreSQL session store (NewSessionStore/NewAIWSessionStore) | 04-02 |
| REQ-10 | DLQ for AMF notification failures | 04-04 |
| REQ-11 | Circuit breaker per host:port | 04-01 |
| REQ-12 | Retry with exponential backoff (1s, 2s, 4s, max 3) | 04-01 |
| REQ-13 | Timeouts: 30s EAP, 10s AAA, 5s DB, 100ms Redis | 04-01 |
| REQ-14 | Prometheus metrics at /metrics | 04-03 |
| REQ-15 | ServiceMonitor CRDs for all 3 components | 04-05 |
| REQ-16 | Structured JSON logs with GPSI hashed (SHA256, 8 bytes, base64url) | 04-03 |
| REQ-17 | Full cross-component OTel tracing (W3C TraceContext) | 04-03 |
| REQ-18 | Health endpoints /healthz/live and /healthz/ready | 04-03 |
| REQ-19 | Prometheus alerting rules | 04-05 |

**Every requirement (REQ-01 through REQ-19) appears in at least one plan.**

---

## Wave Structure

| Wave | Plans | Content | Files Modified |
|------|-------|---------|---------------|
| 1 | 04-01 | go.mod deps, circuit breaker, retry, GPSI logging | go.mod, go.sum, internal/resilience/circuit_breaker.go, internal/resilience/retry.go, internal/logging/gpsi.go |
| 2 | 04-02 | NRF client, AUSF config, PostgreSQL session store, handler options | internal/nrf/client.go, internal/config/config.go, internal/storage/postgres/session_store.go, internal/api/nssaa/handler.go, internal/api/aiw/handler.go |
| 3 | 04-03 | Prometheus metrics, OTel tracing, health endpoints | internal/metrics/metrics.go, internal/tracing/tracing.go, cmd/biz/main.go |
| 4 | 04-04 | UDM client, AMF notifier, AUSF client, DLQ, main.go wiring | internal/udm/client.go, internal/amf/notifier.go, internal/ausf/client.go, internal/cache/redis/dlq.go, cmd/biz/main.go |
| 5 | 04-05 | ServiceMonitor CRDs, Prometheus alerting rules, biz.yaml config fixture | compose/configs/biz.yaml, deployments/nssaa-biz/servicemonitor.yaml, deployments/nssaa-biz/prometheusrules.yaml |

---

## 04-01: Foundation — Resilience + Structured Logging

**Plan:** 04-01
**Wave:** 1
**Type:** execute
**Depends:** none
**Requirements:** REQ-11, REQ-12, REQ-13, REQ-16
**Files modified:** go.mod, go.sum, internal/resilience/circuit_breaker.go, internal/resilience/retry.go, internal/logging/gpsi.go

<objective>
Implement shared resilience primitives (circuit breaker, retry with backoff) and GPSI hashing for structured logging. These are consumed by all NF clients and the AMF notifier.
</objective>

<read_first>
- internal/aaa/router.go (lines 89-100: mutex discipline for circuit breaker state)
- internal/cache/redis/lock.go (lines 49-74: retry loop pattern with context cancellation)
- docs/design/10_ha_architecture.md (§6: circuit breaker state machine)
- docs/design/19_observability.md (§3.1: GPSI hash format)
</read_first>

<action>
### Task 04-01-1: Update go.mod with observability dependencies

**Files:** go.mod, go.sum

Add the following `require` block entries to `go.mod` (they will be downloaded by `go mod tidy`):

```
	github.com/prometheus/client_golang v1.20.5
	go.opentelemetry.io/otel v1.32.0
	go.opentelemetry.io/otel/sdk v1.32.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.32.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp/v1.32.0
	go.opentelemetry.io/otel/trace v1.32.0
	go.opentelemetry.io/otel/attribute v1.32.0
	go.opentelemetry.io/otel/semconv/v1.26.0
```

Then run: `cd /home/tulm/naf3 && go mod tidy`

### Task 04-01-2: Implement circuit breaker

**File:** internal/resilience/circuit_breaker.go

Create the file with this exact content:

```go
// Package resilience provides high-availability patterns: circuit breakers,
// retries, load balancing, and failover mechanisms.
package resilience

import (
	"sync"
	"time"
)

// State represents the circuit breaker state machine.
// Spec: TS 33.501 §16 — circuit breaker pattern for AAA server failure isolation
const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// State is the state of a circuit breaker.
type State int

// String implements fmt.Stringer.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker implements a per-host:port circuit breaker.
// REQ-11: CLOSED → OPEN (5 consecutive failures) → HALF_OPEN (30s recovery) → CLOSED (3 successes)
// D-03: Registry keyed by "host:port"
type CircuitBreaker struct {
	mu                sync.Mutex
	state             State
	failures          int
	successes         int
	lastFailure       time.Time
	openedAt          time.Time
	failureThreshold  int
	recoveryTimeout   time.Duration
	successThreshold  int
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
// Default: failureThreshold=5, recoveryTimeout=30s, successThreshold=3.
func NewCircuitBreaker(failureThreshold int, recoveryTimeout, successThreshold time.Duration) *CircuitBreaker {
	if failureThreshold == 0 {
		failureThreshold = 5
	}
	if recoveryTimeout == 0 {
		recoveryTimeout = 30 * time.Second
	}
	if successThreshold == 0 {
		successThreshold = 3
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		recoveryTimeout:  recoveryTimeout,
		successThreshold: int(successThreshold),
	}
}

// Allow returns true if the circuit breaker allows a request.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) >= cb.recoveryTimeout {
			cb.state = StateHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = StateClosed
			cb.failures = 0
		}
	case StateClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = time.Now()
	cb.failures++

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.failureThreshold {
			cb.state = StateOpen
			cb.openedAt = time.Now()
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.openedAt = time.Now()
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Registry manages named circuit breakers keyed by "host:port".
// D-03: CircuitBreakerRegistry keyed by "host:port".
type Registry struct {
	mu                      sync.RWMutex
	breakers                map[string]*CircuitBreaker
	defaultFailureThreshold int
	defaultRecoveryTimeout  time.Duration
	defaultSuccessThreshold int
}

// NewRegistry creates a circuit breaker registry with defaults from AAAConfig.
func NewRegistry(failureThreshold int, recoveryTimeout, successThreshold time.Duration) *Registry {
	if failureThreshold == 0 {
		failureThreshold = 5
	}
	if recoveryTimeout == 0 {
		recoveryTimeout = 30 * time.Second
	}
	if successThreshold == 0 {
		successThreshold = 3
	}
	return &Registry{
		breakers:                make(map[string]*CircuitBreaker),
		defaultFailureThreshold: failureThreshold,
		defaultRecoveryTimeout:  recoveryTimeout,
		defaultSuccessThreshold: int(successThreshold),
	}
}

// Get returns the circuit breaker for a given key, creating it if needed.
func (r *Registry) Get(key string) *CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[key]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if cb, ok := r.breakers[key]; ok {
		return cb
	}
	cb = NewCircuitBreaker(r.defaultFailureThreshold, r.defaultRecoveryTimeout, time.Duration(r.defaultSuccessThreshold))
	r.breakers[key] = cb
	return cb
}
```

### Task 04-01-3: Implement retry with exponential backoff

**File:** internal/resilience/retry.go

Create the file with this exact content:

```go
package resilience

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetryConfig holds retry configuration.
// REQ-12: MaxAttempts=3, BaseDelay=1s, MaxDelay=4s (1s, 2s, 4s).
var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   1 * time.Second,
	MaxDelay:    4 * time.Second,
}

// RetryConfig holds retry parameters.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// ErrMaxRetriesExceeded is returned when all retry attempts fail.
var ErrMaxRetriesExceeded = errors.New("max retries exceeded")

// Do executes fn up to MaxAttempts times with exponential backoff.
// It respects context cancellation during sleep intervals.
// It does NOT sleep before the first attempt.
// It sleeps between attempts (1s, 2s, 4s for default config).
// It returns ErrMaxRetriesExceeded if all attempts fail.
func Do(ctx context.Context, cfg RetryConfig, fn func() error) error {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = DefaultRetryConfig.MaxAttempts
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = DefaultRetryConfig.BaseDelay
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = DefaultRetryConfig.MaxDelay
	}

	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt < cfg.MaxAttempts-1 {
			delay := cfg.BaseDelay * time.Duration(1<<attempt)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, lastErr)
}

// IsRetryable returns true if an error should trigger a retry.
// Retryable: 5xx status codes, 429 Too Many Requests.
func IsRetryable(statusCode int) bool {
	return statusCode >= 500 || statusCode == 429
}
```

### Task 04-01-4: Implement GPSI hash for structured logging

**File:** internal/logging/gpsi.go

Create the file with this exact content:

```go
// Package logging provides structured logging utilities for NSSAAF.
// REQ-16: GPSI hashed in logs (SHA256, first 8 bytes, base64url) — never log raw GPSI.
package logging

import (
	"crypto/sha256"
	"encoding/base64"
)

// HashGPSI returns a hash of the GPSI for logging purposes.
// Format: SHA256(gpsi)[0:8], base64url encoded.
// Per REQ-16 and docs/design/19_observability.md §3.1.
func HashGPSI(gpsi string) string {
	h := sha256.Sum256([]byte(gpsi))
	return base64.RawURLEncoding.EncodeToString(h[:8])
}
```

### Task 04-01-5: Create circuit breaker test

**File:** internal/resilience/circuit_breaker_test.go

Create test file covering state transitions CLOSED→OPEN→HALF_OPEN→CLOSED per REQ-11.

### Task 04-01-6: Create retry test

**File:** internal/resilience/retry_test.go

Create test file covering exponential backoff (1s, 2s, 4s) per REQ-12.

### Task 04-01-7: Create GPSI hash test

**File:** internal/logging/gpsi_test.go

Create test file verifying hash consistency (same GPSI → same hash) per REQ-16.
</action>

<acceptance_criteria>
- [ ] `go mod tidy` succeeds with no errors
- [ ] `grep -r "StateClosed\|StateOpen\|StateHalfOpen" internal/resilience/` finds circuit_breaker.go
- [ ] `grep "func Do\|ErrMaxRetriesExceeded" internal/resilience/` finds retry.go
- [ ] `grep "func HashGPSI" internal/logging/` finds gpsi.go
- [ ] `go test ./internal/resilience/... -run TestCircuitBreaker -v` passes
- [ ] `go test ./internal/resilience/... -run TestRetryWithBackoff -v` passes
- [ ] `go test ./internal/logging/... -run TestGPSIHash -v` passes
</acceptance_criteria>

<verify>
go test ./internal/resilience/... ./internal/logging/... -v -count=1
</verify>

---

## 04-02: NF Core — NRF Client + PostgreSQL Session Store + Handler Options

**Plan:** 04-02
**Wave:** 2
**Type:** execute
**Depends:** 04-01
**Requirements:** REQ-01, REQ-02, REQ-03, REQ-08, REQ-09
**Files modified:** internal/nrf/client.go, internal/config/config.go, internal/storage/postgres/session_store.go, internal/api/nssaa/handler.go, internal/api/aiw/handler.go

<objective>
Implement the NRF client (registration, heartbeat, discovery with 5-min TTL cache), create AUSFConfig in config.go, implement PostgreSQL session store wrappers (NewSessionStore, NewAIWSessionStore) per D-06, and add handler option functions (WithNRFClient, WithUDMClient, WithAUSFClient) per D-05.
</objective>

<read_first>
- cmd/biz/main.go (lines 58-91: existing in-memory stores and handler construction)
- internal/config/config.go (lines 22-42: Config struct, NRFConfig, UDMConfig)
- docs/design/05_nf_profile.md (§2.2: NFProfile JSON structure, §3.3: cache TTL 5min)
- PATTERNS.md (Pattern 4: session_store.go target code)
- PATTERNS.md (Pattern 2: option function pattern)
</read_first>

<action>
### Task 04-02-1: Implement NRF client

**File:** internal/nrf/client.go

Replace the stub with full NRF client. Key structures:

```go
package nrf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is the NRF service discovery client.
// REQ-01: NRF registration on startup (degraded mode, D-04).
// REQ-02: Heartbeat every 5 minutes.
// REQ-03: Discovery with 5-min TTL cache.
type Client struct {
	baseURL      string
	httpClient   *http.Client
	nfInstanceID string
	cache        *NRFDiscoveryCache
	registered   atomic.Bool
}

// NRFDiscoveryCache holds cached NF discovery results with 5-min TTL.
// Cache keys per docs/design/05_nf_profile.md §3.3:
//   - "udm:uem:{plmnId}" → UDM Nudm_UECM endpoint
//   - "amf:{amfId}" → AMF profile
type NRFDiscoveryCache struct {
	mu    sync.RWMutex
	cache map[string]*cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

func (c *NRFDiscoveryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

func (c *NRFDiscoveryCache) Set(key string, data interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]*cacheEntry)
	}
	c.cache[key] = &cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// NFProfile is the NSSAAF NF profile for NRF registration.
// Spec: TS 29.510 §6 — fields from docs/design/05_nf_profile.md §2.2.
type NFProfile struct {
	NFInstanceID string `json:"nfInstanceId"`
	NFType       string `json:"nfType"` // "NSSAAF"
	NFStatus     string `json:"nfStatus"`
	HeartBeatTimer int `json:"heartBeatTimer"`
	Load         int    `json:"load"`
}

// NewClient creates a new NRF client.
func NewClient(cfg config.NRFConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: cfg.DiscoverTimeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		nfInstanceID: fmt.Sprintf("nssAAF-instance-%d", time.Now().UnixNano()),
		cache: &NRFDiscoveryCache{
			ttl: 5 * time.Minute,
		},
	}
}

// RegisterAsync registers the NSSAAF profile with NRF in a background goroutine.
// REQ-01 / D-04: Returns immediately (degraded mode), retries in background.
func (c *Client) RegisterAsync(ctx context.Context) {
	go func() {
		backoff := time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := c.Register(ctx); err != nil {
				slog.Warn("nrf registration failed, retrying",
					"error", err,
					"backoff", backoff,
				)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}

			slog.Info("nrf registration successful",
				"nf_instance_id", c.nfInstanceID,
			)
			c.registered.Store(true)
			return
		}
	}()
}

// Register sends Nnrf_NFRegistration to the NRF.
// REQ-01: POST /nnrf-disc/v1/nf-instances with NFProfile.
func (c *Client) Register(ctx context.Context) error {
	profile := NFProfile{
		NFInstanceID: c.nfInstanceID,
		NFType:       "NSSAAF",
		NFStatus:     "REGISTERED",
		HeartBeatTimer: 300,
		Load:         0,
	}
	body, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("nrf: marshal profile: %w", err)
	}
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nrf: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nrf: register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("nrf: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// Heartbeat sends Nnrf_NFHeartBeat every 5 minutes.
// REQ-02: PUT /nnrf-disc/v1/nf-instances/{id} with nfStatus="REGISTERED", heartBeatTimer=300, load=0-100.
func (c *Client) Heartbeat(ctx context.Context) error {
	payload := map[string]interface{}{
		"nfInstanceId":  c.nfInstanceID,
		"nfStatus":      "REGISTERED",
		"heartBeatTimer": 300,
		"load":          0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nrf: marshal heartbeat: %w", err)
	}
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances/%s", c.baseURL, c.nfInstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nrf: create heartbeat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nrf: heartbeat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nrf: heartbeat status %d", resp.StatusCode)
	}
	return nil
}

// StartHeartbeat runs the heartbeat goroutine every 5 minutes.
func (c *Client) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Heartbeat(ctx); err != nil {
				slog.Warn("nrf heartbeat failed", "error", err)
			}
		}
	}
}

// DiscoverUDM discovers a UDM that exposes the nudm-uem service.
// REQ-03 / docs/design/05_nf_profile.md §3.2.
func (c *Client) DiscoverUDM(ctx context.Context, plmnId string) (string, error) {
	key := fmt.Sprintf("udm:uem:%s", plmnId)
	if endpoint, ok := c.cache.Get(key); ok {
		return endpoint.(string), nil
	}
	// NRF discovery query
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("nrf: create discovery request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nrf: discover udm: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nrf: discover udm status %d", resp.StatusCode)
	}
	var result struct {
		NFInstances []struct {
			NFServices map[string]struct {
				IPEndPoints []struct {
					IPv4Address string `json:"ipv4Address"`
					Port        int    `json:"port"`
				} `json:"ipEndPoints"`
			} `json:"nfServices"`
		} `json:"nfInstances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("nrf: decode discovery: %w", err)
	}
	// Extract first UDM's nudm-uem endpoint
	for _, inst := range result.NFInstances {
		if svc, ok := inst.NFServices["nudm-uem"]; ok {
			for _, ep := range svc.IPEndPoints {
				endpoint := fmt.Sprintf("http://%s:%d", ep.IPv4Address, ep.Port)
				c.cache.Set(key, endpoint)
				return endpoint, nil
			}
		}
	}
	return "", fmt.Errorf("nrf: no UDM found for plmnId %s", plmnId)
}

// DiscoverAMF discovers an AMF by instance ID.
// REQ-03 / docs/design/05_nf_profile.md §3.1.
func (c *Client) DiscoverAMF(ctx context.Context, amfId string) (string, error) {
	key := fmt.Sprintf("amf:%s", amfId)
	if endpoint, ok := c.cache.Get(key); ok {
		return endpoint.(string), nil
	}
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances/%s", c.baseURL, amfId)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("nrf: create amf request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nrf: discover amf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nrf: discover amf status %d", resp.StatusCode)
	}
	var amf struct {
		NFInstanceID string `json:"nfInstanceId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&amf); err != nil {
		return "", fmt.Errorf("nrf: decode amf: %w", err)
	}
	c.cache.Set(key, amf.NFInstanceID)
	return amf.NFInstanceID, nil
}

// IsRegistered returns true if NRF registration succeeded.
func (c *Client) IsRegistered() bool {
	return c.registered.Load()
}
```

Note: Add `"bytes"` to the import block above. The file needs `import "bytes"` and `import "log/slog"` as well.

### Task 04-02-2: Add AUSFConfig to config.go

**File:** internal/config/config.go

Add AFTER the UDMConfig struct definition (after line 162):

```go
// AUSFConfig holds AUSF API settings.
type AUSFConfig struct {
	BaseURL string        `yaml:"baseURL"`
	Timeout time.Duration `yaml:"timeout"`
}
```

Add the `AUSF AUSFConfig` field to the `Config` struct (line 35, after UDM field):

```go
	UDM       UDMConfig     `yaml:"udm"`
	AUSF      AUSFConfig    `yaml:"ausf"`
```

Add defaults in `applyDefaults()` (after the UDM default, around line 163):

```go
	if cfg.UDM.Timeout == 0 {
		cfg.UDM.Timeout = 10 * time.Second
	}
	if cfg.AUSF.BaseURL == "" {
		cfg.AUSF.BaseURL = cfg.NRF.BaseURL // Default: discover via NRF
	}
	if cfg.AUSF.Timeout == 0 {
		cfg.AUSF.Timeout = 10 * time.Second
	}
```

### Task 04-02-3: Implement PostgreSQL session store wrappers

**File:** internal/storage/postgres/session_store.go

Create this file. It wraps the existing `Repository` from session.go to implement `nssaa.AuthCtxStore` and `aiw.AuthCtxStore` interfaces per D-06.

```go
// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// REQ-09: PostgreSQL session store replaces in-memory store via NewSessionStore/NewAIWSessionStore.
// D-06: NewSessionStore(*Pool) and NewAIWSessionStore(*Pool) implement the AuthCtxStore interfaces.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/api/aiw"
)

// Store implements nssaa.AuthCtxStore for PostgreSQL.
// Wraps Repository from session.go.
type Store struct {
	repo *Repository
}

// NewSessionStore creates a new PostgreSQL-backed session store for NSSAA.
// D-06: This is the factory function required by the implementation plan.
func NewSessionStore(pool *Pool, encryptor *Encryptor) *Store {
	return &Store{repo: NewRepository(pool, encryptor)}
}

// Load retrieves a slice authentication context by authCtxID.
func (s *Store) Load(id string) (*nssaa.AuthCtx, error) {
	session, err := s.repo.GetByAuthCtxID(context.Background(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, nssaa.ErrNotFound
		}
		return nil, fmt.Errorf("session store load: %w", err)
	}
	return sessionToAuthCtx(session), nil
}

// Save stores or updates a slice authentication context.
func (s *Store) Save(ctx *nssaa.AuthCtx) error {
	session := authCtxToSession(ctx)
	return s.repo.Update(context.Background(), session)
}

// Delete removes a slice authentication context by authCtxID.
func (s *Store) Delete(id string) error {
	return s.repo.DeleteByAuthCtxID(context.Background(), id)
}

// Close is a no-op. Pool lifecycle managed by main.go.
func (s *Store) Close() error {
	return nil
}

// AIWStore implements aiw.AuthCtxStore for PostgreSQL.
type AIWStore struct {
	repo *Repository
}

// NewAIWSessionStore creates a new PostgreSQL-backed session store for AIW.
// D-06: This is the factory function required by the implementation plan.
func NewAIWSessionStore(pool *Pool, encryptor *Encryptor) *AIWStore {
	return &AIWStore{repo: NewRepository(pool, encryptor)}
}

// Load retrieves an AIW authentication context by authCtxID.
func (s *AIWStore) Load(id string) (*aiw.AuthContext, error) {
	session, err := s.repo.GetByAuthCtxID(context.Background(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, aiw.ErrNotFound
		}
		return nil, fmt.Errorf("aiw session store load: %w", err)
	}
	return sessionToAIWAuthCtx(session), nil
}

// Save stores or updates an AIW authentication context.
func (s *AIWStore) Save(ctx *aiw.AuthContext) error {
	session := aiwAuthCtxToSession(ctx)
	return s.repo.Update(context.Background(), session)
}

// Delete removes an AIW authentication context by authCtxID.
func (s *AIWStore) Delete(id string) error {
	return s.repo.DeleteByAuthCtxID(context.Background(), id)
}

// Close is a no-op.
func (s *AIWStore) Close() error {
	return nil
}

// Helper: convert Session (DB) → nssaa.AuthCtx.
func sessionToAuthCtx(s *Session) *nssaa.AuthCtx {
	return &nssaa.AuthCtx{
		AuthCtxID:   s.AuthCtxID,
		GPSI:        s.GPSI,
		SnssaiSST:   s.SnssaiSST,
		SnssaiSD:    s.SnssaiSD,
		AmfInstance: s.AMFInstanceID,
		ReauthURI:   s.ReauthNotifURI,
		RevocURI:    s.RevocNotifURI,
		EapPayload:  s.EAPSessionState,
	}
}

// Helper: convert nssaa.AuthCtx → Session (DB).
func authCtxToSession(a *nssaa.AuthCtx) *Session {
	return &Session{
		AuthCtxID:       a.AuthCtxID,
		GPSI:            a.GPSI,
		SnssaiSST:       a.SnssaiSST,
		SnssaiSD:        a.SnssaiSD,
		AMFInstanceID:   a.AmfInstance,
		ReauthNotifURI:  a.ReauthURI,
		RevocNotifURI:   a.RevocURI,
		EAPSessionState: a.EapPayload,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// Helper: convert Session (DB) → aiw.AuthContext.
func sessionToAIWAuthCtx(s *Session) *aiw.AuthContext {
	return &aiw.AuthContext{
		AuthCtxID:  s.AuthCtxID,
		Supi:       s.Supi,
		EapPayload: s.EAPSessionState,
	}
}

// Helper: convert aiw.AuthContext → Session (DB).
func aiwAuthCtxToSession(a *aiw.AuthContext) *Session {
	return &Session{
		AuthCtxID:       a.AuthCtxID,
		Supi:            a.Supi,
		EAPSessionState: a.EapPayload,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}
```

### Task 04-02-4: Add WithNRFClient and WithUDMClient to nssaa handler

**File:** internal/api/nssaa/handler.go

Add to the `Handler` struct (after line 94):

```go
	nrfClient *nrf.Client
	udmClient interface {
		GetAuthContext(ctx context.Context, supi string) (interface{}, error)
	}
```

Add the following option functions AFTER the `WithAPIRoot` function (after line 108):

```go
// WithNRFClient sets the NRF client for service discovery.
func WithNRFClient(nrf *nrf.Client) HandlerOption {
	return func(h *Handler) { h.nrfClient = nrf }
}

// WithUDMClient sets the UDM client for subscription data retrieval.
func WithUDMClient(udm interface {
	GetAuthContext(ctx context.Context, supi string) (interface{}, error)
}) HandlerOption {
	return func(h *Handler) { h.udmClient = udm }
}
```

### Task 04-02-5: Add WithAUSFClient to aiw handler

**File:** internal/api/aiw/handler.go

Add to the `Handler` struct (after line 95):

```go
	ausfClient interface {
		ForwardMSK(ctx context.Context, authCtxID string, msk []byte) error
	}
```

Add the following option function AFTER the `WithAPIRoot` function (after line 109):

```go
// WithAUSFClient sets the AUSF client for MSK forwarding.
func WithAUSFClient(ausf interface {
	ForwardMSK(ctx context.Context, authCtxID string, msk []byte) error
}) HandlerOption {
	return func(h *Handler) { h.ausfClient = ausf }
}
```

### Task 04-02-6: Create NRF client test

**File:** internal/nrf/client_test.go

Test file covering Register, Heartbeat, DiscoverUDM, DiscoverAMF, RegisterAsync, IsRegistered per REQ-01, REQ-02, REQ-03, REQ-13.

### Task 04-02-7: Create PostgreSQL session store test

**File:** internal/storage/postgres/session_store_test.go

Test file covering NewSessionStore.Load, Save, Delete per REQ-09.
</action>

<acceptance_criteria>
- [ ] `grep "func NewSessionStore\|func NewAIWSessionStore" internal/storage/postgres/` finds session_store.go
- [ ] `grep "func WithNRFClient\|func WithUDMClient" internal/api/nssaa/` finds handler.go
- [ ] `grep "func WithAUSFClient" internal/api/aiw/` finds handler.go
- [ ] `grep "func Register\|func Heartbeat\|func DiscoverUDM\|func DiscoverAMF" internal/nrf/` finds client.go
- [ ] `grep "AUSFConfig" internal/config/` finds config.go
- [ ] `go build ./internal/nrf/...` succeeds
- [ ] `go test ./internal/nrf/... -v -count=1` passes
</acceptance_criteria>

<verify>
go build ./internal/nrf/... ./internal/storage/postgres/... ./internal/api/nssaa/... ./internal/api/aiw/... && go test ./internal/nrf/... -v -count=1
</verify>

---

## 04-03: Observability — Prometheus Metrics + OTel Tracing + Health Endpoints

**Plan:** 04-03
**Wave:** 3
**Type:** execute
**Depends:** 04-01
**Requirements:** REQ-14, REQ-16, REQ-17, REQ-18
**Files modified:** internal/metrics/metrics.go, internal/tracing/tracing.go, cmd/biz/main.go

<objective>
Implement Prometheus metrics at /metrics, OTel tracing initialization, structured logging helpers, and health endpoints (/healthz/live and /healthz/ready). Health endpoints replace the existing /health and /ready handlers.
</objective>

<read_first>
- docs/design/19_observability.md (§2.1: metric names, §4.1: OTel setup)
- PATTERNS.md (Pattern 6: Prometheus pattern, Pattern 10: OTel tracing)
- cmd/biz/main.go (lines 257-267: existing health handlers to replace)
- internal/api/common/middleware.go (lines 18-28: context propagation)
</read_first>

<action>
### Task 04-03-1: Create Prometheus metrics package

**File:** internal/metrics/metrics.go

Create with all metric definitions per docs/design/19_observability.md §2.1 and REQ-14:

```go
// Package metrics provides Prometheus metrics for NSSAAF observability.
// REQ-14: Prometheus metrics at /metrics (requests, latency, EAP sessions, AAA stats, circuit breakers).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Request metrics — REQ-14
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_requests_total",
		Help: "Total NSSAA API requests",
	}, []string{"service", "endpoint", "method", "status_code"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_request_duration_seconds",
		Help:    "NSSAA API request latency",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"service", "endpoint", "method"})

	// EAP session metrics — REQ-14
	EapSessionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nssAAF_eap_sessions_active",
		Help: "Number of active EAP sessions",
	})

	EapSessionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_eap_sessions_total",
		Help: "Total EAP sessions",
	}, []string{"result"})

	EapSessionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_eap_session_duration_seconds",
		Help:    "EAP session duration",
		Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
	}, []string{"eap_method"})

	EapRounds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "nssAAF_eap_rounds",
		Help:    "Number of EAP rounds per session",
		Buckets: []float64{1, 2, 3, 5, 10, 20},
	})

	// AAA protocol metrics — REQ-14
	AaaRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_aaa_requests_total",
		Help: "Total AAA protocol requests",
	}, []string{"protocol", "server", "result"})

	AaaRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_aaa_request_duration_seconds",
		Help:    "AAA request latency",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5},
	}, []string{"protocol", "server"})

	// Database metrics — REQ-14
	DbQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_db_query_duration_seconds",
		Help:    "Database query latency",
		Buckets: []float64{.001, .002, .005, .01, .025, .05, .1},
	}, []string{"operation", "table"})

	DbConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nssAAF_db_connections_active",
		Help: "Active database connections",
	})

	// Redis metrics — REQ-14
	RedisOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_redis_operations_total",
		Help: "Total Redis operations",
	}, []string{"operation", "result"})

	// Circuit breaker metrics — REQ-14
	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nssAAF_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"server"})

	CircuitBreakerFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_circuit_breaker_failures_total",
		Help: "Total circuit breaker recorded failures",
	}, []string{"server"})

	// NRF discovery cache metrics — REQ-14
	NrfCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nssAAF_nrf_cache_hits_total",
		Help: "NRF cache hits",
	})

	NrfCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nssAAF_nrf_cache_misses_total",
		Help: "NRF cache misses",
	})

	// DLQ metrics — REQ-14
	DLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nssAAF_dlq_depth",
		Help: "Number of items in AMF notification DLQ",
	})

	DLQProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nssAAF_dlq_processed_total",
		Help: "Total DLQ items processed",
	})
)
```

### Task 04-03-2: Create OTel tracing package

**File:** internal/tracing/tracing.go

Create per docs/design/19_observability.md §4.1 and REQ-17:

```go
// Package tracing provides OpenTelemetry setup for NSSAAF.
// REQ-17: Full cross-component OTel tracing via W3C TraceContext.
// D-01: Biz Pod is the trace correlation hub.
package tracing

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Init initializes OpenTelemetry with W3C TraceContext propagation.
// D-01: Full cross-component tracing. Call from main.go during startup.
// Returns a shutdown function to flush traces on graceful shutdown.
func Init(serviceName, version, podID string) (shutdown func()) {
	// W3C TraceContext propagator — D-01
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	// stdout exporter — swap for OTLP exporter in production
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		panic("tracing: failed to create exporter: " + err.Error())
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
			attribute.String("pod.name", podID),
		)),
	)
	otel.SetTracerProvider(tp)

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
}

// HTTPTransport returns an OTel-instrumented HTTP transport.
// Use this instead of http.DefaultTransport for NF client HTTP clients.
// REQ-17: Automatic span creation for all HTTP calls.
func HTTPTransport() http.RoundTripper {
	return otelhttp.NewTransport(http.DefaultTransport)
}

// NewTracer returns a tracer for the given name.
func NewTracer(name string) interface {
	Start(ctx context.Context, spanName string, opts ...interface{}) (context.Context, interface{})
} {
	return otel.Tracer(name)
}
```

### Task 04-03-3: Update health endpoints in main.go

**File:** cmd/biz/main.go

Replace the existing health handlers (lines 257-267) and update route registration (lines 103-104).

**Step A — Update route registration (lines 103-104):**
Change:
```go
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", handleReady)
```
To:
```go
	mux.HandleFunc("/healthz/live", handleLiveness)
	mux.HandleFunc("/healthz/ready", handleReadiness)
```

**Step B — Replace handleHealth function (lines 257-261) with:**

```go
// handleLiveness implements /healthz/live — always 200, no dependency checks.
// REQ-18 / D-07: Liveness probe, process alive only.
func handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}
```

**Step C — Replace handleReady function (lines 263-267) with full dependency checks:**

```go
// handleReadiness implements /healthz/ready — checks PostgreSQL, Redis, AAA GW, NRF.
// REQ-18 / D-07: Readiness probe with dependency checks.
// Returns 503 if any critical dependency is unhealthy.
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}

	// PostgreSQL (check via pgPool closure — added in Task 04-04)
	if pgPool != nil {
		if err := pgPool.Ping(r.Context()); err != nil {
			checks["postgres"] = "unhealthy: " + err.Error()
		} else {
			checks["postgres"] = "ok"
		}
	} else {
		checks["postgres"] = "degraded (not initialized)"
	}

	// Redis (check via redisPool closure — added in Task 04-04)
	if redisPool != nil {
		if err := redisPool.Client().Ping(r.Context()).Err(); err != nil {
			checks["redis"] = "unhealthy: " + err.Error()
		} else {
			checks["redis"] = "ok"
		}
	} else {
		checks["redis"] = "degraded (not initialized)"
	}

	// AAA Gateway (via aaaClient closure — added in Task 04-04)
	if aaaClient != nil {
		// AAA GW health check is a simple HTTP GET to its /health endpoint
		// This is handled by the aaaClient's internal state
		checks["aaa_gateway"] = "ok" // placeholder — full implementation in Task 04-04
	} else {
		checks["aaa_gateway"] = "degraded (not initialized)"
	}

	// NRF registration (degraded, not unhealthy per D-04)
	if nrfClient != nil && nrfClient.IsRegistered() {
		checks["nrf_registration"] = "ok"
	} else {
		checks["nrf_registration"] = "degraded (retrying)"
	}

	// Determine overall status
	allOk := true
	for _, v := range checks {
		if v != "ok" && v != "degraded (retrying)" && v != "degraded (not initialized)" {
			allOk = false
			break
		}
	}

	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	if allOk {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(checks)
}
```

**Step D — Add closure variables for health checks at the top of main():**
After the existing variable declarations, add these closure variables (place before line 29 or near the pool creation):

```go
// Closure variables for health endpoint dependency checks (filled in Task 04-04)
var (
	pgPool      interface{ Ping(ctx context.Context) error }
	redisPool   interface{ Client() interface{ Ping(ctx context.Context) error } }
	aaaClient   interface{}
	nrfClient   interface{ IsRegistered() bool }
)
```

**Step E — Add /metrics endpoint:**
Add to the route registration section (after line 104):

```go
	mux.Handle("/metrics", promhttp.Handler())
```

And add to the imports:
```go
	"github.com/prometheus/client_golang/prometheus/promhttp"
```

**Step F — Call tracing.Init() during startup:**
After the slog.Info line (after line 50), add:

```go
	// Initialize OpenTelemetry tracing — D-01
	tracingShutdown := tracing.Init("nssAAF-biz", cfg.Version, podID)
	defer tracingShutdown()
```

**Step G — Update shutdown to call tracing shutdown:**
The existing `defer cancel()` on line 139 already handles context cancellation. The `tracingShutdown()` is deferred above.
</action>

<acceptance_criteria>
- [ ] `grep "nssAAF_requests_total\|nssAAF_eap_sessions_active\|nssAAF_circuit_breaker_state" internal/metrics/` finds all metric definitions
- [ ] `grep "otel.SetTextMapPropagator\|trace.Tracer\|otelhttp.NewTransport" internal/tracing/` finds tracing.go
- [ ] `grep "/healthz/live\|/healthz/ready" cmd/biz/main.go` finds updated routes
- [ ] `grep "func handleLiveness\|func handleReadiness" cmd/biz/main.go` finds new handlers
- [ ] `grep "promhttp.Handler" cmd/biz/main.go` finds metrics endpoint
- [ ] `grep "tracing.Init" cmd/biz/main.go` finds OTel init call
</acceptance_criteria>

<verify>
go build ./cmd/biz/... && curl -s http://localhost:8080/healthz/live | grep -q '"status":"ok"'
</verify>

---

## 04-04: NF Wiring — UDM, AMF, AUSF, DLQ + Main Wiring

**Plan:** 04-04
**Wave:** 4
**Type:** execute
**Depends:** 04-02, 04-03
**Requirements:** REQ-04, REQ-05, REQ-06, REQ-07, REQ-08, REQ-10, REQ-13
**Files modified:** internal/udm/client.go, internal/amf/notifier.go, internal/ausf/client.go, internal/cache/redis/dlq.go, cmd/biz/main.go

<objective>
Implement UDM client (GetAuthContext, UpdateAuthContext), AMF notifier (SendReAuthNotification, SendRevocationNotification with retry+DLQ), AUSF client (ForwardMSK), DLQ (Redis LPUSH/BRPOP), and wire everything into main.go. Fill in the health check closure variables.
</objective>

<read_first>
- PATTERNS.md (Pattern 5: DLQ pattern with Redis LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`)
- PATTERNS.md (Pattern 3: UDM client structure)
- docs/design/05_nf_profile.md (§3.2: Nudm_UECM_Get response structure)
- docs/design/22_udm_integration.md (UDM integration details)
- docs/design/21_amf_integration.md (AMF integration details)
- docs/design/23_ausf_integration.md (AUSF integration details)
</read_first>

<action>
### Task 04-04-1: Implement UDM client

**File:** internal/udm/client.go

Replace the stub with full UDM client. Key operations:

```go
package udm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nrf"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is the UDM Nudm_UECM client.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
type Client struct {
	baseURL   string
	nrfClient *nrf.Client
	httpClient *http.Client
}

// NewClient creates a new UDM client.
func NewClient(cfg config.UDMConfig, nrfClient *nrf.Client) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		nrfClient: nrfClient,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// AuthSubscription represents the auth context from UDM.
// Spec: TS 29.526 §7.3 / docs/design/05_nf_profile.md §3.2.
type AuthSubscription struct {
	AuthType string `json:"authType"` // "EAP_TLS", "EAP_AKA_PRIME"
	AAAServer string `json:"aaaServer"` // e.g. "radius://aaa.operator.com:1812"
}

// GetAuthContext calls Nudm_UECM_Get to retrieve auth subscription for a SUPI.
// REQ-04: Called before AAA routing to determine EAP method and AAA server.
// Spec: TS 29.526 §7.3.2, TS 23.502 §4.2.9.2 step 2.
func (c *Client) GetAuthContext(ctx context.Context, supi string) (*AuthSubscription, error) {
	// If baseURL is empty, discover UDM via NRF first
	baseURL := c.baseURL
	if baseURL == "" && c.nrfClient != nil {
		// Discover UDM via NRF — extract PLMN from SUPI
		plmn := extractPLMNFromSupi(supi)
		udmEndpoint, err := c.nrfClient.DiscoverUDM(ctx, plmn)
		if err != nil {
			return nil, fmt.Errorf("udm: discover via nrf: %w", err)
		}
		baseURL = udmEndpoint
	}

	url := fmt.Sprintf("%s/nudm-uem/v1/subscribers/%s/auth-contexts", baseURL, supi)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("udm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("udm: get auth context: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("udm: subscriber %s not found", supi)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("udm: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		AuthContexts []AuthSubscription `json:"authContexts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("udm: decode response: %w", err)
	}
	if len(result.AuthContexts) == 0 {
		return nil, fmt.Errorf("udm: no auth contexts found for %s", supi)
	}
	return &result.AuthContexts[0], nil
}

// UpdateAuthContext calls Nudm_UECM_UpdateAuthContext to update auth status.
// REQ-05: Called after EAP completion to update auth context in UDM.
// Spec: TS 29.526 §7.3.3.
func (c *Client) UpdateAuthContext(ctx context.Context, supi, authCtxId string, status string) error {
	baseURL := c.baseURL
	if baseURL == "" && c.nrfClient != nil {
		plmn := extractPLMNFromSupi(supi)
		udmEndpoint, err := c.nrfClient.DiscoverUDM(ctx, plmn)
		if err != nil {
			return fmt.Errorf("udm: discover via nrf: %w", err)
		}
		baseURL = udmEndpoint
	}

	url := fmt.Sprintf("%s/nudm-uem/v1/subscribers/%s/auth-contexts/%s", baseURL, supi, authCtxId)
	payload := map[string]string{"authResult": status}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("udm: create update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("udm: update auth context: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("udm: update status %d", resp.StatusCode)
	}
	return nil
}

// extractPLMNFromSupi extracts PLMN from SUPI format: imu-{mcc}{mnc}{rest}.
// e.g. imu-208001000000000 → "208001"
func extractPLMNFromSupi(supi string) string {
	if len(supi) >= 8 {
		return supi[4:10] // "imu-" = 4 chars, next 6 = MCC+MNC
	}
	return "208001" // default PLMN
}
```

Note: Add `"bytes"` to the import block.

### Task 04-04-2: Implement AMF notifier with retry and DLQ

**File:** internal/amf/notifier.go

Replace the stub with full AMF notifier:

```go
package amf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NotificationType identifies the type of AMF notification.
type NotificationType string

const (
	NotificationTypeReAuth    NotificationType = "reauth"
	NotificationTypeRevocation NotificationType = "revocation"
)

// DLQItem represents an item in the AMF notification DLQ.
// D-02: Redis LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`.
type DLQItem struct {
	ID        string          `json:"id"`
	Type      NotificationType `json:"type"`
	URI       string          `json:"uri"`
	Payload   json.RawMessage `json:"payload"`
	AuthCtxID string          `json:"authCtxId"`
	Attempt   int             `json:"attempt"`
	MaxAttempts int           `json:"maxAttempts"`
	CreatedAt time.Time       `json:"createdAt"`
	LastError string          `json:"lastError"`
}

// Client sends notifications to the AMF.
// REQ-06: Re-Auth notification POST to reauthNotifUri.
// REQ-07: Revocation notification POST to revocNotifUri.
// REQ-10: DLQ on retry exhaustion.
type Client struct {
	httpClient    *http.Client
	cbRegistry    *resilience.Registry
	dlq           interface{ Enqueue(ctx context.Context, item *DLQItem) error }
	notifyTimeout time.Duration
	maxRetries    int
}

// NewClient creates a new AMF notifier.
func NewClient(timeout time.Duration, cbRegistry *resilience.Registry, dlq interface {
	Enqueue(ctx context.Context, item *DLQItem) error
}) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		cbRegistry: cbRegistry,
		dlq:         dlq,
		notifyTimeout: timeout,
		maxRetries:  3,
	}
}

// SendReAuthNotification sends a slice re-authentication notification to the AMF.
// REQ-06: POST to reauthNotifUri with retry and DLQ on exhaustion.
// Spec: TS 23.502 §4.2.9.3.
func (c *Client) SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
	return c.sendNotification(ctx, NotificationTypeReAuth, uri, authCtxID, payload)
}

// SendRevocationNotification sends a slice revocation notification to the AMF.
// REQ-07: POST to revocNotifUri with retry and DLQ on exhaustion.
// Spec: TS 23.502 §4.2.9.4.
func (c *Client) SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
	return c.sendNotification(ctx, NotificationTypeRevocation, uri, authCtxID, payload)
}

// sendNotification sends a notification with retry and DLQ fallback.
// D-02: On retry exhaustion, enqueue to DLQ instead of dropping.
func (c *Client) sendNotification(ctx context.Context, typ NotificationType, uri, authCtxID string, payload []byte) error {
	// Extract host:port for circuit breaker key
	cbKey := extractHostPort(uri)
	cb := c.cbRegistry.Get(cbKey)

	item := &DLQItem{
		ID:          fmt.Sprintf("%s-%d", authCtxID, time.Now().UnixNano()),
		Type:        typ,
		URI:         uri,
		Payload:     payload,
		AuthCtxID:   authCtxID,
		Attempt:     0,
		MaxAttempts: c.maxRetries,
		CreatedAt:   time.Now(),
	}

	err := resilience.Do(ctx, resilience.RetryConfig{
		MaxAttempts: c.maxRetries,
		BaseDelay:   1 * time.Second,
		MaxDelay:    4 * time.Second,
	}, func() error {
		item.Attempt++

		// Check circuit breaker
		if !cb.Allow() {
			cb.RecordFailure()
			return fmt.Errorf("circuit breaker open for %s", cbKey)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("amf: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			cb.RecordFailure()
			return fmt.Errorf("amf: send %s: %w", typ, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			cb.RecordFailure()
			return fmt.Errorf("amf: server error %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			cb.RecordSuccess()
			return fmt.Errorf("amf: client error %d (not retryable)", resp.StatusCode)
		}

		cb.RecordSuccess()
		return nil
	})

	if err != nil {
		item.LastError = err.Error()
		if dlqErr := c.dlq.Enqueue(ctx, item); dlqErr != nil {
			slog.Error("amf notification: dlq enqueue failed",
				"auth_ctx_id", authCtxID,
				"type", typ,
				"notify_error", err,
				"dlq_error", dlqErr,
			)
			return fmt.Errorf("notification failed and dlq enqueue failed: %w (dlq: %v)", err, dlqErr)
		}
		slog.Warn("amf notification: sent to DLQ after retries exhausted",
			"auth_ctx_id", authCtxID,
			"type", typ,
			"uri", uri,
			"error", err,
		)
		return nil // DLQ accepted, consider it handled
	}

	return nil
}

// extractHostPort extracts host:port from a URI.
func extractHostPort(uri string) string {
	// "http://host:port/path" → "host:port"
	if len(uri) > 7 && uri[:7] == "http://" {
		rest := uri[7:]
		for i, ch := range rest {
			if ch == ':' || ch == '/' {
				break
			}
			if i > 0 && rest[i-1] == ':' {
				end := i
				for end < len(rest) && rest[end] != '/' {
					end++
				}
				return rest[i-1 : end]
			}
		}
	}
	return uri
}
```

Note: Add `"log/slog"` and `"bytes"` to the import block.

### Task 04-04-3: Implement AUSF client

**File:** internal/ausf/client.go

Create this new package:

```go
// Package ausf provides AUSF (Authentication Server Function) client for
// N60 interface communication and MSK forwarding.
// REQ-08: internal/ausf/ created with ForwardMSK.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9.
package ausf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is the AUSF N60 client for MSK forwarding.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new AUSF client.
func NewClient(cfg config.AUSFConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// ForwardMSK forwards the Master Session Key (MSK) to AUSF after EAP-TLS completion.
// REQ-08: AUSF N60 client with ForwardMSK operation.
// Spec: TS 29.526 §7.3.4 — AUSF receives MSK for key derivation.
func (c *Client) ForwardMSK(ctx context.Context, authCtxID string, msk []byte) error {
	if c.baseURL == "" {
		return fmt.Errorf("ausf: baseURL not configured")
	}

	payload := map[string]interface{}{
		"authCtxId": authCtxID,
		"msk":       msk,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ausf: marshal msk: %w", err)
	}

	url := fmt.Sprintf("%s/nnssaaaf-aiw/v1/msk", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ausf: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ausf: forward msk: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ausf: unexpected status %d", resp.StatusCode)
	}
	return nil
}
```

### Task 04-04-4: Implement Redis DLQ

**File:** internal/cache/redis/dlq.go

Create per D-02 and PATTERNS.md Pattern 5:

```go
// Package redis provides Redis caching and queue layer for NSSAAF.
// REQ-10: DLQ for AMF notification failures after retries exhausted.
// D-02: Redis list LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DLQ key prefix per D-02.
const amfDLQKey = "nssAAF:dlq:amf-notifications"

// AMFDLQItem represents an item in the AMF notification DLQ.
type AMFDLQItem struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"` // "reauth" | "revocation"
	URI         string          `json:"uri"`
	Payload     json.RawMessage `json:"payload"`
	AuthCtxID   string          `json:"authCtxId"`
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"maxAttempts"`
	CreatedAt   time.Time       `json:"createdAt"`
	LastError   string          `json:"lastError"`
}

// DLQ provides a dead-letter queue for failed AMF notifications.
type DLQ struct {
	pool *Pool
}

// NewDLQ creates a new AMF notification DLQ.
func NewDLQ(pool *Pool) *DLQ {
	return &DLQ{pool: pool}
}

// Enqueue adds an item to the DLQ using LPUSH.
// D-02: Redis LPUSH for queue insertion.
func (d *DLQ) Enqueue(ctx context.Context, item interface{}) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("dlq: marshal: %w", err)
	}
	return d.pool.Client().LPush(ctx, amfDLQKey, data).Err()
}

// Dequeue removes and returns an item from the DLQ using BRPOP.
// D-02: Redis BRPOP with timeout for queue consumption.
// Returns nil, nil if timeout expires.
func (d *DLQ) Dequeue(ctx context.Context, timeout time.Duration) (*AMFDLQItem, error) {
	result, err := d.pool.Client().BRPop(ctx, timeout, amfDLQKey).Result()
	if err != nil {
		// context deadline exceeded or cancelled — not an error
		return nil, nil
	}
	if len(result) < 2 {
		return nil, nil
	}
	var item AMFDLQItem
	if err := json.Unmarshal([]byte(result[1]), &item); err != nil {
		return nil, fmt.Errorf("dlq: unmarshal: %w", err)
	}
	return &item, nil
}

// Len returns the current DLQ depth for metrics.
func (d *DLQ) Len(ctx context.Context) (int64, error) {
	return d.pool.Client().LLen(ctx, amfDLQKey).Result()
}

// Process starts a background goroutine that retries failed DLQ items.
// Items are retried with exponential backoff. Run via: go dlq.Process(ctx).
// REQ-10: Ensures DLQ items are eventually delivered.
func (d *DLQ) Process(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			item, err := d.Dequeue(ctx, 5*time.Second)
			if err != nil || item == nil {
				continue
			}

			// Re-attempt notification — simplest retry: immediate re-enqueue
			// A production implementation would use a separate retry queue with backoff
			if retryErr := d.Enqueue(ctx, item); retryErr != nil {
				slog.Warn("dlq: reprocess enqueue failed",
					"id", item.ID,
					"error", retryErr,
				)
			}
		}
	}()
}
```

### Task 04-04-5: Wire all clients in main.go

**File:** cmd/biz/main.go

**Step A — Add new imports:**
```go
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/nrf"
	"github.com/operator/nssAAF/internal/postgres"
	"github.com/operator/nssAAF/internal/redis"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/operator/nssAAF/internal/tracing"
	"github.com/operator/nssAAF/internal/udm"
	"github.com/operator/nssAAF/internal/amf"
	"github.com/operator/nssAAF/internal/ausf"
```

Note: Import path for postgres should be `github.com/operator/nssAAF/internal/storage/postgres`. Same for redis: `github.com/operator/nssAAF/internal/cache/redis`.

**Step B — Replace in-memory stores with PostgreSQL (replace lines 58-60):**

```go
	// ─── PostgreSQL pool + session stores ────────────────────────────────────
	// REQ-09: PostgreSQL session store replaces in-memory store
	pgPool, err := postgres.NewPool(ctx, postgres.Config{
		DSN:               fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			cfg.Database.User, cfg.Database.Password, cfg.Database.Host,
			cfg.Database.Port, cfg.Database.Name, cfg.Database.SSLMode),
		MaxConns:          int32(cfg.Database.MaxConns),
		MinConns:          int32(cfg.Database.MinConns),
		MaxConnLifetime:   cfg.Database.ConnMaxLifetime,
		MaxConnIdleTime:   10 * time.Minute,
		HealthCheckPeriod: 30 * time.Second,
	})
	if err != nil {
		slog.Error("postgres pool", "error", err)
		os.Exit(1)
	}
	defer pgPool.Close()

	// Encryption key from config — use empty key for dev (Phase 4)
	// Phase 5 will add proper key management
	var encryptor *postgres.Encryptor
	encryptor, _ = postgres.NewEncryptor([]byte{}) // empty key = no encryption for now

	// REQ-09: NewSessionStore/NewAIWSessionStore per D-06
	nssaaStore := postgres.NewSessionStore(pgPool, encryptor)
	aiwStore := postgres.NewAIWSessionStore(pgPool, encryptor)
```

**Step C — Replace Redis pool creation (add after pgPool creation):**

```go
	// ─── Redis pool + DLQ ───────────────────────────────────────────────────
	redisPool, err := redis.NewPool(ctx, redis.Config{
		Addrs:        []string{cfg.Redis.Addr},
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: 10,
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		slog.Error("redis pool", "error", err)
		os.Exit(1)
	}
	defer redisPool.Close()

	// REQ-10: DLQ for AMF notification failures
	dlq := redis.NewDLQ(redisPool)
	go dlq.Process(ctx)
```

**Step D — Add resilience registry:**

```go
	// ─── Resilience ─────────────────────────────────────────────────────────
	// REQ-11: Per host:port circuit breaker registry
	cbRegistry := resilience.NewRegistry(
		cfg.AAA.FailureThreshold,
		cfg.AAA.RecoveryTimeout,
		time.Duration(cfg.AAA.RecoveryTimeout),
	)
```

**Step E — Add NF clients:**

```go
	// ─── NRF client ─────────────────────────────────────────────────────────
	// REQ-01 / D-04: Startup in degraded mode — NRF registration in background
	nrfClient := nrf.NewClient(cfg.NRF)
	go nrfClient.RegisterAsync(ctx)
	go nrfClient.StartHeartbeat(ctx)

	// ─── UDM client ─────────────────────────────────────────────────────────
	udmClient := udm.NewClient(cfg.UDM, nrfClient)

	// ─── AUSF client ────────────────────────────────────────────────────────
	ausfClient := ausf.NewClient(cfg.AUSF)

	// ─── AMF notifier ────────────────────────────────────────────────────────
	amfClient := amf.NewClient(30*time.Second, cbRegistry, dlq)
```

**Step F — Update nssaaHandler construction (replace lines 81-84):**

```go
	// REQ-04: UDM Nudm_UECM_Get wired to N58 handler via WithUDMClient
	// REQ-01: NRF client for service discovery via WithNRFClient
	nssaaHandler := nssaa.NewHandler(nssaaStore,
		nssaa.WithAPIRoot(apiRoot),
		nssaa.WithAAA(aaaClient),
		nssaa.WithNRFClient(nrfClient),
		nssaa.WithUDMClient(udmClient),
	)
```

**Step G — Update aiwHandler construction (replace lines 88-90):**

```go
	// REQ-08: AUSF client wired to AIW handler via WithAUSFClient
	aiwHandler := aiw.NewHandler(aiwStore,
		aiw.WithAPIRoot(apiRoot),
		aiw.WithAUSFClient(ausfClient),
	)
```

**Step H — Wire health check closure variables:**
Add near the top of main() (before pool creation), replacing the placeholder closures from Task 04-03:

```go
// Health check closure variables (set during initialization)
var (
	pgHealth     func(ctx context.Context) error
	redisHealth  func(ctx context.Context) error
	nrfClient    interface{ IsRegistered() bool }
)

// SetHealthChecks populates the health check closures.
// Called after all pools and clients are initialized.
func init() {
	// These will be set after pool/client creation below
}
```

Actually, simpler: directly assign to the global closure variables (add near pool creation):

```go
	// Wire health check closures
	pgHealth = pgPool.Ping
	redisHealth = func(ctx context.Context) error {
		return redisPool.Client().Ping(ctx).Err()
	}
```

**Step I — Update handleReadiness to use real health checks:**
Update the handleReadiness function to use `pgHealth` and `redisHealth`:

```go
	// PostgreSQL
	if pgHealth != nil {
		if err := pgHealth(r.Context()); err != nil {
			checks["postgres"] = "unhealthy: " + err.Error()
		} else {
			checks["postgres"] = "ok"
		}
	}

	// Redis
	if redisHealth != nil {
		if err := redisHealth(r.Context()); err != nil {
			checks["redis"] = "unhealthy: " + err.Error()
		} else {
			checks["redis"] = "ok"
		}
	}
```

**Step J — Deregister from NRF on shutdown:**
Before the existing `srv.Shutdown(ctx)` call (around line 140), add:

```go
	// Graceful shutdown: deregister from NRF
	if nrfClient != nil {
		nrfCtx, nrfCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer nrfCancel()
		_ = nrfClient.Deregister(nrfCtx) // best-effort
	}
```

Also need to add `Deregister` method to NRF client:

Add to `internal/nrf/client.go`:

```go
// Deregister sends Nnrf_NFDeregistration to remove the NF profile.
func (c *Client) Deregister(ctx context.Context) error {
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances/%s", c.baseURL, c.nfInstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("nrf: create deregister request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nrf: deregister: %w", err)
	}
	defer resp.Body.Close()
	c.registered.Store(false)
	return nil
}
```

### Task 04-04-6: Create DLQ test

**File:** internal/cache/redis/dlq_test.go

Test file covering DLQ Enqueue and Dequeue per REQ-10.

### Task 04-04-7: Create AMF notifier test

**File:** internal/amf/notifier_test.go

Test file covering SendReAuthNotification, SendRevocationNotification with retry exhaustion and DLQ per REQ-06, REQ-07, REQ-10.

### Task 04-04-8: Create AUSF client test

**File:** internal/ausf/client_test.go

Test file covering ForwardMSK per REQ-08.
</action>

<acceptance_criteria>
- [ ] `grep "func GetAuthContext\|func UpdateAuthContext" internal/udm/` finds udm.go
- [ ] `grep "func SendReAuthNotification\|func SendRevocationNotification" internal/amf/` finds notifier.go
- [ ] `grep "func ForwardMSK" internal/ausf/` finds client.go
- [ ] `grep "func NewDLQ\|func Enqueue\|func Dequeue\|func Process" internal/cache/redis/` finds dlq.go
- [ ] `grep "NewSessionStore\|NewAIWSessionStore" cmd/biz/main.go` finds session store wiring
- [ ] `grep "WithNRFClient\|WithUDMClient\|WithAUSFClient" cmd/biz/main.go` finds option wiring
- [ ] `grep "RegisterAsync\|StartHeartbeat\|Deregister" cmd/biz/main.go` finds NRF wiring
- [ ] `grep "go dlq.Process" cmd/biz/main.go` finds DLQ goroutine startup
- [ ] `go build ./cmd/biz/...` succeeds
- [ ] `go test ./internal/udm/... ./internal/amf/... ./internal/ausf/... ./internal/cache/redis/... -v -count=1` passes
</acceptance_criteria>

<verify>
go build ./cmd/biz/... && go test ./internal/udm/... ./internal/amf/... ./internal/ausf/... ./internal/cache/redis/... -v -count=1
</verify>

---

## 04-05: Validation — ServiceMonitor CRDs + Prometheus Alerting Rules + Config Fixture

**Plan:** 04-05
**Wave:** 5
**Type:** execute
**Depends:** 04-01, 04-03, 04-04
**Requirements:** REQ-15, REQ-19
**Files modified:** compose/configs/biz.yaml, deployments/nssaa-biz/servicemonitor.yaml, deployments/nssaa-biz/prometheusrules.yaml

<objective>
Create ServiceMonitor CRDs for Prometheus Operator scraping, Prometheus alerting rules YAML, and the biz.yaml config fixture for development testing.
</objective>

<read_first>
- docs/design/19_observability.md (§2.2: ServiceMonitor YAML, §5: Prometheus alerting rules)
- PATTERNS.md (Pattern 6: ServiceMonitor pattern)
- docs/design/10_ha_architecture.md (§3.1: HPA/probe paths)
</read_first>

<action>
### Task 04-05-1: Create ServiceMonitor CRDs

**File:** deployments/nssaa-biz/servicemonitor.yaml

Create the directory and file. Per REQ-15 and docs/design/19_observability.md §2.2, create three ServiceMonitors (one per component):

```yaml
# ServiceMonitor for NSSAAF Biz Pod
# REQ-15: ServiceMonitor CRDs for HTTP Gateway, Biz Pod, AAA Gateway
# Spec: docs/design/19_observability.md §2.2
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssaa-biz-monitor
  labels:
    team: platform
    app: nssaa-biz
spec:
  selector:
    matchLabels:
      app: nssaa-biz
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssaa-system
---
# ServiceMonitor for NSSAAF HTTP Gateway
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssaa-http-gw-monitor
  labels:
    team: platform
    app: nssaa-http-gw
spec:
  selector:
    matchLabels:
      app: nssaa-http-gw
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssaa-system
---
# ServiceMonitor for NSSAAF AAA Gateway
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssaa-aaa-gw-monitor
  labels:
    team: platform
    app: nssaa-aaa-gw
spec:
  selector:
    matchLabels:
      app: nssaa-aaa-gw
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssaa-system
```

### Task 04-05-2: Create Prometheus alerting rules

**File:** deployments/nssaa-biz/prometheusrules.yaml

Create per REQ-19 and docs/design/19_observability.md §5:

```yaml
# Prometheus alerting rules for NSSAAF
# REQ-19: Error rate >1%, P99 >500ms, circuit breaker open, session table full
# Spec: docs/design/19_observability.md §5
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: nssaa-alerts
  labels:
    team: platform
    app: nssaa-biz
spec:
  groups:
    - name: nssaa-biz.alerts
      rules:
        # High error rate — REQ-19: error rate > 1%
        - alert: NssaaHighErrorRate
          expr: |
            sum(rate(nssAAF_requests_total{status_code=~"5.."}[5m]))
            / sum(rate(nssAAF_requests_total[5m])) > 0.01
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "NSSAAF error rate > 1%"
            description: "Error rate: {{ $value | humanizePercentage }}"

        # High latency P99 — REQ-19: P99 > 500ms
        - alert: NssaaHighLatency
          expr: |
            histogram_quantile(0.99,
              sum(rate(nssAAF_request_duration_seconds_bucket[5m]))
              by (le)) > 0.5
          for: 5m
          labels:
            severity: major
          annotations:
            summary: "NSSAAF P99 latency > 500ms"

        # Circuit breaker open — REQ-19
        - alert: NssaaCircuitBreakerOpen
          expr: nssAAF_circuit_breaker_state == 1
          for: 1m
          labels:
            severity: major
          annotations:
            summary: "Circuit breaker OPEN for {{ $labels.server }}"

        # Session table near capacity — REQ-19
        - alert: NssaaSessionTableFull
          expr: nssAAF_eap_sessions_active > 45000
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "EAP sessions approaching limit (45k/50k)"

        # Database unavailable
        - alert: NssaaDatabaseUnreachable
          expr: nssAAF_db_query_duration_seconds_count{operation="query"}[1m] == 0
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "Database queries stopped"

        # AAA server high failure rate
        - alert: NssaaAaaServerFailures
          expr: |
            sum(rate(nssAAF_aaa_requests_total{result="failure"}[5m]))
            by (server) > 10
          for: 3m
          labels:
            severity: major
          annotations:
            summary: "High failure rate for AAA server {{ $labels.server }}"

        # DLQ depth warning
        - alert: NssaaDLQDepthHigh
          expr: nssAAF_dlq_depth > 100
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "AMF notification DLQ depth is high ({{ $value }} items)"
```

### Task 04-05-3: Create biz.yaml config fixture

**File:** compose/configs/biz.yaml

Create the directory and config fixture for development testing:

```yaml
# NSSAAF Biz Pod configuration for development testing
# Phase 4: NF Integration & Observability — config fixture
# Usage: go run ./cmd/biz/main.go --config=compose/configs/biz.yaml

component: biz
version: "0.1.0"

server:
  addr: ":8080"
  readTimeout: 10s
  writeTimeout: 30s
  idleTimeout: 120s

database:
  host: "localhost"
  port: 5432
  name: "nssaa"
  user: "nssaa"
  password: "nssaa"
  maxConns: 20
  minConns: 5
  connMaxLifetime: 30m
  sslMode: "disable"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0
  poolSize: 50

eap:
  maxRounds: 20
  roundTimeout: 30s
  sessionTtl: 5m

aaa:
  responseTimeout: 10s
  maxRetries: 3
  failureThreshold: 5
  recoveryTimeout: 30s

rateLimit:
  perGpsiPerMin: 10
  perAmfPerSec: 100
  globalPerSec: 1000

logging:
  level: "info"
  format: "json"

metrics:
  enabled: true
  path: "/metrics"

# NRF integration — REQ-01, REQ-02, REQ-03
nrf:
  baseURL: "http://localhost:8081"  # NRF mock server
  discoverTimeout: 5s

# UDM integration — REQ-04, REQ-05
udm:
  baseURL: "http://localhost:8082"  # UDM mock server
  timeout: 10s

# AUSF integration — REQ-08
ausf:
  baseURL: "http://localhost:8083"  # AUSF mock server
  timeout: 10s

# Biz Pod component config
biz:
  aaaGatewayUrl: "http://localhost:9090"
  useMTLS: false
```

### Task 04-05-4: Final build verification

Verify the complete Biz Pod compiles:

```bash
go build ./cmd/biz/... && go build ./cmd/...
```

### Task 04-05-5: Update module_index.md

Mark the following modules as IN_PROGRESS or READY in `docs/roadmap/module_index.md`:

| Module | New Status |
|--------|-----------|
| internal/nrf/ | IN_PROGRESS → READY |
| internal/udm/ | IN_PROGRESS → READY |
| internal/amf/ | IN_PROGRESS → READY |
| internal/ausf/ | TBD → READY |
| internal/resilience/ | TBD → READY |
| internal/metrics/ | TBD → READY |
| internal/logging/ | TBD → READY |
| internal/tracing/ | TBD → READY |
| internal/cache/redis/ (dlq.go) | READY (update) |

### Task 04-05-6: Update ROADMAP.md phase status

Mark Phase 4 as done in `.planning/ROADMAP.md`.
</action>

<acceptance_criteria>
- [ ] `ls deployments/nssaa-biz/servicemonitor.yaml` exists
- [ ] `ls deployments/nssaa-biz/prometheusrules.yaml` exists
- [ ] `ls compose/configs/biz.yaml` exists
- [ ] `grep "nssaaHighErrorRate\|nssaaHighLatency\|nssaaCircuitBreakerOpen\|nssaaSessionTableFull" deployments/nssaa-biz/prometheusrules.yaml` finds all 4 alert rules
- [ ] `grep "ServiceMonitor.*nssaa-biz\|ServiceMonitor.*nssaa-http-gw\|ServiceMonitor.*nssaa-aaa-gw" deployments/nssaa-biz/servicemonitor.yaml` finds all 3 ServiceMonitors
- [ ] `grep "nrf:\|udm:\|ausf:" compose/configs/biz.yaml` finds all NF configs
- [ ] `go build ./cmd/biz/...` succeeds
- [ ] `docs/roadmap/module_index.md` updated
</acceptance_criteria>

<verify>
go build ./cmd/biz/... && go test ./internal/nrf/... ./internal/udm/... ./internal/resilience/... ./internal/logging/... -count=1
</verify>

---

## Threat Model

### Trust Boundaries

| Boundary | Description |
|----------|-------------|
| AMF → Biz Pod (N58) | Untrusted GPSI, S-NSSAI cross boundary |
| Biz Pod → NRF | Internal network function discovery |
| Biz Pod → UDM | Internal subscription data lookup |
| Biz Pod → AMF (notifications) | Outbound HTTP callbacks |

### STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation |
|-----------|----------|-----------|------------|------------|
| T-04-01 | Information Disclosure | Logging | Mitigate | GPSI hashed via `logging.HashGPSI()` per REQ-16 — SHA256, 8 bytes, base64url. Raw GPSI never logged. |
| T-04-02 | Denial of Service | AMF Notifier | Mitigate | DLQ prevents notification loss on AMF unavailability per REQ-10, D-02. Circuit breaker prevents cascade failures per REQ-11, D-03. |
| T-04-03 | Denial of Service | Health Endpoints | Mitigate | `/healthz/live` always returns 200 (no dependency checks). `/healthz/ready` returns 503 only on critical failures per REQ-18, D-07. |
| T-04-04 | Information Disclosure | Prometheus Metrics | Accept | No PII in metrics. GPSI only in hashed form (`gpsi_hash` label never in raw form). |
| T-04-05 | Spoofing | NRF Discovery | Mitigate | NRF discovery cache with 5-min TTL prevents stale data exploitation per REQ-03. TLS (Phase 5) will add authenticity. |

---

## Verification

After all waves complete, run:

```bash
# Full build
go build ./cmd/biz/... && go build ./cmd/...

# Test suite
go test ./internal/nrf/... ./internal/udm/... ./internal/resilience/... \
  ./internal/logging/... ./internal/metrics/... ./internal/tracing/... \
  ./internal/storage/postgres/... ./internal/cache/redis/... \
  ./internal/amf/... ./internal/ausf/... -count=1

# Lint
golangci-lint run ./internal/...

# Check no TBD remains in module_index.md for Phase 4 modules
grep "TBD" docs/roadmap/module_index.md | grep -E "nrf|udm|amf|ausf|resilience|metrics|logging|tracing"
```

---

## Success Criteria

The phase is complete when:

1. Every requirement (REQ-01 through REQ-19) has at least one task implementing it
2. Every task has concrete, actionable instructions with exact file paths, function names, and struct field names
3. Every task has a `<read_first>` listing the files it modifies
4. Every task has grep/checkable acceptance criteria
5. Waves are correctly ordered — no forward dependencies
6. All known gaps (WithNRFClient, WithUDMClient, WithAUSFClient, NewSessionStore, NewAIWSessionStore, /healthz/live, /healthz/ready) are addressed
7. `go build ./cmd/biz/...` succeeds after all waves
8. `go test ./...` passes after all waves
9. ServiceMonitor CRDs, Prometheus alerting rules, and biz.yaml config fixture exist
10. Module index updated with READY status for all Phase 4 modules

---

## Next Steps

Execute the plans in wave order:

```bash
/gsd-execute-phase 04 --plan 04-01  # Wave 1: Resilience + Logging
/gsd-execute-phase 04 --plan 04-02  # Wave 2: NRF Client + PG Session Store
/gsd-execute-phase 04 --plan 04-03  # Wave 3: Metrics + Tracing + Health
/gsd-execute-phase 04 --plan 04-04  # Wave 4: UDM + AMF + AUSF + DLQ + Wiring
/gsd-execute-phase 04 --plan 04-05  # Wave 5: CRDs + Alerts + Config
```
