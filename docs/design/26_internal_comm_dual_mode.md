# Internal Communication: Dual-Mode (Istio / Native) Design

## Metadata

| Field | Value |
|-------|-------|
| Spec | TS 29.526 v18.7.0 §7 (SBI), TS 29.500 §5 (Transport) |
| Section | §5.4.6 (Internal Communication) |
| Interface | Internal (HTTP GW ↔ Biz, Biz ↔ AAA GW) |
| Service | N/A (infrastructure) |
| Operation | N/A |
| eapMethod | N/A |
| aaaProtocol | N/A |

## Overview

NSSAAF supports two internal communication modes:

1. **Native Mode** (`ISTIO_MTLS=0` or unset): Go stdlib HTTP clients with built-in resilience (circuit breaker, retry, connection pooling)
2. **Istio Mode** (`ISTIO_MTLS=1`): Kubernetes service mesh handles load balancing, retries, circuit breaking, and mTLS

Both modes provide equivalent resilience guarantees but differ in operational complexity and dependencies.

---

## 1. Architecture Comparison

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Native Mode (No Istio)                          │
│                                                                         │
│  HTTP GW ──► Biz Svc ──► Biz Pods                                      │
│                 │          │                                            │
│                 │          ├─ Circuit Breaker (per pod)                 │
│                 │          ├─ Retry (exponential backoff)                │
│                 │          └─ Connection Pool (http.Transport)           │
│                 │                                                       │
│  Biz Pod ──► AAA Svc ──► AAA GW                                        │
│                 │          │                                            │
│                 │          ├─ Circuit Breaker (per host:port)            │
│                 │          ├─ Retry (exponential backoff)                │
│                 │          └─ Connection Pool                           │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                          Istio Mode                                      │
│                                                                         │
│  HTTP GW ──► Biz Svc ──► Biz Pods                                      │
│                 │                                                       │
│                 ├─ VirtualService (load balancing, retries)              │
│                 └─ DestinationRule (circuit breaker, connection pool)   │
│                                                                         │
│  Biz Pod ──► AAA Svc ──► AAA GW                                        │
│                 │                                                       │
│                 ├─ VirtualService (load balancing, retries)              │
│                 └─ DestinationRule (circuit breaker)                    │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Configuration Design

### 2.1 Environment Variables

```yaml
# Internal communication mode
ISTIO_MTLS: "0"           # "1" = Istio mode, "0" or unset = Native mode

# Native mode settings (ignored in Istio mode)
NATIVE_RETRY_MAX_ATTEMPTS: "3"
NATIVE_RETRY_BASE_DELAY: "1s"
NATIVE_RETRY_MAX_DELAY: "30s"
NATIVE_CB_FAILURE_THRESHOLD: "5"
NATIVE_CB_RECOVERY_TIMEOUT: "30s"
NATIVE_CB_HALF_OPEN_MAX: "3"
```

### 2.2 Config Schema (Go)

```go
// internal/config/internal_comm.go

// InternalCommConfig holds configuration for internal component communication.
type InternalCommConfig struct {
    // Mode selects the communication mode:
    // - "native": Go stdlib with built-in resilience
    // - "istio": Delegate to Istio service mesh
    Mode string `yaml:"mode"`

    // Native holds configuration for native mode.
    // Ignored when Mode == "istio".
    Native NativeCommConfig `yaml:"native"`

    // Istio holds configuration for Istio mode.
    // Ignored when Mode == "native".
    Istio IstioCommConfig `yaml:"istio"`
}

// NativeCommConfig configures Go native HTTP client resilience.
type NativeCommConfig struct {
    Retry RetryConfig `yaml:"retry"`
    CB    CircuitBreakerConfig `yaml:"circuitBreaker"`
    Pool  ConnectionPoolConfig `yaml:"connectionPool"`
}

// RetryConfig for HTTP client retry behavior.
type RetryConfig struct {
    MaxAttempts int           `yaml:"maxAttempts"`
    BaseDelay   time.Duration `yaml:"baseDelay"`
    MaxDelay    time.Duration `yaml:"maxDelay"`
}

// CircuitBreakerConfig for per-endpoint circuit breaking.
type CircuitBreakerConfig struct {
    FailureThreshold int           `yaml:"failureThreshold"`
    RecoveryTimeout  time.Duration `yaml:"recoveryTimeout"`
    HalfOpenMax      int           `yaml:"halfOpenMax"`
}

// ConnectionPoolConfig for http.Transport tuning.
type ConnectionPoolConfig struct {
    MaxIdleConns        int           `yaml:"maxIdleConns"`
    MaxIdleConnsPerHost int           `yaml:"maxIdleConnsPerHost"`
    IdleConnTimeout     time.Duration `yaml:"idleConnTimeout"`
}

// IstioCommConfig holds Istio-specific settings.
// Note: Enabled is inferred from Mode == "istio" or ISTIO_MTLS=1 env var.
type IstioCommConfig struct {
	// TrustDomain specifies the Istio trust domain (default: "cluster.local").
	TrustDomain string `yaml:"trustDomain"`
}
```

---

## 3. Go Implementation

### 3.1 HTTP Client Factory

```go
// internal/httpclient/factory.go
// Spec: TS 29.500 §5 — transport resilience requirements.

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
// 1. Config file (cfg.Mode)
// 2. Environment variable (ISTIO_MTLS=1)
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

// NewBizServiceClient creates a BizServiceClient for HTTP GW -> Biz Pod communication.
func (f *Factory) NewBizServiceClient(bizServiceURL string) proto.BizServiceClient {
	switch f.mode {
	case ModeIstio:
		// Istio handles retries, CB, load balancing via VirtualService/DestinationRule
		return newIstioBizClient(bizServiceURL)
	default:
		return newNativeBizClient(bizServiceURL, f.cfg.Native)
	}
}

// NewAAAClient creates an AAA client for Biz Pod -> AAA GW communication.
func (f *Factory) NewAAAClient(aaaGatewayURL string) proto.BizAAAClient {
	switch f.mode {
	case ModeIstio:
		return newIstioAAAClient(aaaGatewayURL)
	default:
		return newNativeAAAClient(aaaGatewayURL, f.cfg.Native)
	}
}
```

### 3.2 Native Mode: Biz Service Client

```go
// internal/httpclient/native_biz.go
// Spec: TS 29.500 §5 — transport resilience requirements.
// Circuit breaker per host:port via resilience.Registry (per-pod isolation).

// nativeBizClient implements BizServiceClient with built-in resilience.
type nativeBizClient struct {
	baseURL    string
	httpClient *http.Client
	cbRegistry *resilience.Registry
	retryCfg   config.RetryConfig
}

func newNativeBizClient(baseURL string, cfg config.NativeCommConfig) *nativeBizClient {
	retryCfg := cfg.Retry
	if retryCfg.MaxAttempts == 0 {
		retryCfg.MaxAttempts = 3
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
			},
			Timeout: 30 * time.Second,
		},
		// Per-host:port circuit breaker registry for pod isolation
		cbRegistry: resilience.NewRegistry(
			cfg.CB.FailureThreshold,
			cfg.CB.RecoveryTimeout,
			cfg.CB.HalfOpenMax,
		),
		retryCfg: retryCfg,
	}
}

// ForwardRequest implements BizServiceClient with retry + circuit breaker.
// Spec: TS 29.500 §5 — transport resilience requirements.
// Uses per-destination circuit breaker for isolation between services.
func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	// Per-service circuit breaker (keyed by baseURL/service name)
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

		// Don't retry 4xx errors (client error, won't succeed on retry)
		if status >= 400 && status < 500 {
			return nil // Stop retrying
		}

		// Return error to trigger retry if status is retryable
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

// doRequest executes a single HTTP request and returns (body, status, err).
func (c *nativeBizClient) doRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network errors return 503 Service Unavailable
		return nil, 503, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}
```

### 3.3 Istio Mode: Biz Service Client

```go
// internal/httpclient/istio_biz.go
// Spec: TS 29.500 §5 — in Istio mode, resilience delegated to service mesh.

// istioBizClient delegates resilience to Istio sidecar.
type istioBizClient struct {
	baseURL string
}

func newIstioBizClient(baseURL string) *istioBizClient {
	return &istioBizClient{baseURL: baseURL}
}

func (c *istioBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	// No custom transport - Istio sidecar handles retries, CB, mTLS
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network/timeout errors -> 503 (service unavailable)
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

## 4. Kubernetes Manifests

### 4.1 Native Mode: Kubernetes Service

In native mode, Kubernetes ClusterIP service provides basic load balancing.
Circuit breaker and retry are handled in Go code.

```yaml
# deployments/k8s/native-mode/service-biz.yaml
apiVersion: v1
kind: Service
metadata:
  name: nssaa-biz
  namespace: nssaa
spec:
  type: ClusterIP
  ports:
    - port: 8080
      targetPort: 8080
      name: http
  selector:
    app: nssaa-biz
---
# Headless service for pod discovery (optional, for advanced routing)
apiVersion: v1
kind: Service
metadata:
  name: nssaa-biz-headless
  namespace: nssaa
spec:
  type: ClusterIP
  clusterIP: None  # Headless
  ports:
    - port: 8080
      targetPort: 8080
  publishNotReadyAddresses: true  # Route to pods even during rolling updates
  selector:
    app: nssaa-biz
```

### 4.2 Istio Mode: VirtualService + DestinationRule

```yaml
# deployments/k8s/istio-mode/virtualservice-biz.yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: nssaa-biz
  namespace: nssaa
spec:
  hosts:
    - nssaa-biz
  http:
    - route:
        - destination:
            host: nssaa-biz
            port:
              number: 8080
      retries:
        attempts: 3
        perTryTimeout: 10s
        retryOn: 5xx,reset,connect-failure,retriable-4xx
        retryRemoteLocalities: true
      timeout: 30s
      # Retry budget: up to 300% of original request rate
      retryBudget:
        budgetPercent:
          value: 50
      # Request timeout
      timeout: 30s
```

```yaml
# deployments/k8s/istio-mode/destinationrule-biz.yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: nssaa-biz
  namespace: nssaa
spec:
  host: nssaa-biz
  trafficPolicy:
    connectionPool:
      http:
        h2UpgradePolicy: UPGRADE
        http1MaxPendingRequests: 100
        http2MaxRequests: 1000
        maxRequestsPerConnection: 100
        maxRetries: 3
    loadBalancer:
      simple: LEAST_REQUEST
      localityLbSetting:
        enabled: true
    outlierDetection:
      consecutive5xxErrors: 5
      interval: 30s
      baseEjectionTime: 30s
      maxEjectionPercent: 50
      minHealthPercent: 30
    tls:
      mode: ISTIO_MUTUAL  # mTLS via Istio
```

### 4.3 AAA Gateway (Both Modes)

```yaml
# deployments/k8s/istio-mode/virtualservice-aaa.yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: nssaa-aaa-gateway
  namespace: nssaa
spec:
  hosts:
    - nssaa-aaa-gateway
  http:
    - route:
        - destination:
            host: nssaa-aaa-gateway
            port:
              number: 8080
      retries:
        attempts: 2  # Fewer retries for AAA (protocol sensitivity)
        perTryTimeout: 5s
        retryOn: 5xx,reset,connect-failure
      timeout: 30s
```

```yaml
# deployments/k8s/istio-mode/destinationrule-aaa.yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: nssaa-aaa-gateway
  namespace: nssaa
spec:
  host: nssaa-aaa-gateway
  trafficPolicy:
    connectionPool:
      http:
        http1MaxPendingRequests: 50
        http2MaxRequests: 200
        maxRequestsPerConnection: 50
    outlierDetection:
      consecutive5xxErrors: 3
      interval: 10s
      baseEjectionTime: 10s
      maxEjectionPercent: 100  # Allow full ejection during AAA-S outage
      minHealthPercent: 0
    tls:
      mode: ISTIO_MUTUAL
```

---

## 5. Configuration Examples

### 5.1 Extending Existing Config Files

Each component's config extends with an `internalComm` section:

```yaml
# configs/http-gateway.yaml (HTTP GW config)
component: http-gateway
version: "1.0.0"

httpgw:
  bizServiceURL: "http://nssaa-biz:8080"
  # Existing TLS config...

# New: Internal communication settings
internalComm:
  mode: native  # or "istio"
  native:
    retry:
      maxAttempts: 3
      baseDelay: 1s
      maxDelay: 30s
    circuitBreaker:
      failureThreshold: 5
      recoveryTimeout: 30s
      halfOpenMax: 3
    connectionPool:
      maxIdleConnsPerHost: 100
      idleConnTimeout: 90s
```

```yaml
# configs/biz.yaml (Biz Pod config)
component: biz
version: "1.0.0"

biz:
  aaGatewayURL: "http://nssaa-aaa-gateway:8080"
  useMTLS: true  # mTLS for AAA GW communication

# New: Internal communication settings
internalComm:
  mode: native
  native:
    retry:
      maxAttempts: 2  # Fewer retries for AAA (protocol sensitivity)
      baseDelay: 500ms
      maxDelay: 10s
    circuitBreaker:
      failureThreshold: 3
      recoveryTimeout: 15s
      halfOpenMax: 2
    connectionPool:
      maxIdleConnsPerHost: 50
      idleConnTimeout: 60s
```

### 5.2 Development (Native Mode)

```yaml
# configs/development/internal-comm.yaml
mode: native

native:
  retry:
    maxAttempts: 2
    baseDelay: 500ms
    maxDelay: 5s
  circuitBreaker:
    failureThreshold: 3
    recoveryTimeout: 10s
    halfOpenMax: 2
  connectionPool:
    maxIdleConnsPerHost: 10
    idleConnTimeout: 90s
```

### 5.3 Production with Istio

```yaml
# configs/production/internal-comm.yaml
mode: istio

istio:
  enabled: true
```

### 5.4 Production Native Mode (No Istio)

```yaml
# configs/production-native/internal-comm.yaml
mode: native

native:
  retry:
    maxAttempts: 3
    baseDelay: 1s
    maxDelay: 30s
  circuitBreaker:
    failureThreshold: 5
    recoveryTimeout: 30s
    halfOpenMax: 3
  connectionPool:
    maxIdleConnsPerHost: 100
    idleConnTimeout: 90s
```

---

## 6. Redis Pub/Sub HA (Both Modes)

Internal response routing uses Redis pub/sub (`nssaa:aaa-response`).
For high availability, use Redis Sentinel or Redis Cluster.

### 6.1 Redis Sentinel Configuration

```yaml
# configs/redis-sentinel.yaml
redis:
  mode: sentinel  # vs "standalone"

sentinel:
  masterName: "nssaa-master"
  # Sentinel addresses (odd number recommended: 3 or 5)
  addresses:
    - "redis-sentinel-0:26379"
    - "redis-sentinel-1:26379"
    - "redis-sentinel-2:26379"
```

### 6.2 Go Client Configuration

```go
// internal/cache/redis/pool.go
// Spec: docs/design/12_redis_ha.md

import (
	"github.com/redis/go-redis/v9"
)

// NewSentinelClient creates a Redis client with Sentinel support.
func NewSentinelClient(cfg RedisConfig) *redis.Client {
	if cfg.Mode != "sentinel" {
		return NewStandaloneClient(cfg)
	}

	return redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    cfg.Sentinel.MasterName,
		SentinelAddrs: cfg.Sentinel.Addresses,
		Dialer: func(ctx context.Context, network, addr string) (redis.Conn, error) {
			network = "tcp"
			return redis.DialContext(ctx, network, addr,
				redis.DialPassword(cfg.Password),
				redis.DialDatabase(cfg.DB),
			)
		},
	})
}
```

---

## 7. Decision Matrix

| Criteria | Native Mode | Istio Mode |
|----------|-------------|------------|
| **Complexity** | Low (Go code only) | High (requires Istio) |
| **Observability** | Manual instrumentation | Auto traces, metrics |
| **Load Balancing** | Kubernetes round-robin | LEAST_REQUEST, locality-aware |
| **Circuit Breaker** | Per-endpoint in Go | Per-service in DestinationRule |
| **Retry** | Configurable in Go | Configurable in VirtualService |
| **mTLS** | Manual cert management | Auto ISTIO_MUTUAL |
| **Latency Overhead** | None | ~1-2ms per hop |
| **Memory Overhead** | None | ~50MB per pod |
| **Best For** | Dev, simple prod | Large-scale prod |

---

## 8. Migration Path

1. **Phase 1**: Implement native mode with all resilience features (G1-G5 fixes)
2. **Phase 2**: Add Istio mode support via `ISTIO_MTLS=1`
3. **Phase 3**: Validate both modes in staging
4. **Phase 4**: Production rollout (start with native, migrate to Istio)

---

## 8. mTLS Handling

### Native Mode

mTLS is handled manually via Go TLS configuration:

```go
// cmd/biz/factory.go
tlsCfg := &tls.Config{}
if f.cfg.Biz.UseMTLS {
    tlsCfg.RootCAs = mustLoadCertPool(f.cfg.Biz.TLSCA)
    tlsCfg.Certificates = []tls.Certificate{mustLoadCert(f.cfg.Biz.TLSCert, f.cfg.Biz.TLSKey)}
    tlsCfg.ServerName = "aaa-gateway"
}
httpClient := &http.Client{
    Transport: &http.Transport{TLSClientConfig: tlsCfg},
    Timeout:   30 * time.Second,
}
```

### Istio Mode

mTLS is handled automatically by Istio sidecar via `ISTIO_MUTUAL` mode in DestinationRule. No manual TLS configuration required in Go code.

---

## 9. Metrics & Monitoring

### Native Mode Metrics

```go
// internal/metrics/internal_comm.go

var (
    InternalRequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_internal_request_duration_seconds",
            Help:    "Duration of internal HTTP requests",
            Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1},
        },
        []string{"source", "destination", "status"},
    )

    InternalRequestRetries = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_internal_request_retries_total",
            Help: "Total number of retries for internal requests",
        },
        []string{"source", "destination"},
    )

    CircuitBreakerState = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nssaa_circuit_breaker_state",
            Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
        },
        []string{"destination"},
    )
)
```

### Istio Mode Metrics (Built-in)

- `istio_requests_total` - Request count
- `istio_request_duration_milliseconds` - Latency
- `istio_requests_total` with `response_code="503"` - Circuit breaker trips
- `istio_connection_pool_size` - Connection pool utilization

---

## 10. Acceptance Criteria

| # | Criteria | Mode | Spec Ref |
|---|----------|------|----------|
| AC1 | HTTP GW → Biz Pod load balancing works with N Biz Pod replicas | Both | TS 29.500 §5.2 |
| AC2 | Circuit breaker opens after N consecutive failures | Both | TS 33.501 §16 |
| AC3 | Retry with exponential backoff on 5xx errors | Both | TS 29.500 §5.4 |
| AC4 | Connection pooling reduces connection overhead | Native | TS 29.500 §5.3 |
| AC5 | mTLS works without manual cert management | Istio | TS 33.501 §6.3 |
| AC6 | VirtualService retry configuration is applied | Istio | Istio docs |
| AC7 | DestinationRule circuit breaker is applied | Istio | Istio docs |
| AC8 | Mode can be switched via ISTIO_MTLS env var | Both | This design |

---

## 11. Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `internal/httpclient/factory.go` | Create | HTTP client factory |
| `internal/httpclient/native_biz.go` | Create | Native mode Biz client |
| `internal/httpclient/native_aaa.go` | Create | Native mode AAA client |
| `internal/httpclient/istio_biz.go` | Create | Istio mode Biz client |
| `internal/httpclient/istio_aaa.go` | Create | Istio mode AAA client |
| `internal/httpclient/metrics.go` | Create | Internal comm metrics |
| `internal/config/internal_comm.go` | Create | Config types |
| `cmd/http-gateway/main.go` | Modify | Use factory for Biz client |
| `cmd/biz/factory.go` | Modify | Use factory for AAA client |
| `configs/*.yaml` | Modify | Add internalComm section |
| `deployments/k8s/istio-mode/*.yaml` | Create | Istio VirtualServices/DestinationRules |
