# VIP Health Check: Interface-Based Implementation

## Status

- **Date:** 2026-06-14
- **Author:** NSSAAF Team
- **Replaces:** Keepalived state file approach

## Problem Statement

The current AAA Gateway implementation relies on keepalived to write its state to a file (`/var/run/keepalived/state`). The `/health/vip` endpoint reads this file to determine if this replica owns the VIP. This creates unnecessary coupling to keepalived internals.

## Solution

Replace the file-based state check with direct network interface inspection. The AAA Gateway reads the VIP address from config and checks if it's assigned to any of the pod's network interfaces.

## Design

### Config Changes

**File:** `internal/config/config.go`

| Field | Before | After |
|-------|--------|-------|
| `AAAgwConfig.KeepalivedStatePath` | `string` | REMOVED |
| `AAAgwConfig.VIPAddress` | (nonexistent) | `string` — VIP address, e.g., `"10.1.100.50"` |

**Dev mode:** When `VIPAddress` is empty, the gateway starts listeners immediately without checking interfaces.

### New Function: `isVIPOwner()`

```go
// isVIPOwner checks if the VIP address is assigned to any network interface.
func isVIPOwner(ctx context.Context, vipAddress string) bool {
    interfaces, err := net.Interfaces()
    if err != nil {
        return false
    }
    for _, iface := range interfaces {
        addrs, err := iface.Addrs()
        if err != nil {
            continue
        }
        for _, addr := range addrs {
            if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.String() == vipAddress {
                return true
            }
        }
    }
    return false
}
```

### Updated: `StartVIPAware()`

```go
func (g *Gateway) StartVIPAware(ctx context.Context, vipAddress string) bool {
    // Dev/test mode: no VIP → start immediately
    if vipAddress == "" {
        g.logger.Info("no VIP configured, starting immediately (dev/test mode)")
        if err := g.startListeners(ctx); err != nil {
            g.logger.Error("startListeners failed", "error", err)
            return false
        }
        return true
    }

    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        if isVIPOwner(ctx, vipAddress) {
            g.logger.Info("VIP acquired, starting all listeners")
            if err := g.startListeners(ctx); err != nil {
                g.logger.Error("startListeners failed", "error", err)
                return false
            }
            return true
        }
        g.logger.Info("not VIP owner, waiting")

        select {
        case <-ctx.Done():
            return false
        case <-ticker.C:
        }
    }
}
```

### Updated: `VIPHealthHandler()`

```go
func (g *Gateway) VIPHealthHandler(w http.ResponseWriter, r *http.Request) {
    vipAddress := g.cfg.VIPAddress
    if vipAddress == "" {
        w.WriteHeader(http.StatusServiceUnavailable)
        _, _ = fmt.Fprintf(w, `{"vip_owner":false,"error":"VIP not configured"}`)
        return
    }

    if isVIPOwner(r.Context(), vipAddress) {
        w.WriteHeader(http.StatusOK)
        _, _ = fmt.Fprintf(w, `{"vip_owner":true}`)
    } else {
        w.WriteHeader(http.StatusServiceUnavailable)
        _, _ = fmt.Fprintf(w, `{"vip_owner":false}`)
    }
}
```

### Removed: `readKeepalivedState()`

Delete from `internal/aaa/gateway/redis.go`.

### Config File Updates

**`compose/configs/aaa-gateway.yaml`:**
```yaml
aaaGateway:
  # Before:
  keepalivedStatePath: "/var/run/keepalived/state"
  
  # After:
  vipAddress: "10.1.100.50"
```

**`kubernetes/deployments/helm/nssaa-aaa-gateway/values.yaml`:**
```yaml
config:
  # Before:
  keepalivedStatePath: /var/run/keepalived/state
  
  # After:
  vipAddress: "10.1.100.50"
```

### Kubernetes Deployment Simplification

**Remove from `deployment.yaml`:**
- Keepalived sidecar container
- `keepalived-conf` volume mount
- `keepalived-state` volume mount
- keepalived configMap volume
- `NET_ADMIN` capability (only needed for keepalived VRRP)

## Files Changed

| File | Action |
|------|--------|
| `internal/config/config.go` | Replace `KeepalivedStatePath` with `VIPAddress` |
| `internal/aaa/gateway/redis.go` | Remove `readKeepalivedState()` |
| `internal/aaa/gateway/gateway.go` | Update `StartVIPAware()` and `VIPHealthHandler()` |
| `internal/aaa/gateway/gateway_test.go` | Update tests for interface-based logic |
| `internal/httpclient/native_aaa.go` | No changes needed (uses `/health/vip` endpoint) |
| `compose/configs/aaa-gateway.yaml` | Update config field |
| `kubernetes/.../values.yaml` | Update config field |
| `kubernetes/.../deployment.yaml` | Remove keepalived sidecar and related configs |

## Validation Checklist

- [x] `go build ./...` compiles without errors
- [x] `go test ./internal/aaa/gateway/...` passes
- [x] Health endpoint `/health/vip` returns 200 when VIP is assigned
- [x] Health endpoint `/health/vip` returns 503 when VIP is not assigned
- [x] Startup waits for VIP assignment in HA mode (non-empty VIPAddress)
- [x] Startup starts immediately in dev mode (empty VIPAddress)
- [x] Keepalived sidecar and volumes removed from Kubernetes deployment

## Rationale

1. **Simplicity:** No dependency on keepalived internals or state files
2. **Portability:** Works with any HA solution that assigns VIPs to interfaces
3. **Debugging:** Network interface state is easier to verify than keepalived state files
4. **Security:** Removes need for keepalived sidecar and `NET_ADMIN` capability
