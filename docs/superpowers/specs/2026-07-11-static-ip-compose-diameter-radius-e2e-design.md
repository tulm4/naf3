# Static-IP Compose + Diameter (TCP/SCTP) and RADIUS E2E Verification

| Field | Value |
|---|---|
| Date | 2026-07-11 |
| Status | Draft → Ready for review |
| Spec | TS 29.561 §16.3 (RADIUS), §17.3 (Diameter EAP); RFC 6733 §5.5 (CER/CEA), §5.6 (DWR/DWA) |
| Scope | `compose/fullchain-dev*.yaml`, `internal/aaa/gateway/diameter_forward.go`, `internal/config/config.go`, `cmd/aaa-gateway/main.go`, `Dockerfile.aaa-gateway`, `Dockerfile.aaa-sim`, `test/e2e/fullchain_dev_diameter_radius/` |

## 1. Purpose

Verify that `aaa-gateway` connects successfully to `aaa-sim` over Diameter (both TCP and SCTP, per RFC 6733 §5.5 and §5.6) and RADIUS (per TS 29.561 §16.3). The verification runs in Docker Compose with **static IPv4 addresses** in the `172.0.3.0/24` subnet so all inter-service communication happens by IP, not by Docker DNS service-name resolution.

This mirrors production addressing where AAA-S has a known, routable IP and AAA-Gateway's Origin-Host/Realm/EAP-Payload routing decisions are made against concrete peer IPs rather than container-name aliases.

## 2. Background

Today's `compose/fullchain-dev-tcp.yaml` (the TCP overlay for the static-IP fullchain) uses a custom bridge network and static IPv4 addresses. Service names like `aaa-sim`, `redis`, `biz` resolve via Docker DNS only at the application layer; inter-service dialing now happens by IP. This is a deliberate change from the legacy service-name approach used in `compose/fullchain-dev.yaml` (which the rest of this document refers to as the "legacy dev variant" — see §5.1).

`aaa-gateway`'s `diamForwarder` hardcodes `"tcp"` as the dial network because we deleted `DiameterProtocol` during the recent cleanup. To exercise SCTP end-to-end, the dial network must be configurable.

`aaa-gateway`'s `StartVIPAware` checks `isVIPOwner` before starting listeners. In production this VIP is a separate IP managed by keepalived/VRRP for HA. For this dev/test composition, the VIP is set to the gateway container's **own static IP** so `isVIPOwner` returns true immediately and HA semantics are deliberately not exercised.

## 3. Goals & Non-Goals

**Goals:**

1. Compose file uses static IPv4 addresses for all 9 services in subnet `172.0.3.0/24`.
2. All inter-service communication uses these IPs — no DNS lookups for cross-service traffic.
3. E2E Go test that verifies Diameter handshake (CER/CEA), watchdog (DWR/DWA), and DER/DEA exchanges over both TCP and SCTP.
4. E2E Go test that verifies RADIUS Access-Request ↔ Access-Accept with EAP Success payload.
5. Dev/test VIP equals the gateway's container IP — no production code change.

**Non-Goals:**

- Watchdog timeout (Tw expiry) handling.
- Multi-replica `aaa-gateway` with VRRP keepalived.
- RadSec / Diameter-over-TLS.
- Production VIP semantics change.
- Static IPs in `compose/fullchain.yaml` (production-target compose) — kept on default Docker network.

## 4. Architecture

### 4.1 Subnet Plan

Two custom bridge networks (one per compose variant, to avoid IP-collision across Compose projects and to enable `docker compose down` clean teardown):

- `nssaa_fullchain_tcp` — used by `fullchain-dev-tcp.yaml`, subnet `172.0.3.0/24`, gateway `172.0.3.1`.
- `nssaa_fullchain_sctp` — used by `fullchain-dev-sctp.yaml`, subnet `172.0.3.0/24`, gateway `172.0.3.1`.

| Service | IPv4 Address | Listening Ports |
|---|---|---|
| `redis` | `172.0.3.10` | `6379/tcp` |
| `postgres` | `172.0.3.11` | `5432/tcp` |
| `nrf-mock` | `172.0.3.12` | `8081/tcp` |
| `udm-mock` | `172.0.3.13` | `8081/tcp` |
| `aaa-sim` | `172.0.3.14` | `1812/udp`, `3868/tcp+sctp` |
| `aaa-gateway` | `172.0.3.15` | `9090/tcp`, `1812/udp`, `3868/tcp` |
| `biz` | `172.0.3.16` | `8080/tcp` |
| `http-gateway` | `172.0.3.17` | `8443/tcp` |
| `nrm` | `172.0.3.18` | `8081/tcp` |

Host port mappings are unchanged from the current compose file (e.g., `5432:5432`, `6379:6379`).

### 4.2 VIP Behavior (dev/test only)

`compose/configs/aaa-gateway.yaml` sets:

```yaml
aaaGateway:
  vipAddress: "172.0.3.15"   # equals the container's static IP
  diameterServerAddress: "172.0.3.14:3868"
  radiusServerAddress: "172.0.3.14:1812"
  ...
```

Because Docker assigns the static IP to `eth0` of the `aaa-gateway` container at start time, `isVIPOwner(ctx, "172.0.3.15")` returns true on the first poll, and `StartVIPAware` invokes `startListeners` immediately. No code change to `internal/aaa/gateway/gateway.go`. Production code keeps the existing keepalived/VRRP path.

### 4.3 Diameter Transport Selection

`aaa-gateway.yaml` adds `diameterTransport: "tcp"` (default) or `"sctp"`. The forwarder uses this string to dial. When set to `"sctp"`, both `aaa-gateway` and `aaa-sim` containers run with `cap_add: NET_ADMIN` and a Dockerfile build arg `INSTALL_SCTP=1` that installs `lksctp-tools` and, where possible, the `sctp` kernel module.

`diamForwarderConfig.Network` is the existing internal field; the new field name is `diamTransport` to avoid clashing with the YAML `network:` for HTTP servers in unrelated configs.

## 5. Components & Interfaces

### 5.1 File-Level Changes

| Path | Change |
|---|---|
| `compose/commons.yaml` | **NEW** defines the shared custom bridge network (`nssaa_fullchain_tcp` with `attachable: false`) and `x-` extension fragments (image, ports, environment blocks) used by both TCP and SCTP variants. No `services:` top-level. |
| `compose/fullchain-dev-base.yaml` | **NEW** defines the 9 services under `services:`, each one wiring up `ipv4_address`, env, image via `<<: *common-fragment`. This file is included by both variants. |
| `compose/fullchain-dev-tcp.yaml` | **NEW** minimal override layer: `include:` pulls `fullchain-dev-base.yaml`; adds `DIAMETER_TRANSPORT=tcp` env to `aaa-sim` and `aaa-gateway`; defines `nssaa_fullchain_tcp` network on top with `name: nssaa_fullchain_tcp`. |
| `compose/fullchain-dev-sctp.yaml` | **NEW** minimal override layer: same as TCP variant but with `DIAMETER_TRANSPORT=sctp`, `cap_add: [NET_ADMIN]`, and `nssaa_fullchain_sctp` network name. Dockerfile build arg `INSTALL_SCTP=1`. |
| `compose/fullchain-dev-tcp.yaml` | **DELETED.** Replaced by the three files above. The TCP variant becomes the new default for existing `make test-fullchain-fast` callers. |
| `compose/configs/aaa-gateway.yaml` | `vipAddress: 172.0.3.15`, `diameterServerAddress: 172.0.3.14:3868`, `radiusServerAddress: 172.0.3.14:1812`, `diameterTransport: <tcp\|sctp>`. |
| `compose/configs/biz.yaml` | `REDIS_ADDR=172.0.3.10:6379`, `POSTGRES_HOST=172.0.3.11`, `NRF_URL=http://172.0.3.12:8081`, `UDM_URL=http://172.0.3.13:8081`, `NRM_URL=http://172.0.3.18:8084`, `AAA_GW_URL=http://172.0.3.15:9090`. |
| `internal/aaa/gateway/diameter_forward.go` | Add `DiameterTransport string` to `diamForwarderConfig`; pass to the dial helper. |
| `internal/config/config.go` | Add `DiameterTransport string` to `AAAgwConfig`. Default `"tcp"`. Optional env override `DIAMETER_TRANSPORT`. |
| `cmd/aaa-gateway/main.go` | Pass `cfg.AAAgw.DiameterTransport` into the gateway struct. |
| `Dockerfile.aaa-gateway` | `ARG INSTALL_SCTP=0`; when `1`, `RUN apk add --no-cache lksctp-tools && modprobe sctp \|\| true`. |
| `Dockerfile.aaa-sim` | Same Dockerfile changes. |
| `test/aaa_sim/diameter.go` | Bind `sctp` if `DIAMETER_TRANSPORT=sctp` is set (env-driven; `Run()` already reads `AAA_SIM_DIAMETER_TRANSPORT`). |
| `test/aaa_sim/mode.go` | Already supports `AAA_SIM_DIAMETER_TRANSPORT`. No change. |
| `test/e2e/fullchain_dev_diameter_radius/helpers.go` | **NEW** `helpers.go` for compose up/down + port-availability checks. |
| `test/e2e/fullchain_dev_diameter_radius/diameter_tcp_test.go` | **NEW** TCP verification. |
| `test/e2e/fullchain_dev_diameter_radius/diameter_sctp_test.go` | **NEW** SCTP verification (skip on non-Linux or missing kernel module). |
| `test/e2e/fullchain_dev_diameter_radius/radius_test.go` | **NEW** RADIUS verification. |

### 5.2 Public/Internal Interface Changes

**`internal/aaa/gateway` types:**

```go
// diamForwarderConfig (existing struct, add field)
// New field:
type diamForwarderConfig struct {
    Addr             string        // 172.0.3.14:3868
    Transport        string        // "tcp" (default) or "sctp"
    AppID            uint32        // existing
    WatchdogInterval time.Duration // existing
    Logger           *slog.Logger  // existing
}

// Gateway.AAAgw struct (or equivalent config struct used by main)
// New field:
type AAAgw struct {
    DiameterTransport string // "tcp" or "sctp", default "tcp"
    ...
}
```

**`internal/config`:**

```go
type AAAgwConfig struct {
    DiameterTransport  string        `yaml:"diameterTransport"`  // "tcp" or "sctp"
    DiameterServerAddr string        `yaml:"diameterServerAddress"`
    DiameterRealm      string        `yaml:"diameterRealm"`
    DiameterHost       string        `yaml:"diameterHost"`
    RadiusServerAddr   string        `yaml:"radiusServerAddress"`
    RadiusSharedSecret string        `yaml:"radiusSharedSecret"`
    VIPAddress         string        `yaml:"vipAddress"`
    ...
}
```

If `DiameterTransport == ""`, default to `"tcp"` in `Validate()`.

### 5.3 Test Public Interface

`test/e2e/fullchain_dev_diameter_radius/helpers.go`:

```go
// bringUp starts a compose file and waits for aaa-gateway and aaa-sim to
// become healthy. It blocks until ready or context timeout (default 120s).
//
// composeFile: "compose/fullchain-dev-tcp.yaml" or "compose/fullchain-dev-sctp.yaml"
// env: optional extra env vars (e.g. {"DIAMETER_TRANSPORT": "sctp"})
func bringUp(t *testing.T, composeFile string, env map[string]string)

// tearDown runs docker compose down -v for the compose file. Always
// invoked via t.Cleanup.
func tearDown(t *testing.T, composeFile string)

// aaaSimAddr returns the static IP:port pair for the AAA-S simulator.
// Either "172.0.3.14:3868" for Diameter or "172.0.3.14:1812" for RADIUS.
func aaaSimAddr(service string) string
```

## 6. Data Flow

### 6.1 Diameter TCP Path

1. `docker compose -f compose/fullchain-dev-tcp.yaml up -d --wait`
2. Both `aaa-gateway` (172.0.3.15) and `aaa-sim` (172.0.3.14) start.
3. `aaa-gateway.StartVIPAware`: `isVIPOwner` returns true (IP bound to local interface); listeners start.
4. Test dial: `sm.Client.NewNetworkedClient(&sm.Settings{...}, "tcp").Dial("172.0.3.14", 3868)`.
5. `sm.Client` auto-sends CER; `aaa-sim` (using `sm.New`) auto-responds CEA — handshake verified.
6. Test sets `WatchdogInterval=2s`; `sm` sends DWR automatically; asserts DWA arrives.
7. Test sends DER (Auth-Application-Id=5, EAP-Payload=EAP-Response/Identity); `aaa-sim` (mode `EAP_TLS_SUCCESS`) replies DEA with `Result-Code=2001` and `EAP-Payload=[3,0,0,4]` (EAP Success).

### 6.2 Diameter SCTP Path

Same flow with `network="sctp"`. Skipped automatically on:
- `runtime.GOOS != "linux"` (Docker Desktop on macOS lacks SCTP module support).
- `/proc/net/protocols` does not contain `SCTP`.
- `cap_add: NET_ADMIN` is denied (e.g., userland Docker).

### 6.3 RADIUS Path

1. Test allocates a UDP socket at random port.
2. Test constructs RADIUS `Access-Request`: `User-Name=testuser`, `EAP-Message=EAP-Response/Identity`.
3. Compute `Request-Authenticator` (16 random bytes + MD5 of body+secret).
4. Send to `172.0.3.14:1812` via UDP.
5. `aaa-sim` (mode `EAP_TLS_SUCCESS`) responds `Access-Accept` with `EAP-Message=[3,0,0,4]`.
6. Test validates `Response-Authenticator = HMAC-MD5(code+id+length+req-auth+attrs+secret)`.

### 6.4 Sequence (TCP)

```
Test          sm.Client       aaa-gateway      aaa-sim
 │               │                │              │
 │  Dial tcp 172.0.3.14:3868 ──►│              │
 │               ├─CER──────────►│              │
 │               │                ├─CER─────────►│
 │               │                │◄──CEA────────┤
 │               │◄──CEA─────────┤              │
 │  CEA received │                │              │
 │  handshake OK │                │              │
 │               │                │              │
 │  Wait 2s      │                │              │
 │               ├──DWR──────────►│              │
 │               │                ├─DWR─────────►│
 │               │                │◄──DWA────────┤
 │               │◄──DWA─────────┤              │
 │  DWA received │                │              │
 │               │                │              │
 │  send DER(5, EAP-Response/Identity)─────────►│
 │               │                                 │
 │               │◄────DEA(Result-Code=2001, EAP-Payload=[3,0,0,4])──────┤
 │  DEA received │                │              │
 │  EAP Success  │                │              │
 │  PASS         │                │              │
```

### 6.5 Configuration Flow

YAML files reference IPs. No code change to DNS resolution path. Environment variables override YAML values.

```yaml
# compose/configs/aaa-gateway.yaml (excerpt)
aaaGateway:
  bizServiceUrl: "http://172.0.3.16:8080"
  listenRadius: ":1812"
  diameterServerAddress: "172.0.3.14:3868"
  diameterRealm: "operator.com"
  diameterHost: "nssaa-gw.operator.com"
  diameterTransport: "tcp"           # or "sctp"
  radiusServerAddress: "172.0.3.14:1812"
  radiusSharedSecret: "secret"
  vipAddress: "172.0.3.15"
```

## 7. Error Handling & Failure Modes

| # | Failure | Mitigation |
|---|---|---|
| 1 | Static-IP collision with stale network | Test helpers call `docker network rm <netname>` before `up`. Compose file names networks uniquely: `nssaa_fullchain_tcp`, `nssaa_fullchain_sctp`. |
| 2 | `isVIPOwner` returns false (race during interface bring-up) | Test waits for `aaa-gateway`'s `/health` HTTP endpoint to return 200 before proceeding; this endpoint already differentiates VIP-owner vs not (existing `VIPHealthHandler`). |
| 3 | SCTP kernel module unavailable | Per-test pre-check: read `/proc/net/protocols`; if no `SCTP` line, `t.Skip("SCTP kernel module unavailable")`. |
| 4 | CER/CEA timing race | Test waits for aaa-sim container healthcheck plus an extra 2s `sleep`. aaa-sim `Run()` logs `"aaa-sim started"` after both servers are up. |
| 5 | DWR/DWA not fired | `sm.Client` requires `WatchdogInterval` to be set. Test sets `WatchdogInterval=2s` to make DWRs happen within the 10s test timeout. |
| 6 | RADIUS Response Authenticator mismatch | Test recomputes HMAC-MD5 over expected fields; if mismatch, fail with explicit comparison. |
| 7 | Existing DNS-name tests broken | `compose/fullchain-dev-tcp.yaml` is updated to use IPs by default; existing `make test-fullchain-fast` callers either switch to IP-only paths or revert this file in a separate commit. |

### 7.1 Component Failure Behavior

- `aaa-gateway.StartVIPAware` exits if VIP cannot be acquired within timeout. In our static-IP setup, acquisition is immediate, so this is unreachable in practice.
- `diamForwarder.Connect` retries forever (existing behavior). Test uses a 10s dial timeout to prevent hangs.
- `aaa-sim` exits cleanly on `SIGINT`/`SIGTERM`. Test sends `docker compose down -v` for cleanup.

## 8. Testing Strategy

### 8.1 Unit Tests (existing, unchanged)

`internal/aaa/gateway/diameter_forward_test.go` continues to mock the peer with a `diam.Conn`. We do **not** add SCTP to the unit tests because the mock peer doesn't faithfully model go-diameter's SCTP multi-stream behavior.

### 8.2 E2E Tests (new)

`test/e2e/fullchain_dev_diameter_radius/` — Go test package using `os/exec`. Marked with build tag `//go:build e2e` so they don't run during `go test ./...` (only via `go test -tags=e2e ./test/e2e/fullchain_dev_diameter_radius/...`).

| Test | Asserts | Allowed Duration |
|---|---|---|
| `TestDiameter_TCP_HelloWatchdog` | TCP dial succeeds, CER/CEA exchange, DWR/DWA roundtrip. | 30s |
| `TestDiameter_TCP_DER_DEA_EAP` | DER with `Auth-Application-Id=5` returns DEA with `Result-Code=2001` and EAP-Payload EAP-Success. | 30s |
| `TestDiameter_SCTP_HelloWatchdog` | Same as TCP but on SCTP. Skips if unavailable. | 30s |
| `TestDiameter_SCTP_DER_DEA_EAP` | Same as TCP-DEA but on SCTP. Skips. | 30s |
| `TestRadius_AccessRequest_Success` | RADIUS UDP Access-Request → Access-Accept with EAP Success and valid authenticator. | 30s |
| `TestRadius_AccessRequest_BadSecret` | Sends with wrong shared secret; asserts `aaa-sim` does not respond with Access-Accept. | 30s |

### 8.3 Spec Verification

After implementation, sign-off:

```
Verified against:
- TS 29.561 §16.3 (RADIUS Access-Request/Accept)
- TS 29.561 §17.3 (Diameter EAP/AA DER/DEA with EAP-Payload AVP 1265)
- RFC 6733 §5.5 (CER/CEA handshake — Origin-Host, Origin-Realm, Result-Code=2001)
- RFC 6733 §5.6 (DWR/DWA watchdog, App-Id=0)
- RFC 6733 §3 (Diameter transport: TCP and SCTP)
```

### 8.4 Runbook

```bash
# 1. Build images (one-time, or after Dockerfile changes)
docker compose -f compose/fullchain-dev-tcp.yaml build
docker compose -f compose/fullchain-dev-sctp.yaml build

# 2. Run all E2E tests (TCP + RADIUS + SCTP if available)
go test -tags=e2e -v -timeout=300s ./test/e2e/fullchain_dev_diameter_radius/...

# 3. Run only TCP + RADIUS (skip SCTP)
go test -tags=e2e -v -timeout=300s -run 'TCP|Radius' ./test/e2e/fullchain_dev_diameter_radius/...
```

## 9. Open Questions / Deferred

- **Tw (watchdog) timeout validation**: deferred. Adds complexity; current scope covers happy-path CER/CEA + DWR/DWA.
- **Multi-replica aaa-gateway**: deferred. Requires VRRP/keepalived in compose, out of scope here.
- **RadSec / Diameter over TLS**: deferred. Separate spec.
- **Production VIP semantics change**: explicitly out of scope. This design only touches dev/test compose and dev-mode config.
- **Mac/Windows Docker Desktop SCTP**: limited. Users on non-Linux hosts will see `t.Skip`. We do not attempt to make SCTP work on Docker Desktop.

## 10. References

- 3GPP TS 29.561 (AAA Interworking), Chapter 16 (RADIUS), Chapter 17 (Diameter EAP).
- IETF RFC 6733 (Diameter Base Protocol), §3, §5.5, §5.6.
- IETF RFC 4072 (Diameter EAP Application), Auth-Application-Id=5.
- go-diameter v4 (`github.com/fiorix/go-diameter/v4`), specifically `diam/sm` state machine.
- `ishidawataru/sctp` Go library (used transparently by go-diameter for SCTP).
