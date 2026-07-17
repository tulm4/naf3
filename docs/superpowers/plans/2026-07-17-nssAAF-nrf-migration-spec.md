# NSSAAF NRF Integration Migration: HTTP Gateway as NF Instance

**Status**: Draft
**Date**: 2026-07-17
**Decisions**: Complete

---

## Summary

Migrate NRF registration, heartbeat, and deregistration from **Biz Pod** to **HTTP Gateway**, per Spec §2.1.

**Before**:
```
AMF ──────► HTTP Gateway ──────► Biz Pod ──────► NRF
                         (proxy)    (NRF client)
```

**After**:
```
AMF ──────► HTTP Gateway ──────► Biz Pod
              (NRF client)  (discovery API)
                    │
                    ▼
                  NRF
```

---

## Decisions

| # | Decision | Choice |
|---|----------|--------|
| 1 | Migration scope | **Full Migration** - HTTP Gateway becomes sole NF instance |
| 2 | UDM service discovery | **NRF discovery** - HTTP Gateway provides discovery API |
| 3 | Discovery mechanism | **HTTP Gateway NF Discovery API** - Biz calls `/internal/nf-discovery/{nfType}` |
| 4 | Migration approach | **Incremental** - Add HTTP Gateway NRF first, then remove Biz NRF |
| 5 | NF Profile ownership | **HTTP Gateway owns profile** - Profile at `/etc/nssAAF/nf-profile.yaml` |

---

## New Internal API

### `GET /internal/nf-discovery/{nfType}`

Discovers an NF instance by type via NRF.

**Request**:
```http
GET /internal/nf-discovery/UDM HTTP/1.1
X-Request-ID: <uuid>
```

**Response** (200 OK):
```json
{
  "nfInstanceId": "uuid",
  "nfType": "UDM",
  "nfStatus": "SERVING",
  "fqdn": "udm.operator.com",
  "ipv4Addresses": ["172.0.3.13"],
  "ports": [
    {
      "port": 8081,
      "protocol": "HTTP/2",
      "security": "TLS"
    }
  ],
  "services": [
    {
      "serviceName": "nudm-sdm",
      "versions": [{"apiVersion": "v1", "fullVersion": "1.0.0"}]
    }
  ]
}
```

**Response** (404 Not Found):
```json
{
  "type": "https://nrf.operator.com/problem/nf-not-found",
  "title": "NF Not Found",
  "status": 404,
  "detail": "No serving UDM found in NRF"
}
```

---

## Phase 1: Add NRF Client to HTTP Gateway

### Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `cmd/http-gateway/factory.go` | Create | HTTP Gateway factory with NRF client |
| `cmd/http-gateway/main.go` | Modify | Initialize NRF client, start heartbeat |
| `compose/configs/http-gateway.yaml` | Modify | Add `nrf:` section |
| `internal/types/config.go` | Modify | Add NRF config types (if needed) |

### HTTP Gateway Changes

```go
// cmd/http-gateway/factory.go (new)

// cmd/http-gateway/main.go additions:
func main() {
    // ... existing code ...

    // NRF factory with circuit breaker
    nrfFactory := nfclient.NewFactory(internalNFRegistry)
    nrfClient := nrf.NewClient(cfg.NRF, nrfFactory)

    // Load NF profile and start heartbeat
    if err := nrfClient.SetProfilePath(cfg.NRF.ProfilePath, cfg.NRF.Heartbeat); err != nil {
        // log warning, continue
    }
    if err := nrfClient.StartHeartbeat(context.Background()); err != nil {
        // log warning, background retry
    }

    // ... rest of main ...
}
```

### Config Changes

```yaml
# compose/configs/http-gateway.yaml additions:
nrf:
  baseURL: "${NRF_URL:-http://172.0.3.12:8081}"
  discoverTimeout: 5s
  profilePath: "${NSSAF_NF_PROFILE_PATH:-/etc/nssAAF/nf-profile.yaml}"
  heartbeat:
    initialIntervalSeconds: 30
    acceptNegotiatedInterval: true
    maxConsecutiveFailures: 3
```

---

## Phase 2: Create Internal Discovery API

### Files to Create

| File | Action | Description |
|------|--------|-------------|
| `internal/discovery/client.go` | Create | Discovery client (Biz → HTTP Gateway) |
| `cmd/http-gateway/handlers_discovery.go` | Create | Internal API handler |
| `cmd/http-gateway/mux.go` | Modify | Register internal routes |

### HTTP Gateway Handler

```go
// cmd/http-gateway/handlers_discovery.go

func (h *GatewayHandler) HandleNFFind(w http.ResponseWriter, r *http.Request) {
    // Extract nfType from path
    // Call nrfClient.FindNF(nfType)
    // Return NFProfile JSON
}
```

### Biz Discovery Client

```go
// internal/discovery/client.go

type NFDiscoveryClient interface {
    FindNF(ctx context.Context, nfType string) (*nrf.NFProfile, error)
}

type httpDiscoveryClient struct {
    baseURL string
    client  *http.Client
}

func (c *httpDiscoveryClient) FindNF(ctx context.Context, nfType string) (*nrf.NFProfile, error) {
    url := fmt.Sprintf("%s/internal/nf-discovery/%s", c.baseURL, nfType)
    resp, err := c.client.Get(url)
    // ... handle response
}
```

---

## Phase 3: Update Biz Pod to Use Discovery API

### Files to Modify

| File | Action | Description |
|------|--------|-------------|
| `cmd/biz/factory.go` | Modify | Remove NRF client; add discovery client |
| `internal/udm/client.go` | Modify | Use discovery client instead of NRF client |
| `internal/amf/client.go` | Modify | Use discovery client for AMF notifier |
| `compose/configs/biz.yaml` | Modify | Remove `nrf:` section |

### UDM Client Changes

```go
// internal/udm/client.go

type Client struct {
    cfg     Config
    factory *nfclient.Factory  // Remove nrfClient
    disc    discovery.NFDiscoveryClient  // Add discovery client
}

// NewClient now takes discovery client instead of NRF client
func NewClient(cfg Config, disc discovery.NFDiscoveryClient) *Client {
    // Use disc to find UDM on demand
}
```

---

## Phase 4: Remove NRF Code from Biz Pod

### Files to Delete/Modify

| File | Action | Description |
|------|--------|-------------|
| `cmd/biz/factory.go` | Modify | Remove NRF factory, client, deregistration |
| `cmd/biz/main.go` | Modify | Remove NRF health check |

### Biz Factory Changes

```go
// Remove from factory.go:
- nrfFactory := nfclient.NewFactory(internalNFRegistry)
- nrfClient := nrf.NewClient(f.cfg.NRF, nrfFactory)
- nrfClient.SetProfilePath(...)
- nrfClient.StartHeartbeat(...)

// Remove from shutdown:
- nrfClient.Deregister(...)

// Add to factory.go:
- discClient := discovery.NewClient(cfg.HTTPgw.DiscoveryURL)
```

---

## Phase 5: Update Docker Compose

### Files to Modify

| File | Action | Description |
|------|--------|-------------|
| `compose/fullchain-dev-base.yaml` | Modify | Update http-gateway service |
| `configs/http-gateway.yaml` | Modify | Add NRF config |

### Compose Changes

```yaml
# compose/fullchain-dev-base.yaml

services:
  http-gateway:
    volumes:
      - ./nf-profile.yaml:/etc/nssAAF/nf-profile.yaml:ro  # Add profile mount
    environment:
      NRF_URL: "http://nrf:8081"
      NSSAF_NF_PROFILE_PATH: "/etc/nssAAF/nf-profile.yaml"
    depends_on:
      nrf:
        condition: service_healthy
```

---

## NF Profile YAML

```yaml
# configs/nf-profile.yaml

nfInstanceId: "${NF_INSTANCE_ID:-auto-generated}"
nfType: NSSAAF
nfStatus: REGISTERED
fqdn: http-gateway.nssaa.svc.cluster.local
ipv4Addresses:
  - "${HTTP_GW_IP:-172.0.3.14}"
ports:
  - port: 8443
    protocol: HTTP/2
    security: TLS
services:
  - serviceName: nnssaaf-nssaa
    versions:
      - apiVersion: v1
        fullVersion: 1.0.0
  - serviceName: nnssaaf-aiw
    versions:
      - apiVersion: v1
        fullVersion: 1.0.0
heartBeatInterval: 30
priority: 100
capacity: 1000
load: 0
```

---

## Validation Checklist

- [ ] HTTP Gateway compiles with NRF client
- [ ] HTTP Gateway successfully registers with NRF
- [ ] HTTP Gateway heartbeat loop runs
- [ ] Discovery API returns NF profiles
- [ ] Biz Pod discovers UDM via HTTP Gateway
- [ ] NRF deregistration works on HTTP Gateway shutdown
- [ ] Existing E2E tests pass
- [ ] New migration-specific tests pass

---

## Rollback Plan

If migration fails at Phase 3 or later:

1. Re-enable Biz NRF client code (from git)
2. Update compose to mount profile to Biz Pod
3. Revert biz.yaml to include NRF config

---

## References

- Spec: `docs/superpowers/specs/2026-07-15-nssAAF-nrf-integration-design.md`
- Implementation Plan: `docs/superpowers/plans/2026-07-15-nssAAF-nrf-integration-implementation-plan.md`
- NRF Client: `internal/nrf/client.go`
- Existing Biz Factory: `cmd/biz/factory.go`
