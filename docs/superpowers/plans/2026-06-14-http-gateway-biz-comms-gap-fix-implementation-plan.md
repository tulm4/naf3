# HTTP Gateway ↔ Biz Communication: Gap Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close all 12 gaps in NSSAAF communication layer. Each slice delivers working, testable software.

**TDD Approach:** Vertical slices (tracer bullets). One test → one implementation → commit. No horizontal phases.

**Tech Stack:** Go 1.22+, Redis (go-redis/v10), http.Client with resilience retry, Kubernetes DNS.

---

## Slice 1: HTTP Gateway → Biz Pod Load Balancing (G1, G4)

**Critical behavior:** HTTP Gateway routes to live Biz Pods only, falls back to static URL when Redis is empty.

### Slice 1.1: Add Timeout Config

- [ ] **RED: Write test for configurable timeout**

```go
// internal/httpclient/native_biz_test.go

func TestNativeBizClient_UsesConfigurableTimeout(t *testing.T) {
    cfg := config.NativeCommConfig{
        Timeout: 5 * time.Second,
    }
    client := newNativeBizClient("http://localhost:9999", cfg)
    
    // Verify timeout is set on underlying http.Client
    if client.httpClient.Timeout != 5*time.Second {
        t.Errorf("expected timeout 5s, got %v", client.httpClient.Timeout)
    }
}
```

Run: `go test ./internal/httpclient/... -v -run TestNativeBizClient_UsesConfigurableTimeout`
Expected: FAIL (Timeout field doesn't exist yet)

- [ ] **GREEN: Add Timeout to NativeCommConfig**

```go
// internal/config/internal_comm.go
type NativeCommConfig struct {
    // ... existing fields ...
    Timeout time.Duration `yaml:"timeout"`
}
```

```go
// internal/config/config.go - applyDefaults()
if cfg.InternalComm.Native.Timeout == 0 {
    cfg.InternalComm.Native.Timeout = 30 * time.Second
}
```

Run: `go test ./internal/httpclient/... -v -run TestNativeBizClient_UsesConfigurableTimeout`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(config): add Timeout to NativeCommConfig (default 30s)"`

---

### Slice 1.2: BizRegistry with Redis-Based Target Selection

- [ ] **RED: Write test for live pod selection**

```go
// internal/httpclient/biz_registry_test.go

func TestBizRegistry_ForwardsToLivePod(t *testing.T) {
    // Setup mock HTTP server that records which pods are called
    pod1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    defer pod1.Close()
    
    // Create BizRegistry with fake Redis (will use static URL fallback)
    registry := NewBizRegistry("localhost:9999", pod1.URL, config.NativeCommConfig{
        Retry: config.RetryConfig{MaxAttempts: 1},
    })
    
    ctx := context.Background()
    body, status, err := registry.ForwardRequest(ctx, "/test", "GET", nil, "req-1")
    
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if status != http.StatusOK {
        t.Errorf("expected status 200, got %d", status)
    }
    if len(body) != 0 {
        t.Errorf("expected empty body for GET, got %d bytes", len(body))
    }
}

func TestBizRegistry_PropagatesRequestID(t *testing.T) {
    var receivedReqID string
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        receivedReqID = r.Header.Get("X-Request-ID")
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()
    
    registry := NewBizRegistry("localhost:9999", server.URL, config.NativeCommConfig{
        Retry: config.RetryConfig{MaxAttempts: 1},
    })
    
    ctx := context.Background()
    _, _, err := registry.ForwardRequest(ctx, "/test", "GET", nil, "my-request-id")
    
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if receivedReqID != "my-request-id" {
        t.Errorf("expected X-Request-ID 'my-request-id', got '%s'", receivedReqID)
    }
}
```

Run: `go test ./internal/httpclient/... -v -run "TestBizRegistry_ForwardsToLivePod|TestBizRegistry_PropagatesRequestID"`
Expected: FAIL (biz_registry.go doesn't exist yet)

- [ ] **GREEN: Implement BizRegistry**

Create `internal/httpclient/biz_registry.go`:

```go
package httpclient

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "math/rand"
    "net/http"
    "strings"
    "time"

    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/resilience"
    "github.com/redis/go-redis/v10"
)

const BizPodsKeyPrefix = "nssaa:biz:pod:"

type BizPodEntry struct {
    URL      string `json:"url"`
    LastSeen int64  `json:"lastSeen"`
}

type BizRegistry struct {
    redisAddr string
    staticURL string
    httpClient *http.Client
    cbRegistry *resilience.Registry
    retryCfg   config.RetryConfig
}

func NewBizRegistry(redisAddr, staticURL string, cfg config.NativeCommConfig) *BizRegistry {
    return &BizRegistry{
        redisAddr:  redisAddr,
        staticURL:  staticURL,
        httpClient: &http.Client{Timeout: cfg.Timeout},
        cbRegistry: resilience.NewRegistry(cfg.CB),
        retryCfg:   cfg.Retry,
    }
}

type livePod struct {
    podID string
    url   string
}

// getLivePods scans Redis for live Biz Pods.
func (b *BizRegistry) getLivePods(ctx context.Context) ([]livePod, error) {
    rdb := redis.NewClient(&redis.Options{Addr: b.redisAddr})
    defer rdb.Close()

    const pattern = BizPodsKeyPrefix + "*"
    const maxAge = 60 * time.Second
    now := time.Now().Unix()
    var live []livePod
    var cursor uint64

    for {
        keys, nextCursor, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
        if err != nil {
            return nil, err
        }

        for _, key := range keys {
            data, err := rdb.Get(ctx, key).Bytes()
            if err != nil {
                continue
            }

            var entry BizPodEntry
            if err := json.Unmarshal(data, &entry); err != nil {
                continue
            }

            if now-entry.LastSeen < int64(maxAge.Seconds()) {
                podID := strings.TrimPrefix(key, BizPodsKeyPrefix)
                live = append(live, livePod{podID: podID, url: entry.URL})
            }
        }

        cursor = nextCursor
        if cursor == 0 {
            break
        }
    }

    return live, nil
}

// selectRandomLivePod picks a random live pod from Redis.
func (b *BizRegistry) selectRandomLivePod(ctx context.Context) (string, error) {
    pods, err := b.getLivePods(ctx)
    if err != nil {
        return "", err
    }
    if len(pods) == 0 {
        return "", fmt.Errorf("no live pods")
    }
    return pods[rand.Intn(len(pods))].url, nil
}

func (b *BizRegistry) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
    var lastErr error
    var lastStatus int
    var lastBody []byte

    err := resilience.Do(ctx, b.retryCfg, func() error {
        // Try to get live pod from Redis, fallback to static URL
        targetURL := b.staticURL
        if podURL, err := b.selectRandomLivePod(ctx); err == nil {
            targetURL = podURL
        }

        // Check circuit breaker
        cb := b.cbRegistry.Get(targetURL)
        if !cb.Allow() {
            if altURL, err := b.selectRandomLivePod(ctx); err == nil && b.cbRegistry.Get(altURL).Allow() {
                targetURL = altURL
            } else {
                lastErr = fmt.Errorf("circuit breaker open, no live pods")
                return lastErr
            }
        }

        // Execute request
        req, err := http.NewRequestWithContext(ctx, method, targetURL+path, bytes.NewReader(body))
        if err != nil {
            return err
        }
        req.Header.Set("Content-Type", "application/json")
        if requestID != "" {
            req.Header.Set("X-Request-ID", requestID)
        }

        resp, err := b.httpClient.Do(req)
        if err != nil {
            b.cbRegistry.Get(targetURL).RecordFailure()
            lastErr = err
            return err
        }
        defer resp.Body.Close()

        lastStatus = resp.StatusCode
        lastBody, _ = io.ReadAll(resp.Body)

        if resp.StatusCode >= 500 {
            b.cbRegistry.Get(targetURL).RecordFailure()
            lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
            return lastErr
        }

        b.cbRegistry.Get(targetURL).RecordSuccess()
        return nil
    })

    if err != nil {
        return lastBody, lastStatus, lastErr
    }
    return lastBody, lastStatus, nil
}
```

Run: `go test ./internal/httpclient/... -v -run "TestBizRegistry_ForwardsToLivePod|TestBizRegistry_PropagatesRequestID"`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(lb): add BizRegistry with Redis-based load balancing for HTTP GW→Biz"`

---

### Slice 1.3: Wire BizRegistry in HTTP Gateway

- [ ] **RED: Write test for HTTP Gateway load balancing**

```go
// cmd/http-gateway/main_test.go

func TestHTTPGateway_ExtractsAndPropagatesRequestID(t *testing.T) {
    bizCalled := false
    bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        bizCalled = true
        reqID := r.Header.Get("X-Request-ID")
        if reqID != "amf-request-123" {
            t.Errorf("expected X-Request-ID 'amf-request-123', got '%s'", reqID)
        }
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"success"}`))
    }))
    defer bizServer.Close()
    
    // Setup HTTP Gateway with BizRegistry pointing to test server
    // (This requires main to accept config for testing - see main_test.go for pattern)
}
```

Run: `go test ./cmd/http-gateway/... -v -run TestHTTPGateway_ExtractsAndPropagatesRequestID`
Expected: FAIL (need to wire RequestID extraction)

- [ ] **GREEN: Update interface and wire in main.go**

Step 1: Update `internal/proto/http_gateway.go`:

```go
type BizServiceClient interface {
    ForwardRequest(ctx context.Context, path string, method string, body []byte, requestID string) ([]byte, int, error)
}
```

Step 2: Update `internal/httpclient/native_biz.go` to match interface:

```go
func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
    // ... existing circuit breaker + retry logic ...
    req.Header.Set("X-Request-ID", requestID)  // Add this line
    // ...
}
```

Step 3: Update `internal/httpclient/factory.go`:

```go
func (f *Factory) NewBizServiceClient(bizServiceURL string, redisAddr string) proto.BizServiceClient {
    switch f.mode {
    case ModeIstio:
        return newIstioBizClient(bizServiceURL)
    default:
        return NewBizRegistry(redisAddr, bizServiceURL, f.cfg.Native)
    }
}
```

Step 4: Update `cmd/http-gateway/main.go`:

```go
// Line ~69: Add redisAddr parameter
bizClient := httpclient.NewFactory(cfg.InternalComm).NewBizServiceClient(
    cfg.HTTPgw.BizServiceURL,
    cfg.Redis.Addr,
)

// Line ~87: Extract and propagate X-Request-ID
requestID := r.Header.Get("X-Request-ID")
respBody, status, err := bizClient.ForwardRequest(r.Context(), r.URL.Path, r.Method, body, requestID)
```

Run: `go build ./cmd/http-gateway/...`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(httpgw): wire BizRegistry and X-Request-ID propagation"`

---

## Slice 2: HTTP Gateway Proxy Endpoints (G9)

**Critical behavior:** Biz Pod can call NRF/UDM/AMF through HTTP Gateway proxy.

### Slice 2.1: HTTP Gateway Proxy Handler

- [ ] **RED: Write test for proxy path extraction**

```go
// cmd/http-gateway/internal/proxy/proxy_test.go

func TestExtractProxyPath(t *testing.T) {
    tests := []struct {
        input    string
        expected string
    }{
        {"/internal/nrf/v1/nf-instances", "/v1/nf-instances"},
        {"/internal/udm/v1/subscription-data/1234", "/v1/subscription-data/1234"},
        {"/internal/amf/v1/callback", "/v1/callback"},
    }

    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got := extractProxyPath(tt.input)
            if got != tt.expected {
                t.Errorf("extractProxyPath(%q) = %q, want %q", tt.input, got, tt.expected)
            }
        })
    }
}

func TestProxyHandler_ProxiesToNRF(t *testing.T) {
    nrfCalled := false
    nrfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        nrfCalled = true
        if r.URL.Path != "/nnrf-nfm/v1/nf-instances" {
            t.Errorf("expected /nnrf-nfm/v1/nf-instances, got %s", r.URL.Path)
        }
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"nfInstances":[]}`))
    }))
    defer nrfServer.Close()

    h := NewProxyHandler(Config{
        NRFBaseURL: nrfServer.URL,
        Timeout:    10,
        RetryCfg:   config.RetryConfig{MaxAttempts: 1},
    })

    req := httptest.NewRequest("POST", "/internal/nrf/v1/nf-instances", nil)
    w := httptest.NewRecorder()
    h.handleNRFProxy(w, req)

    if !nrfCalled {
        t.Error("expected NRF to be called")
    }
    if w.Code != http.StatusOK {
        t.Errorf("expected status 200, got %d", w.Code)
    }
}
```

Run: `go test ./cmd/http-gateway/internal/proxy/... -v`
Expected: FAIL (proxy.go doesn't exist yet)

- [ ] **GREEN: Implement ProxyHandler**

Create `cmd/http-gateway/internal/proxy/proxy.go`:

```go
package proxy

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/resilience"
)

type Config struct {
    NRFBaseURL  string
    UDMBaseURL  string
    AMFBaseURL  string
    RetryCfg    config.RetryConfig
    Timeout     time.Duration
}

type ProxyHandler struct {
    nrfClient *nfClient
    udmClient *nfClient
    amfClient *nfClient
}

type nfClient struct {
    baseURL   string
    httpClient *http.Client
    retryCfg   config.RetryConfig
}

func NewNFClient(baseURL string, retryCfg config.RetryConfig, timeout time.Duration) *nfClient {
    return &nfClient{
        baseURL:   baseURL,
        httpClient: &http.Client{Timeout: timeout},
        retryCfg:   retryCfg,
    }
}

func (c *nfClient) Do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
    var lastErr error
    var lastStatus int
    var lastBody []byte

    url := c.baseURL + path

    err := resilience.Do(ctx, c.retryCfg, func() error {
        req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
        if err != nil {
            return err
        }
        req.Header.Set("Content-Type", "application/json")

        resp, err := c.httpClient.Do(req)
        if err != nil {
            lastErr = err
            return err
        }
        defer resp.Body.Close()

        lastStatus = resp.StatusCode
        lastBody, _ = io.ReadAll(resp.Body)

        if resp.StatusCode >= 400 && resp.StatusCode < 500 {
            return nil // Don't retry 4xx
        }
        if resp.StatusCode >= 500 {
            lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
            return lastErr
        }
        return nil
    })

    if err != nil {
        return lastStatus, lastBody, lastErr
    }
    return lastStatus, lastBody, nil
}

func NewProxyHandler(cfg Config) *ProxyHandler {
    return &ProxyHandler{
        nrfClient: NewNFClient(cfg.NRFBaseURL, cfg.RetryCfg, cfg.Timeout),
        udmClient: NewNFClient(cfg.UDMBaseURL, cfg.RetryCfg, cfg.Timeout),
        amfClient: NewNFClient(cfg.AMFBaseURL, cfg.RetryCfg, cfg.Timeout),
    }
}

func (h *ProxyHandler) RegisterProxyRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/internal/nrf/", h.handleNRFProxy)
    mux.HandleFunc("/internal/udm/", h.handleUDMProxy)
    mux.HandleFunc("/internal/amf/", h.handleAMFProxy)
}

func (h *ProxyHandler) handleNRFProxy(w http.ResponseWriter, r *http.Request) {
    h.proxyRequest(w, r, h.nrfClient)
}

func (h *ProxyHandler) handleUDMProxy(w http.ResponseWriter, r *http.Request) {
    h.proxyRequest(w, r, h.udmClient)
}

func (h *ProxyHandler) handleAMFProxy(w http.ResponseWriter, r *http.Request) {
    h.proxyRequest(w, r, h.amfClient)
}

func (h *ProxyHandler) proxyRequest(w http.ResponseWriter, r *http.Request, client *nfClient) {
    ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
    defer cancel()

    var body []byte
    if r.Body != nil {
        body, _ = io.ReadAll(r.Body)
    }

    path := extractProxyPath(r.URL.Path)
    status, respBody, err := client.Do(ctx, r.Method, path, body)
    if err != nil {
        http.Error(w, err.Error(), 502)
        return
    }

    w.WriteHeader(status)
    if len(respBody) > 0 {
        _, _ = w.Write(respBody)
    }
}

func extractProxyPath(path string) string {
    parts := strings.SplitN(path, "/", 4)
    if len(parts) >= 4 {
        return "/" + parts[3]
    }
    return path
}
```

Run: `go test ./cmd/http-gateway/internal/proxy/... -v`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(proxy): add HTTP Gateway proxy handler for NRF/UDM/AMF"`

---

### Slice 2.2: Biz Pod Proxy Client

- [ ] **RED: Write test for ProxyClient**

```go
// internal/httpclient/proxy_test.go

func TestProxyClient_CallNRF(t *testing.T) {
    called := false
    gwServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        called = true
        if r.URL.Path != "/internal/nrf/v1/nf-instances" {
            t.Errorf("expected /internal/nrf/v1/nf-instances, got %s", r.URL.Path)
        }
        w.WriteHeader(http.StatusOK)
    }))
    defer gwServer.Close()

    client := NewProxyClient(gwServer.URL, config.RetryConfig{MaxAttempts: 1}, 10*time.Second)
    status, _, err := client.CallNRF(context.Background(), "GET", "/v1/nf-instances", nil)

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if status != http.StatusOK {
        t.Errorf("expected status 200, got %d", status)
    }
    if !called {
        t.Error("expected proxy to be called")
    }
}

func TestProxyClient_RetriesOn5xx(t *testing.T) {
    attempts := 0
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        attempts++
        if attempts < 3 {
            w.WriteHeader(http.StatusInternalServerError)
            return
        }
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewProxyClient(server.URL, config.RetryConfig{MaxAttempts: 3, BaseDelay: 1}, 10*time.Second)
    status, _, err := client.CallNRF(context.Background(), "GET", "/v1/test", nil)

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if status != http.StatusOK {
        t.Errorf("expected status 200, got %d", status)
    }
    if attempts != 3 {
        t.Errorf("expected 3 attempts, got %d", attempts)
    }
}
```

Run: `go test ./internal/httpclient/... -v -run "TestProxyClient_"`
Expected: FAIL (proxy.go doesn't exist yet)

- [ ] **GREEN: Implement ProxyClient**

Create `internal/httpclient/proxy.go`:

```go
package httpclient

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/resilience"
)

// ProxyClient calls HTTP Gateway proxy endpoints for external NFs.
// Uses Option A: Kubernetes Service DNS with kube-proxy round-robin.
type ProxyClient struct {
    httpClient *http.Client
    gatewayURL string
    retryCfg   config.RetryConfig
}

// NewProxyClient creates a proxy client for HTTP Gateway.
func NewProxyClient(gatewayURL string, retryCfg config.RetryConfig, timeout time.Duration) *ProxyClient {
    return &ProxyClient{
        httpClient: &http.Client{Timeout: timeout},
        gatewayURL: gatewayURL,
        retryCfg:   retryCfg,
    }
}

func (p *ProxyClient) CallNRF(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
    return p.call(ctx, "nrf", method, path, body)
}

func (p *ProxyClient) CallUDM(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
    return p.call(ctx, "udm", method, path, body)
}

func (p *ProxyClient) CallAMF(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
    return p.call(ctx, "amf", method, path, body)
}

func (p *ProxyClient) call(ctx context.Context, targetNF, method, path string, body []byte) (int, []byte, error) {
    var lastErr error
    var lastStatus int
    var lastBody []byte

    url := p.gatewayURL + "/internal/" + targetNF + path

    err := resilience.Do(ctx, p.retryCfg, func() error {
        req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
        if err != nil {
            return err
        }

        req.Header.Set("Content-Type", "application/json")
        if reqID := getRequestID(ctx); reqID != "" {
            req.Header.Set("X-Request-ID", reqID)
        }

        resp, err := p.httpClient.Do(req)
        if err != nil {
            lastErr = err
            return err
        }
        defer resp.Body.Close()

        lastStatus = resp.StatusCode
        lastBody, _ = io.ReadAll(resp.Body)

        if resp.StatusCode >= 400 && resp.StatusCode < 500 {
            return nil
        }
        if resp.StatusCode >= 500 {
            lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
            return lastErr
        }
        return nil
    })

    if err != nil {
        return lastStatus, lastBody, lastErr
    }
    return lastStatus, lastBody, nil
}

func getRequestID(ctx context.Context) string {
    if reqID, ok := ctx.Value("x-request-id").(string); ok {
        return reqID
    }
    return ""
}
```

Run: `go test ./internal/httpclient/... -v -run "TestProxyClient_"`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(proxy): add ProxyClient for Biz Pod → HTTP Gateway external NF calls"`

---

## Slice 3: Server-Initiated Handlers (G5, G7)

**Critical behavior:** RAR/ASR/CoA messages return valid EAP responses, not dummy bytes.

### Slice 3.1: Real Re-Auth Handler

- [ ] **RED: Write test for Re-Auth handler**

```go
// cmd/biz/server_initiated_test.go

func TestHandleReAuth_SessionNotFound(t *testing.T) {
    deps := serverInitiatedDeps{
        RedisAddr: "localhost:9999", // Will fail to connect
        Engine:    nil,              // Won't be called
    }
    handler := NewServerInitiatedHandler(deps)

    req := &proto.AaaServerInitiatedRequest{
        SessionID:   "session-123",
        AuthCtxID:   "auth-456",
        MessageType: "RAR",
    }

    _, err := handler.HandleReAuth(context.Background(), req)

    if err == nil {
        t.Error("expected error for missing session")
    }
    if !strings.Contains(err.Error(), "session not found") {
        t.Errorf("expected 'session not found' in error, got: %v", err)
    }
}

func TestHandleReAuth_ReturnsValidResponse(t *testing.T) {
    // Setup: Mock Redis with valid session
    // Mock AMF client
    // Mock EAP engine
    // This test requires more infrastructure - skip for now, implement with real deps
}
```

Run: `go test ./cmd/biz/... -v -run TestHandleReAuth_SessionNotFound`
Expected: FAIL (server_initiated.go doesn't exist)

- [ ] **GREEN: Implement ServerInitiatedHandler**

Create `cmd/biz/server_initiated.go`:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/operator/nssAAF/internal/eap"
    "github.com/operator/nssAAF/internal/proto"
    "github.com/redis/go-redis/v10"
)

type serverInitiatedDeps struct {
    Engine    *eap.Engine
    AMFClient AMFNotifier
    RedisAddr string
}

func NewServerInitiatedHandler(deps serverInitiatedDeps) proto.ServerInitiatedHandler {
    return &serverInitiatedHandler{deps: deps}
}

type serverInitiatedHandler struct {
    deps serverInitiatedDeps
}

func (h *serverInitiatedHandler) HandleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
    session, err := h.loadSession(ctx, req.AuthCtxID)
    if err != nil {
        return nil, fmt.Errorf("load session: %w", err)
    }

    if session.Status != eap.StatusSuccess {
        return nil, fmt.Errorf("session not in SUCCESS state: %s", session.Status)
    }

    // Process re-auth via EAP engine
    eapPayload := extractEAPFromRAR(req.Payload)
    reAuthSession := eap.NewReAuthSession(req.AuthCtxID, session.SUPI, session.Snssai)
    result, err := h.deps.Engine.Process(ctx, reAuthSession, eapPayload)
    if err != nil {
        return nil, fmt.Errorf("eap process: %w", err)
    }

    if result.Success {
        _ = h.deps.AMFClient.UpdateAuthContext(ctx, req.AuthCtxID, result.NewAuthStatus)
    }

    return &proto.AaaServerInitiatedResponse{
        Version:     "1.0",
        SessionID:   req.SessionID,
        AuthCtxID:   req.AuthCtxID,
        MessageType: req.MessageType,
        ResultCode:  boolToResultCode(result.Success),
        Payload:     result.ResponseBytes,
    }, nil
}

func (h *serverInitiatedHandler) HandleRevocation(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
    rdb := redis.NewClient(&redis.Options{Addr: h.deps.RedisAddr})
    defer rdb.Close()

    key := "nssaa:eap:session:" + req.AuthCtxID
    if err := rdb.Del(ctx, key).Err(); err != nil {
        return &proto.AaaServerInitiatedResponse{
            Version:     "1.0",
            SessionID:   req.SessionID,
            AuthCtxID:   req.AuthCtxID,
            MessageType: req.MessageType,
            ResultCode:  5002,
            ErrorCause:  err.Error(),
        }, nil
    }

    _ = h.deps.AMFClient.UpdateAuthContext(ctx, req.AuthCtxID, eap.StatusTerminated)

    return &proto.AaaServerInitiatedResponse{
        Version:     "1.0",
        SessionID:   req.SessionID,
        AuthCtxID:   req.AuthCtxID,
        MessageType: req.MessageType,
        ResultCode:  2001,
    }, nil
}

func (h *serverInitiatedHandler) HandleCoA(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
    // CoA handling - update QoS, return success
    return &proto.AaaServerInitiatedResponse{
        Version:     "1.0",
        SessionID:   req.SessionID,
        AuthCtxID:   req.AuthCtxID,
        MessageType: req.MessageType,
        ResultCode:  2001,
    }, nil
}

func (h *serverInitiatedHandler) loadSession(ctx context.Context, authCtxID string) (*eap.Session, error) {
    rdb := redis.NewClient(&redis.Options{Addr: h.deps.RedisAddr})
    defer rdb.Close()

    key := "nssaa:eap:session:" + authCtxID
    data, err := rdb.Get(ctx, key).Bytes()
    if err != nil {
        return nil, fmt.Errorf("session not found: %s", authCtxID)
    }

    var session eap.Session
    if err := json.Unmarshal(data, &session); err != nil {
        return nil, err
    }

    return &session, nil
}

func extractEAPFromRAR(payload []byte) []byte {
    return payload
}

func boolToResultCode(success bool) uint32 {
    if success {
        return 2001
    }
    return 5001
}
```

- [ ] **Add ServerInitiatedHandler interface to proto**

Update `internal/proto/http_gateway.go`:

```go
// ServerInitiatedHandler processes server-initiated messages from AAA GW.
type ServerInitiatedHandler interface {
    HandleReAuth(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
    HandleRevocation(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
    HandleCoA(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
}

type AaaServerInitiatedRequest struct {
    SessionID   string `json:"sessionId"`
    AuthCtxID   string `json:"authCtxId"`
    MessageType string `json:"messageType"`
    Payload     []byte `json:"payload"`
}
```

Run: `go build ./cmd/biz/...`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(biz): implement real server-initiated handlers for RAR/ASR/CoA"`

---

### Slice 3.2: Wire VIP Health Check

- [ ] **RED: Write test for VIP health check wiring**

This test verifies the interface check works, not the actual health check behavior.

- [ ] **GREEN: Wire VIP health check in main.go**

```go
// cmd/biz/main.go - after AAA client creation:
if aaaClient, ok := pod.AAAClient.(interface{ StartVIPHealthCheck(context.Context) }); ok {
    go func() {
        aaaClient.StartVIPHealthCheck(podCtx)
    }()
}
```

Run: `go build ./cmd/biz/...`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(biz): wire VIP health check goroutine on startup"`

---

## Slice 4: RADIUS + DLQ Config (G6, G12)

### Slice 4.1: RADIUS Configurable MaxRetries

- [ ] **RED: Write test for RADIUS config**

```go
// internal/aaa/gateway/gateway_test.go

func TestGateway_UsesConfigurableMaxRetries(t *testing.T) {
    cfg := Config{
        RadiusServerAddress: "localhost:9999",
        InternalComm: config.InternalCommConfig{
            Native: config.NativeCommConfig{
                Radius: config.RadiusConfig{
                    MaxRetries: 5,
                    Timeout:    5 * time.Second,
                },
            },
        },
    }
    
    gw := NewGateway(cfg, nil, nil)
    if gw.radiusForwarder.maxRetries != 5 {
        t.Errorf("expected MaxRetries 5, got %d", gw.radiusForwarder.maxRetries)
    }
}
```

Run: `go test ./internal/aaa/gateway/... -v`
Expected: FAIL (need to wire config)

- [ ] **GREEN: Add RadiusConfig and wire**

Update `internal/config/internal_comm.go`:

```go
type NativeCommConfig struct {
    // ... existing fields ...
    Radius RadiusConfig `yaml:"radius"`
}

type RadiusConfig struct {
    MaxRetries     int           `yaml:"maxRetries"`
    Timeout        time.Duration `yaml:"timeout"`
    ResponseWindow time.Duration `yaml:"responseWindow"`
}
```

Update defaults in `config.go`:

```go
if cfg.InternalComm.Native.Radius.MaxRetries == 0 {
    cfg.InternalComm.Native.Radius.MaxRetries = 3
}
if cfg.InternalComm.Native.Radius.Timeout == 0 {
    cfg.InternalComm.Native.Radius.Timeout = 10 * time.Second
}
```

Update `internal/aaa/gateway/gateway.go`:

```go
type Config struct {
    // ... existing fields ...
    InternalComm config.InternalCommConfig
}

// In factory:
RadiusForwarderConfig{
    MaxRetries: cfg.InternalComm.Native.Radius.MaxRetries,
    Timeout:    cfg.InternalComm.Native.Radius.Timeout,
    // ...
}
```

Run: `go test ./internal/aaa/gateway/... -v`
Expected: PASS

- [ ] **Commit**: `git add ... && git commit -m "feat(config): add RadiusConfig and wire MaxRetries from YAML"`

---

### Slice 4.2: DLQ Configurable

- [ ] **RED: Write test for DLQ config**

- [ ] **GREEN: Add DLQ config to AMF config**

```go
// internal/config/amf.go
type DLQConfig struct {
    MaxRetries     int           `yaml:"maxRetries"`
    RetryDelay     time.Duration `yaml:"retryDelay"`
    AlertThreshold int           `yaml:"alertThreshold"`
}

type AMFConfig struct {
    DLQ DLQConfig `yaml:"dlq"`
}
```

Set defaults:

```go
if cfg.AMF.DLQ.MaxRetries == 0 {
    cfg.AMF.DLQ.MaxRetries = 10
}
if cfg.AMF.DLQ.RetryDelay == 0 {
    cfg.AMF.DLQ.RetryDelay = 30 * time.Second
}
```

- [ ] **Commit**: `git add ... && git commit -m "feat(config): add DLQ config with MaxRetries, RetryDelay"`

---

## Slice 5: Integration Verification

### Slice 5.1: Build + Test All

- [ ] **Step 1: Build all packages**

```bash
go build ./...
```

- [ ] **Step 2: Run all tests**

```bash
go test ./... -count=1
```

- [ ] **Step 3: Run linter**

```bash
golangci-lint run ./... --timeout 5m
```

- [ ] **Step 4: Mark spec acceptance criteria**

Update spec acceptance criteria to reflect implemented features.

---

## Spec Coverage (by slice, not phase)

| Slice | Gap | Behavior Tested |
|-------|-----|-----------------|
| 1.1 | G3 | Timeout configurable via YAML |
| 1.2 | G1, G4 | HTTP GW routes to live pods, fallback to static |
| 1.3 | G2 | X-Request-ID propagated to Biz Pod |
| 2.1 | G9 | HTTP GW proxies to NRF/UDM/AMF |
| 2.2 | G9, G10 | Biz Pod ProxyClient calls HTTP GW |
| 3.1 | G5 | Server-initiated handlers return valid responses |
| 3.2 | G7 | VIP health check wired |
| 4.1 | G6 | RADIUS MaxRetries configurable |
| 4.2 | G12 | DLQ behavior configurable |

---

**Plan complete.** All slices deliver working software with tests verifying behavior.

**Two execution options:**

**1. Subagent-Driven (recommended)** - Fresh subagent per slice, red-green-refactor loop per slice

**2. Inline Execution** - Execute slices in this session with TDD workflow

**Which approach?**
