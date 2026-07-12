# Simplify Diameter/RADIUS E2E Test Stack Management

| Field | Value |
|---|---|
| Date | 2026-07-11 |
| Status | Draft → Ready for review |
| Scope | `test/e2e/fullchain_dev_diameter_radius/` (delete), `test/e2e/container_driver.go` (extend), `Makefile` (add 2 targets), new test files under `test/e2e/` |
| Replaces | Static-IP compose overlay spec (file paths preserved; only test execution model changes) |

## 1. Purpose

The Diameter/RADIUS E2E test package added in the previous iteration (`test/e2e/fullchain_dev_diameter_radius/`) embeds `docker compose up/build/wait` directly inside test helpers (`bringUp`, `tearDown`). That makes the tests slow (image rebuild on every run), fragile (depends on Docker daemon state inside the test process), and diverges from the existing `make test-fullchain` / `test-fullchain-fast` / `test-fullchain-no-build` pattern that has been working for the rest of the e2e suite.

This spec **simplifies the test execution model** so the same Makefile-driven lifecycle is reused, and the test code itself only **observes** the running stack — never starts or stops it. The static-IP compose overlay itself (4 files, 2 variants, 2 Dockerfiles with `INSTALL_SCTP`) is preserved as-is. Only the *test side* is reorganized.

## 2. Goals

- **G1.** Reuse the existing `test/e2e/` package and its `ContainerDriver` (`test/e2e/container_driver.go`). All Diameter/RADIUS tests live alongside the existing `TestNSSAAFullchain_*` family.
- **G2.** Tests never invoke `docker compose up/down/build` directly. Docker lifecycle is owned by Makefile targets (`test-diameter-radius`, `test-diameter-radius-sctp`).
- **G3.** Assertions are **log-only** for Diameter (CER/CEA, DWR/DWA, DER/DEA strings in `aaa-gateway` / `aaa-sim` container logs) and **log + UDP probe** for RADIUS (send Access-Request to `localhost:18120`, then assert log).
- **G4.** Tests self-skip cleanly when prerequisites are missing (`E2E_DOCKER_MANAGED` unset, host kernel lacks SCTP, host firewall blocks UDP).
- **G5.** No new Go dependencies. Tests use only `os`, `os/exec`, `strings`, `testing`, `time`.

## 3. Non-Goals

- **NG1.** Do not change the static-IP compose overlay (4 files, `INSTALL_SCTP` Dockerfiles).
- **NG2.** Do not change the underlying Diameter/RADIUS code in `internal/aaa/gateway/` or `internal/config/`. Wave 1 of the prior plan already shipped those changes.
- **NG3.** Do not add HTTP `nnssaaf-Request` triggering to the **DER/DEA** tests — they will observe logs after background traffic (DWR/DWA) plus a single HTTP-triggered NSSAA flow through the biz/http-gateway endpoint.
- **NG4.** Do not introduce a packet-capture / Diameter-parser E2E. Logs-only is sufficient for the verification goal.

## 4. Architecture

### Test execution flow (per Makefile target)

```
$ make test-diameter-radius
  1. make gen-certs                                          (existing target)
  2. make build                                              (builds bin/* binaries for volume mount)
  3. docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
  4. env E2E_DOCKER_MANAGED=1 E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
        FULLCHAIN_NRF_URL=http://localhost:8082 ... \
        go test -tags=e2e -run 'TestDiameter_TCP|TestRadius' -v -count=1 \
                -timeout=10m ./test/e2e/...
  5. on failure: docker compose down --remove-orphans; exit 1
  6. always: docker compose down --remove-orphans
```

### Test file layout (after change)

```
test/e2e/
├── e2e.go                          ← existing test entrypoint
├── driver.go                       ← existing driver interface
├── container_driver.go             ← existing ContainerDriver; **add Logs(service, tail)**
├── helpers.go                      ← existing small helpers
├── diameter_tcp_test.go            ← NEW: TestDiameter_TCP_HelloWatchdog, TestDiameter_TCP_DER_DEA_EAP
├── diameter_sctp_test.go           ← NEW: TestDiameter_SCTP_HelloWatchdog, TestDiameter_SCTP_DER_DEA_EAP (Skip on non-SCTP)
└── radius_test.go                  ← NEW: TestRadius_AccessRequest_Success, TestRadius_AccessRequest_BadSecret
```

### Files removed

```
test/e2e/fullchain_dev_diameter_radius/
├── helpers.go              ← deleted
├── diameter_tcp_test.go     ← deleted
├── diameter_sctp_test.go    ← deleted
└── radius_test.go           ← deleted
```

### `ContainerDriver.Logs` extension

`test/e2e/container_driver.go` gets one new method:

```go
// Logs returns the last `tail` lines of `docker compose logs <service>` for the
// compose project whose file is given by $E2E_COMPOSE_FILE (defaults to
// compose/fullchain-dev-tcp.yaml when unset). The driver does NOT bring the
// stack up or down; that is the Makefile's job.
func (d *ContainerDriver) Logs(service string, tail int) (string, error) { ... }
```

Tests that already have a `*ContainerDriver` can call `d.Logs("aaa-gateway", 200)`.

### Skip semantics

Each test starts with:

```go
func TestDiameter_TCP_HelloWatchdog(t *testing.T) {
    if os.Getenv("E2E_DOCKER_MANAGED") != "1" {
        t.Skip("E2E_DOCKER_MANAGED not set; run via make test-diameter-radius")
    }
    ...
}
```

SCTP tests additionally check `runtime.GOOS == "linux"` and `/proc/net/protocols` for the `SCTP` prefix, otherwise `t.Skip`.

### Assertion strategy

| Test | Trigger | Assertion |
|---|---|---|
| `TestDiameter_TCP_HelloWatchdog` | (background — forwarder auto-CERs on dial) | `Logs("aaa-gateway", 200)` contains `"CEA"` after 3s; contains `"watchdog"` / `"DWR"` after +30s |
| `TestDiameter_TCP_DER_DEA_EAP` | `POST http://localhost:8443/nnssaaf/v1/network-slice-status` with valid GPSI payload | `Logs("aaa-gateway", 200)` contains `"DER"` / `"NSSAA"` after 5s |
| `TestDiameter_SCTP_HelloWatchdog` | (same, SCTP variant) | same as TCP, on `fullchain-dev-sctp.yaml` |
| `TestDiameter_SCTP_DER_DEA_EAP` | same HTTP trigger | same, on `fullchain-dev-sctp.yaml` |
| `TestRadius_AccessRequest_Success` | UDP Access-Request to `localhost:18120` with correct shared secret | `Logs("aaa-sim", 200)` contains `"Access-Accept"` / `"EAP-Success"` |
| `TestRadius_AccessRequest_BadSecret` | UDP Access-Request with wrong secret | `Logs("aaa-sim", 200)` contains `"bad authenticator"` / `"Access-Reject"` or no response within 3s |

HTTP trigger URL comes from env `FULLCHAIN_NRF_URL` / `FULLCHAIN_UDM_URL` etc., already used by `test/e2e/`.

## 5. Makefile additions

Append two new targets after the existing `test-fullchain-no-build` block:

```makefile
.PHONY: test-diameter-radius
test-diameter-radius: gen-certs build ## Diameter TCP + RADIUS E2E
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	E2E_DOCKER_MANAGED=1 \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	$(GOTEST) -tags=e2e -run 'TestDiameter_TCP|TestRadius' -v -count=1 -timeout=10m ./test/e2e/... \
	  || { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans

.PHONY: test-diameter-radius-sctp
test-diameter-radius-sctp: gen-certs build ## Diameter SCTP E2E (skipped if kernel lacks SCTP)
	docker compose -f compose/fullchain-dev-sctp.yaml up -d --quiet-pull --wait
	E2E_DOCKER_MANAGED=1 \
	E2E_COMPOSE_FILE=compose/fullchain-dev-sctp.yaml \
	... (same env block) ...
	$(GOTEST) -tags=e2e -run 'TestDiameter_SCTP' -v -count=1 -timeout=10m ./test/e2e/... \
	  || { docker compose -f compose/fullchain-dev-sctp.yaml down --remove-orphans; exit 1; }
	docker compose -f compose/fullchain-dev-sctp.yaml down --remove-orphans
```

Both targets follow the **exact same shape** as the existing `test-fullchain-fast` (Makefile line 243–265). They can `make` against the same pre-conditions (`gen-certs`, `build`) and use the same env-var block.

## 6. Spec verification

```
Verified against:
- RFC 6733 §5.5 (CER/CEA), §5.6 (DWR/DWA), §5.7 (session termination)
- TS 29.561 §16.3 (RADIUS Access-Request/Access-Accept)
- TS 29.561 §17.3 (Diameter EAP application over TCP/SCTP)
- TS 23.502 §4.2.9 (NSSAA procedure — exercised via biz/http-gateway POST)
```

## 7. Anti-patterns to avoid

1. **Do not call `docker compose up/down` from inside a test file.** That is exactly what this spec removes.
2. **Do not bring back per-test `t.Helper()` lifecycle helpers** (`bringUp`, `tearDown`). One stack, one Makefile invocation, N tests.
3. **Do not parse Diameter/RADIUS packets.** Logs-only is sufficient and the simplest verification signal.
4. **Do not skip the existing `test-fullchain` family.** This spec adds new targets; it does not modify the existing ones.
5. **Do not introduce new test dependencies** (no `go-radius`, no Diameter-parser). Only stdlib.

## 8. Out of scope

- Modifying `Dockerfile.aaa-gateway` / `Dockerfile.aaa-sim` (already done in Task 9).
- Modifying `compose/fullchain-dev-{base,tcp,sctp,commons}.yaml` (already done in Tasks 4–7 + review fixes).
- Modifying `internal/config/AAAgwConfig.DiameterTransport` plumbing (already done in Tasks 1–3).
- Modifying `.github/workflows/fullchain-tests.yml`. CI integration is a follow-up.

## 9. Validation

After implementation:

1. `cd /home/tulm/naf3/.worktrees/static-ip-e2e`
2. `make test-diameter-radius` — exits 0 on a Linux host with Docker.
3. `make test-diameter-radius-sctp` — exits 0 with 2 SCTP tests passing, or all SCTP tests `Skip`ped if `/proc/net/protocols` lacks `SCTP`.
4. `go build ./...` and `go vet ./test/e2e/...` are clean.
5. `git ls-files test/e2e/fullchain_dev_diameter_radius/` returns empty (the old package is gone).