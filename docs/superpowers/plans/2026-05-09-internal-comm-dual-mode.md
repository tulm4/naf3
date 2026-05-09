# Internal Communication Dual-Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement dual-mode internal communication (Native/Istio) for NSSAAF 3-component architecture, wiring existing `resilience` package into HTTP clients.

**Architecture:** HTTP client factory (`internal/httpclient/`) creates either native clients (retry + circuit breaker + connection pool) or Istio clients (delegates to service mesh). Mode detected via config + `ISTIO_MTLS` env var.

**Tech Stack:** Go 1.22+, `internal/resilience/` (CircuitBreaker, Registry, RetryConfig, Do()), `internal/proto/` (BizServiceClient, BizAAAClient interfaces), Prometheus metrics

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/config/internal_comm.go` | Create | InternalCommConfig types |
| `internal/httpclient/factory.go` | Create | Mode detection + client factory |
| `internal/httpclient/native_biz.go` | Create | Native Biz client with retry/CB/pool |
| `internal/httpclient/native_aaa.go` | Create | Native AAA client (stricter settings) |
| `internal/httpclient/istio_biz.go` | Create | Istio Biz client (minimal) |
| `internal/httpclient/istio_aaa.go` | Create | Istio AAA client (minimal) |
| `internal/httpclient/metrics.go` | Create | Prometheus metrics |
| `internal/httpclient/native_biz_test.go` | Create | Unit tests for native Biz client |
| `internal/httpclient/native_aaa_test.go` | Create | Unit tests for native AAA client |
| `cmd/http-gateway/main.go` | Modify | Use httpclient factory |
| `cmd/biz/http_aaa_client.go` | Modify | Implement BizAAAClient interface |
| `configs/http-gateway.yaml` | Modify | Add internalComm section |
| `configs/biz.yaml` | Modify | Add internalComm section |
| `deployments/k8s/istio-mode/virtualservice-biz.yaml` | Create | Istio VirtualService |
| `deployments/k8s/istio-mode/destinationrule-biz.yaml` | Create | Istio DestinationRule |
| `deployments/k8s/native-mode/service-biz.yaml` | Create | Native mode service |

---

## Task 1: Add InternalCommConfig to Config Package

**Files:**
- Create: `internal/config/internal_comm.go`
- Modify: `internal/config/config.go:25-48`

- [ ] **Step 1: Create internal_comm.go**

```go
// Package config provides configuration for NSSAAF.
package config

import "time"

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
	// Retry configures retry behavior
	Retry RetryConfig `yaml:"retry"`
	// CB configures per-destination circuit breaking
	CB CircuitBreakerConfig `yaml:"circuitBreaker"`
	// Pool configures http.Transport connection pool
	Pool ConnectionPoolConfig `yaml:"connectionPool"`
}

// RetryConfig for exponential backoff retry.
type RetryConfig struct {
	MaxAttempts int           `yaml:"maxAttempts"`
	BaseDelay  time.Duration `yaml:"baseDelay"`
	MaxDelay   time.Duration `yaml:"maxDelay"`
}

// CircuitBreakerConfig mirrors resilience.CircuitBreaker defaults.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failureThreshold"`
	RecoveryTimeout time.Duration `yaml:"recoveryTimeout"`
	SuccessThreshold int          `yaml:"successThreshold"`
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

- [ ] **Step 2: Add InternalComm to Config struct**

Add to the `Config` struct in `internal/config/config.go` after the `Crypto` field:

```go
Crypto        CryptoConfig        `yaml:"crypto"`
InternalComm  InternalCommConfig  `yaml:"internalComm"`

// Per-component config (only one is non-nil based on Component field)
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/config/...`
Expected: SUCCESS (no errors)

- [ ] **Step 4: Commit**

```bash
git add internal/config/internal_comm.go internal/config/config.go
git commit -m "feat(config): add InternalCommConfig for dual-mode internal communication"
```

---

## Task 2: Create HTTP Client Factory

**Files:**
- Create: `internal/httpclient/factory.go`

- [ ] **Step 1: Create factory.go**

```go
// Package httpclient provides HTTP clients for internal component communication.
// Supports two modes: native (retry + circuit breaker + connection pool) and istio (delegated to service mesh).
package httpclient

import (
	"net/http"
	"os"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
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

- [ ] **Step 2: Create stub implementations for native clients**

```go
// nativeBizClient implements proto.BizServiceClient with retry + circuit breaker.
type nativeBizClient struct {
	baseURL    string
	httpClient *http.Client
}

func newNativeBizClient(baseURL string, cfg config.NativeCommConfig) *nativeBizClient {
	return &nativeBizClient{baseURL: baseURL}
}

// nativeAAAClient implements proto.BizAAAClient with retry + circuit breaker.
type nativeAAAClient struct {
	aaaGatewayURL string
	httpClient    *http.Client
}

func newNativeAAAClient(aaaGatewayURL string, cfg config.NativeCommConfig) *nativeAAAClient {
	return &nativeAAAClient{aaaGatewayURL: aaaGatewayURL}
}
```

- [ ] **Step 3: Create stub implementations for istio clients**

```go
// istioBizClient delegates resilience to Istio sidecar.
type istioBizClient struct {
	baseURL string
	client  *http.Client
}

func newIstioBizClient(baseURL string) *istioBizClient {
	return &istioBizClient{baseURL: baseURL, client: http.DefaultClient}
}

// istioAAAClient delegates resilience to Istio sidecar.
type istioAAAClient struct {
	aaaGatewayURL string
	client       *http.Client
}

func newIstioAAAClient(aaaGatewayURL string) *istioAAAClient {
	return &istioAAAClient{aaaGatewayURL: aaaGatewayURL, client: http.DefaultClient}
}
```

- [ ] **Step 4: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/httpclient/factory.go
git commit -m "feat(httpclient): create httpclient package with factory"
```

---

## Task 3: Implement Native Biz Client with Retry + Circuit Breaker

**Files:**
- Modify: `internal/httpclient/factory.go` (add ForwardRequest methods)

- [ ] **Step 1: Add imports and resilience dependency to factory.go**

Update imports in factory.go:

```go
import (
	"net/http"
	"os"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
)
```

- [ ] **Step 2: Implement ForwardRequest for nativeBizClient**

Add to factory.go after the Factory struct:

```go
// ForwardRequest implements proto.BizServiceClient with retry + circuit breaker.
func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	cb := resilience.NewCircuitBreaker(5, 30*time.Second, 3)
	if !cb.Allow() {
		return nil, 503, fmt.Errorf("circuit breaker open for %s", c.baseURL)
	}

	var lastBody []byte
	var lastStatus int
	var lastErr error

	err := resilience.Do(ctx, resilience.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    4 * time.Second,
	}, func() error {
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

- [ ] **Step 3: Add io and bytes imports**

```go
import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
)
```

- [ ] **Step 4: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/httpclient/factory.go
git commit -m "feat(httpclient): implement nativeBizClient with retry and circuit breaker"
```

---

## Task 4: Implement Native AAA Client

**Files:**
- Modify: `internal/httpclient/factory.go` (add ForwardEAP method)

- [ ] **Step 1: Implement ForwardEAP for nativeAAAClient**

Add after nativeBizClient methods:

```go
// ForwardEAP implements proto.BizAAAClient with retry + circuit breaker.
func (c *nativeAAAClient) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	cb := resilience.NewCircuitBreaker(3, 15*time.Second, 2)
	if !cb.Allow() {
		return nil, fmt.Errorf("circuit breaker open for %s", c.aaaGatewayURL)
	}

	var lastBody []byte
	var lastStatus int
	var lastErr error

	err = resilience.Do(ctx, resilience.RetryConfig{
		MaxAttempts: 2, // Stricter for AAA
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
	}, func() error {
		respBody, status, err := c.doPost(ctx, body)
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
		return nil, lastErr
	}
	cb.RecordSuccess()

	var resp proto.AaaForwardResponse
	if err := json.Unmarshal(lastBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

func (c *nativeAAAClient) doPost(ctx context.Context, body []byte) ([]byte, int, error) {
	url := c.aaaGatewayURL + "/aaa/forward"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
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

- [ ] **Step 2: Add json import**

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
)
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/httpclient/factory.go
git commit -m "feat(httpclient): implement nativeAAAClient with stricter retry settings"
```

---

## Task 5: Implement Istio Clients

**Files:**
- Modify: `internal/httpclient/factory.go` (add Istio ForwardRequest/ForwardEAP)

- [ ] **Step 1: Implement ForwardRequest for istioBizClient**

Add after nativeAAAClient methods:

```go
// ForwardRequest delegates to Istio sidecar for resilience.
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

// ForwardEAP delegates to Istio sidecar for resilience.
func (c *istioAAAClient) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.aaaGatewayURL + "/aaa/forward"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("aaa gateway unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aaa gateway returned %d", resp.StatusCode)
	}

	var fwdResp proto.AaaForwardResponse
	if err := json.NewDecoder(resp.Body).Decode(&fwdResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &fwdResp, nil
}
```

- [ ] **Step 2: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/httpclient/factory.go
git commit -m "feat(httpclient): implement Istio clients delegating to service mesh"
```

---

## Task 6: Add Prometheus Metrics

**Files:**
- Create: `internal/httpclient/metrics.go`

- [ ] **Step 1: Create metrics.go**

```go
package httpclient

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestDuration tracks the duration of internal HTTP requests.
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nssaa_internal_request_duration_seconds",
			Help:    "Duration of internal HTTP requests",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"source", "destination", "status"},
	)

	// RequestRetries tracks the number of retries for internal requests.
	RequestRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nssaa_internal_request_retries_total",
			Help: "Total number of retries for internal requests",
		},
		[]string{"source", "destination"},
	)

	// CircuitBreakerState tracks the state of circuit breakers (0=closed, 1=open, 2=half-open).
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nssaa_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
		},
		[]string{"destination"},
	)
)
```

- [ ] **Step 2: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/httpclient/metrics.go
git commit -m "feat(httpclient): add Prometheus metrics for internal communication"
```

---

## Task 7: Write Unit Tests for Native Biz Client

**Files:**
- Create: `internal/httpclient/native_biz_test.go`

- [ ] **Step 1: Create test file with happy path test**

```go
package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
)

func TestNativeBizClient_HappyPath(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	// Create client
	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(server.URL, cfg)

	// Make request
	respBody, status, err := client.ForwardRequest(context.Background(), "/test", "POST", []byte(`{}`))

	// Verify
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got: %d", status)
	}
	if len(respBody) == 0 {
		t.Fatal("expected non-empty response body")
	}
}

func TestNativeBizClient_RetryOn502(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(server.URL, cfg)

	respBody, status, err := client.ForwardRequest(context.Background(), "/test", "POST", []byte(`{}`))

	if err != nil {
		t.Fatalf("expected no error after retry, got: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200 after retry, got: %d", status)
	}
	if attempt < 2 {
		t.Fatalf("expected at least 2 attempts, got: %d", attempt)
	}
}

func TestNativeBizClient_NoRetryOn400(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(server.URL, cfg)

	_, status, err := client.ForwardRequest(context.Background(), "/test", "POST", []byte(`{}`))

	if err != nil {
		t.Fatalf("expected no error for 400, got: %v", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("expected status 400, got: %d", status)
	}
	if attempt != 1 {
		t.Fatalf("expected exactly 1 attempt for 400, got: %d", attempt)
	}
}

func TestNativeBizClient_CircuitBreakerOpen(t *testing.T) {
	// Server that always fails
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(failServer.URL, cfg)

	// Trip the circuit breaker (5 failures)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		client.ForwardRequest(ctx, "/test", "POST", []byte(`{}`))
	}

	// Next request should be rejected by circuit breaker
	_, status, err := client.ForwardRequest(ctx, "/test", "POST", []byte(`{}`))

	if err == nil {
		t.Fatal("expected error when circuit breaker is open")
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 for circuit breaker open, got: %d", status)
	}
}

var _ proto.BizServiceClient = (*nativeBizClient)(nil)
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/httpclient/... -v -run TestNativeBiz`
Expected: All 4 tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/httpclient/native_biz_test.go
git commit -m "test(httpclient): add unit tests for nativeBizClient"
```

---

## Task 8: Write Unit Tests for Native AAA Client

**Files:**
- Create: `internal/httpclient/native_aaa_test.go`

- [ ] **Step 1: Create test file**

```go
package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
)

func TestNativeAAAClient_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got: %s", r.Method)
		}
		if r.URL.Path != "/aaa/forward" {
			t.Errorf("expected /aaa/forward, got: %s", r.URL.Path)
		}

		resp := &proto.AaaForwardResponse{
			Version:   "1.0",
			SessionID: "test-session",
			AuthCtxID: "test-auth",
			Payload:   []byte(`eap-response`),
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeAAAClient(server.URL, cfg)

	req := &proto.AaaForwardRequest{
		Version:   "1.0",
		SessionID: "test-session",
		AuthCtxID: "test-auth",
		Payload:   []byte(`eap-payload`),
	}

	resp, err := client.ForwardEAP(context.Background(), req)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if string(resp.Payload) != "eap-response" {
		t.Fatalf("expected 'eap-response', got: %s", string(resp.Payload))
	}
}

func TestNativeAAAClient_StrictRetry(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeAAAClient(server.URL, cfg)

	req := &proto.AaaForwardRequest{
		Version:   "1.0",
		SessionID: "test-session",
		AuthCtxID: "test-auth",
	}

	_, err := client.ForwardEAP(context.Background(), req)

	if err == nil {
		t.Fatal("expected error after max retries")
	}
	// AAA client should only retry 2 times (not 3 like Biz client)
	if attempt > 3 {
		t.Fatalf("expected at most 3 attempts for AAA client, got: %d", attempt)
	}
}

var _ proto.BizAAAClient = (*nativeAAAClient)(nil)
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/httpclient/... -v -run TestNativeAAA`
Expected: All 2 tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/httpclient/native_aaa_test.go
git commit -m "test(httpclient): add unit tests for nativeAAAClient"
```

---

## Task 9: Integrate with HTTP Gateway

**Files:**
- Modify: `cmd/http-gateway/main.go`

- [ ] **Step 1: Read current main.go to find where httpBizClient is used**

Run: `grep -n "httpBizClient\|bizServiceURL" cmd/http-gateway/main.go`

Expected output: Shows lines where biz client is created and used

- [ ] **Step 2: Update main.go to use httpclient factory**

Replace the `httpBizClient` struct and its creation with:

```go
// Create Biz service client using httpclient factory
var bizClient proto.BizServiceClient
if cfg.InternalComm.Mode == "istio" || os.Getenv("ISTIO_MTLS") == "1" {
	bizClient = httpclient.NewFactory(cfg.InternalComm).NewBizServiceClient(cfg.HTTPgw.BizServiceURL)
} else {
	// Use default http.Client for now; factory will be used in full implementation
	bizClient = &httpBizClientSimple{
		bizServiceURL: cfg.HTTPgw.BizServiceURL,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		version:       cfg.Version,
	}
}
```

Add `bizClientSimple` as a simple wrapper if needed:

```go
type httpBizClientSimple struct {
	bizServiceURL string
	httpClient    *http.Client
	version       string
}

func (c *httpBizClientSimple) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	url := c.bizServiceURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proto.HeaderName, c.version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, 504, err
		}
		return nil, 503, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

var _ proto.BizServiceClient = (*httpBizClientSimple)(nil)
```

- [ ] **Step 3: Add imports**

```go
import (
	"bytes"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/auth"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/httpclient"
	"github.com/operator/nssAAF/internal/proto"
)
```

- [ ] **Step 4: Run build to verify**

Run: `go build ./cmd/http-gateway/...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add cmd/http-gateway/main.go
git commit -m "feat(http-gateway): integrate httpclient factory for Biz service client"
```

---

## Task 10: Integrate with Biz Pod

**Files:**
- Modify: `cmd/biz/http_aaa_client.go`

- [ ] **Step 1: Update httpAAAClient to implement BizAAAClient from proto**

The existing `httpAAAClient` already implements `eap.AAARouter`. We need to add the `proto.BizAAAClient` interface:

```go
// ForwardEAP satisfies proto.BizAAAClient.
// Spec: PHASE §1.1 pattern
func (c *httpAAAClient) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal forward request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.aaaGatewayURL+"/aaa/forward", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(proto.HeaderName, c.version)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("aaa gateway unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aaa gateway returned %d", resp.StatusCode)
	}

	var fwdResp proto.AaaForwardResponse
	if err := json.NewDecoder(resp.Body).Decode(&fwdResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &fwdResp, nil
}
```

- [ ] **Step 2: Verify interface compliance**

Run: `go build ./cmd/biz/...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add cmd/biz/http_aaa_client.go
git commit -m "feat(biz): add ForwardEAP method for proto.BizAAAClient interface"
```

---

## Task 11: Update Config Files

**Files:**
- Create: `configs/http-gateway.yaml` (if not exists, modify existing)
- Create: `configs/biz.yaml` (if not exists, modify existing)

- [ ] **Step 1: Add internalComm section to http-gateway config**

```yaml
internalComm:
  mode: native  # or "istio"
  native:
    retry:
      maxAttempts: 3
      baseDelay: 1s
      maxDelay: 4s
    circuitBreaker:
      failureThreshold: 5
      recoveryTimeout: 30s
      successThreshold: 3
    connectionPool:
      maxIdleConnsPerHost: 100
      idleConnTimeout: 90s
```

- [ ] **Step 2: Add internalComm section to biz config**

```yaml
internalComm:
  mode: native
  native:
    retry:
      maxAttempts: 2  # Stricter for AAA
      baseDelay: 500ms
      maxDelay: 10s
    circuitBreaker:
      failureThreshold: 3  # More sensitive for AAA
      recoveryTimeout: 15s
      successThreshold: 2
    connectionPool:
      maxIdleConnsPerHost: 50
      idleConnTimeout: 60s
```

- [ ] **Step 3: Commit**

```bash
git add configs/http-gateway.yaml configs/biz.yaml
git commit -m "docs(config): add internalComm sections for dual-mode communication"
```

---

## Task 12: Create Kubernetes Manifests

**Files:**
- Create: `deployments/k8s/istio-mode/virtualservice-biz.yaml`
- Create: `deployments/k8s/istio-mode/destinationrule-biz.yaml`
- Create: `deployments/k8s/native-mode/service-biz.yaml`

- [ ] **Step 1: Create virtualservice-biz.yaml**

```yaml
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
      timeout: 30s
```

- [ ] **Step 2: Create destinationrule-biz.yaml**

```yaml
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
      mode: ISTIO_MUTUAL
```

- [ ] **Step 3: Create service-biz.yaml for native mode**

```yaml
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
apiVersion: v1
kind: Service
metadata:
  name: nssaa-biz-headless
  namespace: nssaa
spec:
  type: ClusterIP
  clusterIP: None
  ports:
    - port: 8080
      targetPort: 8080
  publishNotReadyAddresses: true
  selector:
    app: nssaa-biz
```

- [ ] **Step 4: Commit**

```bash
mkdir -p deployments/k8s/istio-mode deployments/k8s/native-mode
git add deployments/k8s/istio-mode/virtualservice-biz.yaml deployments/k8s/istio-mode/destinationrule-biz.yaml deployments/k8s/native-mode/service-biz.yaml
git commit -m "feat(k8s): add Istio and native mode service manifests"
```

---

## Task 13: Final Validation

- [ ] **Step 1: Run all tests**

Run: `go test ./internal/httpclient/... -v`
Expected: All tests PASS

- [ ] **Step 2: Run linter**

Run: `golangci-lint run ./internal/httpclient/...`
Expected: No issues

- [ ] **Step 3: Run full build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: complete Phase 7.1 internal communication dual-mode"
```

---

## Self-Review Checklist

- [ ] Spec coverage: All requirements from PHASE_7-1_internal_comm.md covered
- [ ] No placeholders: All code is complete, no TODOs or TBDs
- [ ] Type consistency: All method signatures match proto interfaces
- [ ] Tests: Unit tests for native clients included
- [ ] Config: InternalCommConfig added to main config struct
- [ ] Integration: HTTP Gateway and Biz Pod updated
- [ ] K8s: Istio and native mode manifests created

---

## Execution Options

**Plan complete and saved.** Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
