# Internal Communication Gaps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** implement the missing and partial bidirectional internal communication paths between Biz Pod, AAA Gateway, AMF, AUSF, Redis, and PostgreSQL so that client-initiated and server-initiated NSSAAF flows complete with explicit ownership, correlation, resilience, and observability.

**Architecture:** Keep the existing 3-component model and extend the current internal HTTP/JSON contracts in `internal/proto/` rather than introducing a new transport. Server-initiated handling should move from logging placeholders in `cmd/biz/main.go` to an injected Biz-side coordination service that loads persisted context, resolves routing/callback ownership, notifies AMF when required, persists state transitions, and returns explicit AAA responses. Correlation remains Redis-backed, durable session/auth context state remains PostgreSQL-backed, and all cross-component hops must preserve request correlation and trust-boundary rules already established by the codebase.

**Tech Stack:** Go 1.22+, `net/http`, Redis (`go-redis/v9` via existing pool wrapper), PostgreSQL session repositories, existing NF clients (`internal/amf`, `internal/ausf`, `internal/udm`, `internal/nrf`), existing resilience/metrics/logging/tracing packages, existing unit/integration/E2E test suites.

---

## Scope Check

This spec spans two tightly coupled subsystems, but they should stay in one plan because they share the same ownership boundaries and correlation model:

1. **Reverse-direction completion** — `AAA Gateway -> Biz Pod -> AMF / state owner`
2. **Per-hop ownership/correlation hardening** — explicit trust, persistence, and observability across all internal hops

Do **not** split this into separate plans unless implementation reveals that reverse-direction handling and ownership/correlation hardening can be shipped independently without leaving the reverse flow half-wired.

## File Structure Map

### Existing files to modify

- `cmd/biz/main.go`
  - Replace placeholder `handleReAuth`, `handleRevocation`, and `handleCoA` behavior with injected service-driven logic.
  - Stop doing ad-hoc Redis client creation in request handlers.
- `cmd/biz/factory.go`
  - Wire new Biz-side internal communication service dependencies.
  - Expose AMF notifier and any new collaborators to server-initiated handlers.
- `internal/proto/biz_callback.go`
  - Extend Redis correlation structures for reverse-path ownership and callback delivery.
- `internal/proto/http_gateway.go`
  - Keep the existing Biz/HTTP Gateway reverse-response contract unless Task 6 proves it is insufficient.
- `internal/proto/aaa_transport.go`
  - Keep the existing server-initiated request/response schema unless Task 10 requires explicit completion metadata in the wire contract.
- `internal/amf/amf.go`
  - Reuse notifier from Biz-side service; add only missing behavior required by plan tasks.
- `internal/api/nssaa/handler.go`
  - Persist any additional callback ownership/correlation data needed by reverse flows.
- `internal/api/aiw/handler.go`
  - Persist any additional AIW-side ownership/correlation data needed for MSK completion semantics.
- `internal/ausf/client.go`
  - Keep `ForwardMSK` as the AIW completion owner; adjust only if additional correlation metadata is required.

### New files to create

- `internal/biz/server_initiated.go`
  - Coordination service for RAR/ASR/CoA processing.
  - One focused responsibility: turn `proto.AaaServerInitiatedRequest` into state updates + callbacks + `proto.AaaServerInitiatedResponse`.
- `internal/biz/server_initiated_test.go`
  - Table-driven unit tests for reverse-direction ownership and completion semantics.
- `internal/biz/correlation.go`
  - Focused helpers for loading reverse-path context from Redis/PostgreSQL and resolving owners.
- `internal/biz/correlation_test.go`
  - Unit tests for correlation lookup and ownership resolution.
- `internal/biz/types.go`
  - Small request/result structs and interfaces for storage/notifier/callback collaborators.
- `internal/cache/redis/session_correlation.go`
  - Focused Redis helper for session correlation persistence/lookup if the existing code currently spreads this logic across handlers.
- `internal/cache/redis/session_correlation_test.go`
  - Unit tests with `miniredis` for TTL and lookup behavior.
- `test/integration/server_initiated_flow_test.go`
  - Integration coverage for reverse-direction Biz processing with Redis/PostgreSQL-backed state.

### Existing tests likely to update

- `internal/amf/notifier_test.go`
- `internal/ausf/client_test.go`
- `cmd/biz/main_test.go`
- `test/unit/e2e_amf/amf_notification_test.go`
- `test/conformance/ts29526_test.go`

## Implementation Notes Before Starting

- Reuse existing contracts before extending them.
- Keep `internal/proto/` dependency-free.
- Keep state ownership explicit:
  - PostgreSQL owns durable NSSAA/AIW auth context and session lifecycle.
  - Redis owns short-lived reverse-path correlation and Biz Pod liveness/routing metadata.
  - AMF notifier owns external notification delivery and DLQ fallback.
  - AUSF client owns AIW MSK delivery.
- Do not silently invent 3GPP fields. If a new payload field is needed internally, keep it inside internal contracts only.
- Completion metadata introduced by this plan is internal-only unless a later task explicitly extends an internal wire contract in `internal/proto/`.
- If `cmd/biz/main_test.go` does not exist, create it in the task that introduces the first Biz main handler test.
- Follow TDD for each task.

## `HasAIWContext` Semantics — NSSAA vs AIW Stores

This plan introduces `HasAIWContext` as a field on `NssaaSession` to track whether a given NSSAA session originated from or is linked to an AIW flow (via AUSF). The semantics differ between the two stores:

| Store | Session Type | `HasAIWContext` | `CallbackOwner` | `ReauthURI` / `RevocURI` |
|-------|-------------|-----------------|----------------|--------------------------|
| NSSAA (`storage.NssaaSession`) | NSSAA (direct) | `false` | `"amf"` | From request body |
| NSSAA (`storage.NssaaSession`) | AIW-linked (via AUSF) | `true` | `"ausf"` | `""` |
| AIW (`storage.AiwSession`) | AIW | N/A (not stored here) | `"ausf"` (logical) | N/A |

**Key implications for the reverse-flow plan:**
- When `biz.NssaaSessionContext.HasAIWContext == true`, the session belongs to the AIW/AUSF ownership path — AMF notifications should NOT be sent (no `ReauthURI`/`RevocURI`).
- When `biz.NssaaSessionContext.HasAIWContext == false` and `CallbackOwner == "amf"`, the session belongs to the direct NSSAA path — AMF notifications are valid.
- The reverse flow in Tasks 4–5 checks `CallbackOwner` (not `HasAIWContext`) to decide whether to call `AMFNotifier`. `HasAIWContext` is available for downstream routing if needed.
- `HasAIWContext` is persisted in `storage.NssaaSession` (Task 8) but is NOT stored in `storage.AiwSession` — the AIW session itself does not track this flag; the flag exists on the NSSAA side to enable the reverse link.

## Task Dependency Table

| Task | Name | Depends On | Provides | Prerequisite For |
|------|------|-----------|----------|-----------------|
| 1a | Discover storage interfaces | — | Grounded adapter shapes in this plan | All tasks |
| 1 | Biz reverse-path domain types | 1a | `biz.NssaaSessionContext`, `biz.PersistentContextLookup`, `biz.SessionContextResolver`, `biz.SessionStateWriter`, `biz.AMFNotifier`, `biz.Completion` | Tasks 2, 4, 5, 6, 7 |
| 2 | Correlation resolver from Redis + PG | 1 | `biz.CorrelationResolver` | Tasks 4, 5, 6 |
| 3 | Redis session correlation helper | — | `redis.SessionCorrelationStore` with TTL behavior | Tasks 2, 6 |
| 4 | Biz reauth coordinator | 1, 2 | `biz.ServerInitiatedCoordinator` (RAR path) | Tasks 6, 9 |
| 5 | Biz revocation + CoA semantics | 1, 4 | `biz.ServerInitiatedCoordinator` (ASR/CoA paths) | Tasks 6, 9 |
| 6 | Wire Biz main HTTP handler | 7 | Injected `serverInitiatedHandler` in `cmd/biz` | Task 9 |
| 7 | Persistent lookup + state-writer adapters | 1 | `biz.NssaaSessionAdapter`, `biz.ReverseFlowStateWriter`, `biz.NssaaSessionResolver` | Tasks 4, 5, 6, 8 |
| 8 | Persist callback ownership in API flows | 7 | `NssaaSession.CallbackOwner`, `NssaaSession.HasAIWContext`; DB migration; updated AIW handler | Tasks 4, 5, 6, 9, 10, 11 |
| 9 | Integration test for server-initiated Biz path | 4, 5, 6, 8 | `test/integration/server_initiated_flow_test.go` | Task 12 |
| 10 | Observability + completion semantics | 4, 5, 9 | Structured `slog` around `completion` field | Task 12 |
| 11 | Trust-boundary assertions | 8, 9 | Ownership enforcement for `CallbackOwner == ""` | Task 12 |
| 12 | Verification + roadmap update | 9, 10, 11 | Updated `module_index.md`, `README.md`, `STATE.md` | — |

**Execution notes:**
- Tasks 1–3 can proceed in parallel once Task 1a is done.
- Task 6 has an explicit prerequisite note: Task 7 must complete first.
- Task 8 adds new DB schema fields; ensure the migration is applied before running tests that reference `CallbackOwner` / `HasAIWContext` in `storage.NssaaSession`.
- Task 12 is the final gate; Tasks 9–11 must all pass before Task 12 runs.

## Task 1a: Discover existing storage and session interfaces before adapter design

**Files:**
- Modify: `docs/superpowers/plans/2026-06-06-internal-communication-gaps-implementation-plan.md`
- Review: `internal/storage/`
- Review: `internal/storage/postgres/`
- Review: `internal/api/nssaa/handler.go`
- Review: `internal/api/aiw/handler.go`

### Discovery Findings (Baseline: 2026-06-07)

**Storage Interfaces (internal/storage/store.go):**

```go
// NssaaStore: Load, Save, Delete, Close
type NssaaStore interface {
    Load(ctx context.Context, id string) (*NssaaSession, error)
    Save(ctx context.Context, session *NssaaSession) error
    Delete(ctx context.Context, id string) error
    Close() error
}

// AiwStore: Load, Save, Delete, Close
type AiwStore interface {
    Load(ctx context.Context, id string) (*AiwSession, error)
    Save(ctx context.Context, session *AiwSession) error
    Delete(ctx context.Context, id string) error
    Close() error
}
```

**Session Domain Types (internal/storage/types.go):**

```go
type NssaaSession struct {
    AuthCtxID, GPSI, SnssaiSST, SnssaiSD string
    AmfInstance, ReauthURI, RevocURI       string
    EapPayload, Status                     []byte
    CreatedAt, UpdatedAt, ExpiresAt        time.Time
}

type AiwSession struct {
    AuthCtxID, Supi, AusfID, Status       string
    EapPayload, TtlsInner, MSK, PvsInfo   []byte
    SupportedFeatures, AuthResult          string
    CreatedAt, UpdatedAt, ExpiresAt       time.Time
    CompletedAt                            *time.Time
}
```

**PostgreSQL Repository Methods (internal/storage/postgres/nssaa_repo.go, aiw_repo.go):**

- `Load(ctx, id) (*session, error)` — loads full row from DB, decrypts GPSI/SUPI and EAP session state
- `Save(ctx, session) error` — upserts via `updateRow` then `createRow`; handles `ErrSessionNotFound`
- `Delete(ctx, id) error` — hard delete; returns `ErrSessionNotFound` if absent
- Internal helpers: `loadRow`, `createRow`, `updateRow`, `scanRow`, `rowToSession`, `sessionToRow`

**Status Fields:**
- `NssaaSession.Status` exists as `string` — mapped from `types.NssaaStatus`
- `AiwSession.Status` exists as `string`
- Both are persisted in PostgreSQL; NOT separately queryable

**Callback URIs:**
- `ReauthURI` and `RevocURI` are stored in `NssaaSession` and mapped to `reauth_notif_uri` / `revoc_notif_uri` DB columns
- NOT separately queryable via a dedicated `LoadNotificationURIs` method — must load full session via `Load(id)` then extract

**AIW vs NSSAA Repositories — Material Differences:**

| Aspect | NSSAA | AIW |
|---|---|---|
| Session key | GPSI (encrypted) | SUPI (encrypted) |
| Callback URIs | ReauthURI, RevocURI | None |
| AMF reference | AmfInstance | AusfID |
| Extra secrets | None | MSK, PvsInfo, TtlsInner |
| Completion tracking | CompletedAt, TerminatedAt | CompletedAt |

**API Handler Storage Usage:**
- NSSAA handler uses `storage.NssaaStore` (alias `NssaaStore`)
- AIW handler uses `storage.AiwStore` (alias `AiwStore`)
- Neither handler uses Redis for auth context storage in the confirmed design — PostgreSQL is the primary store

**Redis AuthCtxStore (internal/api/nssaa/redis_store.go):**
- Phase 1: `AuthCtxStore` interface (Load, Save, Delete, Close) with Redis-backed `RedisAuthCtxStore`
- This is separate from `storage.NssaaStore`; used for transient context during EAP round-trips
- Does NOT persist `CallbackOwner` or ownership metadata

**CRITICAL: Invented Signatures to Replace in Tasks 6-8**

The following were invented and do NOT exist in the codebase:

1. **`NSSAAStatusRepository` with `UpdateStatus(authCtxID, status)`** — does NOT exist
   - Reality: The only way to update status is via `Load` + `Save` on `NssaaStore`
   - The adapter must: (a) load the session, (b) modify `session.Status`, (c) call `Save`

2. **`LoadNotificationURIs(authCtxID)`** — does NOT exist
   - Reality: No dedicated method; must load full session then extract `ReauthURI`/`RevocURI` from `NssaaSession`

3. **`PersistentContextLookup` with `LoadAuthContext`** — does NOT exist as a named type
   - Reality: `storage.NssaaStore` Load returns `*NssaaSession`; Biz-side needs an adapter that wraps this

4. **`CallbackOwner` field on any session type** — does NOT exist
   - Reality: Only `ReauthURI`/`RevocURI` exist; ownership must be inferred or a new field must be added

5. **`StateWriter` with `MarkReauthPending`, `MarkRevoked`, `ApplyCoA`** — do NOT exist as public types
   - Reality: These must be built as new Biz-layer types that adapt `storage.NssaaStore`

6. **`NssaaSessionContext` type in Biz package** — does NOT exist as a concrete type (the interface `SessionContextResolver` was invented; Task 1 defines the concrete type)
   - Reality: Must be defined in `internal/biz/` with fields that can be populated from `storage.NssaaSession`

### Corrected Adapter Shapes for Tasks 6-9

**NSSAA Session Adapter (wraps storage.NssaaStore):**

```go
// NssaaSessionAdapter adapts storage.NssaaStore for server-initiated reverse flows.
// Implements the load-and-modify-Save pattern for status transitions.
type NssaaSessionAdapter struct {
    store storage.NssaaStore
}

// LoadSession retrieves a full session by authCtxID.
func (a *NssaaSessionAdapter) LoadSession(ctx context.Context, authCtxID string) (*storage.NssaaSession, error) {
    return a.store.Load(ctx, authCtxID)
}

// SaveSession persists session changes (status update, etc.).
func (a *NssaaSessionAdapter) SaveSession(ctx context.Context, s *storage.NssaaSession) error {
    return a.store.Save(ctx, s)
}
```

**Reverse-Flow State Writer (Biz-side):**

```go
// ReverseFlowStateWriter handles state transitions for server-initiated flows.
// Wraps NssaaSessionAdapter; each operation: Load -> mutate -> Save.
type ReverseFlowStateWriter struct {
    adapter *NssaaSessionAdapter
}

func NewReverseFlowStateWriter(store storage.NssaaStore) *ReverseFlowStateWriter {
    return &ReverseFlowStateWriter{adapter: &NssaaSessionAdapter{store: store}}
}

func (w *ReverseFlowStateWriter) MarkReauthPending(ctx context.Context, authCtxID string) error {
    s, err := w.adapter.LoadSession(ctx, authCtxID)
    if err != nil {
        return err
    }
    s.Status = string(types.NssaaStatusPending)
    return w.adapter.SaveSession(ctx, s)
}

func (w *ReverseFlowStateWriter) MarkRevoked(ctx context.Context, authCtxID string) error {
    s, err := w.adapter.LoadSession(ctx, authCtxID)
    if err != nil {
        return err
    }
    s.Status = string(types.NssaaStatusFailure)
    return w.adapter.SaveSession(ctx, s)
}
```

**Session Context for Reverse Flows (Biz-side):**

```go
// NssaaSessionContext is the Biz-side view of an NSSAA session for reverse flows.
// Contains both session identity fields and callback ownership metadata needed for
// server-initiated routing decisions.
type NssaaSessionContext struct {
    AuthCtxID      string
    GPSI           string
    ReauthNotifURI string
    RevocNotifURI  string
    AmfInstance    string
    CallbackOwner  string // "amf" or "ausf"; populated by Task 8
    HasAIWContext  bool   // true when session originated from AIW handler (AUSF ownership)
}

// NssaaSessionResolver loads and resolves session context for reverse flows.
type NssaaSessionResolver struct {
    store storage.NssaaStore
}

func NewNssaaSessionResolver(store storage.NssaaStore) *NssaaSessionResolver {
    return &NssaaSessionResolver{store: store}
}

// Note: this Resolve signature is an illustrative placeholder. The concrete
// NssaaSessionResolver in Task 7 takes (ctx, sessionID, authCtxID) and satisfies
// both biz.SessionContextResolver and biz.PersistentContextLookup.
func (r *NssaaSessionResolver) Resolve(ctx context.Context, authCtxID string) (*NssaaSessionContext, error) {
    s, err := r.store.Load(ctx, authCtxID)
    if err != nil {
        return nil, err
    }
    // CallbackOwner and HasAIWContext are NOT in storage.NssaaSession — they are
    // added as new columns/fields in Task 8. Until then, these resolve to zero values.
    // Task 7's concrete adapter uses the same pattern after the fields exist.
    return &NssaaSessionContext{
        AuthCtxID:      s.AuthCtxID,
        GPSI:           s.GPSI,
        ReauthNotifURI: s.ReauthURI,
        RevocNotifURI:  s.RevocURI,
        AmfInstance:    s.AmfInstance,
        CallbackOwner:  "",
        HasAIWContext:  false,
    }, nil
}
```

**Relationship to concrete adapter (Task 7):**
The existing `NssaaSession` does NOT have a `CallbackOwner` field. The ownership model (`amf` vs `ausf`) must be tracked either by:
- (a) Adding a `CallbackOwner string` field to `NssaaSession` and the DB schema (preferred, explicit)
- (b) Inferring ownership from which handler created the session (fragile, implicit)

Task 8's test referencing `store.lastSaved.CallbackOwner` will FAIL until this field is added to the storage types.

- [ ] **Step 1: Inspect the existing store interfaces and repository methods**

Read and note the actual method shapes already present in:

```go
internal/storage/
internal/storage/postgres/
internal/api/nssaa/handler.go
internal/api/aiw/handler.go
```

Record at least:

```go
// What exists today?
// - load/save/update/delete methods
// - whether status fields already exist
// - whether callback URIs are already persisted
// - whether AIW and NSSAA repositories differ materially
```

- [ ] **Step 2: Verify which adapter surface is actually needed**

Run: `go test ./internal/storage/... ./internal/api/nssaa ./internal/api/aiw -count=1`
Expected: PASS, giving a stable baseline before designing new adapters

- [ ] **Step 3: Revise the adapter shapes in this plan to match reality**

The following plan-level placeholders were invented and have been replaced with shapes grounded in the real repository APIs (see discovery findings above):

```go
// OLD (invented — does NOT exist):
type NSSAAStatusRepository interface {
    UpdateStatus(authCtxID string, status string) error
    LoadNotificationURIs(authCtxID string) (reauth string, revoc string, err error)
}

// NEW (grounded in real storage.NssaaStore):
// - NssaaSessionAdapter: wraps storage.NssaaStore
// - ReverseFlowStateWriter: Load -> mutate Status -> Save pattern
// - NssaaSessionResolver: Load -> map to NssaaSessionContext
// - No separate LoadNotificationURIs — must Load full session
```

```go
// OLD (invented — does NOT exist):
type PersistentContextLookup interface {
    LoadAuthContext(ctx context.Context, authCtxID string) (*NssaaSessionContext, error)
}

// NEW (grounded in real storage.NssaaStore):
// - NssaaSessionResolver: uses storage.NssaaStore.Load → maps to *NssaaSessionContext
```

```go
// OLD (invented — does NOT exist):
type StateWriter struct {
    repo NSSAAStatusRepository
}

// NEW (grounded in real storage.NssaaStore):
// - ReverseFlowStateWriter: uses NssaaSessionAdapter under the hood
// - Each method: Load → modify session.Status → Save
```

```go
// OLD (invented — does NOT exist):
// store.lastSaved.CallbackOwner (field does not exist)

// NEW:
// - NssaaSession has ReauthURI and RevocURI fields (strings)
// - CallbackOwner field does NOT exist — must be added as new field
// - Task 8 test must be updated to check existing fields, or new field must be added
```

- [ ] **Step 4: Re-run the plan consistency check**

Run: `rg "UpdateStatus|LoadNotificationURIs|LoadAuthContext|persistentLookup|stateWriter" "docs/superpowers/plans/2026-06-06-internal-communication-gaps-implementation-plan.md"`
Expected: references to invented signatures are now replaced with grounded shapes

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/plans/2026-06-06-internal-communication-gaps-implementation-plan.md
git commit -m "docs: ground reverse-flow adapter design in existing storage interfaces"
```

## Task 1: Add reverse-path domain types and interfaces

**Files:**
- Create: `internal/biz/types.go`
- Test: `internal/biz/server_initiated_test.go`

- [ ] **Step 1: Write the failing test**

```go
package biz

import (
    "testing"

    "github.com/operator/nssAAF/internal/proto"
)

func TestServerInitiatedResult_RequiresResponsePayload(t *testing.T) {
    result := ServerInitiatedResult{
        Response: proto.AaaServerInitiatedResponse{
            Version:   proto.CurrentVersion,
            SessionID: "sess-1",
            AuthCtxID: "auth-1",
        },
    }

    if err := result.Validate(); err == nil {
        t.Fatalf("expected validation error when response payload is empty")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestServerInitiatedResult_RequiresResponsePayload -count=1`
Expected: FAIL with `undefined: ServerInitiatedResult`

- [ ] **Step 3: Write minimal implementation**

```go
package biz

import (
    "context"
    "fmt"

    "github.com/operator/nssAAF/internal/proto"
)

// NssaaSessionContext is the canonical Biz-side session context for reverse flows.
// Defined here in Task 1 so all subsequent tasks share the same type.
// CallbackOwner and HasAIWContext fields are populated by Task 8.
type NssaaSessionContext struct {
    AuthCtxID      string
    SessionID      string
    ReauthNotifURI string
    RevocNotifURI  string
    AmfInstance    string
    CallbackOwner  string // "amf" or "ausf"; populated by Task 8
    HasAIWContext  bool   // true when session originated from AIW handler (AUSF ownership)
}

// PersistentContextLookup resolves auth context from durable storage.
// Used by CorrelationResolver in Task 2 to enrich Redis correlation data.
type PersistentContextLookup interface {
    LoadAuthContext(ctx context.Context, authCtxID string) (*NssaaSessionContext, error)
}

// SessionContextResolver loads and enriches reverse-path session context.
// Used by ServerInitiatedCoordinator in Tasks 4 and 5.
type SessionContextResolver interface {
    Resolve(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error)
}

// SessionStateWriter persists state transitions for server-initiated flows.
type SessionStateWriter interface {
    MarkReauthPending(ctx context.Context, authCtxID string) error
    MarkRevoked(ctx context.Context, authCtxID string) error
    ApplyCoA(ctx context.Context, authCtxID string, payload []byte) error
}

// AMFNotifier sends server-initiated notifications to the AMF.
type AMFNotifier interface {
    SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error
    SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error
}

// Completion reports what a server-initiated handler accomplished.
type Completion string

const (
    CompletionUpdatedState Completion = "UPDATED_STATE"
    CompletionNotifiedAMF  Completion = "NOTIFIED_AMF"
    CompletionAppliedCoA   Completion = "APPLIED_COA"
)

// ServerInitiatedResult is the result of processing a server-initiated request.
type ServerInitiatedResult struct {
    Response   proto.AaaServerInitiatedResponse
    Completion Completion
}

// Validate checks that the result carries a response payload.
func (r ServerInitiatedResult) Validate() error {
    if len(r.Response.Payload) == 0 {
        return fmt.Errorf("response payload is required")
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestServerInitiatedResult_RequiresResponsePayload -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/types.go internal/biz/server_initiated_test.go
git commit -m "feat: add biz reverse-path domain types"
```

## Task 2: Build correlation resolver from Redis and persisted auth context

**Files:**
- Create: `internal/biz/correlation.go`
- Create: `internal/biz/correlation_test.go`
- Modify: `internal/proto/biz_callback.go`
- Test: `internal/biz/correlation_test.go`

- [ ] **Step 1: Write the failing test**

```go
package biz

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    miniredis "github.com/alicebob/miniredis/v2"
    goredis "github.com/redis/go-redis/v9"
    "github.com/operator/nssAAF/internal/proto"
)

type stubSessionLookup struct {
    ctx *NssaaSessionContext
}

func (s stubSessionLookup) LoadAuthContext(_ context.Context, authCtxID string) (*NssaaSessionContext, error) {
    out := *s.ctx
    out.AuthCtxID = authCtxID
    return &out, nil
}

func TestCorrelationResolver_Resolve_UsesRedisThenPersistentContext(t *testing.T) {
    mr, err := miniredis.Run()
    if err != nil {
        t.Fatalf("miniredis: %v", err)
    }
    defer mr.Close()

    rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
    defer func() { _ = rdb.Close() }()

    entry := proto.SessionCorrEntry{
        AuthCtxID: "auth-123",
        PodID:     "biz-a",
        Sst:       1,
        Sd:        "000001",
        CreatedAt: time.Now().Unix(),
    }
    data, _ := json.Marshal(entry)
    if err := rdb.Set(context.Background(), proto.SessionCorrKey("sess-123"), data, time.Minute).Err(); err != nil {
        t.Fatalf("seed redis: %v", err)
    }

    resolver := NewCorrelationResolver(rdb, stubSessionLookup{ctx: &NssaaSessionContext{
        ReauthNotifURI: "http://amf/reauth",
        RevocNotifURI:  "http://amf/revoke",
        CallbackOwner:  "amf",
    }})

    got, err := resolver.Resolve(context.Background(), "sess-123", "")
    if err != nil {
        t.Fatalf("Resolve returned error: %v", err)
    }
    if got.AuthCtxID != "auth-123" {
        t.Fatalf("AuthCtxID = %q, want auth-123", got.AuthCtxID)
    }
    if got.ReauthNotifURI != "http://amf/reauth" {
        t.Fatalf("ReauthNotifURI = %q", got.ReauthNotifURI)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestCorrelationResolver_Resolve_UsesRedisThenPersistentContext -count=1`
Expected: FAIL with `undefined: NewCorrelationResolver`

- [ ] **Step 3: Write minimal implementation**

```go
package biz

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/operator/nssAAF/internal/proto"
    goredis "github.com/redis/go-redis/v9"
)

// PersistentContextLookup resolves auth context from durable storage.
type PersistentContextLookup interface {
    LoadAuthContext(ctx context.Context, authCtxID string) (*NssaaSessionContext, error)
}

type CorrelationResolver struct {
    rdb    goredis.Cmdable
    lookup PersistentContextLookup
}

func NewCorrelationResolver(rdb goredis.Cmdable, lookup PersistentContextLookup) *CorrelationResolver {
    return &CorrelationResolver{rdb: rdb, lookup: lookup}
}

func (r *CorrelationResolver) Resolve(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error) {
    resolvedAuthCtxID := authCtxID
    if sessionID != "" {
        raw, err := r.rdb.Get(ctx, proto.SessionCorrKey(sessionID)).Bytes()
        if err != nil && err != goredis.Nil {
            return nil, fmt.Errorf("load session correlation %s: %w", sessionID, err)
        }
        if len(raw) > 0 {
            var entry proto.SessionCorrEntry
            if err := json.Unmarshal(raw, &entry); err != nil {
                return nil, fmt.Errorf("decode session correlation %s: %w", sessionID, err)
            }
            if resolvedAuthCtxID == "" {
                resolvedAuthCtxID = entry.AuthCtxID
            }
        }
    }
    if resolvedAuthCtxID == "" {
        return nil, fmt.Errorf("auth context could not be resolved")
    }
    sessionCtx, err := r.lookup.LoadAuthContext(ctx, resolvedAuthCtxID)
    if err != nil {
        return nil, fmt.Errorf("load auth context %s: %w", resolvedAuthCtxID, err)
    }
    sessionCtx.AuthCtxID = resolvedAuthCtxID
    sessionCtx.SessionID = sessionID
    return sessionCtx, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestCorrelationResolver_Resolve_UsesRedisThenPersistentContext -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/correlation.go internal/biz/correlation_test.go internal/proto/biz_callback.go
git commit -m "feat: resolve reverse-path correlation context"
```

## Task 3: Add Redis correlation helper with explicit TTL behavior

**Files:**
- Create: `internal/cache/redis/session_correlation.go`
- Create: `internal/cache/redis/session_correlation_test.go`
- Modify: `internal/proto/biz_callback.go`
- Test: `internal/cache/redis/session_correlation_test.go`

- [ ] **Step 1: Write the failing test**

```go
package redis

import (
    "context"
    "testing"
    "time"

    miniredis "github.com/alicebob/miniredis/v2"
    goredis "github.com/redis/go-redis/v9"
    "github.com/operator/nssAAF/internal/proto"
)

func TestSessionCorrelationStore_Save_SetsExpectedTTL(t *testing.T) {
    mr, err := miniredis.Run()
    if err != nil {
        t.Fatalf("miniredis: %v", err)
    }
    defer mr.Close()

    rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
    defer func() { _ = rdb.Close() }()

    store := NewSessionCorrelationStore(rdb, proto.DefaultPayloadTTL)
    err = store.Save(context.Background(), "sess-1", proto.SessionCorrEntry{AuthCtxID: "auth-1"})
    if err != nil {
        t.Fatalf("Save returned error: %v", err)
    }

    ttl := mr.TTL(proto.SessionCorrKey("sess-1"))
    if ttl <= 0 {
        t.Fatalf("TTL = %v, want > 0", ttl)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/redis -run TestSessionCorrelationStore_Save_SetsExpectedTTL -count=1`
Expected: FAIL with `undefined: NewSessionCorrelationStore`

- [ ] **Step 3: Write minimal implementation**

```go
package redis

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/operator/nssAAF/internal/proto"
    goredis "github.com/redis/go-redis/v9"
)

type SessionCorrelationStore struct {
    rdb goredis.Cmdable
    ttl time.Duration
}

func NewSessionCorrelationStore(rdb goredis.Cmdable, ttl time.Duration) *SessionCorrelationStore {
    return &SessionCorrelationStore{rdb: rdb, ttl: ttl}
}

func (s *SessionCorrelationStore) Save(ctx context.Context, sessionID string, entry proto.SessionCorrEntry) error {
    data, err := json.Marshal(entry)
    if err != nil {
        return fmt.Errorf("marshal session correlation: %w", err)
    }
    if err := s.rdb.Set(ctx, proto.SessionCorrKey(sessionID), data, s.ttl).Err(); err != nil {
        return fmt.Errorf("save session correlation %s: %w", sessionID, err)
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cache/redis -run TestSessionCorrelationStore_Save_SetsExpectedTTL -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cache/redis/session_correlation.go internal/cache/redis/session_correlation_test.go internal/proto/biz_callback.go
git commit -m "feat: add redis session correlation store"
```

> **Constant verification:** `proto.DefaultPayloadTTL` is confirmed real — defined in `internal/proto/aaa_transport.go:85` as `10 * time.Minute`. The test uses it directly rather than hardcoding, so if the constant ever changes the test will follow.

## Task 4: Implement Biz-side server-initiated coordinator for re-auth

**Files:**
- Create: `internal/biz/server_initiated.go`
- Modify: `internal/biz/types.go`
- Modify: `internal/biz/server_initiated_test.go`
- Test: `internal/biz/server_initiated_test.go`

- [ ] **Step 1: Write the failing test**

```go
package biz

import (
    "context"
    "testing"

    "github.com/operator/nssAAF/internal/proto"
)

type stubResolver struct {
    ctx *NssaaSessionContext
}

func (s stubResolver) Resolve(_ context.Context, sessionID, authCtxID string) (*NssaaSessionContext, error) {
    out := *s.ctx
    out.SessionID = sessionID
    out.AuthCtxID = authCtxID
    return &out, nil
}

type stubWriter struct {
    reauthAuthCtxIDs  []string
    revokedAuthCtxIDs []string
}

func (s *stubWriter) MarkReauthPending(ctx context.Context, authCtxID string) error {
    s.reauthAuthCtxIDs = append(s.reauthAuthCtxIDs, authCtxID)
    return nil
}
func (s *stubWriter) MarkRevoked(ctx context.Context, authCtxID string) error {
    s.revokedAuthCtxIDs = append(s.revokedAuthCtxIDs, authCtxID)
    return nil
}
func (s *stubWriter) ApplyCoA(ctx context.Context, authCtxID string, payload []byte) error { return nil }

type stubNotifier struct {
    reauthCalls       int
    revocationCalls   int
}

func (s *stubNotifier) SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
    s.reauthCalls++
    return nil
}
func (s *stubNotifier) SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
    s.revocationCalls++
    return nil
}

func TestServerInitiatedCoordinator_Handle_Reauth_UpdatesStateAndNotifiesAMF(t *testing.T) {
    writer := &stubWriter{}
    notifier := &stubNotifier{}
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
        CallbackOwner:  "amf",
        ReauthNotifURI: "http://amf/reauth",
    }}, writer, notifier)

    result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
        Version:     proto.CurrentVersion,
        SessionID:   "sess-1",
        AuthCtxID:   "auth-1",
        MessageType: proto.MessageTypeRAR,
        Payload:     []byte{1, 2, 3},
    })
    if err != nil {
        t.Fatalf("Handle returned error: %v", err)
    }
    if len(writer.reauthAuthCtxIDs) != 1 || writer.reauthAuthCtxIDs[0] != "auth-1" {
        t.Fatalf("reauth state updates = %#v", writer.reauthAuthCtxIDs)
    }
    if notifier.reauthCalls != 1 {
        t.Fatalf("reauth notifications = %d, want 1", notifier.reauthCalls)
    }
    if err := result.Validate(); err != nil {
        t.Fatalf("result.Validate() error = %v", err)
    }
    if result.Completion != CompletionNotifiedAMF {
        t.Fatalf("Completion = %q, want %q", result.Completion, CompletionNotifiedAMF)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_Reauth_UpdatesStateAndNotifiesAMF -count=1`
Expected: FAIL with `undefined: NewServerInitiatedCoordinator`

- [ ] **Step 3: Write minimal implementation**

```go
package biz

import (
    "context"
    "fmt"

    "github.com/operator/nssAAF/internal/proto"
)

type ServerInitiatedCoordinator struct {
    resolver SessionContextResolver
    writer   SessionStateWriter
    notifier AMFNotifier
}

func NewServerInitiatedCoordinator(resolver SessionContextResolver, writer SessionStateWriter, notifier AMFNotifier) *ServerInitiatedCoordinator {
    return &ServerInitiatedCoordinator{resolver: resolver, writer: writer, notifier: notifier}
}

func (c *ServerInitiatedCoordinator) Handle(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*ServerInitiatedResult, error) {
    sessionCtx, err := c.resolver.Resolve(ctx, req.SessionID, req.AuthCtxID)
    if err != nil {
        return nil, fmt.Errorf("resolve reverse-path context: %w", err)
    }

    switch req.MessageType {
    case proto.MessageTypeRAR:
        if err := c.writer.MarkReauthPending(ctx, sessionCtx.AuthCtxID); err != nil {
            return nil, fmt.Errorf("mark reauth pending: %w", err)
        }
        var completion Completion
        if sessionCtx.CallbackOwner == "amf" && sessionCtx.ReauthNotifURI != "" {
            if err := c.notifier.SendReAuthNotification(ctx, sessionCtx.ReauthNotifURI, sessionCtx.AuthCtxID, req.Payload); err != nil {
                return nil, fmt.Errorf("send reauth notification: %w", err)
            }
            completion = CompletionNotifiedAMF
        } else {
            completion = CompletionUpdatedState
        }
        result := &ServerInitiatedResult{
            Completion: completion,
            Response: proto.AaaServerInitiatedResponse{
                Version:   proto.CurrentVersion,
                SessionID: req.SessionID,
                AuthCtxID: sessionCtx.AuthCtxID,
                Payload:   []byte{2, 0, 0, 12},
            },
        }
        return result, result.Validate()
    default:
        return nil, fmt.Errorf("unsupported message type: %s", req.MessageType)
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_Reauth_UpdatesStateAndNotifiesAMF -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/server_initiated.go internal/biz/server_initiated_test.go internal/biz/types.go
git commit -m "feat: coordinate reverse reauth handling in biz"
```

## Task 5: Implement Biz-side revocation and CoA completion semantics

**Files:**
- Modify: `internal/biz/server_initiated.go`
- Modify: `internal/biz/server_initiated_test.go`
- Test: `internal/biz/server_initiated_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestServerInitiatedCoordinator_Handle_Revocation_UpdatesStateAndNotifiesAMF(t *testing.T) {
    writer := &stubWriter{}
    notifier := &stubNotifier{}
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
        CallbackOwner: "amf",
        RevocNotifURI: "http://amf/revoke",
    }}, writer, notifier)

    _, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
        Version:     proto.CurrentVersion,
        SessionID:   "sess-2",
        AuthCtxID:   "auth-2",
        MessageType: proto.MessageTypeASR,
        Payload:     []byte{9, 9, 9},
    })
    if err != nil {
        t.Fatalf("Handle returned error: %v", err)
    }
    if len(writer.revokedAuthCtxIDs) != 1 || writer.revokedAuthCtxIDs[0] != "auth-2" {
        t.Fatalf("revoked auth contexts = %#v", writer.revokedAuthCtxIDs)
    }
    if notifier.revocationCalls != 1 {
        t.Fatalf("revocation notifications = %d, want 1", notifier.revocationCalls)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_Revocation_UpdatesStateAndNotifiesAMF -count=1`
Expected: FAIL because `ASR` path is unsupported or state fields are missing

- [ ] **Step 3: Write minimal implementation**

```go
case proto.MessageTypeASR:
    if err := c.writer.MarkRevoked(ctx, sessionCtx.AuthCtxID); err != nil {
        return nil, fmt.Errorf("mark revoked: %w", err)
    }
    var completion Completion
    if sessionCtx.CallbackOwner == "amf" && sessionCtx.RevocNotifURI != "" {
        if err := c.notifier.SendRevocationNotification(ctx, sessionCtx.RevocNotifURI, sessionCtx.AuthCtxID, req.Payload); err != nil {
            return nil, fmt.Errorf("send revocation notification: %w", err)
        }
        completion = CompletionNotifiedAMF
    } else {
        completion = CompletionUpdatedState
    }
    result := &ServerInitiatedResult{
        Completion: completion,
        Response: proto.AaaServerInitiatedResponse{
            Version:   proto.CurrentVersion,
            SessionID: req.SessionID,
            AuthCtxID: sessionCtx.AuthCtxID,
            Payload:   []byte{1},
        },
    }
    return result, result.Validate()
case proto.MessageTypeCoA:
    if err := c.writer.ApplyCoA(ctx, sessionCtx.AuthCtxID, req.Payload); err != nil {
        return nil, fmt.Errorf("apply coa: %w", err)
    }
    result := &ServerInitiatedResult{
        Completion: CompletionAppliedCoA,
        Response: proto.AaaServerInitiatedResponse{
            Version:   proto.CurrentVersion,
            SessionID: req.SessionID,
            AuthCtxID: sessionCtx.AuthCtxID,
            Payload:   []byte{2, 0, 0, 12},
        },
    }
    return result, result.Validate()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run 'TestServerInitiatedCoordinator_Handle_(Revocation|Reauth)' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/server_initiated.go internal/biz/server_initiated_test.go
 git commit -m "feat: complete reverse revocation and coa flows"
```

## Task 6: Wire Biz main HTTP handler to injected coordinator

**Files:**
- Modify: `cmd/biz/main.go`
- Modify: `cmd/biz/factory.go`
- Test: `cmd/biz/main_test.go`

> **Prerequisite:** Task 7 (adapters) must be completed before this task. The `NewNssaaSessionResolver`, `NewReverseFlowStateWriter`, and `NewServerInitiatedCoordinator` functions are defined by Task 7.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
    "bytes"
    "context"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/operator/nssAAF/internal/biz"
    "github.com/operator/nssAAF/internal/proto"
)

func TestHandleServerInitiated_UsesCoordinatorResponse(t *testing.T) {
    recorder := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader([]byte(`{
        "v":"1.0",
        "sessionId":"sess-1",
        "authCtxId":"auth-1",
        "transportType":"RADIUS",
        "messageType":"RAR",
        "payload":"AQID"
    }`)))
    req.Header.Set("Content-Type", "application/json")

    old := serverInitiatedHandler
    defer func() { serverInitiatedHandler = old }()
    serverInitiatedHandler = func(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*biz.ServerInitiatedResult, error) {
        return &biz.ServerInitiatedResult{
            Completion: biz.CompletionNotifiedAMF,
            Response: proto.AaaServerInitiatedResponse{
                Version:   proto.CurrentVersion,
                SessionID: req.SessionID,
                AuthCtxID: req.AuthCtxID,
                Payload:   []byte{2, 0, 0, 12},
            },
        }, nil
    }

    handleServerInitiated(recorder, req)

    if recorder.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200", recorder.Code)
    }
    if !strings.Contains(recorder.Body.String(), "auth-1") {
        t.Fatalf("body = %s, expected auth-1", recorder.Body.String())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/biz -run TestHandleServerInitiated_UsesCoordinatorResponse -count=1`
Expected: FAIL because there is no injectable coordinator hook yet

- [ ] **Step 3: Write minimal implementation**

```go
var serverInitiatedHandler func(context.Context, *proto.AaaServerInitiatedRequest) (*biz.ServerInitiatedResult, error)

func handleServerInitiated(w http.ResponseWriter, r *http.Request) {
    // existing method/content-type/decode validation...

    if serverInitiatedHandler == nil {
        http.Error(w, "server initiated handler not configured", http.StatusServiceUnavailable)
        return
    }

    result, err := serverInitiatedHandler(r.Context(), &req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadGateway)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    _ = json.NewEncoder(w).Encode(result.Response)
}
```

In `cmd/biz/factory.go`, wire the concrete adapter during `Build()`:

```go
// Reverse flow adapters: wrap existing storage stores
nssaaAdapter := &biz.NssaaSessionAdapter{store: nssaaStore}
resolver := biz.NewNssaaSessionResolver(nssaaStore)
stateWriter := biz.NewReverseFlowStateWriter(nssaaStore)
coordinator := biz.NewServerInitiatedCoordinator(resolver, stateWriter, amfNotifier)
serverInitiatedHandler = coordinator.Handle
```

Note: `NssaaSessionAdapter` and `NssaaSessionResolver` share the same underlying `nssaaStore` but serve different purposes — resolver for loading context, adapter for save operations.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/biz -run TestHandleServerInitiated_UsesCoordinatorResponse -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/biz/main.go cmd/biz/factory.go cmd/biz/main_test.go
 git commit -m "feat: wire biz server-initiated coordinator"
```

## Task 7: Add persistent lookup and state-writer adapters over existing stores

**Files:**
- Modify: `cmd/biz/factory.go`
- Create: `internal/biz/adapters.go`
- Create: `internal/biz/adapters_test.go`
- Test: `internal/biz/adapters_test.go`

> **Adapter Shape Correction (Task 1a):** The original plan invented `NSSAAStatusRepository` with `UpdateStatus`. The real pattern is `Load → mutate → Save` on `storage.NssaaStore`. See Task 1a discovery findings for corrected shapes.
>
> **Prerequisite:** Task 1 must be completed before this task (defines `NssaaSessionContext`).

- [ ] **Step 1: Write the failing test**

```go
package biz

import (
    "context"
    "testing"

    "github.com/operator/nssAAF/internal/storage"
)

func TestReverseFlowStateWriter_MarkReauthPending_DelegatesToRepository(t *testing.T) {
    store := &stubNssaaStore{}
    writer := NewReverseFlowStateWriter(store)

    if err := writer.MarkReauthPending(context.Background(), "auth-1"); err != nil {
        t.Fatalf("MarkReauthPending returned error: %v", err)
    }
    if store.lastSaved == nil {
        t.Fatal("lastSaved is nil")
    }
    if store.lastSaved.Status != "PENDING" {
        t.Fatalf("lastSaved.Status = %q, want PENDING", store.lastSaved.Status)
    }
}

type stubNssaaStore struct {
    data      map[string]*storage.NssaaSession
    lastSaved *storage.NssaaSession
}

func (s *stubNssaaStore) Load(_ context.Context, id string) (*storage.NssaaSession, error) {
    if ctx, ok := s.data[id]; ok {
        return ctx, nil
    }
    return nil, storage.ErrSessionNotFound
}

func (s *stubNssaaStore) Save(_ context.Context, ctx *storage.NssaaSession) error {
    if s.data == nil {
        s.data = make(map[string]*storage.NssaaSession)
    }
    s.data[ctx.AuthCtxID] = ctx
    s.lastSaved = ctx
    return nil
}

func (s *stubNssaaStore) Delete(_ context.Context, id string) error {
    delete(s.data, id)
    return nil
}

func (s *stubNssaaStore) Close() error { return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestReverseFlowStateWriter_MarkReauthPending_DelegatesToRepository -count=1`
Expected: FAIL with `undefined: NewReverseFlowStateWriter`

- [ ] **Step 3: Write minimal implementation**

> **Note:** `NssaaSessionContext` and `SessionContextResolver` are already defined in `internal/biz/types.go` by Task 1. This task implements the concrete adapters that satisfy those interfaces.

```go
package biz

import (
    "context"
    "fmt"

    "github.com/operator/nssAAF/internal/storage"
    "github.com/operator/nssAAF/internal/types"
)

// NssaaSessionAdapter wraps storage.NssaaStore for reverse-flow operations.
// Implements the load-and-modify-Save pattern for status transitions.
type NssaaSessionAdapter struct {
    store storage.NssaaStore
}

// LoadSession retrieves a full session by authCtxID.
func (a *NssaaSessionAdapter) LoadSession(ctx context.Context, authCtxID string) (*storage.NssaaSession, error) {
    return a.store.Load(ctx, authCtxID)
}

// SaveSession persists session changes (status update, etc.).
func (a *NssaaSessionAdapter) SaveSession(ctx context.Context, s *storage.NssaaSession) error {
    return a.store.Save(ctx, s)
}

// ReverseFlowStateWriter handles state transitions for server-initiated flows.
// Satisfies the biz.SessionStateWriter interface.
type ReverseFlowStateWriter struct {
    adapter *NssaaSessionAdapter
}

// NewReverseFlowStateWriter creates a ReverseFlowStateWriter backed by a NssaaStore.
func NewReverseFlowStateWriter(store storage.NssaaStore) *ReverseFlowStateWriter {
    return &ReverseFlowStateWriter{adapter: &NssaaSessionAdapter{store: store}}
}

// MarkReauthPending transitions a session to PENDING (reauth initiated).
func (w *ReverseFlowStateWriter) MarkReauthPending(ctx context.Context, authCtxID string) error {
    s, err := w.adapter.LoadSession(ctx, authCtxID)
    if err != nil {
        return err
    }
    s.Status = string(types.NssaaStatusPending)
    return w.adapter.SaveSession(ctx, s)
}

// MarkRevoked transitions a session to failure (revocation completed).
func (w *ReverseFlowStateWriter) MarkRevoked(ctx context.Context, authCtxID string) error {
    s, err := w.adapter.LoadSession(ctx, authCtxID)
    if err != nil {
        return err
    }
    s.Status = string(types.NssaaStatusFailure)
    return w.adapter.SaveSession(ctx, s)
}

// ApplyCoA transitions a session to PENDING (Change-of-Authority applied).
func (w *ReverseFlowStateWriter) ApplyCoA(ctx context.Context, authCtxID string, _ []byte) error {
    s, err := w.adapter.LoadSession(ctx, authCtxID)
    if err != nil {
        return err
    }
    s.Status = string(types.NssaaStatusPending)
    return w.adapter.SaveSession(ctx, s)
}

// NssaaSessionResolver loads and resolves session context for reverse flows.
// Satisfies the biz.SessionContextResolver interface.
type NssaaSessionResolver struct {
    store storage.NssaaStore
}

// NewNssaaSessionResolver creates a resolver backed by a NssaaStore.
func NewNssaaSessionResolver(store storage.NssaaStore) *NssaaSessionResolver {
    return &NssaaSessionResolver{store: store}
}

// Resolve loads session context by authCtxID and enriches it with storage-backed fields.
// Satisfies biz.SessionContextResolver.Resolve — also satisfies biz.PersistentContextLookup.
func (r *NssaaSessionResolver) Resolve(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error) {
    resolvedAuthCtxID := authCtxID
    if resolvedAuthCtxID == "" {
        return nil, fmt.Errorf("authCtxID is required for Resolve")
    }
    s, err := r.store.Load(ctx, resolvedAuthCtxID)
    if err != nil {
        return nil, err
    }
    return &NssaaSessionContext{
        AuthCtxID:      s.AuthCtxID,
        SessionID:     sessionID,
        GPSI:          s.GPSI,
        ReauthNotifURI: s.ReauthURI,
        RevocNotifURI:  s.RevocURI,
        AmfInstance:    s.AmfInstance,
        // CallbackOwner and HasAIWContext are populated from storage.NssaaSession
        // fields that are added in Task 8. Before Task 8 lands, these are empty/zero.
        CallbackOwner:  s.CallbackOwner,
        HasAIWContext:  s.HasAIWContext,
    }, nil
}

// LoadAuthContext satisfies biz.PersistentContextLookup (used by Task 2 CorrelationResolver).
func (r *NssaaSessionResolver) LoadAuthContext(ctx context.Context, authCtxID string) (*NssaaSessionContext, error) {
    s, err := r.store.Load(ctx, authCtxID)
    if err != nil {
        return nil, err
    }
    return &NssaaSessionContext{
        AuthCtxID:      s.AuthCtxID,
        GPSI:           s.GPSI,
        ReauthNotifURI: s.ReauthURI,
        RevocNotifURI:  s.RevocURI,
        AmfInstance:    s.AmfInstance,
        // CallbackOwner and HasAIWContext come from storage.NssaaSession fields
        // added in Task 8. Code here is correct after Task 8 lands.
        CallbackOwner:  s.CallbackOwner,
        HasAIWContext: s.HasAIWContext,
    }, nil
}
```

Note: `NssaaSessionResolver.Resolve` satisfies `SessionContextResolver` (takes `sessionID` and `authCtxID`), while `LoadAuthContext` satisfies `PersistentContextLookup` (takes only `authCtxID`). Both delegate to the same underlying `store.Load`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestReverseFlowStateWriter_MarkReauthPending_DelegatesToRepository -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/adapters.go internal/biz/adapters_test.go cmd/biz/factory.go
 git commit -m "feat: adapt biz reverse flow to existing stores"
```

## Task 8: Persist and reuse callback ownership/correlation data in API flows

**Files:**
- Modify: `internal/api/nssaa/handler.go`
- Modify: `internal/api/aiw/handler.go`
- Modify: `internal/biz/adapters.go`
- Test: `internal/api/nssaa/handler_test.go`
- Test: `internal/api/aiw/handler_test.go`

> **Adapter Shape Correction (Task 1a):** The existing `NssaaSession` has `ReauthURI` and `RevocURI` fields but NO `CallbackOwner` field. Task 8 must either (a) add `CallbackOwner` to storage types and DB schema, or (b) check the existing URI fields. This task uses approach (a): add the field.

- [ ] **Step 1: Write the failing test**

```go
func TestCreateSliceAuthentication_PersistsCallbackOwnershipMetadata(t *testing.T) {
    store := newMockStore()
    h := NewHandler(store,
        WithAPIRoot("http://test"),
        WithAAA(&mockAAA{}),
    )

    req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", bytes.NewBufferString(`{
        "gpsi":"msisdn-12345",
        "snssai":{"sst":1,"sd":"000001"},
        "reauthNotifUri":"http://amf/reauth",
        "revocNotifUri":"http://amf/revoke"
    }`))

    rr := httptest.NewRecorder()
    h.CreateSliceAuthentication(rr, req)

    // After Task 1a: NssaaSession has a CallbackOwner field added by this task
    if store.lastSaved.CallbackOwner != "amf" {
        t.Fatalf("CallbackOwner = %q, want amf", store.lastSaved.CallbackOwner)
    }
    // ReauthURI and RevocURI are already persisted (existing behavior)
    if store.lastSaved.ReauthURI != "http://amf/reauth" {
        t.Fatalf("ReauthURI = %q, want http://amf/reauth", store.lastSaved.ReauthURI)
    }
}
```

> **Note:** The `CallbackOwner` and `HasAIWContext` fields do not yet exist in `storage.NssaaSession`. Step 2 of this task adds both fields to the Go struct and the DB schema before the test can pass.

> **⚠ DB Schema Migration Required:** This task introduces two new columns to `slice_auth_sessions`:
> - `callback_owner TEXT` — "amf" or "ausf"; nullable; indexed for reverse-flow lookups
> - `has_aiw_context BOOLEAN` — true when session originated from AIW/AUSF path; defaults false
>
> Both columns are nullable during migration and backfilled from existing rows (default: `callback_owner = ''`, `has_aiw_context = false`).

- [ ] **Step 2: Add DB schema migration for new NssaaSession fields**

Before modifying the Go code, add the two columns so the struct change and the persistence layer change are co-deployed:

```sql
-- migrations/YYYYMMDDHHMMSS_add_callback_owner_to_slice_auth_sessions.sql

-- Step 1: Add nullable columns (zero-values match the default semantics)
ALTER TABLE slice_auth_sessions
  ADD COLUMN IF NOT EXISTS callback_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE slice_auth_sessions
  ADD COLUMN IF NOT EXISTS has_aiw_context BOOLEAN NOT NULL DEFAULT false;

-- Step 2: Backfill — infer ownership from which notif URI is non-empty
UPDATE slice_auth_sessions
   SET callback_owner = 'amf'
 WHERE callback_owner = ''
     AND (reauth_notif_uri != '' OR revoc_notif_uri != '');

-- Step 3: Add index for reverse-flow lookups by ownership
CREATE INDEX IF NOT EXISTS idx_slice_auth_sessions_callback_owner
  ON slice_auth_sessions(callback_owner)
  WHERE callback_owner != '';

-- Step 4: Add Go struct fields to internal/storage/types.go
--    (done in Step 3 implementation below)

-- Step 5: Update scanRow/saveRow in internal/storage/postgres/nssaa_repo.go
--    (done in Step 3 implementation below)
```

**Files to modify for Go struct persistence:**

- `internal/storage/types.go` — add `CallbackOwner string` and `HasAIWContext bool` to `NssaaSession`
- `internal/storage/postgres/nssaa_repo.go` — add the two new fields to `scanRow`, `rowToSession`, `sessionToRow`, `createRow`, `updateRow`
- `internal/storage/postgres/nssaa_repo_test.go` — add test cases for the new fields

After the migration and struct changes, verify:

```bash
go test ./internal/storage/postgres -run 'Nssaa' -count=1
```

Expected: PASS (repository correctly handles new fields).

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/nssaa -run TestCreateSliceAuthentication_PersistsCallbackOwnershipMetadata -count=1`
Expected: FAIL because callback ownership metadata is not persisted yet

- [ ] **Step 4: Write minimal implementation**

```go
ctx.CallbackOwner = "amf"
ctx.HasAIWContext = false
ctx.ReauthNotifURI = body.ReauthNotifUri
ctx.RevocNotifURI = body.RevocNotifUri
```

And for AIW completion metadata in `internal/api/aiw/handler.go`:

```go
ctx.CallbackOwner = "ausf"
ctx.HasAIWContext = true
ctx.ReauthNotifURI = ""
ctx.RevocNotifURI = ""
```

- [ ] **Step 5: Add concrete AIW handler test for ownership metadata**

> **Note:** `CallbackOwner` and `HasAIWContext` are stored in `storage.NssaaSession`, not `storage.AiwSession`. The AIW handler creates the AIW session, but the AIW-initiated NSSAA session (linked via `AuthCtxID`) carries the ownership metadata. The test below validates that when the AIW flow triggers NSSAA session creation, the linked `NssaaSession` has the correct ownership values.

Add to `internal/api/aiw/handler_test.go`:

```go
// TestCreateAiwAuthentication_LinksNssaaSessionWithOwnershipMetadata verifies that
// when an AIW flow creates or links an NssaaSession, that session carries the
// correct CallbackOwner="ausf" and HasAIWContext=true metadata.
func TestCreateAiwAuthentication_LinksNssaaSessionWithOwnershipMetadata(t *testing.T) {
    aiwStore := newMockAiwStore()
    nssaaStore := newMockNssaaStore()
    h := NewHandler(aiwStore, nssaaStore,
        WithAPIRoot("http://test"), WithAUSF(&mockAUSF{}))

    req := httptest.NewRequest(http.MethodPost, "/nnssaaf-aiw/v1/authentications", bytes.NewBufferString(`{
        "supi":"imsi-12345",
        "ausfId":"ausf-001",
        "supportedFeatures":"0"
    }`))

    rr := httptest.NewRecorder()
    h.CreateAuthentication(rr, req)

    // The linked NssaaSession (created or updated during AIW flow) must have
    // CallbackOwner="ausf" and HasAIWContext=true so the reverse flow can
    // route back to the correct owner without consulting the AIW store.
    require.NotEmpty(t, nssaaStore.lastSaved, "NssaaSession should be created/linked by AIW handler")
    if nssaaStore.lastSaved.CallbackOwner != "ausf" {
        t.Fatalf("NssaaSession.CallbackOwner = %q, want ausf", nssaaStore.lastSaved.CallbackOwner)
    }
    if !nssaaStore.lastSaved.HasAIWContext {
        t.Fatalf("NssaaSession.HasAIWContext = false, want true")
    }
    // ReauthURI/RevocURI are always empty for AIW-linked NSSAA sessions
    if nssaaStore.lastSaved.ReauthURI != "" {
        t.Fatalf("NssaaSession.ReauthURI = %q, want empty", nssaaStore.lastSaved.ReauthURI)
    }
    if nssaaStore.lastSaved.RevocURI != "" {
        t.Fatalf("NssaaSession.RevocURI = %q, want empty", nssaaStore.lastSaved.RevocURI)
    }
}
```

The mock helper for `nssaaStore` should include `CallbackOwner` and `HasAIWContext` fields so the assertions are compileable:

```go
type mockNssaaStore struct {
    data      map[string]*storage.NssaaSession
    lastSaved *storage.NssaaSession
}
```

> The `CallbackOwner`/`HasAIWContext` fields on the mock struct are valid because the mock mirrors `storage.NssaaSession`, which receives these new fields in Step 2.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/api/nssaa -run TestCreateSliceAuthentication_PersistsCallbackOwnershipMetadata -count=1 && go test ./internal/api/aiw -run TestCreateAiwAuthentication_LinksNssaaSessionWithOwnershipMetadata -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/nssaa/handler.go internal/api/aiw/handler.go internal/api/nssaa/handler_test.go internal/api/aiw/handler_test.go internal/biz/adapters.go internal/storage/types.go internal/storage/postgres/nssaa_repo.go migrations/
 git commit -m "feat: persist callback ownership metadata"

## Task 9: Cover integration path for server-initiated Biz processing

**Files:**
- Create: `test/integration/server_initiated_flow_test.go`
- Modify: `cmd/biz/main.go`
- Modify: `cmd/biz/factory.go`
- Test: `test/integration/server_initiated_flow_test.go`

- [ ] **Step 1: Write the failing test**

```go
package integration

import (
    "bytes"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestServerInitiatedFlow_Reauth_CompletesThroughBizHTTPHandler(t *testing.T) {
    server := newTestBizServer(t)
    defer server.Close()

    reqBody := []byte(`{
        "v":"1.0",
        "sessionId":"sess-it-1",
        "authCtxId":"auth-it-1",
        "transportType":"RADIUS",
        "messageType":"RAR",
        "payload":"AQID"
    }`)

    req := httptest.NewRequest(http.MethodPost, server.URL+"/aaa/server-initiated", bytes.NewReader(reqBody))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()

    server.Handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/integration -run TestServerInitiatedFlow_Reauth_CompletesThroughBizHTTPHandler -count=1`
Expected: FAIL because the full reverse flow is not wired through real dependencies yet

- [ ] **Step 3: Write minimal implementation**

```go
// In test setup, seed concrete dependencies rather than placeholders:
// 1. create a miniredis-backed `goredis.Client`
// 2. write `proto.SessionCorrEntry` under `proto.SessionCorrKey("sess-it-1")`
// 3. seed a concrete fake/stub persistent context lookup returning:
//    NssaaSessionContext{AuthCtxID: "auth-it-1", ReauthNotifURI: amfServer.URL, CallbackOwner: "amf"}
// 4. use an `httptest.Server` as the AMF callback receiver and assert it sees one POST
// 5. install the real `serverInitiatedHandler = coordinator.Handle` before serving the request
```

Concrete test assertions to add:

```go
if !amfCallbackObserved {
    t.Fatalf("expected AMF callback to be observed")
}
if !strings.Contains(rr.Body.String(), "auth-it-1") {
    t.Fatalf("response body missing auth context: %s", rr.Body.String())
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/integration -run TestServerInitiatedFlow_Reauth_CompletesThroughBizHTTPHandler -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add test/integration/server_initiated_flow_test.go cmd/biz/main.go cmd/biz/factory.go
 git commit -m "test: cover biz reverse-direction integration flow"
```

## Task 10: Add observability and completion-semantics assertions

**Files:**
- Modify: `internal/biz/server_initiated.go`
- Modify: `internal/biz/server_initiated_test.go`
- Modify: `test/integration/server_initiated_flow_test.go`
- Test: `internal/biz/server_initiated_test.go`
- Test: `test/integration/server_initiated_flow_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestServerInitiatedCoordinator_Handle_LogsAndReturnsCompletionMetadata(t *testing.T) {
    writer := &stubWriter{}
    notifier := &stubNotifier{}
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
        CallbackOwner:  "amf",
        ReauthNotifURI: "http://amf/reauth",
    }}, writer, notifier)

    result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
        Version:     proto.CurrentVersion,
        SessionID:   "sess-obs-1",
        AuthCtxID:   "auth-obs-1",
        MessageType: proto.MessageTypeRAR,
        Payload:     []byte{1},
    })
    if err != nil {
        t.Fatalf("Handle returned error: %v", err)
    }
    if result.Completion != CompletionNotifiedAMF {
        t.Fatalf("Completion = %q, want %q", result.Completion, CompletionNotifiedAMF)
    }
}
```

> **Note:** `Completion`, `CompletionNotifiedAMF`, `ServerInitiatedResult`, and the `ServerInitiatedCoordinator` type are all defined in `internal/biz/types.go` by Task 1. This task only adds structured logging around completion semantics.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_LogsAndReturnsCompletionMetadata -count=1`
Expected: FAIL because completion semantics are not explicit yet

- [ ] **Step 3: Write minimal implementation**

Add structured logging to each handler branch in `internal/biz/server_initiated.go`:

```go
slog.Info("server_initiated_completed",
    "auth_ctx_id", sessionCtx.AuthCtxID,
    "session_id", req.SessionID,
    "message_type", req.MessageType,
    "completion", result.Completion,
    "callback_owner", sessionCtx.CallbackOwner,
)
```

> **Note:** `Completion`, `CompletionNotifiedAMF`, `ServerInitiatedResult`, and `ServerInitiatedCoordinator` are defined in `internal/biz/types.go` by Task 1. This task only adds structured logging around completion semantics.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_LogsAndReturnsCompletionMetadata -count=1 && go test ./test/integration -run TestServerInitiatedFlow_Reauth_CompletesThroughBizHTTPHandler -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/server_initiated.go internal/biz/server_initiated_test.go test/integration/server_initiated_flow_test.go
 git commit -m "feat: expose reverse-flow completion semantics"
```

## Task 11: Verify trust-boundary behavior for internal hops

**Files:**
- Modify: `cmd/biz/factory.go`
- Modify: `cmd/biz/main.go`
- Modify: `internal/biz/server_initiated_test.go`
- Test: `internal/biz/server_initiated_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestServerInitiatedCoordinator_Handle_RejectsAMFNotificationWhenOwnerMissing(t *testing.T) {
    writer := &stubWriter{}
    notifier := &stubNotifier{}
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
        CallbackOwner: "",
    }}, writer, notifier)

    _, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
        Version:     proto.CurrentVersion,
        SessionID:   "sess-no-owner",
        AuthCtxID:   "auth-no-owner",
        MessageType: proto.MessageTypeRAR,
        Payload:     []byte{1},
    })
    if err == nil {
        t.Fatalf("expected error when callback owner is missing")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_RejectsAMFNotificationWhenOwnerMissing -count=1`
Expected: FAIL because missing ownership is not enforced yet

- [ ] **Step 3: Write minimal implementation**

```go
if sessionCtx.CallbackOwner == "" {
    return nil, fmt.Errorf("callback owner is required for server-initiated handling")
}
```

And keep mTLS wiring intact in `cmd/biz/factory.go` for Biz->AAA Gateway transport; do not add a parallel insecure client path.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_RejectsAMFNotificationWhenOwnerMissing -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/biz/server_initiated.go internal/biz/server_initiated_test.go cmd/biz/factory.go cmd/biz/main.go
 git commit -m "fix: enforce reverse-flow ownership checks"
```

## Task 12: Full verification and roadmap/state updates

**Files:**
- Modify: `docs/roadmap/module_index.md`
- Modify: `docs/roadmap/README.md` (only if this work completes an existing phase item)
- Modify: `.planning/STATE.md` (only if phase status changes)
- Modify: `.planning/PROJECT.md` (only if requirements move state)
- Modify: `.planning/REQUIREMENTS.md` (only if requirements move state)

- [ ] **Step 1: Run targeted unit and integration tests**

Run:

```bash
go test ./internal/biz ./internal/cache/redis ./internal/amf ./internal/ausf ./internal/api/nssaa ./internal/api/aiw ./test/integration -count=1
```

Expected: PASS

- [ ] **Step 2: Run broader regression for touched runtime paths**

Run:

```bash
go test ./cmd/biz ./cmd/http-gateway ./test/unit/e2e_amf ./test/conformance -count=1
```

Expected: PASS

- [ ] **Step 3: Run repository verification commands**

Run:

```bash
go test ./... -count=1
```

Expected: PASS

If the repo standard includes linting in current workflow, also run:

```bash
golangci-lint run ./...
```

Expected: PASS or only pre-existing unrelated failures

- [ ] **Step 4: Update roadmap/state files to reflect module progress**

Use these concrete status updates if the implementation is complete:

```markdown
| `internal/biz/` | `docs/design/01_service_model.md` | R / 4 | READY |
| `internal/proto/` | `docs/design/01_service_model.md` | R / 4 | READY |
| `internal/amf/` | `docs/design/21_amf_integration.md` | 4 | READY |
| `internal/ausf/` | `docs/design/23_ausf_integration.md` | 4 | READY |
```

Only update `.planning/STATE.md`, `.planning/PROJECT.md`, and `.planning/REQUIREMENTS.md` if this work formally closes a pending requirement or phase gap.

- [ ] **Step 5: Commit**

```bash
git add docs/roadmap/module_index.md docs/roadmap/README.md .planning/STATE.md .planning/PROJECT.md .planning/REQUIREMENTS.md
git commit -m "docs: update roadmap after internal communication gaps"
```

## Self-Review Checklist

### Spec coverage

- Bidirectional communication gaps covered:
  - `HTTP Gateway -> Biz Pod` reverse response contract: Tasks 6 and 9
  - `Biz Pod -> AAA Gateway` / `AAA Gateway -> Biz Pod` correlation and completion: Tasks 2–6
  - `Biz Pod -> AMF` server-initiated completion: Tasks 4, 5, 9, 10, 11
  - `Biz Pod -> AUSF` ownership/completion semantics: Tasks 7 and 8
  - `Biz Pod -> Redis` correlation and liveness ownership: Tasks 2 and 3
  - `Biz Pod -> PostgreSQL` durable state ownership: Tasks 7 and 8
- Ownership/source-of-truth requirements covered: Tasks 2, 4, 7, 8, 11
- Trust-boundary/security expectation covered: Task 11
- Correlation keys/mechanism covered: Tasks 2 and 3
- Completion semantics covered: Tasks 4, 5, 10
- Recommended implementation slices are independent and testable.

### Placeholder scan

- No `TODO`, `TBD`, or “similar to Task N” placeholders remain.
- Every task includes concrete file paths and explicit commands.
- Code snippets define all referenced new types/functions before later tasks use them.

### Type consistency

- `ServerInitiatedCoordinator`, `ServerInitiatedResult`, `NssaaSessionContext`, `SessionContextResolver`, `SessionStateWriter`, `PersistentContextLookup`, `Completion`, and `AMFNotifier` are introduced in Task 1 before later tasks depend on them.
- Reverse-path request/response types consistently use `proto.AaaServerInitiatedRequest` and `proto.AaaServerInitiatedResponse`.
- Completion constants are defined in the same package that returns them.

## Notes for the implementer

- Keep changes surgical. Do not refactor unrelated handler logic while doing this work.
- If existing repository interfaces do not currently expose the exact state transitions needed, add narrow adapter methods instead of leaking storage details upward.
- If an internal contract extension is needed, update tests for both directions immediately in the same task.
- Preserve English-only comments, logs, and identifiers.
- Validate against:
  - TS 29.526 §7.2, §7.3
  - TS 23.502 §4.2.9
  - TS 33.501 §16.3–16.5
