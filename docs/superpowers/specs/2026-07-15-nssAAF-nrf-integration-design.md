# NSSAAF NRF Integration Design

**Status:** Draft for review
**Date:** 2026-07-15
**Author:** Design working session

## 1. Purpose

This spec defines the NRF (Network Repository Function) integration for NSSAAF, enabling:

1. **NF Registration** — Register NSSAAF's NFProfile with NRF
2. **NF Discovery** — Discover other NFs (AMF, AUSF, UDM) via NRF
3. **NF Heartbeat** — Keep registration alive with periodic PATCH updates
4. **OAuth2 Token** — Obtain access tokens for SBI calls to other NFs

### Scope

- **In scope:** NRF client library, NFProfile management, heartbeat manager, OAuth2 token caching
- **Out of scope:** NRF mock for testing (separate spec), UDM/AUSF discovery consumers

### Spec Reference

TS 29.510 V18.11.0 (2026-03) — NRF interfaces for 5G SBA

---

## 2. Architecture

### 2.1 Component Placement

NRF integration lives in the **HTTP Gateway** (not Biz Pod). Rationale: the HTTP Gateway owns the external SBI interface and lifecycle management.

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP Gateway                            │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────┐ │
│  │   NRF Client    │  │  NFProfile Mgr  │  │ Heartbeat   │ │
│  │  - Register     │  │  - Load from    │  │ Manager     │ │
│  │  - Heartbeat    │  │    YAML         │  │ - Negotiate │ │
│  │  - Deregister   │  │  - Validate     │  │ - Retry     │ │
│  │  - OAuth2 token │  │  - Merge env    │  │ - Re-reg    │ │
│  └─────────────────┘  └─────────────────┘  └─────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Package Structure

```
internal/
  nrf/
    client.go        # HTTP client for NRF SBI calls
    types.go         # NRF-specific types (NFProfile, etc.)
    config.go        # Configuration structs
    profile.go       # NFProfile builder from YAML
    heartbeat.go     # HeartbeatManager
    discovery.go     # NFDiscovery with caching
    token.go         # OAuth2 token cache
    client_test.go
    profile_test.go
    heartbeat_test.go
```

---

## 3. Configuration

### 3.1 NFProfile YAML

```yaml
# config/nf-profile.yaml
nfProfile:
  instanceId: "550e8400-e29b-41d4-a716-446655440000"

  # Identity
  instanceName: "nssAAF-gw-001"
  fqdn: "nssAAF.operator.com"
  locality: "dc-1"
  nfSetId: "nssAAF-set-001"

  # Network addresses
  ipv4Addresses:
    - "10.0.1.50"
    - "10.0.2.50"

  # PLMN configuration
  plmnList:
    - mcc: "208"
      mnc: "001"
    - mcc: "208"
      mnc: "93"

  # S-NSSAI support
  snssais:
    - sst: 1
      sd: "000001"
    - sst: 2

  # NSSAAF-specific info
  nssaafInfo:
    supiRanges:
      - start: "imsi-208010000000001"
        end: "imsi-208019999999999"
        pattern: "^imsi-20801[0-9]{8}$"
        size: "LARGE"
    internalGroupIdentifiersRanges:
      - start: "group-001"
        end: "group-999"

  # Services
  nfServices:
    nnssaaf-nssaa:
      serviceInstanceId: "nnssaaf-nssaa-001"
      apiPrefix: "/nnssaaf-nssaa/v1"
      allowedNfTypes: ["AMF"]
      capacity: 1000
      priority: 100
      supportedFeatures: "3GPP-R18-NSSAA-REAUTH-REVOC"
    nnssaaf-aiw:
      serviceInstanceId: "nnssaaf-aiw-001"
      apiPrefix: "/nnssaaf-aiw/v1"
      allowedNfTypes: ["AUSF"]
      capacity: 1000
      priority: 100
      supportedFeatures: "3GPP-R18-AIW"

  # Custom capabilities
  customInfo:
    supportedAaaProtocols: ["RADIUS", "DIAMETER"]
    maxEapRounds: 20
    eapTimeoutSeconds: 30
```

### 3.2 NRF Client Configuration

```yaml
nrf:
  baseUrl: "https://nrf.operator.com"
  timeoutSeconds: 30

  accessToken:
    enabled: true
    authServer: "https://nrf.operator.com/oauth2/token"
    clientId: "nssAAF-client"
    clientSecret: "${NRF_CLIENT_SECRET}"
    scope: "nnrf-nfm"

  heartbeat:
    initialIntervalSeconds: 300
    acceptNegotiatedInterval: true
    maxConsecutiveFailures: 3

  discoveryCache:
    enabled: true
    defaultTTLSeconds: 3600
```

---

## 4. NRF Client

### 4.1 HTTP Client

```go
// internal/nrf/client.go

type NRFClient struct {
    baseURL    string
    httpClient *http.Client
    tokenCache *TokenCache
}

func NewNRFClient(cfg NRFConfig) *NRFClient {
    return &NRFClient{
        baseURL: cfg.BaseURL,
        httpClient: &http.Client{
            Timeout: cfg.Timeout,
            Transport: &http.Transport{
                MaxConnsPerHost: 10,
            },
        },
        tokenCache: NewTokenCache(cfg.AccessToken),
    }
}
```

### 4.2 NF Registration

```go
// Register creates/updates NFProfile with NRF.
// Returns negotiated heartbeat interval and ETag.
func (c *NRFClient) Register(ctx context.Context, profile *NFProfile) (heartbeatInterval time.Duration, etag string, err error) {
    body, err := json.Marshal(profile)
    if err != nil {
        return 0, "", fmt.Errorf("marshaling profile: %w", err)
    }

    url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", c.baseURL, profile.NFInstanceID)
    req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
    if err != nil {
        return 0, "", err
    }

    req.Header.Set("Content-Type", "application/json")
    if err := c.addAuth(ctx, req); err != nil {
        return 0, "", err
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return 0, "", fmt.Errorf("register request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
        return 0, "", parseNRFError(resp)
    }

    // Parse HeartBeat-Interval header (seconds)
    interval := parseHeartbeatInterval(resp.Header.Get("HeartBeat-Interval"))
    etag := resp.Header.Get("ETag")

    return interval, etag, nil
}
```

### 4.3 NF Heartbeat

```go
// Heartbeat sends PATCH to keep registration alive.
func (c *NRFClient) Heartbeat(ctx context.Context, instanceID, etag string) (newEtag string, err error) {
    patch := fmt.Sprintf(`{"nfStatus":"REGISTERED"}`)

    url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", c.baseURL, instanceID)
    req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, strings.NewReader(patch))
    if err != nil {
        return "", err
    }

    req.Header.Set("Content-Type", "application/json-patch+json")
    req.Header.Set("If-Match", etag)
    if err := c.addAuth(ctx, req); err != nil {
        return "", err
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("heartbeat request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusNoContent {
        return "", parseNRFError(resp)
    }

    return resp.Header.Get("ETag"), nil
}
```

### 4.4 NF Deregistration

```go
// Deregister removes NFProfile from NRF.
func (c *NRFClient) Deregister(ctx context.Context, instanceID string) error {
    url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", c.baseURL, instanceID)
    req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
    if err != nil {
        return err
    }

    if err := c.addAuth(ctx, req); err != nil {
        return err
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("deregister request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusNoContent {
        return parseNRFError(resp)
    }

    return nil
}
```

---

## 5. Heartbeat Manager

### 5.1 Self-Healing State Machine

```
START → REGISTER → HEARTBEATING → Check failures
                              ↓            ↓
                         success      failures >= max
                              ↓            ↓
                        continue    Mark unregistered
                                            ↓
                                      RE-REGISTER (async)
                                            ↓
                                       success → HEARTBEATING
```

### 5.2 Implementation

```go
// internal/nrf/heartbeat.go

type HeartbeatManager struct {
    nrfClient  *NRFClient
    instanceID string
    etag       string

    // Config
    initialInterval  time.Duration
    maxFailures      int

    // State
    mu                  sync.RWMutex
    registered          bool
    heartbeatInterval   time.Duration
    consecutiveFailures int

    stopCh chan struct{}
    wg     sync.WaitGroup
}

func (m *HeartbeatManager) Start(ctx context.Context) error {
    if err := m.register(ctx); err != nil {
        return fmt.Errorf("initial registration: %w", err)
    }

    m.wg.Add(1)
    go m.run(ctx)

    return nil
}

func (m *HeartbeatManager) run(ctx context.Context) {
    defer m.wg.Done()

    ticker := time.NewTicker(m.heartbeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            m.deregister(ctx)
            return
        case <-m.stopCh:
            m.deregister(ctx)
            return
        case <-ticker.C:
            if err := m.heartbeat(ctx); err != nil {
                m.handleFailure(ctx, err)
            } else {
                m.mu.Lock()
                m.consecutiveFailures = 0
                m.mu.Unlock()
            }
        }
    }
}

func (m *HeartbeatManager) handleFailure(ctx context.Context, err error) {
    m.mu.Lock()
    m.consecutiveFailures++
    failures := m.consecutiveFailures
    m.mu.Unlock()

    log.Warnf("NRF heartbeat failed (attempt %d/%d): %v", failures, m.maxFailures, err)

    if failures >= m.maxFailures {
        log.Error("NRF heartbeat degraded, initiating re-registration")

        m.mu.Lock()
        m.registered = false
        m.mu.Unlock()

        go func() {
            for {
                if err := m.register(context.Background()); err != nil {
                    log.Warnf("Re-registration failed, retrying: %v", err)
                    time.Sleep(exponentialBackoff(failures))
                } else {
                    log.Info("Re-registration successful, resuming heartbeat")
                    return
                }
            }
        }()
    }
}

func exponentialBackoff(attempt int) time.Duration {
    base := 5 * time.Second
    max := 5 * time.Minute

    delay := base * time.Duration(1<<uint(attempt))
    if delay > max {
        delay = max
    }

    // Add jitter (±10%)
    jitter := time.Duration(rand.Int63n(int64(delay / 5)))

    return delay + jitter
}
```

---

## 6. OAuth2 Token Cache

### 6.1 Token Request

```go
// internal/nrf/token.go

type TokenCache struct {
    mu    sync.RWMutex
    token *CachedToken
    cfg   TokenConfig
}

type CachedToken struct {
    AccessToken string
    ExpiresAt  time.Time
}

func (c *TokenCache) GetToken(ctx context.Context) (string, error) {
    c.mu.RLock()
    if c.token != nil && time.Until(c.token.ExpiresAt) > 5*time.Minute {
        token := c.token.AccessToken
        c.mu.RUnlock()
        return token, nil
    }
    c.mu.RUnlock()

    // Refresh token
    return c.refresh(ctx)
}

func (c *TokenCache) refresh(ctx context.Context) (string, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // Double-check after acquiring write lock
    if c.token != nil && time.Until(c.token.ExpiresAt) > 5*time.Minute {
        return c.token.AccessToken, nil
    }

    form := url.Values{}
    form.Set("grant_type", "client_credentials")
    form.Set("client_id", c.cfg.ClientID)
    form.Set("client_secret", c.cfg.ClientSecret)
    form.Set("scope", c.cfg.Scope)
    form.Set("requester_nf_type", "NSSAAF")

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.AuthServer, strings.NewReader(form.Encode()))
    if err != nil {
        return "", err
    }
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("token request: %w", err)
    }
    defer resp.Body.Close()

    var tokenResp struct {
        AccessToken string `json:"access_token"`
        ExpiresIn   int    `json:"expires_in"`
        Scope       string `json:"scope"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
        return "", fmt.Errorf("parsing token response: %w", err)
    }

    c.token = &CachedToken{
        AccessToken: tokenResp.AccessToken,
        ExpiresAt:   time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
    }

    return c.token.AccessToken, nil
}
```

---

## 7. NF Discovery

### 7.1 Discovery with Cache

```go
// internal/nrf/discovery.go

type DiscoveryCache struct {
    mu    sync.RWMutex
    cache map[string]*CacheEntry
    ttl   time.Duration
}

type CacheEntry struct {
    Data      interface{}
    ExpiresAt time.Time
}

func (d *DiscoveryCache) Get(key string) (interface{}, bool) {
    d.mu.RLock()
    defer d.mu.RUnlock()

    entry, ok := d.cache[key]
    if !ok {
        return nil, false
    }

    if time.Now().After(entry.ExpiresAt) {
        return nil, false
    }

    return entry.Data, true
}

func (d *DiscoveryCache) Set(key string, data interface{}, ttl time.Duration) {
    d.mu.Lock()
    defer d.mu.Unlock()

    d.cache[key] = &CacheEntry{
        Data:      data,
        ExpiresAt: time.Now().Add(ttl),
    }
}
```

### 7.2 Discover AMF

```go
// DiscoverAMF finds all registered AMFs.
func (c *NRFClient) DiscoverAMF(ctx context.Context, params DiscoveryParams) ([]AMFInfo, error) {
    // Check cache
    cacheKey := fmt.Sprintf("amf:%v:%v", params.PLMN, params.SNSSAI)
    if c.discoveryCache != nil {
        if data, ok := c.discoveryCache.Get(cacheKey); ok {
            return data.([]AMFInfo), nil
        }
    }

    // Query NRF
    query := url.Values{}
    query.Set("target-nf-type", "AMF")
    query.Set("requester-nf-type", "NSSAAF")

    // ... execute request ...

    // Cache result
    if c.discoveryCache != nil {
        c.discoveryCache.Set(cacheKey, amfs, time.Duration(validityPeriod)*time.Second)
    }

    return amfs, nil
}
```

---

## 8. Error Handling

All NRF errors return ProblemDetails (RFC 7807):

| HTTP | Cause | Description |
|------|-------|-------------|
| 400 | INVALID_NF_PROFILE | NFProfile encoding error |
| 403 | FORBIDDEN | Not authorized |
| 404 | NOT_FOUND | NF instance not found |
| 412 | PRECONDITION_FAILED | ETag mismatch |
| 500 | INTERNAL_ERROR | NRF internal error |
| 503 | SERVICE_UNAVAILABLE | NRF unavailable |

---

## 9. Acceptance Criteria

| # | Criteria | Spec Reference |
|---|----------|----------------|
| AC1 | NSSAAF registers with NRF using PUT `/nnrf-nfm/v1/nf-instances/{id}` | TS 29.510 §5.2.2.2 |
| AC2 | NFProfile contains mandatory fields: nfInstanceId, nfType, nfStatus | TS 29.510 §6.1.6.2.2 |
| AC3 | nfServices is an array with versions, serviceName, fqdn | TS 29.510 §6.1.6.2.3 |
| AC4 | nssaafInfo contains supiRanges and internalGroupIdentifiersRanges | TS 29.510 §6.1.6.2.104 |
| AC5 | Heartbeat uses PATCH with nfStatus=REGISTERED | TS 29.510 §5.2.2.3.1B |
| AC6 | NFDiscovery discovers AMF, AUSF, UDM | TS 29.510 §5.3.2.2 |
| AC7 | Token request to `/oauth2/token` with client_credentials | TS 29.510 §5.4.2.2 |
| AC8 | JWT validation with scope check for incoming requests | TS 29.510 §5.4 |
| AC9 | Deregister on graceful shutdown using DELETE | TS 29.510 §5.2.2.4 |
| AC10 | Handle 3xx redirects from NRF | TS 29.510 §5.2.2.2 |
| AC11 | NFProfile loaded from YAML config file | Internal requirement |
| AC12 | Heartbeat interval negotiated via HeartBeat-Interval header | TS 29.510 §5.2.2.3.2 |
| AC13 | Auto re-registration after maxConsecutiveFailures | Self-healing requirement |
| AC14 | OAuth2 client credentials for NRF authentication | TS 29.510 §5.4.2.2 |

---

## 10. Dependencies

- `internal/httpgw` — HTTP Gateway that owns the NRF lifecycle
- `internal/types` — Shared 3GPP types (NFProfile, NFType, etc.)
- `internal/config` — Configuration loading

## 11. Out of Scope

- NRF mock server for testing (separate spec)
- UDM/AUSF discovery consumers (inbound AIW handler)
- Token validation for incoming requests (handled by middleware)

---

## 12. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| NRF unavailable at startup | Retry with backoff, don't block startup |
| Token expires during operation | Token cache refreshes 5 min before expiry |
| ETag staleness | Re-register on 412 Precondition Failed |
| Network partitions | Heartbeat manager handles re-registration |
