# NSSAAF NRF Migration: HTTP Gateway as NF Instance

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate NRF registration, heartbeat, and deregistration from Biz Pod to HTTP Gateway. HTTP Gateway becomes the sole NF instance that registers with NRF. Biz Pod discovers UDM via HTTP Gateway's internal discovery API.

**Architecture:**
- HTTP Gateway adds NRF client for self-registration and heartbeat (Phase 1)
- HTTP Gateway exposes internal API `/internal/nf-discovery/{nfType}` (Phase 2)
- Biz Pod uses HTTP Gateway's discovery API instead of direct NRF calls (Phase 3)
- Biz Pod removes all NRF code (Phase 4)
- Docker Compose updated for new configuration (Phase 5)

**Tech Stack:** Go, `internal/nrf`, `internal/nfclient`, YAML config, Docker Compose

---

## Phase 1: Add NRF Client to HTTP Gateway

### Task 1: Create HTTP Gateway Factory

**Files:**
- Create: `cmd/http-gateway/factory.go`
- Reference: `cmd/biz/factory.go:282-308`

- [ ] **Step 1: Create cmd/http-gateway/factory.go**

```go
// Package main is the entry point for the NSSAAF HTTP Gateway.
// Spec: TS 29.526 v18.7.0
package main

import (
    "context"
    "fmt"
    "log/slog"
    "time"

    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/nfclient"
    "github.com/operator/nssAAF/internal/nrf"
    "github.com/operator/nssAAF/internal/resilience"
)

// HTTPGatewayPod holds all dependencies for the HTTP Gateway Pod.
type HTTPGatewayPod struct {
    NRFClient *nrf.Client
    Logger    *slog.Logger
}

// httpGatewayFactory creates HTTPGatewayPod instances with dependency injection.
type httpGatewayFactory struct {
    cfg    *config.Config
    logger *slog.Logger
}

// NewHTTPGatewayFactory creates a new factory.
func NewHTTPGatewayFactory(cfg *config.Config, opts ...HTTPGatewayOption) *httpGatewayFactory {
    f := &httpGatewayFactory{cfg: cfg, logger: slog.Default()}
    for _, opt := range opts {
        opt(f)
    }
    return f
}

// HTTPGatewayOption configures an httpGatewayFactory.
type HTTPGatewayOption func(*httpGatewayFactory)

// WithHTTPGatewayLogger sets the logger on the factory.
func WithHTTPGatewayLogger(logger *slog.Logger) HTTPGatewayOption {
    return func(f *httpGatewayFactory) { f.logger = logger }
}

// newNFRegistry creates the circuit breaker registry for NRF communication.
func (f *httpGatewayFactory) newNFRegistry() *resilience.Registry {
    cfg := f.cfg.InternalComm.Native.CB
    if cfg.FailureThreshold == 0 {
        cfg.FailureThreshold = 3
    }
    if cfg.RecoveryTimeout == 0 {
        cfg.RecoveryTimeout = 10 * time.Second
    }
    if cfg.SuccessThreshold == 0 {
        cfg.SuccessThreshold = 2
    }
    return resilience.NewRegistry(cfg.FailureThreshold, cfg.RecoveryTimeout, cfg.SuccessThreshold)
}

// Build creates a fully initialized HTTPGatewayPod with all dependencies wired.
func (f *httpGatewayFactory) Build(ctx context.Context) (*HTTPGatewayPod, func(), error) {
    cleanup := func() {}

    // Circuit breaker registry for NRF (CB-G1)
    internalNFRegistry := f.newNFRegistry()
    nrfFactory := nfclient.NewFactory(internalNFRegistry)
    nrfClient := nrf.NewClient(f.cfg.NRF, nrfFactory)

    // Load the NF profile YAML (if configured) before starting the heartbeat.
    // Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 1
    if f.cfg.NRF.ProfilePath != "" {
        if err := nrfClient.SetProfilePath(f.cfg.NRF.ProfilePath, f.cfg.NRF.Heartbeat); err != nil {
            f.logger.Warn("failed to load NFProfile; continuing without profile-based registration",
                "path", f.cfg.NRF.ProfilePath,
                "error", err,
            )
        }
    }

    // StartHeartbeat performs the initial PUT registration synchronously and
    // then runs the PATCH heartbeat loop.
    // Failures are non-fatal: StartHeartbeat handles background retries.
    if err := nrfClient.StartHeartbeat(ctx); err != nil {
        f.logger.Warn("nrf heartbeat start failed; NRF registration will retry in background",
            "error", err,
        )
    }

    f.logger.Info("HTTP Gateway NRF client initialized",
        "base_url", f.cfg.NRF.BaseURL,
        "profile_path", f.cfg.NRF.ProfilePath,
    )

    return &HTTPGatewayPod{
        NRFClient: nrfClient,
        Logger:    f.logger,
    }, cleanup, nil
}
```

- [ ] **Step 2: Run build to verify compilation**

Run: `cd /home/tulm/naf3 && go build ./cmd/http-gateway/...`
Expected: No errors (may have warnings about unused imports — those will resolve in Task 2)

- [ ] **Step 3: Commit**

```bash
cd /home/tulm/naf3 && git add cmd/http-gateway/factory.go && git commit -m "feat(http-gw): add factory with NRF client for self-registration"
```

---

### Task 2: Wire NRF Client in HTTP Gateway main.go

**Files:**
- Modify: `cmd/http-gateway/main.go:31-202`

- [ ] **Step 1: Add imports to main.go**

After line 27 (`github.com/operator/nssAAF/internal/tracing`), add:

```go
    "github.com/operator/nssAAF/internal/resilience"
```

- [ ] **Step 2: Replace the main function startup section**

Find the section starting at line 47 (`slog.Info("starting NSSAAF HTTP Gateway"...`) and ending around line 118 (after `shutdownTracing := tracing.Init...`).

Replace that entire block with:

```go
    slog.Info("starting NSSAAF HTTP Gateway",
        "version", cfg.Version,
        "tls_enabled", cfg.HTTPgw.TLS != nil && cfg.HTTPgw.TLS.Cert != "",
        "tls_version", "1.3",
        "istio_mtls", os.Getenv("ISTIO_MTLS") == "1",
    )

    // REQ-22: Initialize JWT validator with NRF JWKS URL.
    // Falls back to default if nrf: section absent from http-gateway.yaml.
    nrfBaseURL := cfg.NRF.BaseURL
    if nrfBaseURL == "" {
        nrfBaseURL = "https://nrf.operator.com"
    }
    jwksURL := nrfBaseURL + "/.well-known/jwks.json"
    if err := auth.Init(auth.TokenValidatorConfig{
        JWKSURL:        jwksURL,
        Issuer:         nrfBaseURL,
        Audiences:      []string{"nnssaaf-nssaa", "nnssaaf-aiw"},
        AllowedNfTypes: []string{"AMF", "AUSF"},
        AllowedScopes:  []string{"nnssaaf-nssaa", "nnssaaf-aiw"},
    }); err != nil {
        // Use a local logger so the error is logged regardless of slog.SetDefault
        // ordering — avoids silent failure if SetDefault is moved in a future refactor.
        tmpLog := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
        tmpLog.Error("auth.Init failed", "error", err)
        os.Exit(1)
    }
    slog.Info("auth initialized", "jwks_url", jwksURL)

    // Phase 1: Initialize NRF client for self-registration and heartbeat.
    // Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 1
    gwFactory := NewHTTPGatewayFactory(cfg, WithHTTPGatewayLogger(logger))
    gwPod, gwCleanup, err := gwFactory.Build(context.Background())
    if err != nil {
        slog.Error("HTTP Gateway factory build failed", "error", err)
        os.Exit(1)
    }
    defer gwCleanup()

    // Per-UE debug subsystem (optional; off by default).
    // Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
    initCtx, initCancel := context.WithCancel(context.Background())
    defer initCancel()
```

- [ ] **Step 3: Add NRF deregistration on shutdown**

Find the shutdown section (lines 194-201) and modify the signal handler:

Replace:
```go
    <-signalReceived()
    slog.Info("shutting down HTTP Gateway")
    if dbg != nil {
        _ = dbg.Close()
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _ = srv.Shutdown(ctx)
```

With:
```go
    <-signalReceived()
    slog.Info("shutting down HTTP Gateway")

    // Deregister from NRF on graceful shutdown.
    // Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 1
    if gwPod.NRFClient != nil {
        nrfCtx, nrfCancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer nrfCancel()
        if err := gwPod.NRFClient.Deregister(nrfCtx, gwPod.NRFClient.NFInstanceID()); err != nil {
            gwPod.Logger.Warn("NRF deregistration failed", "error", err)
        } else {
            gwPod.Logger.Info("NRF deregistration successful")
        }
    }

    if dbg != nil {
        _ = dbg.Close()
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _ = srv.Shutdown(ctx)
```

- [ ] **Step 4: Add buildHandlerDeps field for NRF client**

Modify the `buildHandlerDeps` struct (around line 126) to include the NRF client:

```go
type buildHandlerDeps struct {
    BizClient  proto.BizServiceClient
    AuthCfg    auth.Config
    Debug      *debug.Debug
    NRFClient  *nrf.Client  // Phase 1: NRF client for discovery API
}
```

And update the `buildHandler` call:

```go
    handler := buildHandler(buildHandlerDeps{
        BizClient: bizClient,
        AuthCfg:   authCfg,
        Debug:     dbg,
        NRFClient: gwPod.NRFClient,
    })
```

- [ ] **Step 5: Add nrf import**

Add to the imports section:

```go
    "github.com/operator/nssAAF/internal/nrf"
```

- [ ] **Step 6: Run build to verify**

Run: `go build ./cmd/http-gateway/...`
Expected: Compiles without errors

- [ ] **Step 7: Commit**

```bash
git add cmd/http-gateway/main.go && git commit -m "feat(http-gw): wire NRF client in main.go for self-registration"
```

---

### Task 3: Update HTTP Gateway Config for NRF

**Files:**
- Modify: `compose/configs/http-gateway.yaml`

- [ ] **Step 1: Add NRF configuration section to http-gateway.yaml**

After the `httpGateway:` block (around line 13), add:

```yaml
# NRF integration — HTTP Gateway registers as NSSAAF NF instance (Phase 1)
# REQ-01: NRF registration on startup
# REQ-02: Heartbeat every 5 minutes (negotiated)
nrf:
  baseURL: "${NRF_URL:-http://172.0.3.12:8081}"
  discoverTimeout: 5s
  profilePath: "${NSSAF_NF_PROFILE_PATH:-/etc/nssAAF/nf-profile.yaml}"
  heartbeat:
    initialIntervalSeconds: 30
    acceptNegotiatedInterval: true
    maxConsecutiveFailures: 3
```

- [ ] **Step 2: Commit**

```bash
git add compose/configs/http-gateway.yaml && git commit -m "feat(config): add NRF config to http-gateway.yaml for self-registration"
```

---

## Phase 2: Create Internal Discovery API

### Task 4: Create Discovery Handler in HTTP Gateway

**Files:**
- Create: `cmd/http-gateway/handlers_discovery.go`
- Modify: `cmd/http-gateway/main.go` (buildHandler function)

- [ ] **Step 1: Create cmd/http-gateway/handlers_discovery.go**

```go
// Package main provides HTTP Gateway handlers including internal discovery API.
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 2
package main

import (
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "strings"

    "github.com/operator/nssAAF/internal/nrf"
)

// discoveryHandler handles internal NF discovery requests from Biz Pod.
type discoveryHandler struct {
    nrfClient *nrf.Client
    logger    *slog.Logger
}

// newDiscoveryHandler creates a discovery handler.
func newDiscoveryHandler(nrfClient *nrf.Client, logger *slog.Logger) *discoveryHandler {
    return &discoveryHandler{
        nrfClient: nrfClient,
        logger:    logger,
    }
}

// HandleNFFind handles GET /internal/nf-discovery/{nfType}.
// Discovers an NF instance by type via NRF and returns the NF profile.
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §New Internal API
func (h *discoveryHandler) HandleNFFind(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        writeProblemDetails(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
            "Only GET method is allowed", r.RequestURI)
        return
    }

    // Extract nfType from path: /internal/nf-discovery/{nfType}
    path := strings.TrimPrefix(r.URL.Path, "/internal/nf-discovery/")
    nfType := strings.TrimSuffix(path, "/")
    if nfType == "" || r.URL.Path == "/internal/nf-discovery/" {
        writeProblemDetails(w, http.StatusBadRequest, "INVALID_NF_TYPE",
            "NF type is required in path /internal/nf-discovery/{nfType}", r.RequestURI)
        return
    }

    // Normalize NF type to NRF format (uppercase)
    nfType = strings.ToUpper(nfType)

    h.logger.Info("NF discovery request",
        "nf_type", nfType,
        "request_id", r.Header.Get("X-Request-ID"),
    )

    // Discover the NF via NRF client
    // Spec: TS 29.510 §5.3 (NF discovery query)
    profile, err := h.nrfClient.FindNF(r.Context(), nfType)
    if err != nil {
        h.logger.Warn("NF discovery failed", "nf_type", nfType, "error", err)
        writeProblemDetails(w, http.StatusServiceUnavailable, "NRF_UNAVAILABLE",
            fmt.Sprintf("Failed to discover %s: %v", nfType, err), r.RequestURI)
        return
    }

    if profile == nil {
        h.logger.Info("NF not found in NRF", "nf_type", nfType)
        writeProblemDetails(w, http.StatusNotFound, "NF_NOT_FOUND",
            fmt.Sprintf("No serving %s found in NRF", nfType), r.RequestURI)
        return
    }

    // Return NF profile as JSON
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(profile); err != nil {
        h.logger.Error("failed to encode NF profile", "error", err)
    }
}

// writeProblemDetails writes a RFC 7807 Problem Details response.
func writeProblemDetails(w http.ResponseWriter, status int, cause, detail, instance string) {
    problem := struct {
        Type   string `json:"type"`
        Title  string `json:"title"`
        Status int    `json:"status"`
        Detail string `json:"detail"`
    }{
        Type:   fmt.Sprintf("https://nrf.operator.com/problem/%s", strings.ToLower(cause)),
        Title:  cause,
        Status: status,
        Detail: detail,
    }
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(problem)
}
```

- [ ] **Step 2: Add FindNF method to NRF client**

The discovery handler calls `h.nrfClient.FindNF()`, but this method doesn't exist yet in `internal/nrf/client.go`. Add it:

**Modify internal/nrf/client.go**, after the `DiscoverAMF` method (around line 410), add:

```go
// FindNF discovers an NF instance by type and returns its profile.
// REQ-03 / TS 29.510 §5.3.
func (c *Client) FindNF(ctx context.Context, nfType string) (*NFProfile, error) {
    // Normalize NF type
    normalizedType := NFType(nfType)

    path := fmt.Sprintf("/nnrf-disc/v1/nf-instances?target-nf-type=%s", normalizedType)
    status, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
    if err != nil {
        return nil, fmt.Errorf("nrf: find nf: %w", err)
    }
    if status != http.StatusOK {
        return nil, fmt.Errorf("nrf: find nf status %d: %s", status, respBody)
    }

    var result struct {
        NFInstances []NFProfile `json:"nfInstances"`
    }
    if err := json.Unmarshal(respBody, &result); err != nil {
        return nil, fmt.Errorf("nrf: decode find result: %w", err)
    }

    if len(result.NFInstances) == 0 {
        return nil, nil // Not found, return nil without error
    }

    // Return the first matching NF instance
    return &result.NFInstances[0], nil
}
```

- [ ] **Step 3: Add NFType constant and NFProfile struct to nrf package**

Check if `NFType` and `NFProfile` are already defined in the nrf package. If not, add the necessary types:

**Modify internal/nrf/types.go** (create if not exists):

```go
package nrf

// NFType represents the type of Network Function.
// Spec: TS 29.510 §5.4.2
type NFType string

const (
    NFTypeNRF      NFType = "NRF"
    NFTypeNSSAAF   NFType = "NSSAAF"
    NFTypeUDM      NFType = "UDM"
    NFTypeAMF      NFType = "AMF"
    NFTypeAUSF     NFType = "AUSF"
    NFTypeSMF      NFType = "SMF"
    NFTypeUPF      NFType = "UPF"
    NFTypePCF      NFType = "PCF"
    NFTypeUDMF     NFType = "UDMF"
    NFTypeAUSFType NFType = "AUSF"
)

// NFStatus represents the operational status of an NF.
// Spec: TS 29.510 §5.4.3
type NFStatus string

const (
    NFStatusRegistered  NFStatus = "REGISTERED"
    NFStatusSuspended   NFStatus = "SUSPENDED"
    NFStatusUndiscovery NFStatus = "UNDISCOVERABLE"
)

// NFProfile represents a Network Function profile from NRF.
// Spec: TS 29.510 §6.1
type NFProfile struct {
    NFInstanceID   string            `json:"nfInstanceId"`
    NFType         NFType            `json:"nfType"`
    NFStatus       NFStatus          `json:"nfStatus"`
    FQDN           string            `json:"fqdn,omitempty"`
    IPv4Addresses  []string         `json:"ipv4Addresses,omitempty"`
    IPv6Addresses  []string         `json:"ipv6Addresses,omitempty"`
    Ports          []NFPort         `json:"ports,omitempty"`
    Services       []NFService      `json:"services,omitempty"`
    HeartBeatTimer int              `json:"heartBeatTimer,omitempty"`
    Priority       int              `json:"priority,omitempty"`
    Capacity       int              `json:"capacity,omitempty"`
    Load           int              `json:"load,omitempty"`
}

// NFPort represents a port configuration for an NF.
// Spec: TS 29.510 §6.1.6.8
type NFPort struct {
    Port      int    `json:"port"`
    Protocol  string `json:"protocol"`
    Security  string `json:"security,omitempty"`
}

// NFService represents a service exposed by an NF.
// Spec: TS 29.510 §6.1.6.9
type NFService struct {
    ServiceName string       `json:"serviceName"`
    Versions    []NFVersion `json:"versions,omitempty"`
}

// NFVersion represents a version of an NF service.
// Spec: TS 29.510 §6.1.6.9
type NFVersion struct {
    APIVersion   string `json:"apiVersion"`
    FullVersion  string `json:"fullVersion,omitempty"`
}
```

**Note:** If `NFProfile`, `NFType`, and `NFStatus` already exist in the nrf package (check `internal/nrf/profile.go` or similar), skip this step and use the existing types.

- [ ] **Step 4: Register internal discovery route in buildHandler**

Modify `cmd/http-gateway/main.go`, the `buildHandler` function. Add the discovery handler:

After the `// Health endpoints` comment block, add:

```go
    // Internal NF discovery API — no auth required (internal network only)
    // Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 2
    discHandler := newDiscoveryHandler(deps.NRFClient, slog.Default())
    mux.HandleFunc("/internal/nf-discovery/", discHandler.HandleNFFind)
```

- [ ] **Step 5: Add slog import if needed**

Ensure `log/slog` is imported in main.go. It should already be there from the original file.

- [ ] **Step 6: Run build to verify**

Run: `go build ./cmd/http-gateway/... && go build ./internal/nrf/...`
Expected: Compiles without errors

- [ ] **Step 7: Commit**

```bash
git add cmd/http-gateway/handlers_discovery.go internal/nrf/client.go internal/nrf/types.go cmd/http-gateway/main.go && git commit -m "feat(http-gw): add internal NF discovery API /internal/nf-discovery/{nfType}"
```

---

### Task 5: Create Discovery Client Package for Biz Pod

**Files:**
- Create: `internal/discovery/client.go`
- Create: `internal/discovery/client_test.go`

- [ ] **Step 1: Create internal/discovery/client.go**

```go
// Package discovery provides NF discovery client for Biz Pod to discover
// NFs (UDM, AMF) via HTTP Gateway's internal discovery API.
//
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 2
package discovery

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/operator/nssAAF/internal/nrf"
)

// NFDiscoveryClient is the interface for NF discovery.
// Allows swapping between HTTP Gateway discovery and direct NRF calls.
type NFDiscoveryClient interface {
    // FindNF discovers an NF instance by type and returns its profile.
    FindNF(ctx context.Context, nfType string) (*nrf.NFProfile, error)
}

// httpDiscoveryClient implements NFDiscoveryClient using HTTP Gateway's internal API.
type httpDiscoveryClient struct {
    baseURL string
    client  *http.Client
}

// NewClient creates a new HTTP-based NF discovery client.
func NewClient(baseURL string) NFDiscoveryClient {
    return &httpDiscoveryClient{
        baseURL: baseURL,
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
    }
}

// FindNF discovers an NF instance by type via HTTP Gateway's internal API.
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §New Internal API
func (c *httpDiscoveryClient) FindNF(ctx context.Context, nfType string) (*nrf.NFProfile, error) {
    url := fmt.Sprintf("%s/internal/nf-discovery/%s", c.baseURL, nfType)

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, fmt.Errorf("discovery: create request: %w", err)
    }
    req.Header.Set("Accept", "application/json")

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("discovery: http request: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("discovery: read body: %w", err)
    }

    if resp.StatusCode == http.StatusNotFound {
        return nil, fmt.Errorf("discovery: %s not found", nfType)
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("discovery: unexpected status %d: %s", resp.StatusCode, body)
    }

    var profile nrf.NFProfile
    if err := json.Unmarshal(body, &profile); err != nil {
        return nil, fmt.Errorf("discovery: decode profile: %w", err)
    }

    return &profile, nil
}
```

- [ ] **Step 2: Create internal/discovery/client_test.go**

```go
package discovery

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/operator/nssAAF/internal/nrf"
)

func TestHTTPRemoteDiscoveryClient_FindNF(t *testing.T) {
    t.Run("returns NF profile on 200 OK", func(t *testing.T) {
        profile := &nrf.NFProfile{
            NFInstanceID: "test-udm-instance",
            NFType:       nrf.NFTypeUDM,
            NFStatus:     nrf.NFStatusRegistered,
            FQDN:         "udm.operator.com",
            IPv4Addresses: []string{"172.0.3.13"},
        }

        var receivedMethod, receivedPath string
        ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            receivedMethod = r.Method
            receivedPath = r.URL.Path
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            _ = json.NewEncoder(w).Encode(profile)
        }))
        defer ts.URL

        client := NewClient(ts.URL)
        result, err := client.FindNF(context.Background(), "UDM")

        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if result.NFInstanceID != "test-udm-instance" {
            t.Errorf("NFInstanceID = %q, want %q", result.NFInstanceID, "test-udm-instance")
        }
        if receivedMethod != http.MethodGet {
            t.Errorf("method = %q, want %q", receivedMethod, http.MethodGet)
        }
        if receivedPath != "/internal/nf-discovery/UDM" {
            t.Errorf("path = %q, want %q", receivedPath, "/internal/nf-discovery/UDM")
        }
    })

    t.Run("returns error on 404 Not Found", func(t *testing.T) {
        ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Content-Type", "application/problem+json")
            w.WriteHeader(http.StatusNotFound)
            _ = json.NewEncoder(w).Encode(map[string]string{
                "title":  "NF_NOT_FOUND",
                "detail": "No serving UDM found in NRF",
            })
        }))
        defer ts.URL

        client := NewClient(ts.URL)
        _, err := client.FindNF(context.Background(), "UDM")

        if err == nil {
            t.Fatal("expected error, got nil")
        }
    })

    t.Run("returns error on non-200 status", func(t *testing.T) {
        ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.WriteHeader(http.StatusServiceUnavailable)
        }))
        defer ts.URL

        client := NewClient(ts.URL)
        _, err := client.FindNF(context.Background(), "UDM")

        if err == nil {
            t.Fatal("expected error, got nil")
        }
    })

    t.Run("handles context cancellation", func(t *testing.T) {
        ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Slow response
            select {}
        }))
        defer ts.URL

        client := NewClient(ts.URL)
        ctx, cancel := context.WithCancel(context.Background())
        cancel() // Cancel immediately

        _, err := client.FindNF(ctx, "UDM")
        if err == nil {
            t.Fatal("expected error from cancelled context, got nil")
        }
    })
}
```

- [ ] **Step 3: Run tests to verify**

Run: `go test ./internal/discovery/... -v`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/discovery/client.go internal/discovery/client_test.go && git commit -m "feat(discovery): add NF discovery client package for HTTP Gateway internal API"
```

---

## Phase 3: Update Biz Pod to Use Discovery API

### Task 6: Update UDM Client to Use Discovery Client

**Files:**
- Modify: `internal/udm/udm.go`

- [ ] **Step 1: Replace UDM client to use discovery client**

**Modify internal/udm/udm.go:**

Replace the `Client` struct and `NewClient` function with:

```go
// Client is the UDM Nudm_UECM client.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
type Client struct {
    baseURL string
    disc    discovery.NFDiscoveryClient
    factory *nfclient.Factory
}

// NewClient creates a new UDM client.
// discoveryClient is used for on-demand NF discovery when baseURL is not configured.
func NewClient(cfg config.UDMConfig, factory *nfclient.Factory, discoveryClient discovery.NFDiscoveryClient) *Client {
    return &Client{
        baseURL: cfg.BaseURL,
        disc:    discoveryClient,
        factory: factory,
    }
}
```

Replace the `discoverBaseURL` method with:

```go
func (c *Client) discoverBaseURL(ctx context.Context, supi string) (string, error) {
    if c.baseURL != "" {
        return c.baseURL, nil
    }
    if c.disc == nil {
        return "", errors.New("udm: no baseURL and no discovery client configured")
    }

    // Discover UDM via HTTP Gateway's internal discovery API.
    // Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 3
    profile, err := c.disc.FindNF(ctx, "UDM")
    if err != nil {
        return "", fmt.Errorf("udm: discover UDM: %w", err)
    }

    // Extract base URL from NF profile.
    // Prefer first IPv4 address + first port.
    if len(profile.IPv4Addresses) == 0 {
        return "", errors.New("udm: no IPv4 addresses in UDM profile")
    }
    ip := profile.IPv4Addresses[0]

    var port int
    if len(profile.Ports) > 0 {
        port = profile.Ports[0].Port
    } else {
        port = 8081 // Default N59 port
    }

    return fmt.Sprintf("http://%s:%d", ip, port), nil
}
```

- [ ] **Step 2: Add discovery import**

Add to imports:

```go
    "github.com/operator/nssAAF/internal/discovery"
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/udm/...`
Expected: Compiles without errors

- [ ] **Step 4: Run tests**

Run: `go test ./internal/udm/... -v`
Expected: All tests pass (may need to update existing tests for new constructor signature)

- [ ] **Step 5: Commit**

```bash
git add internal/udm/udm.go && git commit -m "refactor(udm): use discovery.NFDiscoveryClient instead of direct NRF client"
```

---

### Task 7: Update Biz Factory to Use Discovery Client

**Files:**
- Modify: `cmd/biz/factory.go`

- [ ] **Step 1: Update Biz factory to create discovery client and pass to UDM**

Find the section around line 283-310 in `cmd/biz/factory.go` (where NRF client, UDM client, AUSF client are created).

Replace the NRF client section with:

```go
    // ─── NF clients with circuit breakers (CB-G1) ─────────────────────
    // Phase 3: Biz Pod discovers NFs via HTTP Gateway's internal API
    // instead of direct NRF calls.
    // Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 3
    internalNFRegistry := resilience.NewRegistry(cbCfg.FailureThreshold, cbCfg.RecoveryTimeout, cbCfg.SuccessThreshold)

    // HTTP Gateway URL for NF discovery
    httpGatewayDiscoveryURL := f.cfg.HTTPgw.DiscoveryURL
    if httpGatewayDiscoveryURL == "" {
        httpGatewayDiscoveryURL = "http://172.0.3.14:8443" // Default HTTP Gateway URL
    }
    discClient := discovery.NewClient(httpGatewayDiscoveryURL)

    udmClient := udm.NewClient(f.cfg.UDM, nil, discClient)
    ausfClient := ausf.NewClient(f.cfg.AUSF, nfclient.NewFactory(internalNFRegistry))
```

- [ ] **Step 2: Add discovery import to biz factory.go**

Add to imports section:

```go
    "github.com/operator/nssAAF/internal/discovery"
```

- [ ] **Step 3: Add HTTPgwConfig with DiscoveryURL to config**

**Modify internal/config/config.go:**

Find the `HTTPgwConfig` struct (around line 152-157) and add `DiscoveryURL`:

```go
// HTTPgwConfig holds HTTP Gateway configuration.
type HTTPgwConfig struct {
    BizServiceURL   string      `yaml:"bizServiceUrl"` // http://svc-nssaa-biz:8080
    DiscoveryURL   string      `yaml:"discoveryUrl"`  // http://svc-nssaa-http-gw:8443
    Auth           *AuthConfig `yaml:"auth,omitempty"`
    TLS            *TLSConfig  `yaml:"tls,omitempty"`
}
```

- [ ] **Step 4: Run build to verify**

Run: `go build ./cmd/biz/...`
Expected: Compiles without errors

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/biz/... -v 2>&1 | head -50`
Expected: Tests pass or failures are unrelated to this change

- [ ] **Step 6: Commit**

```bash
git add cmd/biz/factory.go internal/config/config.go && git commit -m "refactor(biz): use discovery client for NF discovery via HTTP Gateway"
```

---

## Phase 4: Remove NRF Code from Biz Pod

### Task 8: Remove NRF Client from Biz Pod

**Files:**
- Modify: `cmd/biz/factory.go`
- Modify: `cmd/biz/main.go`

- [ ] **Step 1: Remove NRF factory, client, heartbeat from Biz factory.go**

In `cmd/biz/factory.go`, find and remove the following code sections:

**Section A - Remove NRF factory variable (around line 283):**
```go
    // ─── NF clients with circuit breakers (CB-G1) ─────────────────────
    // (already replaced in Task 7)
```

**Section B - Remove nrfFactory and nrfClient creation (lines 283-308):**
The old code that was:
```go
    nrfFactory := nfclient.NewFactory(internalNFRegistry)
    nrfClient := nrf.NewClient(f.cfg.NRF, nrfFactory)

    // Load the NF profile YAML...
    if f.cfg.NRF.ProfilePath != "" {
        if err := nrfClient.SetProfilePath(...); err != nil {...}
    }

    // StartHeartbeat...
    if err := nrfClient.StartHeartbeat(context.Background()); err != nil {...}
```

**These should already be removed in Task 7's replacement. Verify and clean up any remaining NRF references.**

- [ ] **Step 2: Remove NRF-related imports from factory.go**

Remove from imports:
```go
    "github.com/operator/nssAAF/internal/nfclient"  // Keep if still needed for ausfClient
    "github.com/operator/nssAAF/internal/nrf"
```

Remove `nfclient` only if it's no longer used anywhere. If `ausfClient` still uses `nfclient.NewFactory`, keep the import.

- [ ] **Step 3: Remove NRF client from BizPod struct and Close method**

**Modify `cmd/biz/factory.go` — BizPod struct (around line 39-51):**

Remove `NRFClient *nrf.Client` from the struct.

**Modify `cmd/biz/factory.go` — Build return statement (around line 429-440):**

Remove `NRFClient: nrfClient,` from the return statement.

**Modify `cmd/biz/factory.go` — Close method (around line 443-469):**

Remove the NRF deregistration block:
```go
    if bp.NRFClient != nil {
        nrfCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = bp.NRFClient.Deregister(nrfCtx, bp.NRFClient.NFInstanceID())
    }
```

- [ ] **Step 4: Remove NRF health check from Biz main.go**

**Modify `cmd/biz/main.go`:**

Find and remove NRF-related code. Check for any NRF health checks or startup NRF registration code. Update the readiness handler if it checks NRF status.

- [ ] **Step 5: Run build to verify**

Run: `go build ./cmd/biz/...`
Expected: Compiles without errors. If there are errors, resolve them by removing stale references.

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -short 2>&1 | tail -20`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add cmd/biz/factory.go cmd/biz/main.go && git commit -m "refactor(biz): remove NRF client now that HTTP Gateway handles registration"
```

---

## Phase 5: Update Docker Compose

### Task 9: Update Docker Compose Configuration

**Files:**
- Modify: `compose/fullchain-dev-base.yaml`
- Modify: `compose/configs/biz.yaml`

- [ ] **Step 1: Add NF profile volume mount to HTTP Gateway service**

**Modify `compose/fullchain-dev-base.yaml`:**

Find the `http-gateway` service definition and add the volume mount and NRF environment variables:

```yaml
  http-gateway:
    image: ${IMAGE_PREFIX:-nssAAF}/http-gateway:${IMAGE_TAG:-latest}
    volumes:
      - ./compose/configs/http-gateway.yaml:/etc/nssAAF/http-gateway.yaml:ro
      - ./compose/configs/nf-profile.yaml:/etc/nssAAF/nf-profile.yaml:ro  # Add this line
    environment:
      BIZ_URL: "http://biz:8080"
      NRF_URL: "http://nrf:8081"                          # Add this line
      NSSAF_NF_PROFILE_PATH: "/etc/nssAAF/nf-profile.yaml"  # Add this line
      REDIS_ADDR: "redis:6379"
      ISTIO_MTLS: "0"
    depends_on:
      nrf:
        condition: service_healthy                          # Add this line
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8443/healthz/"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
    networks:
      nssaa-net:
        ipv4_address: 172.0.3.14
```

- [ ] **Step 2: Remove NRF config from Biz service**

**Modify `compose/fullchain-dev-base.yaml`:**

In the `biz` service definition, remove the NRF-related environment variables and volume mounts:

```yaml
  biz:
    image: ${IMAGE_PREFIX:-nssAAF}/biz:${IMAGE_TAG:-latest}
    volumes:
      - ./compose/configs/biz.yaml:/etc/nssAAF/biz.yaml:ro
      # Remove: nf-profile.yaml mount (no longer needed)
    environment:
      POSTGRES_HOST: "postgres"
      REDIS_ADDR: "redis:6379"
      # Remove: NRF_URL, NSSAF_NF_PROFILE_PATH from here
      AAA_GW_URL: "http://aaa-gateway:9090"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      # Remove: nrf depends_on (no longer needed)
```

- [ ] **Step 3: Remove NRF section from biz.yaml config**

**Modify `compose/configs/biz.yaml`:**

Remove the entire `nrf:` section (lines 55-64):

```yaml
# REMOVE THIS ENTIRE SECTION:
# NRF integration — REQ-01, REQ-02, REQ-03
nrf:
  baseURL: "${NRF_URL:-http://172.0.3.12:8081}"
  discoverTimeout: 5s
  profilePath: "${NSSAF_NF_PROFILE_PATH:-/etc/nssAAF/nf-profile.yaml}"
  heartbeat:
    initialIntervalSeconds: 30
    acceptNegotiatedInterval: true
    maxConsecutiveFailures: 3
```

Add HTTP Gateway discovery URL instead:

```yaml
# HTTP Gateway URL for NF discovery (Phase 3)
httpGateway:
  discoveryUrl: "${HTTP_GW_URL:-http://172.0.3.14:8443}"
```

- [ ] **Step 4: Create nf-profile.yaml config file**

**Create `compose/configs/nf-profile.yaml`:**

```yaml
# NSSAAF NF Profile for HTTP Gateway
# Loaded by HTTP Gateway at startup for NRF self-registration
# Spec: TS 29.510 §6.1; docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md

nfInstanceId: "${NF_INSTANCE_ID:-}"
nfType: NSSAAF
nfStatus: REGISTERED
fqdn: "http-gateway.nssaa.svc.cluster.local"
ipv4Addresses:
  - "${HTTP_GW_IP:-172.0.3.14}"
ports:
  - port: 8443
    protocol: "HTTP/2"
    security: "TLS"
services:
  - serviceName: nnssaaf-nssaa
    versions:
      - apiVersion: v1
        fullVersion: 1.0.0
  - serviceName: nnssaaf-aiw
    versions:
      - apiVersion: v1
        fullVersion: 1.0.0
heartBeatTimer: 30
priority: 100
capacity: 1000
load: 0
```

- [ ] **Step 5: Verify compose file syntax**

Run: `docker compose -f compose/fullchain-dev-base.yaml config --quiet`
Expected: No errors (command succeeds silently)

- [ ] **Step 6: Commit**

```bash
git add compose/fullchain-dev-base.yaml compose/configs/biz.yaml compose/configs/nf-profile.yaml && git commit -m "feat(compose): update for HTTP Gateway NRF registration, remove from Biz Pod"
```

---

## Phase 6: End-to-End Verification

### Task 10: Verify Migration

- [ ] **Step 1: Build all binaries**

Run:
```bash
go build ./cmd/http-gateway/... && go build ./cmd/biz/... && go build ./...
echo "All builds successful"
```
Expected: All compile without errors

- [ ] **Step 2: Run unit tests**

Run: `go test ./... -short 2>&1 | tail -30`
Expected: All tests pass

- [ ] **Step 3: Verify lint**

Run: `golangci-lint run ./cmd/http-gateway/... ./cmd/biz/... ./internal/discovery/... 2>&1 | head -30`
Expected: No critical errors

- [ ] **Step 4: Update roadmap status**

Mark Phase 4 as complete in relevant roadmap docs.

- [ ] **Step 5: Final commit**

```bash
git status
```
Expected: All migration changes committed

---

## Rollback Plan

If migration fails at Phase 3 or later:

1. Revert Biz Pod changes: `git revert <commit-hash>` for Task 7 and Task 8
2. Restore Biz NRF config in `compose/configs/biz.yaml`
3. Restore NRF volume mount for Biz Pod in `compose/fullchain-dev-base.yaml`
4. Test with Biz Pod NRF client restored

---

## File Summary

| Phase | File | Action |
|-------|------|--------|
| 1 | `cmd/http-gateway/factory.go` | Create |
| 1 | `cmd/http-gateway/main.go` | Modify |
| 1 | `compose/configs/http-gateway.yaml` | Modify |
| 2 | `cmd/http-gateway/handlers_discovery.go` | Create |
| 2 | `internal/nrf/client.go` | Modify |
| 2 | `internal/nrf/types.go` | Create (if needed) |
| 2 | `internal/discovery/client.go` | Create |
| 2 | `internal/discovery/client_test.go` | Create |
| 3 | `internal/udm/udm.go` | Modify |
| 3 | `internal/config/config.go` | Modify |
| 3 | `cmd/biz/factory.go` | Modify |
| 4 | `cmd/biz/factory.go` | Modify |
| 4 | `cmd/biz/main.go` | Modify |
| 5 | `compose/fullchain-dev-base.yaml` | Modify |
| 5 | `compose/configs/biz.yaml` | Modify |
| 5 | `compose/configs/nf-profile.yaml` | Create |
