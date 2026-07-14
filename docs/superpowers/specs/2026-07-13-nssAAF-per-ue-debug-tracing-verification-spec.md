# Per-UE Debug Tracing — Verification & Operator Experience Spec

**Status:** Draft for review
**Date:** 2026-07-13
**Author:** Per-UE Debug working session
**Parent design:** `docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md`

## 1. Purpose

This spec defines the **end-to-end verification** of the per-UE debug subsystem and the **operator-facing CLI** used to inspect per-UE traces. It complements the parent design (which defines the emitter, storage layout, and instrumentation points) by specifying:

1. How GPSI/SUPI propagates through the request context.
2. Which events each hop must emit for a successful NSSAA round-trip.
3. The CLI output format (grouped table + JSON; timeline deferred to v2).
4. The verification harness that asserts the trace is complete and correlated.
5. Out-of-scope items, dependencies, and known risks.

## 2. Context-Based Subscriber Propagation

### Problem

The current `Debug.Emit` reads GPSI/SUPI from the `Event` struct (`debug.go:53-54`). For instrumentation helpers like `WrapDB(ctx, op, table, fn)` the caller would have to thread the GPSI through every call site, which is brittle and noisy.

### Solution

Add `internal/debug/subctx.go` with context-based GPSI/SUPI propagation. The HTTP middleware extracts the GPSI **once** at request entry and stores it in `ctx`; all downstream helpers read it from `ctx`.

```go
// internal/debug/subctx.go

type subscriberKey struct{}

// WithSubscriber returns a new context carrying the GPSI/SUPI for the
// current request. Both may be empty for background jobs; helpers must
// tolerate that and fall through to the existing _no_sub stream.
func WithSubscriber(ctx context.Context, gpsi, supi string) context.Context {
    return context.WithValue(ctx, subscriberKey{}, subscriber{gpsi: gpsi, supi: supi})
}

// SubscriberFrom returns the GPSI/SUPI stored in ctx, if any.
// Callers use these to populate Event.GPSI / Event.SUPI when emitting.
func SubscriberFrom(ctx context.Context) (gpsi, supi string) {
    s, ok := ctx.Value(subscriberKey{}).(subscriber)
    if !ok {
        return "", ""
    }
    return s.gpsi, s.supi
}

type subscriber struct{ gpsi, supi string }
```

### Wiring

1. **`http-gw` middleware** (`internal/api/nssaa/middleware.go` or equivalent): after extracting GPSI from the request body, call `ctx = debug.WithSubscriber(ctx, gpsi, "")`.

2. **`biz` inbound handler** (`internal/api/nssaa/biz/handler.go`): the existing GPSI extraction point sets `ctx = debug.WithSubscriber(ctx, gpsi, "")`.

3. **`aaa-gateway` inbound handler** (`internal/gateway/forward.go`): after extracting GPSI from the forwarded body, call `ctx = debug.WithSubscriber(ctx, gpsi, "")`.

4. **`WrapDB`, `WrapRedis`, `WrapProtocol` helpers**: read `SubscriberFrom(ctx)` and populate `Event.GPSI` / `Event.SUPI` from it. The existing optional `gpsi, supi` parameters on `Event` remain as **fallback overrides** for cases where the subscriber is known but not in the context (e.g., background jobs, cleanup tasks).

### Behavior contract

- `Emit` with an empty `Event.GPSI` and an empty context-subscriber: lands in `_no_sub` (existing behavior, preserved).
- `Emit` with `Event.GPSI` set, even if context-subscriber is empty: uses `Event.GPSI` (existing behavior, preserved).
- `Emit` with both set: `Event.GPSI` wins (caller override).

This means **no call-site signature changes** are required for existing `WrapDB(ctx, op, table, fn)` invocations. They automatically tag events correctly because they already pass `ctx`.

## 3. Required Event Coverage per Hop

For a successful NSSAA round-trip (`POST /nssaa-auth` → AAA-S `Access-Accept` → `201 Created`), each hop **must** emit the following events:

### `http-gw` (`cmd/http-gateway`)

| # | `op` | When | Notes |
|---|------|------|-------|
| 1 | `http.request` | After GPSI extraction, before dispatching to biz | `Detail`: `{method, path, gpsi}` |
| 2 | `http.request.exit` | After dispatch completes | `Detail`: `{status, duration_ms}` |

### `biz` (`cmd/biz`)

| # | `op` | When |
|---|------|------|
| 3 | `http.request` | Entry, after GPSI extraction |
| 4 | `pg.session.create` | When `NssaaRepo.CreateSession` runs |
| 5 | `redis.session.set` | When `SessionCache.Set` runs |
| 6 | `pg.audit.write` | When audit log writes |
| 7 | `http.request.out` | Outbound to `aaa-gateway` |
| 8 | `http.request.exit` | Exit back to http-gw |

Optional (emit if present in the flow): `pg.session.update`, `redis.session.get`, `redis.session.del`.

### `aaa-gw` (`cmd/aaa-gateway`)

| # | `op` | When |
|---|------|------|
| 9 | `http.request` | Inbound from biz |
| 10 | `aaa.radius.forward` OR `aaa.diameter.forward` | After building the RADIUS/Diameter packet, before send |
| 11 | `http.request.exit` | Exit back to biz |

(One of `aaa.radius.forward` or `aaa.diameter.forward` per request, never both.)

### `aaa-gw` server-initiated reception (used by direction **c**)

When AAA-S sends a RAR/ASR/CoA/DM to aaa-gw unsolicited, the following events are required:

| # | `op` | When |
|---|------|------|
| 12 | `aaa.radius.recv` OR `aaa.diameter.recv` | After aaa-gw receives the server-initiated packet and decodes it |
| 13 | `http.request.out` | Outbound to biz (the server-initiated HTTP endpoint) |
| 14 | `http.request.exit` | After biz responds 200 OK |

### `aaa-sim` (AAA Simulator)

aaa-sim is an **external** container (separate binary, runs in compose). It speaks RADIUS/Diameter over real sockets but does NOT emit debug events to Redis directly. All aaa-sim-related events are observed from the **aaa-gateway side** via `aaa.radius.recv` and `aaa.diameter.recv` events emitted by aaa-gw when it receives an inbound RADIUS/Diameter packet. This means the per-UE debug subsystem traces the *response side* of the AAA conversation without needing aaa-sim to be instrumented.

### Bidirectional coverage (flows a + c)

Per TS 23.502 §4.2.9 and TS 29.526 §7.2.4-5, NSSAAF is a hub for two directions of flow:

- **Direction (a) — Forward**: AMF → http-gw → biz → aaa-gw → AAA-S (RADIUS Access-Request). Triggered by `POST /nssaa-auth`. Covered by §3 above and `TestDebugFullFlow_RADIUS_Forward` / `_DIAMETER_Forward`.
- **Direction (c) — Server-initiated / AMF callback**: AAA-S → aaa-gw (RAR/ASR/CoA/DM) → biz → AMF. Triggered by AAA-S pushing a change-of-auth or session-termination message. Covered by `TestDebugFullFlow_AMFCallback` in §5.

**Out of scope (v2)**: direction (b) — async EAP challenge reverse path (AAA-S → NSSAAF → AAA-S multi-pass EAP-AKA' exchange). This is its own state machine and warrants a separate verification suite.

The forward and reverse directions share a **trace_id boundary**: a forward-direction trace ends at `201 Created`, and a server-initiated trace starts fresh at the RAR/ASR arrival. They never share a trace_id.

### Background jobs and timers

Background tasks (e.g., session expiry in `biz/cleanup.go`) run outside any request context and have no GPSI in their `ctx`. They will always land in `_no_sub`. The verification harness **must not** require these events in the round-trip list.

## 4. CLI Output Format

The `nssAAF-debug` CLI today (`cmd/nssAAF-debug/main.go`) emits a flat table sorted by Redis insertion order. v1 changes:

1. **Sort by `ts` ascending, then group by `trace_id`** with a blank line between groups.
2. **Add `AUTH` and `GPSI_H` columns** to the table.
3. **Add `--json` flag** that emits one JSON object per line (no grouping).
4. **Keep color-coded service column** (cyan=http-gw, green=biz, yellow=aaa-gw) via `fatih/color`.
5. **Apply `--since` cutoff** during sort/filter, not at XRange time (current behavior).

### Default (grouped table)

```
TIME                          POD        SVC       TRACE       AUTH          GPSI_H   OP                          STATUS  DUR    DETAIL
2026-07-13T18:30:00.123       pod-1      http-gw   4bf92f35    -             8a3f…    http.request                ok      -      {"method":"POST","path":"/nssaa-auth"}
2026-07-13T18:30:00.156       pod-1      http-gw   4bf92f35    -             8a3f…    http.request.exit           ok      33     {"status":201}

2026-07-13T18:30:00.200       pod-2      biz       4bf92f35    -             8a3f…    http.request                ok      -      {"method":"POST","path":"/nssaa-auth"}
2026-07-13T18:30:00.240       pod-2      biz       4bf92f35    abc-123       8a3f…    pg.session.create           ok      12     {"table":"nssaa_session"}
2026-07-13T18:30:00.280       pod-2      biz       4bf92f35    abc-123       8a3f…    redis.session.set           ok      3      {"key":"nssaa:session:abc-123"}
2026-07-13T18:30:00.310       pod-2      biz       4bf92f35    abc-123       8a3f…    pg.audit.write              ok      8      {"table":"nssaa_audit"}
2026-07-13T18:30:00.330       pod-2      biz       4bf92f35    abc-123       8a3f…    http.request.out            ok      -      {"method":"POST","target":"aaa-gw"}
2026-07-13T18:30:00.420       pod-2      biz       4bf92f35    abc-123       8a3f…    http.request.exit           ok      220

2026-07-13T18:30:00.330       pod-3      aaa-gw    4bf92f35    abc-123       8a3f…    http.request                ok      -      {"method":"POST","path":"/forward"}
2026-07-13T18:30:00.350       pod-3      aaa-gw    4bf92f35    abc-123       8a3f…    aaa.radius.forward          ok      18     {"code":1,"name":"Access-Request"}
2026-07-13T18:30:00.380       pod-3      aaa-gw    4bf92f35    abc-123       8a3f…    http.request.exit           ok      50     {"status":200}
```

Service column is color-coded (ANSI escapes; terminal-only).

### `--json` mode

```json
{"ts":1752400000123,"pod":"pod-1","svc":"http-gw","trace":"4bf92f357d9e8a01","span":"00f1abcd","sub_h":"8a3f…","sub_kind":"gpsi","auth":"","op":"http.request","kind":"http","status":"ok","detail":{"method":"POST","path":"/nssaa-auth"}}
{"ts":1752400000240,"pod":"pod-2","svc":"biz","trace":"4bf92f357d9e8a01","span":"00f2abcd","sub_h":"8a3f…","sub_kind":"gpsi","auth":"abc-123","op":"pg.session.create","kind":"db","status":"ok","detail":{"table":"nssaa_session"}}
```

One event per line. Operators pipe to `jq` for filtering (`jq 'select(.op == "aaa.radius.forward")'`).

### Deferred to v2

- Timeline ASCII sequence diagram (`--timeline` flag)
- Streaming/follow mode (`-f` like `tail -f`)
- `authCtxId` / `trace_id` / `X-Request-ID` direct lookup (v1 stays `--gpsi`/`--supi` only)
- Cross-binary stream aggregation in CLI

## 5. Verification Harness

### Goal

Drive a complete NSSAA round-trip end-to-end and assert that every required event in §3 lands in Redis, tagged with the right `svc`, `op`, `trace_id`, and `sub_h`.

### File layout

```
internal/debug/subctx_test.go       — unit tests for WithSubscriber / SubscriberFrom
internal/debug/wrap_helpers_test.go — unit tests for WrapDB / WrapRedis / WrapProtocol context-subscriber tagging
test/e2e/debug_full_flow_test.go    — full-stack RADIUS + Diameter round-trip assertions
test/e2e/aaa_sim_driver.go          — thin control client for the aaa-sim container (start/stop scenarios)
```

### Unit test: `TestDebugContextPropagation` (`internal/debug/subctx_test.go`)

No Redis, no compose.

```go
func TestSubscriberFrom_Empty(t *testing.T) {
    g, s := debug.SubscriberFrom(context.Background())
    if g != "" || s != "" {
        t.Fatalf("expected empty, got (%q, %q)", g, s)
    }
}

func TestWithSubscriber_RoundTrip(t *testing.T) {
    ctx := debug.WithSubscriber(context.Background(), "msisdn-208046123456789", "")
    g, s := debug.SubscriberFrom(ctx)
    if g != "msisdn-208046123456789" || s != "" {
        t.Fatalf("got (%q, %q)", g, s)
    }
}

func TestWithSubscriber_Replace(t *testing.T) {
    // WithSubscriber replaces the entire (gpsi, supi) pair, matching Go's
    // normal context idiom (e.g. metadata.NewOutgoingContext is the exception).
    // Callers who need both values set must pass both in one call.
    ctx := debug.WithSubscriber(context.Background(), "msisdn-1", "")
    ctx = debug.WithSubscriber(ctx, "", "imsi-208046000000001")
    g, s := debug.SubscriberFrom(ctx)
    if g != "" || s != "imsi-208046000000001" {
        t.Fatalf("got (%q, %q); second WithSubscriber should replace the first", g, s)
    }
}

func TestWithSubscriber_BothAtOnce(t *testing.T) {
    ctx := debug.WithSubscriber(context.Background(), "msisdn-1", "imsi-1")
    g, s := debug.SubscriberFrom(ctx)
    if g != "msisdn-1" || s != "imsi-1" {
        t.Fatalf("got (%q, %q)", g, s)
    }
}
```

Additional assertions:
- `WrapDB(ctx, ...)` emits an `Event` with `GPSI` set from context when context carries a subscriber and the Event does not.
- `Emit(ctx, Event{GPSI: "explicit"})` uses the Event's value when both are set.

### Unit test: `TestDebugMultiHopCorrelatesByTrace`

Verifies that events emitted from "different goroutines with the same context" all share the same `trace_id`. Uses a fake span context and asserts the emitted field.

### Integration test: `TestDebugFullFlow_RADIUS_Forward` (`test/e2e/debug_full_flow_test.go`)

Setup:
- Start the full `deploy/fullchain-dev-tcp.yaml` compose stack (or the slim `aaa-sim` + NSSAAF subset if the slim stack is available). This includes the real **aaa-sim** container speaking RADIUS over UDP on its exposed port.
- The test asserts the aaa-sim container is up (`docker inspect --format '{{.State.Running}}' aaa-sim`) and that `radtest`-style probes return `Access-Accept` before exercising the NSSAA path.
- Pre-create debug streams empty.
- Compute `gpsi_h = logging.HashGPSI("msisdn-208046123456789")`.

Action:
- `POST /nssaa-auth` with valid NSSAA_AuthRequest JSON, GPSI in body.
- Poll Redis `XRange "nssaa:debug:stream:<gpsi_h>" - +` with exponential backoff up to 5s.

Assertions:

```go
required := []expectedEvent{
    // http-gw
    {svc: "http-gw", op: "http.request"},
    {svc: "http-gw", op: "http.request.exit"},

    // biz
    {svc: "biz", op: "http.request"},
    {svc: "biz", op: "pg.session.create"},
    {svc: "biz", op: "redis.session.set"},
    {svc: "biz", op: "pg.audit.write"},
    {svc: "biz", op: "http.request.out"},
    {svc: "biz", op: "http.request.exit"},

    // aaa-gw
    {svc: "aaa-gw", op: "http.request"},
    {svc: "aaa-gw", op: "aaa.radius.forward"},
    {svc: "aaa-gw", op: "http.request.exit"},
}

events := readEvents(t, redisClient, "nssaa:debug:stream:"+gpsi_h)
traceIDs := uniqueTraceIDs(events)
if len(traceIDs) != 1 {
    t.Fatalf("expected one trace_id across all events, got %d: %v", len(traceIDs), traceIDs)
}
traceID := traceIDs[0]

filtered := events.filter(traceID)
for _, want := range required {
    if !filtered.hasOp(want.svc, want.op) {
        t.Errorf("missing event: svc=%s op=%s", want.svc, want.op)
    }
}
```

Additional assertion: every required event has the same `trace_id`. If multi-trace correlation breaks, the test fails.

### Integration test: `TestDebugFullFlow_DIAMETER_Forward`

Same as `TestDebugFullFlow_RADIUS_Forward`, but the aaa-sim container runs in Diameter mode (separate compose file or env override). Required event list includes `aaa.diameter.forward` instead of `aaa.radius.forward`.

### Integration test: `TestDebugFullFlow_AMFCallback` (server-initiated, RAR + ASR)

Exercises the **reverse direction** of the flow per TS 29.526 §7.2.4-5:

1. The forward-direction test (`TestDebugFullFlow_RADIUS_Forward`) seeds a session in Redis/Postgres via the AMF→http-gw path (or we insert the session row directly via `internal/storage/postgres/nssaa_repo`).
2. The test triggers an AAA-S → aaa-gateway **RAR** (Re-Auth-Request) by issuing an out-of-band command to aaa-sim (e.g., `aaa-sim-cli send-rar <session-id>` — see `test/e2e/aaa_sim_driver.go`).
3. aaa-gw receives the RAR over real RADIUS, forwards it to biz via the server-initiated HTTP endpoint, biz updates the session and calls the AMF callback (`/namf` notification), and biz responds 200 OK.
4. The test polls Redis and asserts the following required events share a single `trace_id` (the trace is rooted at the RAR arrival, not at the original POST):

| svc | op |
|-----|----|
| aaa-gw | `aaa.radius.recv` |
| aaa-gw | `http.request.out` (to biz) |
| biz | `http.request` (server-initiated) |
| biz | `pg.session.update` (or `pg.session.revoke` for ASR) |
| biz | `http.request.out` (to AMF) |
| biz | `http.request.exit` |

5. The trace_id assertion is the key: events #1–6 must all share one trace_id. If aaa-gw's RAR receiver fails to start a fresh span (or fails to propagate it to biz), the test fails.

This test is the explicit coverage for flow direction (c) per the bidirectional design (see §3.5).

### Full-stack test (Makefile target, not unit)

`make e2e-debug-full RUN_E2E=1` runs `TestDebugFullFlow_RADIUS_Forward`, `TestDebugFullFlow_DIAMETER_Forward`, and `TestDebugFullFlow_AMFCallback` against the real `deploy/fullchain-dev-tcp.yaml` compose stack (which includes the real aaa-sim container). Gated on `RUN_E2E=1`. The test is the **primary** verification — there is no parallel in-process fast version. Trade-off accepted: slower CI, but real RADIUS/Diameter wire traffic and real aaa-sim behavior, which the in-process stubs cannot guarantee.

## 6. Out of Scope (Explicitly Deferred)

| Item | Defer to | Reason |
|------|----------|--------|
| ASCII timeline mode (`--timeline`) | v2 | ~150 LOC additional, low ROI for v1 |
| Follow/streaming mode (`-f`) | v2 | Operators can `watch nssAAF-debug trace` |
| AIW (N60) round-trip coverage | v2 | v1 covers N58 only; AIW repo instrumentation included if it exists, but no round-trip assertion |
| `authCtxId` / `trace_id` direct CLI inputs | v2 | v1 stays `--gpsi`/`--supi` only |
| aaa-sim emits its own `protocol.recv` debug events | v2 | aaa-sim is an external container; events are inferred from aaa-gateway's `aaa.radius.recv` |
| PII redaction in CLI output | N/A | Operators are trusted; `sanitize()` already runs at emit time |
| Cross-binary stream aggregation in CLI | v2 | One Redis stream per subscriber; use `--trace` to disambiguate |
| Persistent storage beyond Redis TTL (24h) | N/A | Redis TTL is the bound |
| Async EAP challenge reverse path (direction **b**) | v2 | Per-UE debug verifies flow a+c only; EAP-AKA' challenge/response multi-pass is separate |

## 7. Dependencies

| Component | Status | Notes |
|-----------|--------|-------|
| `internal/debug/subctx.go` | New | No external deps |
| `internal/api/...` instrumentation | Exists | WrapDB / WrapRedis / WrapProtocol helpers already in place |
| `cmd/nssAAF-debug/main.go` CLI | Exists | Modify to add grouping, AUTH/GPSI_H columns, `--json` flag |
| `internal/logging.HashGPSI` | Exists | Used by stream key derivation |
| `aaa-sim` container | Exists | Provided by `deploy/fullchain-dev-tcp.yaml`; test triggers RAR via out-of-band CLI |
| `deploy/fullchain-dev-tcp.yaml` | Exists | Compose stack including real aaa-sim |
| `test/e2e/aaa_sim_driver.go` | New | Thin shell-out helper to drive aaa-sim (start, stop, send-rar, send-asr) for the bidirectional test |

## 8. Risks

### Risk 1: W3C `traceparent` propagation across HTTP hops

**Likelihood:** High if unaddressed.
**Impact:** Every event after the first hop has an invalid span context and is dropped or tagged with no trace_id.

`Emit` skips events with no valid span (`debug.go:120-123`). The HTTP instrumentation in `http-gw → biz` and `biz → aaa-gw` MUST inject and extract W3C `traceparent` headers in both directions. The verification harness will fail loudly if this is missing, but it's the single biggest implementation risk.

**Mitigation:** The plan includes an explicit "traceparent injection" task in both `biz/http_client.go` and `aaa-gateway/forward.go`.

### Risk 2: Background jobs and timers

Some events (session expiry, cleanup) run outside any request context. They land in `_no_sub`. The verification harness must not require these in the round-trip list.

**Mitigation:** Documented in §3 "Background jobs and timers".

### Risk 3: Race: events still in flight when test reads

A single round-trip can take longer than 200ms under CI load.

**Mitigation:** Poll with exponential backoff up to 5s. On timeout, fail with the list of missing events.

### Risk 4: Hop-internal events discovered during instrumentation

As we instrument `nssaa_repo.go`, `radius_forward.go`, etc., we may want more events than listed in §3.

**Mitigation:** §3 lists the **required minimum**. Additional events are fine and may be appended to the required list during plan-execute.

### Risk 5: GPSI vs SUPI in AIW handlers

`aiw_handler_test.go` uses SUPI, not GPSI. If the same `Debug` subsystem serves both, the stream key uses `HashGPSI(supi)` per `debug.go:131-134`. v1 verification runs N58 only; AIW is out of scope. The instrumentation code supports both, but no round-trip test asserts SUPI coverage.

### Risk 6: aaa-sim server-initiated CLI unknown

`TestDebugFullFlow_AMFCallback` requires a way to trigger a RAR/ASR from aaa-sim on demand. The exact CLI/API surface of the aaa-sim container is not verified in this spec — it must be confirmed during planning. If aaa-sim lacks such a control surface, the test cannot trigger server-initiated messages without modifying aaa-sim itself.

**Mitigation:** During planning, verify aaa-sim has a documented CLI or HTTP control endpoint. If not, fall back to: (i) using `radclient` directly to send a RAR to aaa-gw on its RADIUS port (which assumes aaa-gw also binds a server socket), or (ii) defer `TestDebugFullFlow_AMFCallback` to v2 and ship v1 with only forward-direction coverage.

### Risk 7: aaa-sim container does not match deployment-topology ports

If aaa-sim listens on RADIUS port 1812/1813 by default but aaa-gw expects 2812/2813 in the compose stack, the forward test will fail at the protocol layer (not the debug layer) and the failure mode will be confusing.

**Mitigation:** A pre-flight check at the top of `TestDebugFullFlow_RADIUS_Forward` issues a probe RADIUS Access-Request using `internal/radius` client and asserts `Access-Accept` before exercising the full NSSAA path. If the probe fails, the test fails fast with a clear "aaa-sim not reachable on RADIUS port N" message.

## 9. Acceptance Criteria

The verification is considered passing when:

1. All unit tests in `internal/debug/...` pass under `go test ./internal/debug/...` (no compose required).
2. All §5 integration tests pass under `RUN_E2E=1 go test ./test/e2e/...` (compose required).
2. `make e2e-debug-full RUN_E2E=1` passes against the real compose stack, including the real aaa-sim container.
3. `TestDebugFullFlow_RADIUS_Forward`, `TestDebugFullFlow_DIAMETER_Forward`, and `TestDebugFullFlow_AMFCallback` all pass.
4. `nssAAF-debug trace --redis <addr> --gpsi msisdn-208046123456789` displays the grouped table from §4.
5. `nssAAF-debug trace --redis <addr> --gpsi msisdn-208046123456789 --json` emits valid JSON, one event per line.
6. All events for one forward round-trip share a single `trace_id`; all events for one server-initiated round-trip share a (different) single `trace_id`.
7. Each required event in §3 is emitted at least once per round-trip in both directions.

## 10. Open Questions Resolved

- **Q: Run against full compose stack or slim in-process?**
  **A:** Real compose stack only. The aaa-sim container is real, and we want real RADIUS/Diameter wire traffic. The test is gated on `RUN_E2E=1`.
- **Q: Which flow directions are in scope?**
  **A:** Forward (a) and server-initiated / AMF callback (c). Async EAP challenge reverse path (b) is v2.
- **Q: How do we trigger a server-initiated message from aaa-sim in a test?**
  **A:** Via a thin driver (`test/e2e/aaa_sim_driver.go`) that shells out to aaa-sim's CLI (e.g., `aaa-sim-cli send-rar <session-id>`). Exact CLI to be confirmed during planning.

## 11. References

- Parent design: `docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md`
- Implementation plan: `docs/superpowers/plans/2026-07-12-nssAAF-per-ue-debug-tracing-plan.md` (parent)
- 3GPP TS 29.526 §7.2 — NSSAA API structure
- 3GPP TS 23.502 §4.2.9 — NSSAA procedure flow