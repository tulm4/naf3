# aaa-gateway / aaa-sim Role Cleanup — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the architecturally-wrong `listen_diameter` config from `aaa-gateway`, delete orphan code paths, and align log keys with the actual server-initiated inbound semantics. (All per the revised spec `docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md`.)

**Architecture:** `aaa-gateway` is a Diameter **client** — it dials a single bidirectional TCP socket to `aaa-sim` and carries DER/DEA/ASR/RAR/STR/DWR/DWA through it. It is a RADIUS **client + server** — the forwarder dials UDP for Access-Request, and a UDP listener receives CoA/DM. No inbound Diameter TCP listener is opened on the gateway. The Diameter handlers (ASR/RAR/STR) currently live on a separate `sm.StateMachine` inside `DiameterHandler` that is bound to a TCP listener — they must be re-registered on the forwarder's state machine so they fire on the gateway's outbound socket.

**Tech Stack:** Go 1.22+, `github.com/fiorix/go-diameter/v4`, `slog`, YAML config via `gopkg.in/yaml.v3`.

---

## Spec / Code Reconciliation (READ FIRST)

Before executing, note these gaps between the spec and the current code (verified 2026-07-11):

| Spec item | Reality | Plan response |
|-----------|---------|---------------|
| §3.1 Issue A (diamForwarder reconnect dead-code) | Already implemented in `internal/aaa/gateway/diameter_forward.go` lines 192-219 (`watchDisconnect`), 223-272 (`monitorConnection`), and 275-287 (`clearPending`). Tests exist at lines 426-512 of `diameter_forward_test.go`. | **Skip — no-op.** Task 0 only verifies with `go test`. |
| §3.2 Issue B (delete `deploy/compose/aiw-tests/`) | Directory does not exist in this repo. | **Skip — no-op.** Task 0 verifies. |
| §3.3 Issue C (delete `compose/mock_aaa_s.go`) | File exists at `compose/mock_aaa_s.go` (393 lines, 11.4K). `grep -r "compose/mock_aaa_s" .` confirms no references. `Dockerfile.mock-aaa-s:16` copies `bin/aaa-sim`, not this file. `compose/fullchain-dev.yaml` uses `Dockerfile.aaa-sim`. | **Implement Task 1.** |
| §3.4 Issue D (fix stale roadmap doc) | `docs/roadmap/PHASE_Refactor_3Component.md` lines 531-532 reference `AAA_S_RADIUS_ADDR`/`AAA_S_DIAMETER_ADDR`. | **Implement Task 2.** |
| §3.5 Issue E (architectural correction — remove Diameter listen) | Spec is right, code is wrong. `gateway.go:170` constructs `DiameterHandler` with its own `sm.StateMachine`, `gateway.go:206-214` opens TCP/SCTP listen, `diameter_handler.go:107-191` implements `Listen`/`listenTCP`/`listenTLS`/`listenSCTP`. | **Implement Tasks 3-7** (the bulk of the work). |
| §3.5 RADIUS log-key rename | `cmd/aaa-gateway/main.go:39` logs `"radius_addr"` (cosmetic). | **Implement Task 8.** |
| §3.6 / §4 new tests | Two of three already exist (Test 2 + watchDisconnect test). One new test needed (Test 3 — ASR fires on forwarder's machine). | **Implement Task 9.** |

---

## File Structure

After this plan, these files exist with the following responsibilities:

- `internal/aaa/gateway/diameter_handler.go` — **deleted entirely** in Task 5 (handlers migrate to `diamForwarder`, listen/TLS paths are removed). The file is left with only the package comment.
- `internal/aaa/gateway/diameter_forward.go` — **owns** the single state machine and the server-initiated inbound handlers. Constructor accepts `forwardToBiz`, `registry`, `bizURL`, `httpClient`.
- `internal/aaa/gateway/gateway.go` — `Config` no longer has `ListenDIAMETER`. Constructs `diamForwarder` with handlers; no `DiameterHandler` field.
- `internal/config/config.go` — `AAAgwConfig` no longer has `ListenDIAMETER` or `DiameterProtocol`.
- `compose/configs/aaa-gateway.yaml` — `listenDiameter` and `diameterProtocol` lines removed.
- `cmd/aaa-gateway/main.go` — log key `"radius_addr"` renamed to `"listen_radius"`; `"listen_diameter"` line removed; `ListenDIAMETER` removed from `gateway.Config` literal; `DiameterProtocol` removed.
- `compose/mock_aaa_s.go` — **deleted**.
- `docs/roadmap/PHASE_Refactor_3Component.md` — lines 531-532 fixed.
- `internal/aaa/gateway/diameter_handler_test.go` — `TestDiameterHandler_Listen_TLSProtocol*` tests deleted (Listen gone).
- `internal/aaa/gateway/diameter_forward_test.go` — `TestDiamForwarder_ASR_FiresOnForwarderMachine` added.

---

## Task 0: Verify current state and lock in the baseline

**Files:** none (verification only)

- [ ] **Step 1: Run existing tests for the package**

Run: `cd /home/tulm/naf3 && go test ./internal/aaa/gateway/... -run 'TestDiamForwarder_WatchDisconnect|TestDiamForwarder_GetConn_AfterDisconnect' -v`
Expected: PASS (confirms Issue A is already implemented; we are not regressing it).

- [ ] **Step 2: Verify aiw-tests directory absent**

Run: `cd /home/tulm/naf3 && ls deploy/compose/aiw-tests 2>&1`
Expected: `ls: cannot access 'deploy/compose/aiw-tests': No such file or directory` (or `deploy/` itself does not exist).

- [ ] **Step 3: Verify mock_aaa_s.go is truly orphaned**

Run: `cd /home/tulm/naf3 && grep -rn "compose/mock_aaa_s\|mock_aaa_s.go" --include="*.go" --include="*.yaml" --include="*.yml" .`
Expected: only matches in `docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md` and `docs/superpowers/specs/2026-05-02-e2e-fullchain-testing-design.md` (historical).

If grep matches anywhere else, STOP and re-evaluate before deleting.

---

## Task 1: Delete orphan `compose/mock_aaa_s.go`

**Files:**
- Delete: `compose/mock_aaa_s.go`

- [ ] **Step 1: Delete the file**

Run: `cd /home/tulm/naf3 && git rm compose/mock_aaa_s.go`
Expected: file removed, staged.

- [ ] **Step 2: Verify build still compiles**

Run: `cd /home/tulm/naf3 && go build ./...`
Expected: success (no other code references this file per Task 0 Step 3).

- [ ] **Step 3: Commit**

```bash
cd /home/tulm/naf3 && git commit -m "refactor: delete orphan compose/mock_aaa_s.go

This 393-line file duplicated test/aaa_sim/ but was never referenced.
Dockerfile.mock-aaa-s copies bin/aaa-sim (the real binary), and
compose/fullchain-dev.yaml uses Dockerfile.aaa-sim, not this file.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.3.
Verified unreferenced: grep finds no callers in Go/YAML.
"
```

---

## Task 2: Fix stale roadmap doc references

**Files:**
- Modify: `docs/roadmap/PHASE_Refactor_3Component.md:531-532`

- [ ] **Step 1: Read the current lines to get exact text**

Run: `cd /home/tulm/naf3 && sed -n '528,535p' docs/roadmap/PHASE_Refactor_3Component.md`
Expected: shows the env-var block referencing `AAA_S_RADIUS_ADDR` / `AAA_S_DIAMETER_ADDR`.

- [ ] **Step 2: Replace the env-var block with config-key comment**

Use `StrReplace` to swap the two lines (and only those two) with the comment block from the spec §3.4:

old_string:
```
  AAA_S_RADIUS_ADDR: mock-aaa-s:1812
  AAA_S_DIAMETER_ADDR: mock-aaa-s:3868
```
new_string:
```
  # Config keys are set in compose/configs/aaa-gateway.yaml under `aaaGateway.`
  # (YAML fields: diameterServerAddress / radiusServerAddress).
  # Default example: diameterServerAddress: aaa-sim:3868
  #                  radiusServerAddress:   aaa-sim:1812
```

Verify with `git diff docs/roadmap/PHASE_Refactor_3Component.md` — should show 2 lines removed, 5 lines added.

- [ ] **Step 3: Commit (force-add because docs/ is gitignored)**

```bash
cd /home/tulm/naf3 && git add -f docs/roadmap/PHASE_Refactor_3Component.md && git commit -m "docs: fix stale AAA_S_*_ADDR env-var references in PHASE_Refactor_3Component.md

These env-var names never existed in the codebase. The actual config
keys are YAML fields under aaaGateway (diameterServerAddress /
radiusServerAddress) set in compose/configs/aaa-gateway.yaml.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.4.
"
```

---

## Task 3: Extend `newDiamForwarder` to accept handler dependencies

**Files:**
- Modify: `internal/aaa/gateway/diameter_forward.go:100-151`

**Goal:** The forwarder's constructor must accept the dependencies needed to register ASR/RAR/STR handlers on `df.machine`: `forwardToBiz`, `registry`, `bizURL`, `httpClient`.

- [ ] **Step 1: Add new dependencies to `diamForwarder` struct**

In `internal/aaa/gateway/diameter_forward.go`, add fields to the struct (currently lines 55-91). Insert after `logger`:

```go
    logger      *slog.Logger
    connected   bool

    // Server-initiated inbound (handlers live on df.machine, fire on the
    // forwarder's outbound socket — TCP is bidirectional, RFC 6733 §5.6).
    forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
    registry     *ServerInitiatedRegistry
    bizURL       string
    httpClient   *http.Client
```

- [ ] **Step 2: Extend `newDiamForwarder` signature and wire dependencies**

Replace the function signature at line 100:

```go
func newDiamForwarder(
    addr, network, originHost, originRealm, destHost, destRealm string,
    cfg *diamForwarderConfig,
    logger *slog.Logger,
    forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte),
    registry *ServerInitiatedRegistry,
    bizURL string,
    httpClient *http.Client,
) *diamForwarder {
```

In the `df := &diamForwarder{...}` literal (around line 109-119), add the four new fields:

```go
    df := &diamForwarder{
        addr:         addr,
        network:      network,
        originHost:   originHost,
        originRealm:  originRealm,
        destHost:     destHost,
        destRealm:    destRealm,
        logger:       logger,
        cfg:          cfg,
        pending:      make(map[uint32]chan *diam.Message),
        forwardToBiz: forwardToBiz,
        registry:     registry,
        bizURL:       bizURL,
        httpClient:   httpClient,
    }
```

After `df.machine.Handle("DWA", df.handleDWA())` at line 148, register the server-initiated handlers (these are methods we'll move from `DiameterHandler` in Task 5):

```go
    // Register server-initiated inbound handlers (ASR/RAR/STR) on this same
    // state machine so they fire on the forwarder's outbound TCP socket.
    // RFC 6733 §5.6: TCP connections are bidirectional; server-initiated
    // messages arrive on the same socket the gateway dialed.
    df.machine.Handle("ASR", df.handleASR())
    df.machine.Handle("ASA", df.handleASA())
    df.machine.Handle("RAR", df.handleRAR())
    df.machine.Handle("RAA", df.handleRAA())
    df.machine.Handle("STR", df.handleSTR())
    df.machine.Handle("STA", df.handleSTA())
```

(Note: the `handleASR` etc. methods are still on `DiameterHandler` at this point. Task 5 migrates them to `diamForwarder`. The compiler will break between this task and Task 5; that's expected.)

- [ ] **Step 3: Update existing test callers in `diameter_forward_test.go`**

Two patterns exist: `newDiamForwarder(...)` calls without the new args (search `grep -n "newDiamForwarder" internal/aaa/gateway/diameter_forward_test.go`). Each call must add four `nil` arguments at the end:

```go
df := newDiamForwarder(
    "localhost:3868",
    "tcp",
    "aaa-gateway.example.com",
    "example.com",
    "aaa-server.example.com",
    "example.com",
    DefaultConfig(),
    slog.Default(),
    nil, // forwardToBiz — tests don't exercise server-initiated path
    nil, // registry
    "",  // bizURL
    nil, // httpClient
)
```

For all 12 test functions that construct a forwarder, apply this change. Use `StrReplace` per test or `replace_all` if old_string is unique.

- [ ] **Step 4: Run go build to confirm the constructor compiles (other callers will break — that's fine)**

Run: `cd /home/tulm/naf3 && go build ./internal/aaa/gateway/...`
Expected: FAIL — `gateway.go:156` calls `newDiamForwarder(...)` with the old signature.

That's expected. Move on. The full build won't succeed until Task 7 updates the gateway.

- [ ] **Step 5: Commit**

```bash
cd /home/tulm/naf3 && git add internal/aaa/gateway/diameter_forward.go internal/aaa/gateway/diameter_forward_test.go && git commit -m "refactor(diam_forward): accept handler dependencies in constructor

The forwarder now owns the server-initiated inbound state machine
(ASR/RAR/STR handlers). Constructor accepts forwardToBiz, registry,
bizURL, httpClient. Test callers pass nil for these (tests don't
exercise server-initiated path).

Build is intentionally broken until the gateway migration in Task 7
and the handler move in Task 5.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.5 step 8.
"
```

---

## Task 4: Move ASR/ASA/RAR/RAA/STR/STA handler methods from `DiameterHandler` to `diamForwarder`

**Files:**
- Modify: `internal/aaa/gateway/diameter_handler.go`
- Modify: `internal/aaa/gateway/diameter_forward.go`

**Goal:** The six handler methods currently on `DiameterHandler` move to `diamForwarder`. They reference `df.forwardToBiz`, `df.registry`, `df.bizURL`, `df.httpClient`, `df.logger`, `df.originHost`, `df.originRealm`, `df.settings`, and the package-level `extractSessionIDFromMsg` plus the `extractAuthCtxID` method (which also moves to `diamForwarder`).

- [ ] **Step 1: Read each handler method and its helper calls**

- [ ] **Step 2: Move `handleASR` to `diamForwarder`**

The new method on `diamForwarder`. Note: `extractAuthCtxID` is currently a method on `DiameterHandler` (line 585). For now, inline its extraction logic in the moved method (call `df.extractAuthCtxID(m)` — Task 5 migrates that method too):

```go
func (df *diamForwarder) handleASR() diam.HandlerFunc {
    return func(conn diam.Conn, m *diam.Message) {
        sessionID := extractSessionIDFromMsg(m)
        if sessionID == "" {
            sessionID = "unknown"
        }

        authCtxID := df.extractAuthCtxID(m)

        df.logger.Info("diameter_asr_received",
            "session_id", sessionID,
            "hop_by_hop", m.Header.HopByHopID,
            "end_to_end", m.Header.EndToEndID,
        )

        raw, err := m.Serialize()
        if err != nil {
            df.logger.Error("failed to serialize ASR", "error", err)
            df.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
            return
        }

        respCh, err := df.registry.Register(sessionID, authCtxID, "ASR", 10*time.Second)
        if err != nil {
            df.logger.Error("failed to register ASR", "error", err)
            df.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
            return
        }

        go func() {
            df.forwardToBiz(context.Background(), sessionID, "DIAMETER", "ASR", raw)
            resp := respCh.Wait()
            df.logger.Info("ASR: received response from registry",
                "session_id", sessionID,
                "result_code", resp.ResultCode,
            )
            df.sendASAWithResult(conn, m, resp)
        }()
    }
}
```

- [ ] **Step 3: Move `handleASA` (line 338) to `diamForwarder`**

```go
func (df *diamForwarder) handleASA() diam.HandlerFunc {
    return func(conn diam.Conn, m *diam.Message) {
        sessionID := extractSessionIDFromMsg(m)
        df.logger.Debug("diameter_asa_received", "session_id", sessionID)
    }
}
```

- [ ] **Step 4: Move `handleRAR` (line 350) to `diamForwarder`**

```go
func (df *diamForwarder) handleRAR() diam.HandlerFunc {
    return func(conn diam.Conn, m *diam.Message) {
        sessionID := extractSessionIDFromMsg(m)
        if sessionID == "" {
            sessionID = "unknown"
        }
        authCtxID := df.extractAuthCtxID(m)
        df.logger.Info("diameter_rar_received",
            "session_id", sessionID,
            "hop_by_hop", m.Header.HopByHopID,
            "end_to_end", m.Header.EndToEndID,
        )
        raw, err := m.Serialize()
        if err != nil {
            df.logger.Error("failed to serialize RAR", "error", err)
            df.sendRAAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
            return
        }
        respCh, err := df.registry.Register(sessionID, authCtxID, "RAR", 10*time.Second)
        if err != nil {
            df.logger.Error("failed to register RAR", "error", err)
            df.sendRAAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
            return
        }
        go func() {
            df.forwardToBiz(context.Background(), sessionID, "DIAMETER", "RAR", raw)
            resp := respCh.Wait()
            df.logger.Info("RAR: received response from registry",
                "session_id", sessionID,
                "result_code", resp.ResultCode,
            )
            df.sendRAAWithResult(conn, m, resp)
        }()
    }
}
```

- [ ] **Step 5: Move `handleRAA` (line 404) to `diamForwarder`**

```go
func (df *diamForwarder) handleRAA() diam.HandlerFunc {
    return func(conn diam.Conn, m *diam.Message) {
        sessionID := extractSessionIDFromMsg(m)
        df.logger.Debug("diameter_raa_received", "session_id", sessionID)
    }
}
```

- [ ] **Step 6: Move `handleSTR` (line 417) to `diamForwarder`**

```go
func (df *diamForwarder) handleSTR() diam.HandlerFunc {
    return func(conn diam.Conn, m *diam.Message) {
        sessionID := extractSessionIDFromMsg(m)
        if sessionID == "" {
            sessionID = "unknown"
        }
        authCtxID := df.extractAuthCtxID(m)
        df.logger.Info("diameter_str_received",
            "session_id", sessionID,
            "hop_by_hop", m.Header.HopByHopID,
            "end_to_end", m.Header.EndToEndID,
        )
        raw, err := m.Serialize()
        if err != nil {
            df.logger.Error("failed to serialize STR", "error", err)
            df.sendSTAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
            return
        }
        respCh, err := df.registry.Register(sessionID, authCtxID, "STR", 10*time.Second)
        if err != nil {
            df.logger.Error("failed to register STR", "error", err)
            df.sendSTAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
            return
        }
        go func() {
            df.forwardToBiz(context.Background(), sessionID, "DIAMETER", "STR", raw)
            resp := respCh.Wait()
            df.logger.Info("STR: received response from registry",
                "session_id", sessionID,
                "result_code", resp.ResultCode,
            )
            df.sendSTAWithResult(conn, m, resp)
        }()
    }
}
```

- [ ] **Step 7: Move `handleSTA` (line 457) to `diamForwarder`**

```go
func (df *diamForwarder) handleSTA() diam.HandlerFunc {
    return func(conn diam.Conn, m *diam.Message) {
        sessionID := extractSessionIDFromMsg(m)
        df.logger.Debug("diameter_sta_received", "session_id", sessionID)
    }
}
```

- [ ] **Step 8: Move the send-helper methods to `diamForwarder`**

Move `sendASAWithResult` (line 322), `sendRAAWithResult` (line 389), `sendSTAWithResult` (line 467), and the wrapper methods `sendASA` (line 483), `sendRAA` (line 496), `sendSTA` (line 509). Each:
- Change receiver `(h *DiameterHandler)` → `(df *diamForwarder)`.
- Replace `h.sm.Settings().OriginHost` → `df.settings.OriginHost`.
- Replace `h.sm.Settings().OriginRealm` → `df.settings.OriginRealm`.

Read each method first to capture the exact body before transforming. (The bodies are mechanical — `m.Answer(code)` + set HopByHop/EndToEnd + AVPs + `WriteTo(conn)`.)

- [ ] **Step 9: Move `extractAuthCtxID` (line 585) to `diamForwarder`**

Change receiver `(h *DiameterHandler)` → `(df *diamForwarder)`. The body reads AVPs from the message; no `DiameterHandler`-specific state. Move as-is with the receiver change.

- [ ] **Step 10: Move `validatePeer` (line 547) to `diamForwarder`**

Note: `validatePeer` was used in `HandleConnection` to check `smpeer.FromContext(...)` after CER/CEA. Without the inbound TCP listener, it has no inbound connection to validate. Two options:
- **Delete `validatePeer`** — the gateway's outbound TCP socket's peer is `aaa-sim`, which we trust (it was configured). Just delete the method.
- **Move it for defense-in-depth** — call it after `DialNetwork` succeeds before installing the conn in `df.conn`. Recommended for spec compliance (TS 29.561).

Recommended: delete it. The gateway's outbound socket has a known peer (the configured AAA-S); per RFC 6733 §5.6 the gateway is the client and the configured peer is trusted. Add a `// deleted` comment in the plan's Step 10 commit message.

```go
// validatePeer DELETED: inbound peer validation no longer needed.
// aaa-gateway never accepts inbound TCP connections — its outbound socket
// connects to the configured AAA-S, which is trusted by configuration.
// Spec: RFC 6733 §5.6, TS 29.561 Ch.17.
```

- [ ] **Step 11: Delete the moved methods from `DiameterHandler`**

In `diameter_handler.go`, remove the original `handleASR/ASA/RAR/RAA/STR/STA` methods, the `sendASAWithResult/sendRAAWithResult/sendSTAWithResult/sendASA/sendRAA/sendSTA` methods, `extractAuthCtxID`, and `validatePeer`.

- [ ] **Step 12: Run go vet on `diameter_forward.go`**

Run: `cd /home/tulm/naf3 && go vet ./internal/aaa/gateway/diameter_forward.go 2>&1 | head -40`
Expected: 0 errors in `diameter_forward.go`.

- [ ] **Step 13: Commit**

```bash
cd /home/tulm/naf3 && git add internal/aaa/gateway/diameter_forward.go internal/aaa/gateway/diameter_handler.go && git commit -m "refactor(diam): migrate ASR/ASA/RAR/RAA/STR/STA handlers from DiameterHandler to diamForwarder

The handlers now live on the type that owns the state machine
(diamForwarder). They fire on the gateway's outbound TCP socket
when AAA-S sends server-initiated messages (RFC 6733 §5.6: TCP is
bidirectional; ASR/RAR/STR arrive on the same connection the gateway
dialed).

Also migrates extractAuthCtxID. validatePeer is deleted: the
gateway's outbound socket connects to the configured AAA-S, which
is trusted by configuration; no inbound TCP listener exists to
validate.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.5 step 4.
"
```

---

## Task 5: Strip `DiameterHandler` to message-handler-only — delete `Listen`/TLS/serve/Forward

**Files:**
- Modify: `internal/aaa/gateway/diameter_handler.go`
- Modify: `internal/aaa/gateway/diameter_handler_test.go`

**Goal:** Remove `Listen`, `listenTCP`, `listenTLS`, `listenSCTP`, `serveTCP`, `serveSCTP`, `HandleConnection`, `DiameterHandlerConfig`, `Forward`. The struct becomes a thin shell holding logger + state machine (now unused — will be removed in next task).

- [ ] **Step 1: Delete `DiameterHandlerConfig` struct (lines 26-33)**

Replace the struct + its comment with nothing. Add a TODO if removing imports becomes unclear; resolve in Step 6.

- [ ] **Step 2: Delete `Listen`, `listenTCP`, `listenTLS`, `listenSCTP`, `serveTCP`, `serveSCTP`, `HandleConnection` methods**

These are the methods from line 102 (`Listen`) through line 269 (`HandleConnection` end). Delete all of them in one StrReplace.

- [ ] **Step 3: Delete `Forward` method (lines ~521-527)**

This was already a stub returning an error; remove it.

- [ ] **Step 4: Delete `NewDiameterHandler` (lines 58-100)**

After Task 4, the constructor no longer needs to register handlers on `h.sm` (they moved). And once `gateway.go` no longer constructs `DiameterHandler` (Task 7), the constructor is dead. Delete it now.

- [ ] **Step 5: Delete `DiameterHandler` struct (lines 35-56)**

After Steps 1-4, the struct has no fields in use. Delete it.

- [ ] **Step 6: Delete unused imports**

After the deletions, the following imports become unused: `crypto/tls`, `crypto/x509`, `net`, `os`, `github.com/fiorix/go-diameter/v4/diam/dict`, `github.com/fiorix/go-diameter/v4/diam/smpeer`. (Keep `diam`, `avp`, `datatype`, `sm` if used by anything left — most likely nothing is.)

The file should now be empty (or contain only a package comment). Reduce it to:

```go
// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 2865, RFC 3579, RFC 6733, RFC 4072, TS 29.561 Ch.16/17
package gateway
```

- [ ] **Step 7: Delete `TestDiameterHandler_Listen_TLSProtocol` and `TestDiameterHandler_Listen_TLSProtocol_MissingCert` from `diameter_handler_test.go`**

These tests reference the deleted `Listen` method. Delete them (and any helpers they use, like `generateTestCerts` if no other test references it).

Also delete any test that uses `&DiameterHandler{...}` or `&DiameterHandlerConfig{...}`.

- [ ] **Step 8: Verify file compiles**

Run: `cd /home/tulm/naf3 && go build ./internal/aaa/gateway/... 2>&1 | head -30`
Expected: errors only in `gateway.go` (still references `DiameterHandler`). Fix in Task 7.

- [ ] **Step 9: Commit**

```bash
cd /home/tulm/naf3 && git add internal/aaa/gateway/diameter_handler.go internal/aaa/gateway/diameter_handler_test.go && git commit -m "refactor(diam_handler): delete Listen/TLS/serve/Forward paths

aaa-gateway no longer opens a Diameter TCP/TLS/SCTP listener. The
single bidirectional TCP socket dialed by diamForwarder carries all
Diameter traffic in both directions per RFC 6733 §5.6.

DiameterHandler struct, NewDiameterHandler, Listen/listenTCP/TLS/SCTP,
serveTCP/SCTP, HandleConnection, DiameterHandlerConfig, and Forward
are deleted. Two TLS-listener tests are deleted.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.5 step 1.
"
```

---

## Task 6: Remove `ListenDIAMETER`, `DiameterProtocol` from config + YAML

**Files:**
- Modify: `internal/config/config.go:115-117, 485-490`
- Modify: `compose/configs/aaa-gateway.yaml:19-21`
- Modify: `cmd/aaa-gateway/main.go:40, 47-48` (lines for the deleted fields; the cosmetic rename comes in Task 8)

- [ ] **Step 1: Delete `ListenDIAMETER` and `DiameterProtocol` fields from `AAAgwConfig`**

In `internal/config/config.go` lines 116-117, delete:

```go
ListenDIAMETER   string `yaml:"listenDiameter"`   // ":3868"
DiameterProtocol string `yaml:"diameterProtocol"` // "tcp" or "sctp"
```

- [ ] **Step 2: Delete the default-value blocks for those fields**

In `internal/config/config.go` around lines 485-490, delete:

```go
if cfg.AAAgw.ListenDIAMETER == "" {
    cfg.AAAgw.ListenDIAMETER = ":3868"
}
if cfg.AAAgw.DiameterProtocol == "" {
    cfg.AAAgw.DiameterProtocol = "tcp"
}
```

(Read lines 480-500 first to find exact code; if other defaults live nearby, only delete the two `if cfg.AAAgw.ListenDIAMETER` / `if cfg.AAAgw.DiameterProtocol` blocks.)

- [ ] **Step 3: Remove `listenDiameter` and `diameterProtocol` from the YAML config**

In `compose/configs/aaa-gateway.yaml` delete lines 20 (`listenDiameter: ":3868"`) and 21 (`diameterProtocol: "tcp"`).

- [ ] **Step 4: Run config tests**

Run: `cd /home/tulm/naf3 && go test ./internal/config/... -v 2>&1 | tail -30`
Expected: PASS. If any test references the deleted fields, update the test fixture.

- [ ] **Step 5: Commit**

```bash
cd /home/tulm/naf3 && git add internal/config/config.go compose/configs/aaa-gateway.yaml && git commit -m "refactor(config): drop ListenDIAMETER and DiameterProtocol

aaa-gateway is a Diameter client and does not pick a listener
protocol. The forwarder dials a single TCP socket (hardcoded 'tcp').

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.5 step 5-6.
"
```

---

## Task 7: Update `gateway.go` to construct forwarder with handlers and drop `DiameterHandler`

**Files:**
- Modify: `internal/aaa/gateway/gateway.go`

**Goal:** Remove `ListenDIAMETER` from `Config`, remove `DiameterHandler` construction and listen-goroutine, update `newDiamForwarder` call to pass handler deps, remove `diameterHandler` field.

- [ ] **Step 1: Read the affected sections**

Read `gateway.go:51-52, 56` (Config struct), `:109-110` (Gateway struct fields), `:155-186` (constructor block), `:206-214` (ListenDIAMETER goroutine).

- [ ] **Step 2: Remove `ListenDIAMETER` and `DiameterProtocol` from `Config`**

In `gateway.go:51-56`, delete `ListenDIAMETER` and `DiameterProtocol` lines.

- [ ] **Step 3: Remove `diameterHandler` field from `Gateway` struct**

In `gateway.go:108-111`, delete the `diameterHandler *DiameterHandler` line.

- [ ] **Step 4: Update `newDiamForwarder` call to pass handler dependencies**

In `gateway.go:155-168`, replace the call to add the four new arguments: `g.forwardToBiz`, `g.registry`, `cfg.BizServiceURL`, `g.bizHTTPClient`. The call becomes:

```go
g.diamForwarder = newDiamForwarder(
    cfg.DiameterServerAddress,
    cfg.DiameterProtocol,   // forwarder still takes a network; keep this for now
    cfg.DiameterHost,
    cfg.DiameterRealm,
    cfg.DiameterServerAddress,
    cfg.DiameterRealm,
    &diamForwarderConfig{
        AuthRequestType:   cfg.DiameterAuthRequestType,
        AuthApplicationID: cfg.DiameterAuthApplicationID,
    },
    cfg.Logger,
    g.forwardToBiz,
    g.registry,
    cfg.BizServiceURL,
    g.bizHTTPClient,
)
```

If `cfg.DiameterProtocol` is deleted in Task 6, replace it with the literal `"tcp"`. (The forwarder's `network` field is set but only used during `DialNetwork`. The plan keeps `tcp` as a constant here.)

- [ ] **Step 5: Remove the `DiameterHandler` construction block**

In `gateway.go:170-186`, delete the `g.diameterHandler = NewDiameterHandler(...)` block.

- [ ] **Step 6: Remove the Diameter-listen goroutine**

In `gateway.go:206-214`, delete:

```go
if g.cfg.ListenDIAMETER != "" {
    g.wg.Add(1)
    go func() {
        defer g.wg.Done()
        if err := g.diameterHandler.Listen(g.ctx, g.cfg.ListenDIAMETER, g.cfg.DiameterProtocol); err != nil {
            g.logger.Error("diameter listener failed", "error", err)
        }
    }()
}
```

- [ ] **Step 7: Run go build**

Run: `cd /home/tulm/naf3 && go build ./...`
Expected: success.

- [ ] **Step 8: Run gateway tests**

Run: `cd /home/tulm/naf3 && go test ./internal/aaa/gateway/... 2>&1 | tail -30`
Expected: PASS.

If a test references `g.diameterHandler`, `cfg.ListenDIAMETER`, or `cfg.DiameterProtocol`, update the test fixture to match the new Config shape.

- [ ] **Step 9: Commit**

```bash
cd /home/tulm/naf3 && git add internal/aaa/gateway/gateway.go && git commit -m "refactor(gateway): drop DiameterHandler, forwarder owns server-initiated path

Config has no ListenDIAMETER/DiameterProtocol. Gateway struct has no
diameterHandler field. The DiamForwarder is constructed with handler
dependencies and registers ASR/RAR/STR on its own state machine, so
those messages fire on the gateway's outbound TCP socket.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.5 step 4.
"
```

---

## Task 8: Update `cmd/aaa-gateway/main.go` — drop Diameter log/config fields, rename RADIUS log key

**Files:**
- Modify: `cmd/aaa-gateway/main.go:37-50`

- [ ] **Step 1: Remove the `"listen_diameter"` log key**

In `cmd/aaa-gateway/main.go:40`, delete:

```go
"listen_diameter", cfg.AAAgw.ListenDIAMETER, // server-initiated inbound
```

- [ ] **Step 2: Rename `"radius_addr"` to `"listen_radius"`**

In `cmd/aaa-gateway/main.go:39`, change:

```go
"radius_addr", cfg.AAAgw.ListenRADIUS,
```

to:

```go
"listen_radius", cfg.AAAgw.ListenRADIUS,
```

- [ ] **Step 3: Remove `ListenDIAMETER` and `DiameterProtocol` from the `gateway.Config` literal**

In `cmd/aaa-gateway/main.go:46-48`, delete:

```go
ListenDIAMETER:        cfg.AAAgw.ListenDIAMETER,
DiameterProtocol:      cfg.AAAgw.DiameterProtocol,
```

If line 48 is just a trailing field, also delete it (keep one trailing comma on the line above).

- [ ] **Step 4: Build and run**

Run: `cd /home/tulm/naf3 && go build ./... && go test ./cmd/... ./internal/aaa/gateway/... 2>&1 | tail -20`
Expected: success.

- [ ] **Step 5: Commit**

```bash
cd /home/tulm/naf3 && git add cmd/aaa-gateway/main.go && git commit -m "refactor(main): drop Diameter log+config fields, rename RADIUS log key

listen_diameter removed (aaa-gateway doesn't listen on Diameter).
radius_addr renamed to listen_radius for grep-ability between
server-initiated listen vs. client-initiated forwarding addrs.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §3.5 + RADIUS rename.
"
```

---

## Task 9: Add ASR-handler-fires-on-forwarder-machine test

**Files:**
- Modify: `internal/aaa/gateway/diameter_forward_test.go` (append)

- [ ] **Step 1: Append the new test**

At the end of `diameter_forward_test.go`, add:

```go
// TestDiamForwarder_ASR_FiresOnForwarderMachine verifies the architectural
// migration: an inbound ASR on the gateway's outbound TCP socket fires
// handleASR (now registered on diamForwarder.machine, not on a separate
// DiameterHandler state machine).
func TestDiamForwarder_ASR_FiresOnForwarderMachine(t *testing.T) {
    // Construct forwarder with a real registry and stub forwardToBiz.
    registry := NewServerInitiatedRegistry()
    var forwarded []byte
    var forwardedMu sync.Mutex
    forwardToBiz := func(ctx context.Context, sessionID, transportType, messageType string, raw []byte) {
        forwardedMu.Lock()
        forwarded = raw
        forwardedMu.Unlock()
        // Simulate Biz Pod response so the handler goroutine exits cleanly.
        resp := &ServerInitiatedResponse{ResultCode: 2001}
        registry.Complete(sessionID, resp)
    }

    df := newDiamForwarder(
        "127.0.0.1:1", // never connects
        "tcp",
        "aaa-gateway.example.com",
        "example.com",
        "aaa-server.example.com",
        "example.com",
        DefaultConfig(),
        slog.Default(),
        forwardToBiz,
        registry,
        "http://biz:8080",
        nil,
    )

    // Build an ASR message and dispatch it through the state machine so the
    // registered handler fires (ServeDIAM is the public dispatch API).
    fake := newFakeNotifierConn()
    m := diam.NewRequest(diam.AbortSession, df.cfg.AuthApplicationID, dict.Default)
    _, _ = m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String("sess-1"))
    m.Header.HopByHopID = 1
    m.Header.EndToEndID = 2

    df.machine.ServeDIAM(fake, m)

    // Wait briefly for the goroutine that calls forwardToBiz.
    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        forwardedMu.Lock()
        done := len(forwarded) > 0
        forwardedMu.Unlock()
        if done {
            break
        }
        time.Sleep(10 * time.Millisecond)
    }
    forwardedMu.Lock()
    forwardedLen := len(forwarded)
    forwardedMu.Unlock()
    if forwardedLen == 0 {
        t.Fatal("forwardToBiz was never called by ASR handler")
    }
}
```

- [ ] **Step 2: Add the missing imports**

`diameter_forward_test.go` already imports `context`, `crypto/tls`, `io`, `log/slog`, `net`, `sync/atomic`, `testing`, `time`, `dict`. Add:
- `"sync"` — for `sync.Mutex`.
- `"github.com/fiorix/go-diameter/v4/diam"` — for `diam.NewRequest`, `diam.AbortSession`.
- `"github.com/fiorix/go-diameter/v4/diam/avp"` — for `avp.SessionID`, `avp.Mbit`.
- `"github.com/fiorix/go-diameter/v4/diam/datatype"` — for `datatype.UTF8String`.

(If the existing test file already uses some of these, only add what's missing.)

- [ ] **Step 3: Run the new test**

Run: `cd /home/tulm/naf3 && go test ./internal/aaa/gateway/ -run TestDiamForwarder_ASR_FiresOnForwarderMachine -v`
Expected: PASS.

- [ ] **Step 4: Run full gateway tests**

Run: `cd /home/tulm/naf3 && go test ./internal/aaa/gateway/... -v 2>&1 | tail -50`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/tulm/naf3 && git add internal/aaa/gateway/diameter_forward_test.go && git commit -m "test(diam_forward): verify ASR fires on forwarder's state machine

Confirms the architectural migration: ASR/RAR/STR handlers now live on
diamForwarder.machine and fire on the gateway's outbound TCP socket,
not on a separate DiameterHandler state machine bound to an inbound
listener.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md §4 Test 3.
"
```

---

## Task 10: Final verification sweep

**Files:** none (verification only)

- [ ] **Step 1: Full build**

Run: `cd /home/tulm/naf3 && go build ./...`
Expected: success.

- [ ] **Step 2: Full test run for affected packages**

Run: `cd /home/tulm/naf3 && go test ./internal/aaa/gateway/... ./internal/config/... ./cmd/... 2>&1 | tail -40`
Expected: all PASS.

- [ ] **Step 3: Architectural invariants**

```bash
cd /home/tulm/naf3
grep -rn "ListenDIAMETER\|listenDiameter\|listen_diameter" --include="*.go" --include="*.yaml" .
grep -rn "AAA_S_RADIUS_ADDR\|AAA_S_DIAMETER_ADDR\|AAA_GW_RADIUS_ADDR\|AAA_GW_DIAMETER_ADDR" --include="*.go" --include="*.yaml" --include="*.yml" .
grep -rn "mock_aaa_s" --include="*.go" --include="*.yaml" --include="*.yml" .
grep -rn "DiameterHandler" --include="*.go" . | grep -v "_test.go"
ls deploy/compose/aiw-tests 2>/dev/null
```

Expected outputs:
- First grep: no results (Diameter listen removed everywhere).
- Second grep: no results.
- Third grep: no results.
- Fourth grep: only references inside test files (`server_initiated_test.go` may still reference registry/helpers) — should be empty in production code.
- Fifth ls: "No such file or directory" or `deploy/` doesn't exist.

- [ ] **Step 4: Lint**

Run: `cd /home/tulm/naf3 && go vet ./...`
Expected: clean.

If `golangci-lint` is configured in the repo, also run `golangci-lint run ./internal/aaa/gateway/... ./cmd/aaa-gateway/...`.

- [ ] **Step 5: End-to-end conformance (optional, manual)**

Run: `cd /home/tulm/naf3 && make conformance` (or the project's documented conformance command).

Expected: ASR/RAR/STR still flow end-to-end through the forwarder's outbound TCP socket. If they don't, Task 4's handler move missed something — most likely a helper method like `extractAuthCtxID` not being migrated alongside the handler.

- [ ] **Step 6: Final summary commit (if any drift was fixed)**

If Steps 1-5 forced any small fixups, commit them with:

```bash
cd /home/tulm/naf3 && git add -A && git commit -m "fix(gateway): final verification sweep fixes

Adjustments discovered during final go build / go test / conformance
sweep after the aaa-gateway / aaa-sim role cleanup.

Per docs/superpowers/specs/2026-07-11-aaa-gateway-aaa-sim-role-cleanup-design.md.
"
```

---

## Summary

| Defect | Spec § | Plan Task(s) | Status |
|--------|--------|--------------|--------|
| A: Reconnect dead code | 3.1 | Task 0 (verify; already done) | Already implemented |
| B: aiw-tests compose | 3.2 | Task 0 (verify; already absent) | Already absent |
| C: orphan mock_aaa_s.go | 3.3 | Task 1 | Will be deleted |
| D: stale roadmap doc | 3.4 | Task 2 | Will be fixed |
| E: Diameter listen removal | 3.5 | Tasks 3-7, 8 | Will be implemented |
| E: RADIUS log rename | 3.5 (kept) | Task 8 | Will be renamed |
| F: ASR-on-forwarder test | 4 (Test 3) | Task 9 | Will be added |

Tasks execute in order 0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10. Each is committed atomically.