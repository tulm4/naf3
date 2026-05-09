# Phase 7.1: Internal Communication Dual-Mode

## Overview

Implements dual-mode internal communication (Native/Istio) for the 3-component NSSAAF architecture, fixing critical resilience gaps (G1-G5) identified in the architecture review.

**Design Doc:** `docs/design/26_internal_comm_dual_mode.md`

---

## Prerequisites (Already Done)

| Dependency | Status | Notes |
|-----------|--------|-------|
| `internal/resilience/` | ✅ READY | `CircuitBreaker`, `Registry`, `RetryConfig`, `Do()` exist |
| `internal/proto/` | ✅ READY | `BizServiceClient`, `BizAAAClient` interfaces exist |

---

## Scope

### In Scope

1. HTTP client factory with mode detection
2. Native mode: wire existing `resilience` package into HTTP clients
3. Istio mode: delegation to service mesh
4. Config schema for dual-mode (embed existing `resilience.RetryConfig`)
5. Metrics for internal communication
6. Kubernetes manifests for both modes

### Out of Scope

- Redis Sentinel (exists in `internal/cache/redis/`, just needs config wiring)
- gRPC migration (future work)
- Performance testing (Phase 8)

---

## Wave 1: Config Schema

### Task 1.1: Add InternalCommConfig to main config

**File:** `internal/config/config.go`

Add to the `Config` struct and create `InternalCommConfig` types.

```go
// InternalCommConfig holds configuration for internal component communication.
type InternalCommConfig struct {
    // Mode selects the communication mode: "native" or "istio"
    // Default: "native"
    // Can be overridden by ISTIO_MTLS=1 env var
    Mode string `yaml:"mode"`

    // Native holds settings for Go native HTTP client (default)
    Native NativeCommConfig `yaml:"native"`

    // Istio holds settings for Istio service mesh mode
    Istio IstioCommConfig `yaml:"istio"`
}

// NativeCommConfig for Go native HTTP client.
type NativeCommConfig struct {
    // Retry uses the shared RetryConfig from resilience package
    Retry resilience.RetryConfig `yaml:"retry"`
    // CB configures per-destination circuit breaking
    CB CircuitBreakerConfig `yaml:"circuitBreaker"`
    // Pool configures http.Transport connection pool
    Pool ConnectionPoolConfig `yaml:"connectionPool"`
}

// CircuitBreakerConfig mirrors resilience.CircuitBreaker defaults.
type CircuitBreakerConfig struct {
    FailureThreshold int           `yaml:"failureThreshold"`
    RecoveryTimeout time.Duration `yaml:"recoveryTimeout"`
    SuccessThreshold int         `yaml:"successThreshold"`
}

// ConnectionPoolConfig for http.Transport tuning.
type ConnectionPoolConfig struct {
    MaxIdleConns        int           `yaml:"maxIdleConns"`
    MaxIdleConnsPerHost int           `yaml:"maxIdleConnsPerHost"`
    IdleConnTimeout     time.Duration `yaml:"idleConnTimeout"`
    DialTimeout         time.Duration `yaml:"dialTimeout"`
}

// IstioCommConfig for Istio service mesh mode.
type IstioCommConfig struct {
    // TrustDomain specifies the Istio trust domain (default: "cluster.local")
    TrustDomain string `yaml:"trustDomain"`
}
```

**Validation:**
- `go build ./internal/config/...` compiles
- Unit test verifies default values applied

---

## Wave 2: HTTP Client Package

### Task 2.1: Create httpclient package

**File:** `internal/httpclient/factory.go`

```go
package httpclient

import (
    "net/http"
    "os"

    "github.com/5gcore/nssAAF/internal/config"
    "github.com/5gcore/nssAAF/internal/proto"
    "github.com/5gcore/nssAAF/internal/resilience"
)

// Mode determines which resilience strategy to use.
type Mode string

const (
    ModeNative Mode = "native"
    ModeIstio  Mode = "istio"
)

// Factory creates HTTP clients based on mode.
type Factory struct {
    mode Mode
    cfg  config.InternalCommConfig
}

// NewFactory creates a new HTTP client factory.
// Mode is determined by:
// 1. cfg.Mode ("native" or "istio")
// 2. ISTIO_MTLS=1 env var (overrides config)
func NewFactory(cfg config.InternalCommConfig) *Factory {
    mode := ModeNative
    if cfg.Mode == "istio" || os.Getenv("ISTIO_MTLS") == "1" {
        mode = ModeIstio
    }
    return &Factory{mode: mode, cfg: cfg}
}

// Mode returns the active communication mode.
func (f *Factory) Mode() Mode {
    return f.mode
}

// NewBizServiceClient creates a BizServiceClient for HTTP GW -> Biz Pod.
func (f *Factory) NewBizServiceClient(bizServiceURL string) proto.BizServiceClient {
    switch f.mode {
    case ModeIstio:
        return newIstioBizClient(bizServiceURL)
    default:
        return newNativeBizClient(bizServiceURL, f.cfg.Native)
    }
}

// NewAAAClient creates an AAA client for Biz Pod -> AAA GW.
func (f *Factory) NewAAAClient(aaaGatewayURL string) proto.BizAAAClient {
    switch f.mode {
    case ModeIstio:
        return newIstioAAAClient(aaaGatewayURL)
    default:
        return newNativeAAAClient(aaaGatewayURL, f.cfg.Native)
    }
}
```

---

### Task 2.2: Native Biz Client

**File:** `internal/httpclient/native_biz.go`

```go
package httpclient

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/5gcore/nssAAF/internal/config"
    "github.com/5gcore/nssAAF/internal/resilience"
)

type nativeBizClient struct {
    baseURL    string
    httpClient *http.Client
    cbRegistry *resilience.Registry
    retryCfg   resilience.RetryConfig
}

func newNativeBizClient(baseURL string, cfg config.NativeCommConfig) *nativeBizClient {
    retryCfg := cfg.Retry
    if retryCfg.MaxAttempts == 0 {
        retryCfg = resilience.DefaultRetryConfig
    }

    cbCfg := cfg.CB
    if cbCfg.FailureThreshold == 0 {
        cbCfg.FailureThreshold = 5
    }
    if cbCfg.RecoveryTimeout == 0 {
        cbCfg.RecoveryTimeout = 30 * time.Second
    }
    if cbCfg.SuccessThreshold == 0 {
        cbCfg.SuccessThreshold = 3
    }

    poolCfg := cfg.Pool
    if poolCfg.MaxIdleConnsPerHost == 0 {
        poolCfg.MaxIdleConnsPerHost = 100
    }

    return &nativeBizClient{
        baseURL: baseURL,
        httpClient: &http.Client{
            Transport: &http.Transport{
                MaxIdleConns:        poolCfg.MaxIdleConns,
                MaxIdleConnsPerHost: poolCfg.MaxIdleConnsPerHost,
                IdleConnTimeout:      poolCfg.IdleConnTimeout,
                DialTimeout:          poolCfg.DialTimeout,
            },
            Timeout: 30 * time.Second,
        },
        cbRegistry: resilience.NewRegistry(
            cbCfg.FailureThreshold,
            cbCfg.RecoveryTimeout,
            cbCfg.SuccessThreshold,
        ),
        retryCfg: retryCfg,
    }
}

// ForwardRequest implements proto.BizServiceClient with retry + circuit breaker.
func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    cb := c.cbRegistry.Get(c.baseURL)
    if !cb.Allow() {
        return nil, 503, fmt.Errorf("circuit breaker open for %s", c.baseURL)
    }

    var lastBody []byte
    var lastStatus int
    var lastErr error

    err := resilience.Do(ctx, c.retryCfg, func() error {
        respBody, status, err := c.doRequest(ctx, path, method, body)
        if err != nil {
            lastErr = err
            lastStatus = status
            return err
        }

        lastStatus = status
        lastBody = respBody
        lastErr = nil

        // Don't retry 4xx errors
        if status >= 400 && status < 500 {
            return nil
        }

        // Retry 5xx errors
        if resilience.IsRetryable(status) {
            lastErr = fmt.Errorf("retryable status: %d", status)
            return lastErr
        }

        return nil
    })

    if err != nil {
        cb.RecordFailure()
        return lastBody, lastStatus, lastErr
    }
    cb.RecordSuccess()
    return lastBody, lastStatus, nil
}

func (c *nativeBizClient) doRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    url := c.baseURL + path
    req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
    if err != nil {
        return nil, 0, err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, 503, err
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    return respBody, resp.StatusCode, nil
}
```

**Validation:**
- `go test ./internal/httpclient/...` passes
- Circuit breaker integration tests

---

### Task 2.3: Native AAA Client

**File:** `internal/httpclient/native_aaa.go`

Same pattern as native biz client but with stricter settings (2 retries, 10s max delay) for AAA protocol.

```go
func newNativeAAAClient(baseURL string, cfg config.NativeCommConfig) *nativeAAAClient {
    // Override with stricter AAA settings
    cfg.Retry.MaxAttempts = 2
    cfg.Retry.MaxDelay = 10 * time.Second
    cfg.CB.FailureThreshold = 3 // More sensitive for AAA
    cfg.Pool.MaxIdleConnsPerHost = 50
    return &nativeAAAClient{...}
}
```

---

### Task 2.4: Istio Biz Client

**File:** `internal/httpclient/istio_biz.go`

Minimal client — resilience delegated to Istio sidecar.

```go
type istioBizClient struct {
    baseURL string
    client  *http.Client
}

func newIstioBizClient(baseURL string) *istioBizClient {
    return &istioBizClient{
        baseURL: baseURL,
        client:  http.DefaultClient,
    }
}

func (c *istioBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    url := c.baseURL + path
    req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
    if err != nil {
        return nil, 0, err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.client.Do(req)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return nil, 504, context.DeadlineExceeded
        }
        return nil, 503, err
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    return respBody, resp.StatusCode, nil
}
```

---

### Task 2.5: Istio AAA Client

**File:** `internal/httpclient/istio_aaa.go`

Same as Istio Biz client.

---

### Task 2.6: Internal Communication Metrics

**File:** `internal/httpclient/metrics.go`

```go
package httpclient

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    requestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_internal_request_duration_seconds",
            Help:    "Duration of internal HTTP requests",
            Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1},
        },
        []string{"source", "destination", "status"},
    )

    requestRetries = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_internal_request_retries_total",
            Help: "Total number of retries for internal requests",
        },
        []string{"source", "destination"},
    )
)
```

---

### Task 2.7: Unit Tests

**Files:**
- `internal/httpclient/native_biz_test.go`
- `internal/httpclient/native_aaa_test.go`

Test cases:
- Happy path (no retries)
- Retry on 502/503
- No retry on 400/404
- Circuit breaker opens after N failures
- Circuit breaker closes after recovery timeout

---

## Wave 3: Component Integration

### Task 3.1: HTTP Gateway Integration

**File:** `cmd/http-gateway/main.go`

Changes:
1. Add `InternalCommConfig` field
2. Use `httpclient.NewFactory()` to create Biz client
3. Replace existing direct HTTP calls

**Validation:** E2E tests pass

---

### Task 3.2: Biz Pod Integration

**File:** `cmd/biz/factory.go` (or main.go)

Changes:
1. Add `InternalCommConfig` field
2. Use `httpclient.NewFactory()` to create AAA client
3. Replace existing `newHTTPAAAClient()` calls

**Validation:** E2E tests pass

---

### Task 3.3: Config Files

**Files:**
- `configs/http-gateway.yaml`
- `configs/biz.yaml`

Add `internalComm` section with environment-appropriate settings.

---

## Wave 4: Kubernetes Manifests

### Task 4.1: Istio Mode Manifests

**Files:**
- `deployments/k8s/istio-mode/virtualservice-biz.yaml`
- `deployments/k8s/istio-mode/destinationrule-biz.yaml`
- `deployments/k8s/istio-mode/virtualservice-aaa.yaml`
- `deployments/k8s/istio-mode/destinationrule-aaa.yaml`

---

### Task 4.2: Native Mode Service

**File:** `deployments/k8s/native-mode/service-biz.yaml`

Standard ClusterIP + headless service.

---

## Wave 5: Validation

### Integration Tests

1. HTTP GW → Biz Pod: Load balancing with 3 replicas
2. Biz Pod → AAA GW: Circuit breaker isolation
3. Mode switch: `ISTIO_MTLS=1` activates Istio clients
4. Metrics: Prometheus shows internal call latencies

---

## Validation Checklist

### Code Quality
- [ ] `go build ./...` compiles without errors
- [ ] `go test ./internal/httpclient/...` passes
- [ ] `golangci-lint run ./internal/httpclient/...` passes

### Functional
- [ ] Native mode: retry works on 502/503 errors
- [ ] Native mode: circuit breaker opens after N failures
- [ ] Native mode: connection pool reuses connections
- [ ] Istio mode: `ISTIO_MTLS=1` activates Istio clients
- [ ] Config: `mode: native` / `mode: istio` works
- [ ] Metrics: `nssaa_internal_request_duration_seconds` exposed

### Integration
- [ ] HTTP GW → Biz Pod load balancing works
- [ ] Biz Pod → AAA GW load balancing works
- [ ] Circuit breaker isolates failing service
- [ ] Retry doesn't cause duplicate EAP rounds

---

## Success Criteria

1. **HTTP GW → Biz Pod**: Load balancing across N replicas, circuit breaker prevents cascade
2. **Biz Pod → AAA GW**: Circuit breaker per service, retry on transient failures
3. **Mode switching**: `ISTIO_MTLS=1` env var switches modes without code change
4. **Observability**: Internal call latencies visible in Prometheus
5. **Backward compatible**: Existing config files work without `internalComm` section

---

## Dependencies

| Task | Depends On | Blocking |
|------|-----------|----------|
| Wave 1 | — | No |
| Wave 2 | Wave 1 | No |
| Wave 3 | Wave 2 | No |
| Wave 4 | Wave 3 | No |
| Wave 5 | Wave 4 | No |

---

## Files to Create/Modify

| File | Action | Wave |
|------|--------|------|
| `internal/config/config.go` | Modify | 1 |
| `internal/httpclient/factory.go` | Create | 2 |
| `internal/httpclient/native_biz.go` | Create | 2 |
| `internal/httpclient/native_aaa.go` | Create | 2 |
| `internal/httpclient/istio_biz.go` | Create | 2 |
| `internal/httpclient/istio_aaa.go` | Create | 2 |
| `internal/httpclient/metrics.go` | Create | 2 |
| `internal/httpclient/*_test.go` | Create | 2 |
| `cmd/http-gateway/main.go` | Modify | 3 |
| `cmd/biz/factory.go` | Modify | 3 |
| `configs/http-gateway.yaml` | Modify | 3 |
| `configs/biz.yaml` | Modify | 3 |
| `deployments/k8s/istio-mode/*.yaml` | Create | 4 |
| `deployments/k8s/native-mode/service-biz.yaml` | Create | 4 |

---

## Time Estimate

| Wave | Tasks | Est. Time |
|------|-------|-----------|
| Wave 1 | Config schema | 1 hour |
| Wave 2 | HTTP client package + tests | 4 hours |
| Wave 3 | Component integration | 2 hours |
| Wave 4 | K8s manifests | 1 hour |
| Wave 5 | Validation | 2 hours |
| **Total** | | **10 hours** |

**Reduced from 13h** because existing `resilience` package provides CB/Registry/Retry/Do().
