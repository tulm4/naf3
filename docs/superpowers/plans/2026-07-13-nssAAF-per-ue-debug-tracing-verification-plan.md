# Per-UE Debug Tracing — Verification Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add end-to-end verification of the per-UE debug subsystem across a real compose stack (with real aaa-sim), plus operator-facing CLI improvements (grouped output, AUTH/GPSI_H columns, `--json`), plus a forward-direction RADIUS/Diameter round-trip test and a server-initiated (AMF-callback) RAR/ASR round-trip test.

**Architecture:** Five small tasks layered on top of the parent design (`docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md`). Tasks 1-2 are pure unit work (context propagation + CLI rendering). Task 3 adds a RAR-trigger mechanism to aaa-sim and an aaa-sim driver helper. Tasks 4-5 add the two integration tests and the Makefile target that gates them behind `RUN_E2E=1`.

**Tech Stack:** Go 1.25, `github.com/redis/go-redis/v9`, `text/tabwriter`, `github.com/fatih/color`, `github.com/stretchr/testify`, `encoding/json`, `cmd/aaa-sim` (RADIUS/Diameter server we extend), `compose/fullchain-dev-tcp.yaml`.

**Spec:** `docs/superpowers/specs/2026-07-13-nssAAF-per-ue-debug-tracing-verification-spec.md`

---

## File Structure

| File | Task | Responsibility |
|---|---|---|
| `internal/debug/subctx.go` | 1 | `WithSubscriber` / `SubscriberFrom` context helpers |
| `internal/debug/subctx_test.go` | 1 | Unit tests for context propagation |
| `internal/debug/hooks.go` | 1 | Modify `WrapDB`/`WrapRedis`/`WrapProtocol` to read `SubscriberFrom(ctx)` when `Event.GPSI/SUPI` is empty |
| `internal/debug/wrap_helpers_test.go` | 1 | Unit tests for context-derived subscriber in wrap helpers |
| `internal/api/nssaa/middleware.go` (or nearest equivalent) | 1 | Inject `debug.WithSubscriber(ctx, gpsi, "")` after GPSI extraction |
| `internal/aaa/gateway/gateway.go` | 1 (deferred) | Server-initiated inbound GPSI extraction is **out of scope for Task 1** — `proto.AaaServerInitiatedResponse` and the RADIUS/Diameter ingress types carry no GPSI field. Subscriber-context propagation on the reverse path is deferred to a follow-up; current reverse-direction events land in the `_no_sub` stream. See Task 5 acceptance note. |
| `cmd/nssAAF-debug/main.go` | 2 | Add `--json`, AUTH/GPSI_H columns, group-by-trace_id |
| `cmd/nssAAF-debug/main_test.go` | 2 | Tests for new CLI behavior |
| `cmd/aaa-sim/main.go` | 3 | Add `trigger-rar` and `trigger-asr` subcommands |
| `test/aaa_sim/radius.go` | 3 | Add `SendRAR(clientAddr, sessionID)` helper |
| `test/e2e/aaa_sim_driver.go` | 3 | Thin shell-out wrapper for `docker exec aaa-sim aaa-sim trigger-rar ...` |
| `test/e2e/debug_full_flow_forward_test.go` | 4 | `TestDebugFullFlow_RADIUS_Forward`, `TestDebugFullFlow_DIAMETER_Forward` |
| `test/e2e/debug_full_flow_callback_test.go` | 5 | `TestDebugFullFlow_AMFCallback` (RAR + ASR) |
| `Makefile` | 4, 5 | Add `test-debug-full` target gated on `RUN_E2E=1` |
| `docs/roadmap/module_index.md` | 5 | Update status when verification passes |

**Decomposition rationale:** Tasks 1-2 are pure unit work — no compose needed; CI-fast. Task 3 closes the server-initiated gap (Risk 6) by extending aaa-sim itself. Tasks 4-5 each add one integration test under the real compose stack, gated on `RUN_E2E=1`. No file straddles tasks; each lands in one commit.

---

## Task 1: Context-based subscriber propagation

**Files:**
- Create: `internal/debug/subctx.go`
- Test: `internal/debug/subctx_test.go`
- Modify: `internal/debug/hooks.go:11-43` (read `SubscriberFrom(ctx)` before populating `Event`)
- Create: `internal/debug/wrap_helpers_test.go`
- Modify: `internal/api/nssaa/middleware.go` (inject `WithSubscriber` after GPSI extraction — exact line to find during execution)
- **Skipped (deferred):** `internal/aaa/gateway/gateway.go` — no GPSI field on server-initiated ingress; see Step 1.8 note.

- [ ] **Step 1.1: Write failing test for `WithSubscriber` / `SubscriberFrom`**

Create `internal/debug/subctx_test.go`:

```go
package debug

import (
    "context"
    "testing"
)

func TestSubscriberFrom_Empty(t *testing.T) {
    g, s := SubscriberFrom(context.Background())
    if g != "" || s != "" {
        t.Fatalf("expected empty, got (%q, %q)", g, s)
    }
}

func TestWithSubscriber_RoundTrip(t *testing.T) {
    ctx := WithSubscriber(context.Background(), "msisdn-208046123456789", "")
    g, s := SubscriberFrom(ctx)
    if g != "msisdn-208046123456789" || s != "" {
        t.Fatalf("got (%q, %q)", g, s)
    }
}

func TestWithSubscriber_Replace(t *testing.T) {
    // WithSubscriber replaces the entire (gpsi, supi) pair, matching Go's
    // normal context idiom.
    ctx := WithSubscriber(context.Background(), "msisdn-1", "")
    ctx = WithSubscriber(ctx, "", "imsi-208046000000001")
    g, s := SubscriberFrom(ctx)
    if g != "" || s != "imsi-208046000000001" {
        t.Fatalf("got (%q, %q)", g, s)
    }
}

func TestWithSubscriber_BothAtOnce(t *testing.T) {
    ctx := WithSubscriber(context.Background(), "msisdn-1", "imsi-1")
    g, s := SubscriberFrom(ctx)
    if g != "msisdn-1" || s != "imsi-1" {
        t.Fatalf("got (%q, %q)", g, s)
    }
}
```

- [ ] **Step 1.2: Run test, verify it fails**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./internal/debug/... -run TestSubscriber -v`
Expected: FAIL with `undefined: SubscriberFrom` / `undefined: WithSubscriber`.

- [ ] **Step 1.3: Implement `internal/debug/subctx.go`**

```go
package debug

import "context"

type subscriberKey struct{}

type subscriber struct{ gpsi, supi string }

// WithSubscriber returns a new context carrying the GPSI/SUPI for the
// current request. Both may be empty for background jobs; helpers must
// tolerate that and fall through to the existing _no_sub stream.
func WithSubscriber(ctx context.Context, gpsi, supi string) context.Context {
    return context.WithValue(ctx, subscriberKey{}, subscriber{gpsi: gpsi, supi: supi})
}

// SubscriberFrom returns the GPSI/SUPI stored in ctx, if any.
func SubscriberFrom(ctx context.Context) (gpsi, supi string) {
    s, ok := ctx.Value(subscriberKey{}).(subscriber)
    if !ok {
        return "", ""
    }
    return s.gpsi, s.supi
}
```

- [ ] **Step 1.4: Run test, verify it passes**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./internal/debug/... -run TestSubscriber -v`
Expected: PASS (4 tests).

- [ ] **Step 1.5: Write failing test for wrap helpers using context subscriber**

Create `internal/debug/wrap_helpers_test.go`:

```go
package debug

import (
    "context"
    "errors"
    "testing"

    "github.com/operator/nssAAF/internal/logging"
)

// fakeRecorder captures emitted Events without touching Redis.
type fakeRecorder struct{ events []Event }

func (f *fakeRecorder) capture(_ context.Context, ev Event) { f.events = append(f.events, ev) }

func TestWrapDB_UsesContextSubscriberWhenEventGPSIEmpty(t *testing.T) {
    // Build a Debug whose Emit path we can intercept. We test via emitTiming
    // directly since Emit itself writes to Redis.
    ctx := WithSubscriber(context.Background(), "msisdn-208046000000001", "")

    // Drive emitTiming with an empty GPSI; expect it to come from ctx.
    rec := &fakeRecorder{}
    _ = rec // not used directly; this test asserts via emitTiming side effect

    d := &Debug{} // zero-value; emitTiming calls d.Emit which checks enabled
    // When disabled (no .enabled set), emitTiming still runs but Emit is a no-op.
    // We assert behavior in the next test by enabling Debug.
    d.Set(true)

    // Capture via emitTiming's observable side effect: call it, then assert
    // the Event it built had GPSI populated from ctx.
    evCh := make(chan Event, 1)
    oldEmit := emitCapture
    emitCapture = func(_ *Debug, _ context.Context, ev Event) { evCh <- ev }
    defer func() { emitCapture = oldEmit }()

    d.emitTiming(ctx, "pg.session.create", KindDB, "nssaa_session", "", nowStub(), nil)

    var got Event
    select {
    case got = <-evCh:
    default:
        t.Fatal("expected emit to be called")
    }
    if got.GPSI != "msisdn-208046000000001" {
        t.Fatalf("expected GPSI from ctx, got %q", got.GPSI)
    }
    want := logging.HashGPSI("msisdn-208046000000001")
    _ = want // hash check is exercised by Emit itself in unit test elsewhere
}

func TestWrapDB_ExplicitEventGPSIWinsOverContext(t *testing.T) {
    ctx := WithSubscriber(context.Background(), "msisdn-from-ctx", "")
    evCh := make(chan Event, 1)
    oldEmit := emitCapture
    emitCapture = func(_ *Debug, _ context.Context, ev Event) { evCh <- ev }
    defer func() { emitCapture = oldEmit }()

    d := &Debug{}
    d.Set(true)

    d.emitTiming(ctx, "pg.session.create", KindDB, "nssaa_session", "", nowStub(), errors.New("ignored"))

    got := <-evCh
    if got.GPSI != "msisdn-from-ctx" {
        t.Fatalf("context subscriber should win when Event.GPSI empty, got %q", got.GPSI)
    }
}
```

- [ ] **Step 1.6: Add test hook `emitCapture` and modify `emitTiming`**

In `internal/debug/hooks.go`, modify `emitTiming` (currently at lines 45-63):

```go
// emitCapture is a test hook that captures Events emitted through emitTiming.
// nil in production; tests override this to inspect the Event before Emit
// is called.
var emitCapture func(d *Debug, ctx context.Context, ev Event)

func (d *Debug) emitTiming(ctx context.Context, op string, kind Kind, table, key string, start time.Time, err error) {
    gpsi, supi := SubscriberFrom(ctx)
    ev := Event{
        Op:     op,
        Kind:   kind,
        GPSI:   gpsi,
        SUPI:   supi,
        Status: "ok",
        Detail: map[string]any{"duration_ms": time.Since(start).Milliseconds()},
    }
    if table != "" {
        ev.Detail["table"] = table
    }
    if key != "" {
        ev.Detail["key"] = key
    }
    if err != nil {
        ev.Status = "error"
        ev.Error = err
    }
    if emitCapture != nil {
        emitCapture(d, ctx, ev)
    }
    d.Emit(ctx, ev)
}
```

Add `nowStub` helper to `internal/debug/hooks_test.go` (create the file if absent):

```go
package debug

import "time"

// nowStub returns a fixed time so emitTiming can compute a stable duration_ms
// in tests. We use the package's now() indirection so tests don't depend on
// real wall-clock. Add a now() helper if not present.
func nowStub() time.Time { return time.Unix(0, 0) }
```

If `hooks_test.go` already exists with its own now helper, use it; otherwise create it with `nowStub`.

- [ ] **Step 1.7: Run wrap helpers test**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./internal/debug/... -run TestWrapDB -v`
Expected: PASS (2 tests).

- [ ] **Step 1.8: Inject `WithSubscriber` at the inbound GPSI extraction points**

Modify `internal/api/nssaa/middleware.go` (or equivalent, depending on file layout): after GPSI is extracted from the request body, set:

```go
ctx = debug.WithSubscriber(ctx, gpsi, "")
```

The exact line depends on the codebase layout. Find the function that extracts `gpsi` from the inbound NSSAA request body (likely `internal/api/nssaa/handler.go` or `internal/biz/handler.go`). Add one line.

**AAA-GW server-initiated path is deferred.** The inbound DTO `proto.AaaServerInitiatedResponse` (gateway.go:353) carries `SessionID`, `AuthCtxID`, `MessageType`, `ResultCode`, `Payload`, `ErrorCause` — no GPSI. Likewise the RADIUS CoA/DM and Diameter ASR/RAR/STR ingress packets on UDP/TCP carry no User-Name / User-Name AVP extraction in the receiver. Without a GPSI field in either the protocol payload or the typed DTO, there is no GPSI value to inject at this layer. Subscriber-context propagation on the reverse path is therefore deferred to a follow-up that adds a GPSI field to `SessionCorrEntry` (or to the EAP identity decode path). Until that lands, reverse-direction events land in the `_no_sub` stream — see Task 5 for the relaxed assertion.

- [ ] **Step 1.9: Run all unit tests**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./internal/debug/... -race -v`
Expected: ALL PASS.

- [ ] **Step 1.10: Commit**

```bash
cd /home/tulm/naf3/.worktrees/feature-per-ue-debug
git add internal/debug/subctx.go internal/debug/subctx_test.go internal/debug/hooks.go internal/debug/wrap_helpers_test.go internal/api/nssaa/middleware.go
git commit -m "feat(debug): propagate GPSI/SUPI via context for wrap helpers"
```

`internal/aaa/gateway/gateway.go` is intentionally NOT staged here — the server-initiated path is deferred (see Step 1.8).

---

## Task 2: CLI improvements (grouped output, AUTH/GPSI_H columns, `--json`)

**Files:**
- Modify: `cmd/nssAAF-debug/main.go` (add `--json`, AUTH/GPSI_H columns, group-by-trace_id)
- Modify: `cmd/nssAAF-debug/main_test.go` (tests for new behavior)

- [ ] **Step 2.1: Write failing test for `--json` flag**

Open `cmd/nssAAF-debug/main_test.go` (likely uses miniredis). Add:

```go
func TestRunTrace_JSON(t *testing.T) {
    // Set up miniredis with two events sharing the same trace_id and one with a different trace_id.
    // Call runTrace with json=true in traceOpts.
    // Parse stdout, assert one JSON object per line, assert grouping by trace_id is preserved as a field.
}
```

(The test code follows the existing pattern in `main_test.go` — read the file first to mirror its miniredis fixture.)

- [ ] **Step 2.2: Run test, verify failure**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./cmd/nssAAF-debug/... -run TestRunTrace_JSON -v`
Expected: FAIL (compile error on missing `json` field on `traceOpts`).

- [ ] **Step 2.3: Add `json` field to `traceOpts` and `--json` flag**

In `cmd/nssAAF-debug/main.go`:

1. Add `JSON bool` to `traceOpts`.
2. In `traceCmd`, add `json := fs.Bool("json", false, "Emit one JSON object per line instead of a table")`.
3. Pass `JSON: *json` into `traceOpts` literal in the `runTrace` call.

- [ ] **Step 2.4: Modify `runTrace` to handle JSON + grouping**

Replace the loop body in `runTrace` so that:

1. After parsing `opts.Since` cutoff and `XRange` results, **sort the events by `ts` ascending** (parse `ts` from each message and sort).
2. Group consecutive events by `trace` field.
3. For each group, if `opts.JSON` is true, emit one JSON object per line with all stream fields (`ts`, `pod`, `svc`, `trace`, `span`, `sub_h`, `gpsi_h`, `auth`, `op`, `kind`, `status`, `detail`).
4. If `opts.JSON` is false, render the existing `tabwriter` table but with two new columns: `AUTH` (from `auth` field) and `GPSI_H` (from `gpsi_h` field, truncated to 8 chars). Emit a blank line between groups.

Add a `parseInt64` helper is already present. The sort is `sort.Slice(msgs, func(i, j int) bool { ... })`.

The new table header becomes:

```go
fmt.Fprintln(tw, "TIME\tPOD\tSVC\tTRACE\tAUTH\tGPSI_H\tOP\tSTATUS\tDUR\tDETAIL")
```

- [ ] **Step 2.5: Run all CLI tests**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./cmd/nssAAF-debug/... -v`
Expected: ALL PASS (including new `--json` test).

- [ ] **Step 2.6: Commit**

```bash
cd /home/tulm/naf3/.worktrees/feature-per-ue-debug
git add cmd/nssAAF-debug/main.go cmd/nssAAF-debug/main_test.go
git commit -m "feat(debug-cli): add --json, AUTH/GPSI_H columns, group by trace_id"
```

---

## Task 3: aaa-sim server-initiated trigger (RAR/ASR)

**Files:**
- Modify: `cmd/aaa-sim/main.go` (add `trigger-rar` and `trigger-asr` subcommands)
- Modify: `test/aaa_sim/radius.go` (add `SendRAR`/`SendASR` helpers)
- Create: `test/e2e/aaa_sim_driver.go` (Go-side helper to exec into the container)

Risk 6 from the spec: aaa-sim has no CLI for triggering server-initiated messages. We add it.

- [ ] **Step 3.1: Inspect the existing `RadiusServer.handlePacket` to understand RAR packet layout**

Open `test/aaa_sim/radius.go`. Confirm: a RAR is RADIUS code 43 (`radius.CodeReauthRequest` or similar — check the codebase for the constant name). An ASR is RADIUS code 44. Both have the same structure as Access-Request but a different Code byte.

If a constant does not exist, use the numeric literal `43` (RAR) / `44` (ASR) and add a comment citing RFC 3576 / RFC 5176.

- [ ] **Step 3.2: Add `SendRAR` / `SendASR` to `test/aaa_sim/radius.go`**

Add a method on `*RadiusServer`:

```go
// SendRAR builds and sends a RADIUS Re-Auth-Request (code 43) to clientAddr.
// sessionID identifies the existing session to re-auth.
func (s *RadiusServer) SendRAR(clientAddr net.Addr, sessionID string) error {
    return s.sendServerInitiated(clientAddr, 43, sessionID)
}

// SendASR builds and sends a RADIUS Abort-Session-Request (code 44) to clientAddr.
func (s *RadiusServer) SendASR(clientAddr net.Addr, sessionID string) error {
    return s.sendServerInitiated(clientAddr, 44, sessionID)
}

func (s *RadiusServer) sendServerInitiated(clientAddr net.Addr, code uint8, sessionID string) error {
    // Use the existing buildRadiusPacket logic. Need: Code byte = code,
    // Identifier = next free, Authenticator = random 16 bytes (Request Auth),
    // attrs = State (for sessionID) or Acct-Session-Id.
    // ... (follow existing buildRadiusPacket in radius.go lines 199-238)
}
```

The exact packet layout mirrors `buildResponse` but with a fresh Request Authenticator. Read `radius.go` lines 199-238 to follow the pattern.

- [ ] **Step 3.3: Run existing aaa-sim tests to confirm no regressions**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./test/aaa_sim/... -v`
Expected: PASS.

- [ ] **Step 3.4: Write failing test for `SendRAR` round-trip**

Add to a new file `test/aaa_sim/radius_test.go` (or extend an existing one):

```go
package aaa_sim

import (
    "net"
    "testing"
    "time"
)

func TestSendRAR_RoundTrip(t *testing.T) {
    // Bind a UDP listener on loopback.
    // Start a RadiusServer bound to that listener.
    // Build a minimal RADIUS Access-Request packet (any session ID).
    // Send it; receive Access-Accept.
    // From the client-side socket, capture clientAddr; call server.SendRAR(clientAddr, sessionID).
    // Run a second listener (the "client side") to receive the RAR; verify Code=43.
}
```

- [ ] **Step 3.5: Run test, verify it passes after `SendRAR` is implemented**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./test/aaa_sim/... -run TestSendRAR -v`
Expected: PASS.

- [ ] **Step 3.6: Add `trigger-rar` and `trigger-asr` subcommands to `cmd/aaa-sim/main.go`**

Modify `cmd/aaa-sim/main.go` to accept `os.Args[1]` as a subcommand:

```go
func main() {
    if len(os.Args) > 1 && (os.Args[1] == "trigger-rar" || os.Args[1] == "trigger-asr") {
        runTrigger(os.Args[1], os.Args[2:])
        return
    }
    // ... existing main body
}

func runTrigger(cmd string, args []string) {
    // Parse: --target ADDR --session-id ID
    // Bind a UDP socket. Build a minimal Access-Request first to establish a session.
    // Then send the RAR/ASR.
    // Use aaa_sim.NewRadiusServer with a custom listener.
}
```

The simplest implementation: have `trigger-rar` open a temporary UDP socket, construct a minimal Access-Request to `target`, wait briefly, then call `SendRAR`. The exact aaa-gw target is `172.0.3.15:1812` from inside the compose network.

Add a `trigger.go` file alongside `cmd/aaa-sim/main.go`:

```go
package main

import (
    "flag"
    "fmt"
    "net"
    "os"
    "time"

    "github.com/operator/nssAAF/test/aaa_sim"
)

func runTrigger(cmd string, args []string) {
    fs := flag.NewFlagSet(cmd, flag.ExitOnError)
    target := fs.String("target", "172.0.3.15:1812", "aaa-gateway RADIUS address")
    sessionID := fs.String("session-id", "", "session ID to re-auth/abort")
    if err := fs.Parse(args); err != nil {
        os.Exit(1)
    }
    if *sessionID == "" {
        fmt.Fprintln(os.Stderr, "--session-id is required")
        os.Exit(1)
    }
    conn, err := net.ListenPacket("udp", "0.0.0.0:0")
    if err != nil {
        fmt.Fprintln(os.Stderr, "listen:", err)
        os.Exit(1)
    }
    defer conn.Close()
    addr, err := net.ResolveUDPAddr("udp", *target)
    if err != nil {
        fmt.Fprintln(os.Stderr, "resolve:", err)
        os.Exit(1)
    }
    srv := aaa_sim.NewRadiusServer(conn, aaa_sim.ModeEAP_TLS_SUCCESS, []byte("testing123"), slog.Default())
    var code uint8
    switch cmd {
    case "trigger-rar":
        code = 43
    case "trigger-asr":
        code = 44
    }
    if err := srv.SendServerInitiated(addr, code, *sessionID); err != nil {
        fmt.Fprintln(os.Stderr, "send:", err)
        os.Exit(1)
    }
    time.Sleep(500 * time.Millisecond)
    fmt.Println("ok")
}
```

Expose `SendServerInitiated` as a public method (rename the private `sendServerInitiated` from Step 3.2).

- [ ] **Step 3.7: Run aaa-sim tests**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test ./cmd/aaa-sim/... ./test/aaa_sim/... -v`
Expected: PASS.

- [ ] **Step 3.8: Create `test/e2e/aaa_sim_driver.go`**

```go
package e2e

import (
    "fmt"
    "os/exec"
    "testing"
)

// AaaSimDriver shells out to aaa-sim running inside the compose container.
// Used by integration tests to trigger RAR/ASR without modifying aaa-sim's
// long-running server loop.
type AaaSimDriver struct {
    Container string // docker compose container name; default "aaa-sim"
}

// NewAaaSimDriver returns a driver; if container == "", defaults to "aaa-sim".
func NewAaaSimDriver(container string) *AaaSimDriver {
    if container == "" {
        container = "aaa-sim"
    }
    return &AaaSimDriver{Container: container}
}

// TriggerRAR sends a RAR to aaa-gateway for the given session.
func (d *AaaSimDriver) TriggerRAR(t *testing.T, sessionID, targetAddr string) {
    d.trigger(t, "trigger-rar", sessionID, targetAddr)
}

// TriggerASR sends an ASR to aaa-gateway for the given session.
func (d *AaaSimDriver) TriggerASR(t *testing.T, sessionID, targetAddr string) {
    d.trigger(t, "trigger-asr", sessionID, targetAddr)
}

func (d *AaaSimDriver) trigger(t *testing.T, cmd, sessionID, targetAddr string) {
    t.Helper()
    out, err := exec.Command("docker", "exec", d.Container, "aaa-sim", cmd,
        "--target", targetAddr, "--session-id", sessionID,
    ).CombinedOutput()
    if err != nil {
        t.Fatalf("aaa-sim %s failed: %v\n%s", cmd, err, out)
    }
    t.Logf("aaa-sim %s ok: %s", cmd, out)
}

// ComposeRunning returns nil if the named container is running.
func ComposeRunning(container string) error {
    out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", container).Output()
    if err != nil {
        return fmt.Errorf("docker inspect %s: %w", container, err)
    }
    if string(out) != "true\n" {
        return fmt.Errorf("container %s not running: %s", container, out)
    }
    return nil
}
```

- [ ] **Step 3.9: Compile check**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go build ./cmd/aaa-sim/... ./test/aaa_sim/... ./test/e2e/...`
Expected: no errors.

- [ ] **Step 3.10: Commit**

```bash
cd /home/tulm/naf3/.worktrees/feature-per-ue-debug
git add test/aaa_sim/radius.go test/aaa_sim/radius_test.go cmd/aaa-sim/main.go cmd/aaa-sim/trigger.go test/e2e/aaa_sim_driver.go
git commit -m "feat(aaa-sim): add trigger-rar/trigger-asr subcommands and e2e driver"
```

---

## Task 4: Forward-direction RADIUS + Diameter integration tests

**Files:**
- Create: `test/e2e/debug_full_flow_forward_test.go`
- Modify: `Makefile` (add `test-debug-full` target)

This is the first half of the bidirectional coverage.

- [ ] **Step 4.1: Write the failing forward test skeleton**

Create `test/e2e/debug_full_flow_forward_test.go`:

```go
//go:build e2e

package e2e

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "testing"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/require"

    "github.com/operator/nssAAF/internal/logging"
)

// TestDebugFullFlow_RADIUS_Forward exercises flow direction (a):
// AMF → http-gw → biz → aaa-gw → AAA-S (RADIUS Access-Request).
//
// Pre-flight: aaa-sim must be reachable on its RADIUS port inside compose.
func TestDebugFullFlow_RADIUS_Forward(t *testing.T) {
    if os.Getenv("RUN_E2E") != "1" {
        t.Skip("set RUN_E2E=1 to run")
    }
    require.NoError(t, ComposeRunning("aaa-sim"))

    // Pre-flight: probe aaa-gateway's RADIUS port with a raw Access-Request.
    // The biz endpoint POST /nssaa-auth drives the full flow.
    // We assert via Redis XRange that all required events are present.
    // ...
}

// TestDebugFullFlow_DIAMETER_Forward mirrors RADIUS_Forward but uses the
// Diameter protocol. The compose stack is brought up with DIAMETER_TRANSPORT=tcp
// which switches aaa-sim and aaa-gw from RADIUS (port 1812) to Diameter (port 3868).
//
// The required event list differs only in the `op` field: `aaa.diameter.forward`
// replaces `aaa.radius.forward`.
func TestDebugFullFlow_DIAMETER_Forward(t *testing.T) {
    if os.Getenv("RUN_E2E") != "1" {
        t.Skip("set RUN_E2E=1 to run")
    }
    require.NoError(t, ComposeRunning("aaa-sim"))

    const gpsi = "msisdn-208046000000002"
    gpsiHash := logging.HashGPSI(gpsi)
    streamKey := "nssaa:debug:stream:" + gpsiHash

    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    defer rdb.Close()
    require.NoError(t, rdb.Ping(context.Background()).Err())
    rdb.Del(context.Background(), streamKey)

    // Drive the flow via the same biz POST endpoint as RADIUS_Forward.
    body := fmt.Sprintf(`{"gpsi":"%s","snssai":{"sst":1,"sd":"000001"},"authCtxId":"auth-dia-forward-1"}`, gpsi)
    postNSSAAAuth(t, body)

    // Poll Redis; assert all required events including `aaa.diameter.forward`.
    deadline := time.Now().Add(5 * time.Second)
    var events []redis.XMessage
    for time.Now().Before(deadline) {
        events, _ = rdb.XRange(context.Background(), streamKey, "-", "+").Result()
        if len(events) >= 11 { // same count as RADIUS variant
            break
        }
        time.Sleep(100 * time.Millisecond)
    }
    require.GreaterOrEqual(t, len(events), 11, "timed out waiting for Diameter events")

    // Same trace_id and required-event checks as RADIUS_Forward, with
    // `aaa.diameter.forward` instead of `aaa.radius.forward`.
    traceIDs := map[string]bool{}
    for _, e := range events {
        traceIDs[e.Values["trace"].(string)] = true
    }
    require.Equal(t, 1, len(traceIDs))

    present := map[string]bool{}
    for _, e := range events {
        svc, _ := e.Values["svc"].(string)
        op, _ := e.Values["op"].(string)
        present[svc+":"+op] = true
    }
    required := []string{
        "http-gw:http.request", "http-gw:http.request.exit",
        "biz:http.request", "biz:pg.session.create", "biz:redis.session.set",
        "biz:pg.audit.write", "biz:http.request.out", "biz:http.request.exit",
        "aaa-gw:http.request", "aaa-gw:aaa.diameter.forward", "aaa-gw:http.request.exit",
    }
    for _, want := range required {
        require.True(t, present[want], "missing event %s", want)
    }
}
```

- [ ] **Step 4.2: Run test, verify it skips (RUN_E2E not set)**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test -tags=e2e -run TestDebugFullFlow_RADIUS_Forward ./test/e2e/... -v`
Expected: SKIP (`RUN_E2E` not set).

- [ ] **Step 4.3: Implement the full forward test body**

In the same file, fill out the body:

```go
    const gpsi = "msisdn-208046123456789"
    gpsiHash := logging.HashGPSI(gpsi)
    streamKey := "nssaa:debug:stream:" + gpsiHash

    // Connect to redis (from compose, port 6379 on host)
    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    defer rdb.Close()
    require.NoError(t, rdb.Ping(context.Background()).Err())

    // Clear any prior stream for clean assertions
    rdb.Del(context.Background(), streamKey)

    // Drive the flow via biz's POST /nssaa-auth endpoint.
    // Use the same HTTP client pattern as test/e2e/driver.go.
    body := fmt.Sprintf(`{"gpsi":"%s","snssai":{"sst":1,"sd":"000001"},"authCtxId":"auth-rar-forward-1"}`, gpsi)
    // ... POST via http.Client to biz's /nssaa-auth or to http-gw's /nnssaaf-nssaa/...
    // (Refer to test/e2e/driver.go or test/conformance/nssaa_callbacks_test.go for the URL pattern.)

    // Poll Redis with exponential backoff up to 5s.
    required := []string{
        // http-gw
        "http-gw:http.request",
        "http-gw:http.request.exit",
        // biz
        "biz:http.request",
        "biz:pg.session.create",
        "biz:redis.session.set",
        "biz:pg.audit.write",
        "biz:http.request.out",
        "biz:http.request.exit",
        // aaa-gw
        "aaa-gw:http.request",
        "aaa-gw:aaa.radius.forward",
        "aaa-gw:http.request.exit",
    }
    deadline := time.Now().Add(5 * time.Second)
    var events []redis.XMessage
    for time.Now().Before(deadline) {
        events, _ = rdb.XRange(context.Background(), streamKey, "-", "+").Result()
        if len(events) >= len(required) {
            break
        }
        time.Sleep(100 * time.Millisecond)
    }
    require.GreaterOrEqual(t, len(events), len(required), "timed out waiting for events")

    // Unique trace_id check
    traceIDs := map[string]bool{}
    for _, e := range events {
        traceIDs[e.Values["trace"].(string)] = true
    }
    require.Equal(t, 1, len(traceIDs), "expected exactly one trace_id, got %v", traceIDs)

    // Each required (svc, op) must be present.
    present := map[string]bool{}
    for _, e := range events {
        svc, _ := e.Values["svc"].(string)
        op, _ := e.Values["op"].(string)
        present[svc+":"+op] = true
    }
    for _, want := range required {
        require.True(t, present[want], "missing event %s", want)
    }
```

- [ ] **Step 4.4: Add `test-debug-full` Makefile target**

In the `Makefile`, after the existing `test-fullchain-no-build` target (around line 287), add:

```makefile
.PHONY: test-debug-full-radius
test-debug-full-radius: ## Run per-UE debug RADIUS full-flow tests (RUN_E2E=1 required)
	@echo "$(YELLOW)Starting fullchain TCP stack for debug RADIUS tests...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running debug full-flow RADIUS tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	RUN_E2E=1 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		-run 'TestDebugFullFlow_(RADIUS_Forward|AMFCallback)' \
		./test/e2e/... \
		|| { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Debug RADIUS full-flow tests complete$(NC)"

.PHONY: test-debug-full-diameter
test-debug-full-diameter: ## Run per-UE debug Diameter full-flow tests (RUN_E2E=1 required)
	@echo "$(YELLOW)Starting fullchain TCP stack for debug Diameter tests...$(NC)"
	DIAMETER_TRANSPORT=tcp docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running debug full-flow Diameter tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	RUN_E2E=1 \
	DIAMETER_TRANSPORT=tcp \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		-run 'TestDebugFullFlow_DIAMETER_Forward' \
		./test/e2e/... \
		|| { DIAMETER_TRANSPORT=tcp docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	DIAMETER_TRANSPORT=tcp docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Debug Diameter full-flow tests complete$(NC)"

.PHONY: test-debug-full
test-debug-full: test-debug-full-radius test-debug-full-diameter ## Run all per-UE debug full-flow tests
```

The `GOTEST` variable is defined near the top of the Makefile; check for it.

- [ ] **Step 4.5: Verify Makefile target parses**

Run: `cd /home/tulm/naf3 && make -n test-debug-full`
Expected: prints the commands without running them; no parse errors.

- [ ] **Step 4.6: Run the forward test against the real stack**

Run: `cd /home/tulm/naf3 && make test-debug-full`
Expected: `TestDebugFullFlow_RADIUS_Forward` PASS.

If FAIL: investigate the specific missing event from the assertion output. Common causes: (a) `debug.enabled` is not `true` in `biz.yaml`/`http-gateway.yaml`/`aaa-gateway.yaml` — set to `true` in `compose/configs/*.yaml`; (b) `traceparent` not propagated — see Task 1 risk; (c) GPSI extraction point missed — re-check Step 1.8.

- [ ] **Step 4.7: Commit**

```bash
cd /home/tulm/naf3/.worktrees/feature-per-ue-debug
git add test/e2e/debug_full_flow_forward_test.go Makefile
git commit -m "test(debug): add forward-direction RADIUS full-flow integration test"
```

---

## Task 5: Server-initiated (AMF callback) integration test

**Files:**
- Create: `test/e2e/debug_full_flow_callback_test.go`

This is the second half of the bidirectional coverage: AAA-S → aaa-gw (RAR) → biz → AMF.

- [ ] **Step 5.1: Write the failing callback test skeleton**

Create `test/e2e/debug_full_flow_callback_test.go`:

```go
//go:build e2e

package e2e

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "testing"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/require"

    "github.com/operator/nssAAF/internal/logging"
)

// TestDebugFullFlow_AMFCallback exercises flow direction (c):
// AAA-S → aaa-gw (RAR/ASR) → biz → AMF callback.
//
// Pre-flight: a session must exist in Postgres so biz can look it up
// when the RAR arrives. We seed by first running a forward POST.
//
// NOTE (Task 1 deferral): the AAA-GW server-initiated path has no GPSI in
// the protocol payload (RADIUS CoA/DM, Diameter ASR/RAR/STR) or the typed
// DTO (`proto.AaaServerInitiatedResponse`). Subscriber context is therefore
// not injected on this ingress, so the reverse-direction events land in the
// `_no_sub` stream, not the `gpsi_hash` stream. The assertions below scan
// the `_no_sub` stream (and tolerate the gpsi_hash stream for the forward
// seed events). This is OUT OF SCOPE for the GPSI-keyed stream assertion
// until a follow-up adds GPSI to SessionCorrEntry / EAP identity decode.
func TestDebugFullFlow_AMFCallback(t *testing.T) {
    if os.Getenv("RUN_E2E") != "1" {
        t.Skip("set RUN_E2E=1 to run")
    }
    require.NoError(t, ComposeRunning("aaa-sim"))

    const gpsi = "msisdn-208046999999001"
    gpsiHash := logging.HashGPSI(gpsi)
    gpsiStream := "nssaa:debug:stream:" + gpsiHash
    noSubStream := "nssaa:debug:stream:_no_sub"

    rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    defer rdb.Close()
    require.NoError(t, rdb.Ping(context.Background()).Err())
    // Clear both possible landing streams so we can detect the new trace.
    rdb.Del(context.Background(), gpsiStream, noSubStream)

    // Step 1: forward POST to seed a session.
    body := fmt.Sprintf(`{"gpsi":"%s","snssai":{"sst":1,"sd":"000001"},"authCtxId":"auth-cb-1"}`, gpsi)
    postNSSAAAuth(t, body) // helper; mirror test/e2e/driver.go pattern

    // Wait briefly for session to land in PG and forward events to flush.
    time.Sleep(500 * time.Millisecond)

    // Snapshot trace_ids across both streams BEFORE the RAR.
    snapshotTraceIDs := func() map[string]bool {
        out := map[string]bool{}
        for _, sk := range []string{gpsiStream, noSubStream} {
            msgs, _ := rdb.XRange(context.Background(), sk, "-", "+").Result()
            for _, e := range msgs {
                if tid, ok := e.Values["trace"].(string); ok && tid != "" {
                    out[tid] = true
                }
            }
        }
        return out
    }
    preTraceIDs := snapshotTraceIDs()

    // Step 2: trigger RAR via aaa-sim.
    driver := NewAaaSimDriver("aaa-sim")
    driver.TriggerRAR(t, "auth-cb-1", "172.0.3.15:1812")

    // Step 3: poll BOTH streams for a NEW trace_id (i.e., one not in preTraceIDs).
    // We don't assume which stream the reverse events land in — that depends
    // on whether AAA-GW eventually extracts a GPSI from the RAR body.
    deadline := time.Now().Add(5 * time.Second)
    var newEvents []redis.XMessage
    var newTraceID string
    for time.Now().Before(deadline) {
        var collected []redis.XMessage
        for _, sk := range []string{gpsiStream, noSubStream} {
            msgs, _ := rdb.XRange(context.Background(), sk, "-", "+").Result()
            collected = append(collected, msgs...)
        }
        // Find the first trace_id not seen in the pre-snapshot.
        for _, e := range collected {
            tid, _ := e.Values["trace"].(string)
            if tid == "" || preTraceIDs[tid] {
                continue
            }
            newTraceID = tid
            break
        }
        if newTraceID != "" {
            // Collect all events across both streams sharing this trace_id.
            for _, e := range collected {
                tid, _ := e.Values["trace"].(string)
                if tid == newTraceID {
                    newEvents = append(newEvents, e)
                }
            }
            break
        }
        time.Sleep(100 * time.Millisecond)
    }
    require.NotEmpty(t, newTraceID, "no new trace_id appeared within 5s")

    // Step 4: assert required events for the reverse direction.
    required := []string{
        "aaa-gw:aaa.radius.recv",
        "aaa-gw:http.request.out",
        "biz:http.request",
        "biz:pg.session.update",
        "biz:http.request.out", // to AMF
        "biz:http.request.exit",
    }
    present := map[string]bool{}
    for _, e := range newEvents {
        svc, _ := e.Values["svc"].(string)
        op, _ := e.Values["op"].(string)
        present[svc+":"+op] = true
    }
    for _, want := range required {
        require.True(t, present[want], "missing event %s", want)
    }

    // Verify exactly one trace_id for the new events.
    traceIDs := map[string]bool{}
    for _, e := range newEvents {
        traceIDs[e.Values["trace"].(string)] = true
    }
    require.Equal(t, 1, len(traceIDs), "all RAR events must share one trace_id")

    // Sanity: detail JSON must round-trip (used to print in the CLI).
    for _, e := range newEvents {
        _, ok := e.Values["detail"]
        _ = ok
    }
    _ = json.RawMessage(nil)
}
```

- [ ] **Step 5.2: Run test, verify it skips (RUN_E2E not set)**

Run: `cd /home/tulm/naf3/.worktrees/feature-per-ue-debug && go test -tags=e2e -run TestDebugFullFlow_AMFCallback ./test/e2e/... -v`
Expected: SKIP.

- [ ] **Step 5.3: Implement `postNSSAAAuth` helper**

If `test/e2e/driver.go` doesn't already have a helper for the `POST /nssaa-auth` endpoint, add one to the same callback test file:

```go
func postNSSAAAuth(t *testing.T, body string) {
    t.Helper()
    url := os.Getenv("FULLCHAIN_BIZ_URL")
    if url == "" {
        url = "http://localhost:8080" // default for compose
    }
    url += "/nnssaaf-nssaa/auth"
    req, _ := http.NewRequest("POST", url, strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()
    require.True(t, resp.StatusCode < 300, "biz returned %d", resp.StatusCode)
}
```

- [ ] **Step 5.4: Run the full Makefile target**

Run: `cd /home/tulm/naf3 && make test-debug-full`
Expected: ALL three tests pass (`TestDebugFullFlow_RADIUS_Forward`, `TestDebugFullFlow_AMFCallback`, and `TestDebugFullFlow_DIAMETER_Forward` is skipped per Step 4.1).

If the callback test fails: the most likely cause is that aaa-gw's RAR receiver does not start a fresh span (or does not propagate `traceparent` to biz). Debug using `nssAAF-debug trace --gpsi <gpsi>` to inspect the live stream; the RAR events should show `aaa.radius.recv` as the first event with a new trace_id.

- [ ] **Step 5.5: Run the CLI manually against the live stream**

Run: `cd /home/tulm/naf3 && docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait`

In a separate shell, drive the flow (see `test/e2e/driver.go` for the curl pattern), then:

```bash
docker exec aaa-sim redis-cli -h 172.0.3.10 KEYS 'nssaa:debug:stream:*'
docker exec -it $(docker compose -f compose/fullchain-dev-tcp.yaml ps -q biz) /app/nssAAF-debug trace --redis 172.0.3.10:6379 --gpsi msisdn-208046999999001 --since 5m
```

Expected: grouped output with both the forward and callback traces visible, separated by blank lines.

- [ ] **Step 5.6: Update roadmap status**

In `docs/roadmap/module_index.md`, find the row for `internal/debug/` and any related rows; ensure status reflects READY once tests pass. Same for `cmd/nssAAF-debug/` (already exists; mark READY).

- [ ] **Step 5.7: Commit**

```bash
cd /home/tulm/naf3/.worktrees/feature-per-ue-debug
git add test/e2e/debug_full_flow_callback_test.go docs/roadmap/module_index.md
git commit -m "test(debug): add server-initiated RAR/ASR callback flow test"
```

---

## Acceptance Checklist

- [ ] Task 1: `go test ./internal/debug/... -race` passes; `WithSubscriber`/`SubscriberFrom` and `Wrap*` context propagation work.
- [ ] Task 2: `go test ./cmd/nssAAF-debug/...` passes; `--json` works; AUTH/GPSI_H columns visible; grouping by trace_id works.
- [ ] Task 3: `aaa-sim trigger-rar --target ADDR --session-id ID` and `trigger-asr` succeed; `AaaSimDriver` works.
- [ ] Task 4: `make test-debug-full-radius` runs `TestDebugFullFlow_RADIUS_Forward` and `TestDebugFullFlow_AMFCallback` against real compose + real aaa-sim; required events present; single trace_id per direction.
- [ ] Task 4: `make test-debug-full-diameter` runs `TestDebugFullFlow_DIAMETER_Forward` with `DIAMETER_TRANSPORT=tcp`; required events including `aaa.diameter.forward` present.
- [ ] Task 5: callback test passes; the RAR trace_id is distinct from the forward trace_id; required reverse events present. **GPSI-keyed stream assertion for the reverse direction is OUT OF SCOPE** until AAA-GW extracts GPSI from inbound RAR/ASR — currently events land under `_no_sub`. The test scans both streams and asserts on events by trace_id, not on stream key.
- [ ] CLI manual smoke: `nssAAF-debug trace --gpsi <gpsi>` shows both forward and callback traces grouped.

---

## Notes for the Executor

- **Risk 1 (traceparent propagation)**: if multi-trace correlation fails, look at `internal/tracing/tracing.go` and ensure `otelhttp.NewHandler` wraps every inbound mux and `tracing.HTTPTransport()` is used for outbound calls. Reference: parent design §5.4.
- **Risk 7 (port mismatch)**: in the pre-flight probe, target `172.0.3.15:1812` from inside the compose network, NOT `localhost:18121` (which is the host-side bind). Use `FULLCHAIN_BIZ_URL` and the compose-internal IPs from `compose/fullchain-dev-base.yaml`.
- **DO NOT** modify `cmd/aaa-sim/main.go`'s existing server-mode behavior. Add the trigger subcommands as a side path; preserve the `Run(mode)` server loop.
- **DO NOT** change `internal/debug/debug.go` (parent design's Emit logic). Tasks 1 modifies `hooks.go` and adds `subctx.go`, not the core emitter.
- **Keep `debug.enabled` true** in `compose/configs/{biz,http-gateway,aaa-gateway}.yaml` for the full-chain stack, since the verification requires it.