# VIP Health Check: Interface-Based Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace keepalived state file dependency with direct network interface checking for VIP health.

**Architecture:** The AAA Gateway reads the VIP address from config and checks if it's assigned to any network interface. This removes the keepalived sidecar, state file dependency, and NET_ADMIN capability.

**Tech Stack:** Go stdlib (`net` package), YAML config

---

## File Map

| File | Action |
|------|--------|
| `internal/config/config.go` | Replace `KeepalivedStatePath` with `VIPAddress` in `AAAgwConfig` |
| `internal/aaa/gateway/gateway.go` | Update `Config` struct, add `isVIPOwner()`, update `StartVIPAware()`, update `VIPHealthHandler()` |
| `internal/aaa/gateway/redis.go` | Remove `readKeepalivedState()` |
| `internal/aaa/gateway/gateway_test.go` | Update tests for interface-based logic |
| `cmd/aaa-gateway/main.go` | Update wire to use `VIPAddress` instead of `KeepalivedStatePath` |
| `compose/configs/aaa-gateway.yaml` | Replace `keepalivedStatePath` with `vipAddress` |
| `kubernetes/.../values.yaml` | Replace `keepalivedStatePath` with `vipAddress` |
| `kubernetes/.../deployment.yaml` | Remove keepalived sidecar, volumes, mounts, NET_ADMIN capability |

---

## Task 1: Update Config Struct

**Files:**
- Modify: `internal/config/config.go:131`

- [ ] **Step 1: Replace KeepalivedStatePath with VIPAddress**

Replace line 131 in `internal/config/config.go`:

```go
// Before:
KeepalivedStatePath string `yaml:"keepalivedStatePath"` // "/var/run/keepalived/state"

// After:
VIPAddress string `yaml:"vipAddress"` // e.g., "10.1.100.50"
```

- [ ] **Step 2: Update default value in Load()**

In the `Load()` function around line 497, replace the keepalived default:

```go
// Before:
if cfg.AAAgw.KeepalivedStatePath == "" {
    cfg.AAAgw.KeepalivedStatePath = "/var/run/keepalived/state"
}

// After:
// VIPAddress has no default — empty means dev/test mode (no VIP check)
```

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "refactor: replace KeepalivedStatePath with VIPAddress in config"
```

---

## Task 2: Update Gateway Config and Add isVIPOwner()

**Files:**
- Modify: `internal/aaa/gateway/gateway.go:28-55`

- [ ] **Step 1: Update Config struct**

In `internal/aaa/gateway/gateway.go`, replace the `KeepalivedStatePath` field:

```go
// Before:
KeepalivedStatePath string // path to keepalived state file

// After:
VIPAddress string // VIP address to check (e.g., "10.1.100.50")
```

- [ ] **Step 2: Add isVIPOwner() function**

Add this function after the imports (around line 25):

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

- [ ] **Step 3: Commit**

```bash
git add internal/aaa/gateway/gateway.go
git commit -m "feat: add isVIPOwner() function for interface-based VIP check"
```

---

## Task 3: Update StartVIPAware()

**Files:**
- Modify: `internal/aaa/gateway/gateway.go:222-257`

- [ ] **Step 1: Update StartVIPAware()**

Replace the current `StartVIPAware()` function (lines 222-257):

```go
// StartVIPAware blocks until this pod becomes VIP owner, then starts all listeners.
// Returns true if started successfully, false on context cancellation or error.
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

- [ ] **Step 2: Commit**

```bash
git add internal/aaa/gateway/gateway.go
git commit -m "refactor: update StartVIPAware() to use interface-based VIP check"
```

---

## Task 4: Update VIPHealthHandler()

**Files:**
- Modify: `internal/aaa/gateway/gateway.go:625-643`

- [ ] **Step 1: Update VIPHealthHandler()**

Replace the current `VIPHealthHandler()` function:

```go
// VIPHealthHandler returns 200 if this AAA Gateway replica owns the VIP, 503 otherwise.
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

- [ ] **Step 2: Commit**

```bash
git add internal/aaa/gateway/gateway.go
git commit -m "refactor: update VIPHealthHandler() to use interface-based check"
```

---

## Task 5: Remove readKeepalivedState() and Update Imports

**Files:**
- Modify: `internal/aaa/gateway/redis.go`

- [ ] **Step 1: Remove readKeepalivedState() function**

Delete the `readKeepalivedState()` function from `internal/aaa/gateway/redis.go` (lines 34-45).

- [ ] **Step 2: Clean up unused imports**

If `os` and `strings` are only used by `readKeepalivedState()`, remove them from the imports.

- [ ] **Step 3: Verify build**

```bash
go build ./internal/aaa/gateway/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/aaa/gateway/redis.go
git commit -m "refactor: remove readKeepalivedState(), no longer needed"
```

---

## Task 6: Update Tests

**Files:**
- Modify: `internal/aaa/gateway/gateway_test.go:1-147`

- [ ] **Step 1: Add test for isVIPOwner()**

Add a new test function:

```go
func TestIsVIPOwner(t *testing.T) {
    // Test with a non-existent IP (should return false)
    if isVIPOwner(context.Background(), "192.0.2.1") {
        t.Error("expected false for non-assigned IP")
    }

    // Test with empty IP (should return false)
    if isVIPOwner(context.Background(), "") {
        t.Error("expected false for empty IP")
    }
}
```

- [ ] **Step 2: Update TestVIPHealthHandler_MissingStateFile**

Replace `TestVIPHealthHandler_MissingStateFile` with a new test for no-config:

```go
func TestVIPHealthHandler_NoVIPConfigured(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
    g := &Gateway{
        cfg: Config{
            VIPAddress: "", // No VIP configured
        },
        logger: logger,
    }

    req := httptest.NewRequest("GET", "/health/vip", nil)
    rec := httptest.NewRecorder()

    g.VIPHealthHandler(rec, req)

    if rec.Code != http.StatusServiceUnavailable {
        t.Errorf("code: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
    }
    if !strings.Contains(rec.Body.String(), "VIP not configured") {
        t.Errorf("body: got %q, want to contain 'VIP not configured'", rec.Body.String())
    }
}
```

- [ ] **Step 3: Update TestStartVIPAware_DevModeNoStateFile**

Rename and update:

```go
func TestStartVIPAware_DevModeNoVIP(t *testing.T) {
    // When VIPAddress is empty, should start immediately without polling
    gw := &Gateway{
        cfg: Config{
            VIPAddress:     "",
            ListenRADIUS:   "",
            ListenDIAMETER: "",
        },
        logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
        wg:     sync.WaitGroup{},
    }
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    started := gw.StartVIPAware(ctx, "")
    if !started {
        t.Fatal("expected StartVIPAware to return true in dev mode")
    }
}
```

- [ ] **Step 4: Remove TestStartVIPAware_DevModeDevNull**

This test is no longer relevant since we don't use state paths. Delete it.

- [ ] **Step 5: Remove TestReadKeepalivedState test**

Delete the tests for `readKeepalivedState()` at lines 38-67.

- [ ] **Step 6: Run tests**

```bash
go test ./internal/aaa/gateway/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/aaa/gateway/gateway_test.go
git commit -m "test: update tests for interface-based VIP check"
```

---

## Task 7: Update main.go Wiring

**Files:**
- Modify: `cmd/aaa-gateway/main.go:43-80`

- [ ] **Step 1: Update gateway.New() call**

Replace the `KeepalivedStatePath` field:

```go
// Before:
KeepalivedStatePath:  cfg.AAAgw.KeepalivedStatePath,

// After:
VIPAddress: cfg.AAAgw.VIPAddress,
```

- [ ] **Step 2: Update StartVIPAware() call**

Replace the parameter:

```go
// Before:
if !gw.StartVIPAware(ctx, cfg.AAAgw.KeepalivedStatePath) {

// After:
if !gw.StartVIPAware(ctx, cfg.AAAgw.VIPAddress) {
```

- [ ] **Step 3: Verify build**

```bash
go build ./cmd/aaa-gateway/...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/aaa-gateway/main.go
git commit -m "refactor: update main.go to use VIPAddress instead of KeepalivedStatePath"
```

---

## Task 8: Update Docker Compose Config

**Files:**
- Modify: `compose/configs/aaa-gateway.yaml:17-32`

- [ ] **Step 1: Update aaaGateway section**

Replace line 32:

```yaml
# Before:
  keepalivedStatePath: "/var/run/keepalived/state"

# After:
  vipAddress: "10.1.100.50"
```

- [ ] **Step 2: Commit**

```bash
git add compose/configs/aaa-gateway.yaml
git commit -m "refactor: replace keepalivedStatePath with vipAddress in compose config"
```

---

## Task 9: Update Kubernetes Helm Values

**Files:**
- Modify: `kubernetes/deployments/helm/nssaa-aaa-gateway/values.yaml:1-33`

- [ ] **Step 1: Update config section**

Replace line 18:

```yaml
# Before:
  keepalivedStatePath: /var/run/keepalived/state

# After:
  vipAddress: "10.1.100.50"
```

- [ ] **Step 2: Commit**

```bash
git add kubernetes/deployments/helm/nssaa-aaa-gateway/values.yaml
git commit -m "refactor: replace keepalivedStatePath with vipAddress in Helm values"
```

---

## Task 10: Remove Keepalived Sidecar from Kubernetes Deployment

**Files:**
- Modify: `kubernetes/deployments/helm/nssaa-aaa-gateway/templates/deployment.yaml:1-118`

- [ ] **Step 1: Remove NET_ADMIN capability from aaa-gw container**

Remove lines 55-58:

```yaml
# REMOVE this block:
securityContext:
  capabilities:
    add:
      - NET_ADMIN
```

- [ ] **Step 2: Remove keepalived-state volume mount from aaa-gw container**

Remove lines 53-54:

```yaml
# REMOVE this:
- name: keepalived-state
  mountPath: /var/run/keepalived
```

- [ ] **Step 3: Remove keepalived sidecar container**

Remove lines 73-88:

```yaml
# REMOVE entire block:
- name: keepalived
  image: osixopen/keepalived:2.3.1
  securityContext:
    capabilities:
      add:
        - NET_ADMIN
  volumeMounts:
    - name: keepalived-conf
      mountPath: /etc/keepalived
    - name: keepalived-state
      mountPath: /var/run/keepalived
  env:
    - name: POD_IP
      valueFrom:
        fieldRef:
          fieldPath: status.podIP
```

- [ ] **Step 4: Remove keepalived volumes**

Remove lines 93-97:

```yaml
# REMOVE these two volumes:
- name: keepalived-conf
  configMap:
    name: nssaa-aaa-keepalived
- name: keepalived-state
  emptyDir: {}
```

- [ ] **Step 5: Commit**

```bash
git add kubernetes/deployments/helm/nssaa-aaa-gateway/templates/deployment.yaml
git commit -m "refactor: remove keepalived sidecar from Kubernetes deployment"
```

---

## Task 11: Final Verification

- [ ] **Step 1: Run full build**

```bash
go build ./...
```

- [ ] **Step 2: Run all tests**

```bash
go test ./...
```

- [ ] **Step 3: Verify no remaining references to KeepalivedStatePath**

```bash
grep -r "KeepalivedStatePath\|keepalivedStatePath\|readKeepalivedState" --include="*.go" .
```

Expected: No output

- [ ] **Step 4: Update validation checklist in spec**

Mark all items as complete in `docs/superpowers/specs/2026-06-14-vip-health-interface-design.md`

---

## Summary

| Task | Files | Commit |
|------|-------|--------|
| 1 | config.go | refactor: replace KeepalivedStatePath with VIPAddress |
| 2 | gateway.go | feat: add isVIPOwner() function |
| 3 | gateway.go | refactor: update StartVIPAware() |
| 4 | gateway.go | refactor: update VIPHealthHandler() |
| 5 | redis.go | refactor: remove readKeepalivedState() |
| 6 | gateway_test.go | test: update tests |
| 7 | main.go | refactor: update wiring |
| 8 | aaa-gateway.yaml | refactor: update compose config |
| 9 | values.yaml | refactor: update Helm values |
| 10 | deployment.yaml | refactor: remove keepalived sidecar |
| 11 | - | Verification |
