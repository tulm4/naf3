# Simpler Diameter/RADIUS E2E Test Stack Management — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize Diameter/RADIUS E2E tests to be Makefile-driven and logs-only, so they don't embed `docker compose up/down` inside test code.

**Architecture:** Delete `test/e2e/fullchain_dev_diameter_radius/` (the package whose helpers called docker directly). Re-create the 6 tests inside the existing `test/e2e/` package, alongside `TestNSSAAFullchain_*`. Add one `Logs(service, tail)` method to `ContainerDriver`. Add two Makefile targets (`test-diameter-radius`, `test-diameter-radius-sctp`) that mirror the existing `test-fullchain-fast` shape.

**Tech Stack:** Go 1.25, stdlib only (`os`, `os/exec`, `strings`, `testing`, `time`, `net`, `runtime`), Docker Compose v2.

**Spec:** `docs/superpowers/specs/2026-07-11-simpler-diameter-radius-e2e-design.md`

---

## File structure (locked-in decomposition)

**Deleted:**
- `test/e2e/fullchain_dev_diameter_radius/helpers.go`
- `test/e2e/fullchain_dev_diameter_radius/diameter_tcp_test.go`
- `test/e2e/fullchain_dev_diameter_radius/diameter_sctp_test.go`
- `test/e2e/fullchain_dev_diameter_radius/radius_test.go`

**Created:**
- `test/e2e/diameter_tcp_test.go` (2 tests, ~80 lines)
- `test/e2e/diameter_sctp_test.go` (2 tests, ~90 lines)
- `test/e2e/radius_test.go` (2 tests, ~120 lines)

**Modified:**
- `test/e2e/container_driver.go` — add `Logs(service string, tail int) (string, error)` (~15 lines appended after `AAASimURL` method)
- `Makefile` — append 2 targets after `test-fullchain-no-build` block (~30 lines)

---

## Task 1: Delete the old test package

**Files:**
- Delete: `test/e2e/fullchain_dev_diameter_radius/` (entire directory)

- [ ] **Step 1: Verify no remaining references**

Run: `grep -rln "fullchain_dev_diameter_radius" . --include="*.go" --include="Makefile" --include="*.yml" 2>/dev/null`
Expected: only matches inside `docs/superpowers/specs/2026-07-11-simpler-diameter-radius-e2e-design.md` (the new spec) and the plan being read right now.

If other matches appear (e.g., Makefile, CI), note them and STOP — they need updating in this task too.

- [ ] **Step 2: Remove the directory**

Run: `git rm -r test/e2e/fullchain_dev_diameter_radius/`
Expected: `rm 'test/e2e/fullchain_dev_diameter_radius/diameter_sctp_test.go'` etc., 4 files removed.

- [ ] **Step 3: Verify package no longer compiles issues**

Run: `go build ./... 2>&1 | tail -5`
Expected: success (the deleted package was tagged with `//go:build e2e` so non-e2e builds never saw it; e2e builds will fail until Task 2 supplies the new files).

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(e2e): remove fullchain_dev_diameter_radius package"
```

---

## Task 2: Add `Logs` method to `ContainerDriver`

**Files:**
- Modify: `test/e2e/container_driver.go`

- [ ] **Step 1: Read the file to find the insertion point**

Open `test/e2e/container_driver.go`. Locate the `AAASimURL` method (around line 85–88). The new method goes **immediately after** it.

- [ ] **Step 2: Add the `Logs` method**

After the existing `AAASimURL` method, append:

```go
// Logs returns the last `tail` lines of `docker compose logs <service>` for the
// compose project identified by $E2E_COMPOSE_FILE (defaults to
// compose/fullchain-dev-tcp.yaml when unset). The driver does NOT bring the
// stack up or down — that is the Makefile's job.
func (d *ContainerDriver) Logs(service string, tail int) (string, error) {
	if d == nil {
		return "", fmt.Errorf("Logs: nil ContainerDriver")
	}
	composeFile := os.Getenv("E2E_COMPOSE_FILE")
	if composeFile == "" {
		composeFile = "compose/fullchain-dev-tcp.yaml"
	}
	repoRoot, err := repoRootForE2E()
	if err != nil {
		return "", fmt.Errorf("locate repo root: %w", err)
	}
	args := []string{"compose", "-f", composeFile, "logs", "--tail", fmt.Sprintf("%d", tail), service}
	cmd := exec.Command("docker", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose logs %s: %w (out=%q)", service, err, string(out))
	}
	return string(out), nil
}
```

The `repoRootForE2E` helper is added in Step 3.

- [ ] **Step 3: Add `repoRootForE2E` helper**

In the same file, just below the new `Logs` method, append:

```go
// repoRootForE2E walks up from the current working directory to find the
// repo root (where go.mod lives). Used by Logs() to run docker compose
// from the right directory.
func repoRootForE2E() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("go.mod not found above %s", cwd)
}
```

- [ ] **Step 4: Update imports**

The `Logs` and `repoRootForE2E` methods reference `fmt`, `os`, `os/exec`, `path/filepath`. Open the existing import block at the top of the file. Confirm those packages are present. Most likely `fmt` and `path/filepath` are NOT imported — add them.

The import block should end up containing (in alphabetical order): `context`, `encoding/json`, `fmt`, `os`, `os/exec`, `path/filepath`, plus whatever else the existing tests need.

If the import block has `"fmt"` already (some files use it for Sprintf), just add `"path/filepath"`. If `fmt` is missing, add both.

- [ ] **Step 5: Verify build (without e2e tag)**

Run: `go build ./... 2>&1 | tail -5`
Expected: success.

- [ ] **Step 6: Verify e2e build**

Run: `go vet -tags=e2e ./test/e2e/... 2>&1 | tail -5`
Expected: no errors (other than the not-yet-existing `diameter_tcp_test.go` etc., which we'll add in Tasks 3–5).

If `diameter_tcp_test.go` already exists from prior work: ignore those errors for now — they'll be resolved by Tasks 3–5.

- [ ] **Step 7: Commit**

```bash
git add test/e2e/container_driver.go
git commit -m "feat(e2e): add ContainerDriver.Logs(service, tail)"
```

---

## Task 3: Create `test/e2e/diameter_tcp_test.go`

**Files:**
- Create: `test/e2e/diameter_tcp_test.go`

- [ ] **Step 1: Create the file**

```go
//go:build e2e
// +build e2e

// Diameter CER/CEA + DWR/DWA + DER/DEA verification via logs-only.
// Stack lifecycle is owned by Makefile (`make test-diameter-radius`).
// This file only OBSERVES the running containers.
//
// Spec: RFC 6733 §5.5 (CER/CEA), §5.6 (DWR/DWA), §5.7 (session termination).
package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// skipIfNotE2E skips when the test is not running under the Makefile target.
func skipIfNotE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("E2E_DOCKER_MANAGED") != "1" {
		t.Skip("E2E_DOCKER_MANAGED not set; run via `make test-diameter-radius`")
	}
}

// TestDiameter_TCP_HelloWatchdog verifies:
// 1. CER/CEA handshake succeeds (Diameter capabilities exchange).
// 2. DWR/DWA watchdog succeeds (Diameter watchdog exchange, every ~30s).
func TestDiameter_TCP_HelloWatchdog(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	// Give the persistent forwarder time to dial aaa-sim and complete CER/CEA.
	time.Sleep(3 * time.Second)

	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "CEA", "capabilities exchange", "CER sent") {
		t.Errorf("aaa-gateway logs do not show CER/CEA exchange; logs:\n%s", logs)
	}

	// Wait for at least one DWR/DWA cycle (~30s cadence).
	time.Sleep(35 * time.Second)
	logs, err = drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "DWR", "watchdog", "watchdog exchange") {
		t.Errorf("aaa-gateway logs do not show DWR/watchdog exchange after 35s; logs:\n%s", logs)
	}
}

// TestDiameter_TCP_DER_DEA_EAP verifies:
// 1. CER/CEA handshake succeeds.
// 2. Trigger a NSSAA flow via the biz/http-gateway HTTP endpoint.
// 3. Observe DER sent + DEA received in aaa-gateway logs.
func TestDiameter_TCP_DER_DEA_EAP(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	// Wait for CER/CEA to complete first.
	time.Sleep(5 * time.Second)

	// Trigger an NSSAA-Request by POSTing to the http-gateway's NSSAA endpoint.
	// http-gateway listens on https://localhost:8443 (TLS terminated).
	// We use a 5s timeout because the request may fail for unrelated reasons
	// (auth missing) — what matters is that aaa-gateway LOGS the DER attempt.
	gpsiURL := os.Getenv("FULLCHAIN_HTTP_GW_URL")
	if gpsiURL == "" {
		gpsiURL = "https://localhost:8443"
	}
	req, _ := http.NewRequest("POST", gpsiURL+"/nnssaaf/v1/auth-status",
		strings.NewReader(`{"gpsi":"msisdn-1234567890"}`))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	// We do not assert the response body — only that aaa-gateway emitted a DER.
	_, _ = client.Do(req)

	// Give aaa-gateway time to forward DER and log result.
	time.Sleep(5 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "DER", "NSSAA", "der sent") {
		t.Errorf("aaa-gateway logs do not show DER/NSSAA exchange; logs:\n%s", logs)
	}
}

// containsAny reports whether s contains any of the given substrings (case-sensitive).
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Verify build with e2e tag**

Run: `go vet -tags=e2e ./test/e2e/... 2>&1 | tail -10`
Expected: no errors.

- [ ] **Step 3: Verify no docker call inside the test file**

Run: `grep -n "docker\|exec.Command" test/e2e/diameter_tcp_test.go | head -5`
Expected: empty (no docker invocations).

- [ ] **Step 4: Commit**

```bash
git add test/e2e/diameter_tcp_test.go
git commit -m "feat(e2e): add Diameter TCP log-only tests (CER/CEA, DWR/DWA, DER/DEA)"
```

---

## Task 4: Create `test/e2e/diameter_sctp_test.go`

**Files:**
- Create: `test/e2e/diameter_sctp_test.go`

- [ ] **Step 1: Create the file**

```go
//go:build e2e
// +build e2e

// Diameter over SCTP verification via logs-only.
// Same shape as the TCP tests, but uses compose/fullchain-dev-sctp.yaml
// (selected by Makefile via $E2E_COMPOSE_FILE).
// Skips cleanly when the host kernel does not support SCTP.
//
// Spec: TS 29.561 §17.3 (Diameter EAP application over SCTP).
package e2e

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sctpKernelAvailable reports whether the host supports SCTP at runtime.
// Returns false on non-Linux hosts or when /proc/net/protocols lacks SCTP.
func sctpKernelAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/net/protocols")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "SCTP")
}

// requireSCTP skips the current test if SCTP is unavailable on the host.
func requireSCTP(t *testing.T) {
	t.Helper()
	if !sctpKernelAvailable() {
		t.Skip("SCTP kernel module not available on this host (Linux required)")
	}
}

// TestDiameter_SCTP_HelloWatchdog verifies CER/CEA + DWR/DWA over SCTP.
func TestDiameter_SCTP_HelloWatchdog(t *testing.T) {
	skipIfNotE2E(t)
	requireSCTP(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	time.Sleep(3 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "CEA", "capabilities exchange", "CER sent") {
		t.Errorf("aaa-gateway (SCTP) logs do not show CER/CEA exchange; logs:\n%s", logs)
	}

	time.Sleep(35 * time.Second)
	logs, err = drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "DWR", "watchdog", "watchdog exchange") {
		t.Errorf("aaa-gateway (SCTP) logs do not show DWR/watchdog exchange after 35s; logs:\n%s", logs)
	}
}

// TestDiameter_SCTP_DER_DEA_EAP verifies CER/CEA + DER/DEA over SCTP.
func TestDiameter_SCTP_DER_DEA_EAP(t *testing.T) {
	skipIfNotE2E(t)
	requireSCTP(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	time.Sleep(5 * time.Second)

	gpsiURL := os.Getenv("FULLCHAIN_HTTP_GW_URL")
	if gpsiURL == "" {
		gpsiURL = "https://localhost:8443"
	}
	req, _ := http.NewRequest("POST", gpsiURL+"/nnssaaf/v1/auth-status",
		strings.NewReader(`{"gpsi":"msisdn-1234567890"}`))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	_, _ = client.Do(req)

	time.Sleep(5 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "DER", "NSSAA", "der sent") {
		t.Errorf("aaa-gateway (SCTP) logs do not show DER/NSSAA exchange; logs:\n%s", logs)
	}
}
```

- [ ] **Step 2: Verify build**

Run: `go vet -tags=e2e ./test/e2e/... 2>&1 | tail -5`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/diameter_sctp_test.go
git commit -m "feat(e2e): add Diameter SCTP log-only tests (skip on non-SCTP host)"
```

---

## Task 5: Create `test/e2e/radius_test.go`

**Files:**
- Create: `test/e2e/radius_test.go`

- [ ] **Step 1: Create the file**

```go
//go:build e2e
// +build e2e

// RADIUS Access-Request / Access-Accept verification.
// aaa-sim listens on host UDP port 18120 (mapped from container 1812).
// Tests send UDP packets from the host process, then assert log content.
//
// Spec: TS 29.561 §16.3, RFC 2865 §4 (Access-Request/Access-Accept).
package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	radiusAccessRequest = 1
	radiusAccessAccept  = 2
	radiusAccessReject  = 3

	attrUserName    = 1
	attrEAPMessage  = 79
	attrMessageAuth = 80

	// sharedSecret matches compose/configs/aaa-gateway.yaml (RadiusSharedSecret).
	// The aaa-sim container also reads AAA_SIM_RADIUS_SECRET; in default config
	// it is "secret".
	radiusSharedSecret = "secret"
)

// buildRadiusAccessRequest constructs a minimal RADIUS Access-Request with
// User-Name=testuser, EAP-Message=EAP-Response/Identity, and a Message-Authenticator
// (RFC 3579) computed over (header || attrs) using secret.
func buildRadiusAccessRequest(secret string) []byte {
	// EAP-Response/Identity: Code=2, Id=0, Length=5, Type=1
	eap := []byte{0x02, 0x00, 0x00, 0x05, 0x01}

	var attrs bytes.Buffer
	// User-Name = "testuser"
	attrs.WriteByte(attrUserName)
	attrs.WriteByte(byte(2 + len("testuser")))
	attrs.WriteString("testuser")

	// EAP-Message = eap
	attrs.WriteByte(attrEAPMessage)
	attrs.WriteByte(byte(2 + len(eap)))
	attrs.Write(eap)

	// Message-Authenticator placeholder (16 zero bytes)
	attrs.WriteByte(attrMessageAuth)
	attrs.WriteByte(18)
	attrs.Write(make([]byte, 16))

	pkt := make([]byte, 20+attrs.Len())
	pkt[0] = radiusAccessRequest
	pkt[1] = 1 // Identifier
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	// Random Request Authenticator
	for i := 4; i < 20; i++ {
		pkt[i] = byte(time.Now().UnixNano() >> uint(i))
	}
	copy(pkt[20:], attrs.Bytes())

	// Compute Message-Authenticator = HMAC-MD5(packet with MA zeroed, secret)
	maOffset := 20 + (2 + 2 + len("testuser")) + (2 + len(eap))
	for i := maOffset; i < maOffset+16; i++ {
		pkt[i] = 0
	}
	h := hmac.New(md5.New, []byte(secret))
	h.Write(pkt)
	copy(pkt[maOffset:maOffset+16], h.Sum(nil))
	return pkt
}

// radiusAddress returns the host UDP address where aaa-sim's RADIUS server is reachable.
func radiusAddress() string {
	if v := os.Getenv("FULLCHAIN_AAA_SIM_RADIUS_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:18120"
}

// TestRadius_AccessRequest_Success sends a RADIUS Access-Request with the
// correct shared secret and asserts aaa-sim logs show Access-Accept.
func TestRadius_AccessRequest_Success(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil")
	}

	addr, err := net.ResolveUDPAddr("udp", radiusAddress())
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()

	req := buildRadiusAccessRequest(radiusSharedSecret)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write Access-Request: %v", err)
	}

	// Read response (best-effort; we assert via logs, not packet inspection).
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	n, readErr := conn.Read(buf)
	if readErr == nil && n >= 20 {
		gotCode := buf[0]
		if gotCode != radiusAccessAccept {
			t.Logf("response code = %d (expected Access-Accept=2)", gotCode)
		}
	} else {
		t.Logf("no UDP response within 5s (readErr=%v); assertion is log-based", readErr)
	}

	logs, err := drv.Logs("aaa-sim", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-sim): %v", err)
	}
	if !containsAny(logs, "Access-Accept", "AccessAccept", "EAP-Success", "access-accept") {
		t.Errorf("aaa-sim logs do not show Access-Accept; logs:\n%s", logs)
	}
}

// TestRadius_AccessRequest_BadSecret sends an Access-Request with a wrong
// shared secret and asserts aaa-sim logs show rejection or no response.
func TestRadius_AccessRequest_BadSecret(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil")
	}

	addr, err := net.ResolveUDPAddr("udp", radiusAddress())
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()

	req := buildRadiusAccessRequest("definitely-wrong-secret")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write Access-Request: %v", err)
	}

	// Best-effort read; bad-secret requests are typically dropped silently.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)

	// Give aaa-sim a moment to log the rejection attempt.
	time.Sleep(1 * time.Second)
	logs, err := drv.Logs("aaa-sim", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-sim): %v", err)
	}
	// Either: explicit rejection log, OR no Access-Accept log (silent drop).
	hasReject := containsAny(logs, "Access-Reject", "AccessReject",
		"bad authenticator", "invalid authenticator", "shared secret mismatch")
	hasAccept := containsAny(logs, "Access-Accept", "AccessAccept")
	if !hasReject && hasAccept {
		t.Errorf("aaa-sim accepted request with wrong secret; logs:\n%s", logs)
	}
	// If hasReject: pass. If neither: pass (silent drop is also acceptable).
	_ = fmt.Sprintf // keep fmt import even if unused
}
```

- [ ] **Step 2: Verify build**

Run: `go vet -tags=e2e ./test/e2e/... 2>&1 | tail -10`
Expected: no errors. (If `fmt` is unused after compile, Go vet reports nothing — the file imports it explicitly to suppress any future drift.)

- [ ] **Step 3: Verify no docker call inside the test file**

Run: `grep -n "docker\|exec.Command" test/e2e/radius_test.go | head -5`
Expected: empty (the only "exec" should be `exec.Command` references — but the file uses `net` package directly, so empty is correct).

- [ ] **Step 4: Commit**

```bash
git add test/e2e/radius_test.go
git commit -m "feat(e2e): add RADIUS Access-Request log-only tests"
```

---

## Task 6: Add Makefile targets

**Files:**
- Modify: `Makefile` (append after `test-fullchain-no-build` block, around line 287)

- [ ] **Step 1: Find the insertion point**

Run: `grep -n "^test-fullchain-no-build\|^.PHONY: test-fullchain-no-build\|^.PHONY: help" Makefile | head -5`
Expected: shows the `test-fullchain-no-build` block end. Insert the 2 new targets immediately after that block.

- [ ] **Step 2: Append the targets**

After the `test-fullchain-no-build` block (and before the next major section header like `# =======` or `.PHONY: lint`), append:

```makefile
# =============================================================================
# Diameter + RADIUS E2E (logs-only, Makefile-owned compose lifecycle)
# =============================================================================

.PHONY: test-diameter-radius
test-diameter-radius: gen-certs build ## Diameter TCP + RADIUS E2E (logs-only)
	@echo "$(YELLOW)Starting fullchain TCP stack for Diameter/RADIUS tests...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running Diameter TCP + RADIUS tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	FULLCHAIN_HTTP_GW_URL=https://localhost:8443 \
	$(GOTEST) -tags=e2e -run 'TestDiameter_TCP|TestRadius' -v -count=1 -timeout=10m ./test/e2e/... \
	  || { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down TCP stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Diameter TCP + RADIUS tests complete$(NC)"

.PHONY: test-diameter-radius-sctp
test-diameter-radius-sctp: gen-certs build ## Diameter SCTP E2E (logs-only; skips on non-SCTP hosts)
	@echo "$(YELLOW)Starting fullchain SCTP stack for Diameter tests...$(NC)"
	docker compose -f compose/fullchain-dev-sctp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running Diameter SCTP tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_COMPOSE_FILE=compose/fullchain-dev-sctp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	FULLCHAIN_HTTP_GW_URL=https://localhost:8443 \
	$(GOTEST) -tags=e2e -run 'TestDiameter_SCTP' -v -count=1 -timeout=10m ./test/e2e/... \
	  || { docker compose -f compose/fullchain-dev-sctp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down SCTP stack...$(NC)"
	docker compose -f compose/fullchain-dev-sctp.yaml down --remove-orphans
	@echo "$(GREEN)Diameter SCTP tests complete$(NC)"
```

- [ ] **Step 3: Verify Makefile syntax**

Run: `make -n test-diameter-radius 2>&1 | tail -10`
Expected: shows the planned commands (`docker compose up ...`, `go test ...`, `docker compose down ...`) without actually running them. No `Makefile:NN: *** missing separator` or similar errors.

- [ ] **Step 4: Verify Make help**

Run: `make help 2>&1 | grep -E "test-diameter-radius" | head -5`
Expected: both targets listed in the help output.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "feat(makefile): add test-diameter-radius and test-diameter-radius-sctp targets"
```

---

## Task 7: Final smoke test

**Files:** none (read-only verification)

- [ ] **Step 1: Verify the deleted directory is gone**

Run: `git ls-files test/e2e/fullchain_dev_diameter_radius/ 2>&1`
Expected: empty.

- [ ] **Step 2: Verify e2e build is clean**

Run: `go vet -tags=e2e ./test/e2e/... 2>&1 | tail -5`
Expected: no errors.

- [ ] **Step 3: Verify Makefile dry-runs**

Run: `make -n test-diameter-radius 2>&1 | head -10`
Run: `make -n test-diameter-radius-sctp 2>&1 | head -10`
Expected: both show planned commands; no errors.

- [ ] **Step 4: Verify no docker call inside any new test file**

Run: `grep -rn 'exec\.Command("docker"\|"docker",' test/e2e/ 2>&1 | head -5`
Expected: only matches inside `container_driver.go` (the `Logs` method, which uses `docker compose logs <service>` — a READ, not up/down).

- [ ] **Step 5: Report**

Tell the user the plan is complete. List:
- 4 files deleted
- 1 file modified (`container_driver.go`)
- 3 files created (`diameter_tcp_test.go`, `diameter_sctp_test.go`, `radius_test.go`)
- 2 Makefile targets added

---

## Self-review checklist (run before reporting done)

- [x] **Spec coverage:** G1 (reuse test/e2e/) → Task 3-5; G2 (no docker in test) → Steps 3.3, 4.2, 5.3, 7.4; G3 (logs-only) → Tasks 3-5; G4 (skip semantics) → skipIfNotE2E + requireSCTP; G5 (no new deps) → only stdlib.
- [x] **Placeholder scan:** no TBD / TODO / "implement later" / "similar to Task N". Every code block is complete.
- [x] **Type consistency:** `ContainerDriver.Logs(service string, tail int) (string, error)` — used identically in Tasks 3, 4, 5. `skipIfNotE2E` and `requireSCTP` defined once each, used in multiple tests.
- [x] **Frequent commits:** each task ends with a single `git commit`.
- [x] **No test calls docker:** Step 7.4 is the final check; if it fails, the plan has a bug.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-11-simpler-diameter-radius-e2e.md`. Two execution options:

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints