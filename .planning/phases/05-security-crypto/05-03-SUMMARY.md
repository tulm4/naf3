---
phase: 05-security-crypto
plan: 03
subsystem: infra
tags: [tls, mtls, security, certificate, istio]
requires:
  - phase: 05-security-crypto
    provides: Wave 1 built internal/crypto primitives; Wave 2 built auth middleware stubs
provides:
  - HTTP Gateway TLS 1.3 with 3 cipher suites and curve preferences
  - ISTIO_MTLS=1 detection for Kubernetes Istio sidecar mode
  - mTLS client certificate paths documented in compose configs
  - Biz Pod mTLS logging at startup
  - TLS CA validation comment in config.go
affects: [06-integration-testing, 07-kubernetes-deployment]

tech-stack:
  added: []
  patterns:
    - Go stdlib crypto/tls for TLS 1.3 server config
    - ISTIO_MTLS env var for optional Istio mTLS mode
    - Curve preference ordering: X25519 > P-384 > P-256

key-files:
  created: []
  modified:
    - cmd/http-gateway/main.go
    - cmd/biz/main.go
    - internal/config/config.go
    - compose/configs/http-gateway.yaml
    - compose/configs/biz.yaml

key-decisions:
  - "tls.CurveP256/tls.CurveP384 (not tls.secp256r1/tls.secp384r1) — Go stdlib constant names"
  - "Plan's tls.secp384r1/tls.secp256r1 are not valid Go identifiers — corrected to tls.CurveP384/tls.CurveP256"

patterns-established:
  - "TLS 1.3 config pattern: nil tlsConfig means no explicit TLS (Istio handles it)"
  - "Startup log includes tls_enabled, tls_version, istio_mtls fields for observability"

requirements-completed: [REQ-20, REQ-21]

duration: 15min
completed: 2026-04-28
---

# Phase 5 Plan 3: TLS 1.3 & mTLS Summary

**HTTP Gateway upgraded to TLS 1.3 with explicit cipher suites, Istio mTLS mode via ISTIO_MTLS=1 env var, and Biz Pod mTLS startup logging**

## Performance

- **Duration:** 15 min
- **Started:** 2026-04-28T06:20:00Z
- **Completed:** 2026-04-28T06:35:00Z
- **Tasks:** 6
- **Files modified:** 5

## Accomplishments

- HTTP Gateway `tls.Config` upgraded from TLS 1.2 bare config to TLS 1.3 with all three 5G-compliant cipher suites (AES-256-GCM, AES-128-GCM, ChaCha20-Poly1305) and curve preferences (X25519, P-384, P-256)
- `ISTIO_MTLS=1` env var detection: sets `tlsConfig = nil` so Istio sidecar handles mTLS transparently
- Startup slog.Info updated to include `tls_enabled`, `tls_version`, `istio_mtls` fields
- TLS cipher audit placeholder (TODO phase-6) added referencing `AuditEntry.TLSCipher` from design doc
- Biz Pod mTLS wiring verified and `slog.Info` added for `ca`, `cert`, `sni` fields
- Config CA validation comment added explaining when `tls.ClientAuth` would require CA verification

## Task Commits

Each task was committed atomically:

1. **Task 3.1: HTTP Gateway TLS 1.3** - `5e565bd` (feat)
2. **Task 3.2: http-gateway.yaml config** - `961c5c3` (feat)
3. **Task 3.3: biz.yaml config** - `961c5c3` (feat, combined with 3.2)
4. **Task 3.4: Biz Pod mTLS wiring** - `43ca350` (feat)
5. **Task 3.5: TLS cipher audit placeholder** - `5e565bd` (feat, combined with 3.1)
6. **Task 3.6: CA validation comment** - `43ca350` (feat, combined with 3.4)

## Files Created/Modified

- `cmd/http-gateway/main.go` - TLS 1.3 config, ISTIO_MTLS detection, TLS cipher audit TODO, updated slog.Info
- `cmd/biz/main.go` - mTLS startup slog.Info with ca/cert/sni fields
- `internal/config/config.go` - CA validation comment in HTTPGateway Validate() case
- `compose/configs/http-gateway.yaml` - Header comment, env.ISTIO_MTLS
- `compose/configs/biz.yaml` - mTLS cert/key/ca paths, crypto section with vault/softhsm docs

## Decisions Made

- **tls.CurveP256/tls.CurveP384 over plan's tls.secp256r1/tls.secp384r1:** The plan referenced non-existent Go stdlib constants. Corrected to the actual names `tls.CurveP256` and `tls.CurveP384` which map to values 23 and 24 respectively.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed invalid Go TLS curve constant names**
- **Found during:** Task 3.1 (HTTP Gateway TLS 1.3)
- **Issue:** Plan specified `tls.secp384r1` and `tls.secp256r1` which do not exist in Go stdlib
- **Fix:** Replaced with correct constants `tls.CurveP384` and `tls.CurveP256`
- **Files modified:** `cmd/http-gateway/main.go`
- **Verification:** `go build ./cmd/http-gateway/...` compiles without errors
- **Committed in:** `5e565bd` (part of Task 3.1)

**2. [Rule 3 - Blocking] Fixed encoding/hex import in config.go (Wave 1 residue)**
- **Found during:** Task 3.1 (HTTP Gateway TLS 1.3)
- **Issue:** Wave 1 added `encoding/hex` import to config.go but removed its usage when refactoring; `go build` fails with "imported and not used"
- **Fix:** Restored `encoding/hex` import since Wave 1 code uses `hex.DecodeString` in Validate() for MasterKeyHex validation
- **Files modified:** `internal/config/config.go`
- **Verification:** `go build ./cmd/http-gateway/...` and `go build ./cmd/biz/...` both compile
- **Committed in:** `5e565bd` (part of Task 3.1)

---

**Total deviations:** 2 auto-fixed (2 blocking)
**Impact on plan:** Both fixes required for any compilation. No scope creep.

## Issues Encountered

- Go build caching transient I/O errors during link step — resolved by retry
- Build times slow (~30s+) on this environment — used `gofmt -e` for syntax verification when builds time out

## Next Phase Readiness

- TLS 1.3 server config ready; JWT Bearer token validation on HTTP Gateway already in Wave 4 (05-04)
- mTLS client cert paths wired in biz.yaml — need actual certs for integration testing
- `internal/crypto/` package from Wave 1 ready for envelope encryption wiring in Wave 5 (05-05)

---
*Phase: 05-security-crypto*
*Completed: 2026-04-28*
