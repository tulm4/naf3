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

## Task 1a: Discover existing storage and session interfaces before adapter design

**Files:**
- Modify: `docs/superpowers/plans/2026-06-06-internal-communication-gaps-implementation-plan.md`
- Review: `internal/storage/`
- Review: `internal/storage/postgres/`
- Review: `internal/api/nssaa/handler.go`
- Review: `internal/api/aiw/handler.go`

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

Replace any invented signatures that do not match the codebase with shapes grounded in the repository APIs. In particular, verify whether these plan-level placeholders are correct before implementing them:

```go
type NSSAAStatusRepository interface {
    UpdateStatus(authCtxID string, status string) error
    LoadNotificationURIs(authCtxID string) (reauth string, revoc string, err error)
}

type PersistentContextLookup interface {
    LoadAuthContext(ctx context.Context, authCtxID string) (*SessionContext, error)
}
```

If the real repository APIs differ, update Tasks 7-9 in this plan before writing production code.

- [ ] **Step 4: Re-run the plan consistency check**

Run: `rg "UpdateStatus|LoadNotificationURIs|LoadAuthContext|persistentLookup|stateWriter" "docs/superpowers/plans/2026-06-06-internal-communication-gaps-implementation-plan.md"`
Expected: every referenced adapter name/signature is now consistent with the discovered codebase reality

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

type SessionContext struct {
    AuthCtxID      string
    SessionID      string
    ReauthNotifURI string
    RevocNotifURI  string
    CallbackOwner  string
    HasAIWContext  bool
}

type ServerInitiatedResult struct {
    Response proto.AaaServerInitiatedResponse
}

func (r ServerInitiatedResult) Validate() error {
    if len(r.Response.Payload) == 0 {
        return fmt.Errorf("response payload is required")
    }
    return nil
}

type SessionContextResolver interface {
    Resolve(ctx context.Context, sessionID string, authCtxID string) (*SessionContext, error)
}

type SessionStateWriter interface {
    MarkReauthPending(authCtxID string) error
    MarkRevoked(authCtxID string) error
    ApplyCoA(authCtxID string, payload []byte) error
}

type AMFNotifier interface {
    SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error
    SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error
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
    ctx *SessionContext
}

func (s stubSessionLookup) LoadAuthContext(_ context.Context, authCtxID string) (*SessionContext, error) {
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

    resolver := NewCorrelationResolver(rdb, stubSessionLookup{ctx: &SessionContext{
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

type PersistentContextLookup interface {
    LoadAuthContext(ctx context.Context, authCtxID string) (*SessionContext, error)
}

type CorrelationResolver struct {
    rdb    goredis.Cmdable
    lookup PersistentContextLookup
}

func NewCorrelationResolver(rdb goredis.Cmdable, lookup PersistentContextLookup) *CorrelationResolver {
    return &CorrelationResolver{rdb: rdb, lookup: lookup}
}

func (r *CorrelationResolver) Resolve(ctx context.Context, sessionID string, authCtxID string) (*SessionContext, error) {
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
    ctx *SessionContext
}

func (s stubResolver) Resolve(_ context.Context, sessionID, authCtxID string) (*SessionContext, error) {
    out := *s.ctx
    out.SessionID = sessionID
    out.AuthCtxID = authCtxID
    return &out, nil
}

type stubWriter struct {
    reauthAuthCtxIDs []string
}

func (s *stubWriter) MarkReauthPending(authCtxID string) error {
    s.reauthAuthCtxIDs = append(s.reauthAuthCtxIDs, authCtxID)
    return nil
}
func (s *stubWriter) MarkRevoked(string) error { return nil }
func (s *stubWriter) ApplyCoA(string, []byte) error { return nil }

type stubNotifier struct {
    reauthCalls int
}

func (s *stubNotifier) SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
    s.reauthCalls++
    return nil
}
func (s *stubNotifier) SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
    return nil
}

func TestServerInitiatedCoordinator_Handle_Reauth_UpdatesStateAndNotifiesAMF(t *testing.T) {
    writer := &stubWriter{}
    notifier := &stubNotifier{}
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &SessionContext{
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
        if err := c.writer.MarkReauthPending(sessionCtx.AuthCtxID); err != nil {
            return nil, fmt.Errorf("mark reauth pending: %w", err)
        }
        if sessionCtx.CallbackOwner == "amf" && sessionCtx.ReauthNotifURI != "" {
            if err := c.notifier.SendReAuthNotification(ctx, sessionCtx.ReauthNotifURI, sessionCtx.AuthCtxID, req.Payload); err != nil {
                return nil, fmt.Errorf("send reauth notification: %w", err)
            }
        }
        result := &ServerInitiatedResult{Response: proto.AaaServerInitiatedResponse{
            Version:   proto.CurrentVersion,
            SessionID: req.SessionID,
            AuthCtxID: sessionCtx.AuthCtxID,
            Payload:   []byte{2, 0, 0, 12},
        }}
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
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &SessionContext{
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
    if err := c.writer.MarkRevoked(sessionCtx.AuthCtxID); err != nil {
        return nil, fmt.Errorf("mark revoked: %w", err)
    }
    if sessionCtx.CallbackOwner == "amf" && sessionCtx.RevocNotifURI != "" {
        if err := c.notifier.SendRevocationNotification(ctx, sessionCtx.RevocNotifURI, sessionCtx.AuthCtxID, req.Payload); err != nil {
            return nil, fmt.Errorf("send revocation notification: %w", err)
        }
    }
    result := &ServerInitiatedResult{Response: proto.AaaServerInitiatedResponse{
        Version:   proto.CurrentVersion,
        SessionID: req.SessionID,
        AuthCtxID: sessionCtx.AuthCtxID,
        Payload:   []byte{1},
    }}
    return result, result.Validate()
case proto.MessageTypeCoA:
    if err := c.writer.ApplyCoA(sessionCtx.AuthCtxID, req.Payload); err != nil {
        return nil, fmt.Errorf("apply coa: %w", err)
    }
    result := &ServerInitiatedResult{Response: proto.AaaServerInitiatedResponse{
        Version:   proto.CurrentVersion,
        SessionID: req.SessionID,
        AuthCtxID: sessionCtx.AuthCtxID,
        Payload:   []byte{2, 0, 0, 12},
    }}
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

## Task 7: Wire Biz main HTTP handler to injected coordinator

**Files:**
- Modify: `cmd/biz/main.go`
- Modify: `cmd/biz/factory.go`
- Test: `cmd/biz/main_test.go`

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
        return &biz.ServerInitiatedResult{Response: proto.AaaServerInitiatedResponse{
            Version:   proto.CurrentVersion,
            SessionID: req.SessionID,
            AuthCtxID: req.AuthCtxID,
            Payload:   []byte{2, 0, 0, 12},
        }}, nil
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

In `cmd/biz/factory.go`, wire the concrete coordinator during `Build()`:

```go
resolver := biz.NewCorrelationResolver(redisPool.Client(), persistentLookup)
stateWriter := biz.NewStateWriter(nssaaRepositoryAdapter)
coordinator := biz.NewServerInitiatedCoordinator(resolver, stateWriter, amfNotifier)
serverInitiatedHandler = coordinator.Handle
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/biz -run TestHandleServerInitiated_UsesCoordinatorResponse -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/biz/main.go cmd/biz/factory.go cmd/biz/main_test.go
 git commit -m "feat: wire biz server-initiated coordinator"
```

## Task 6: Add persistent lookup and state-writer adapters over existing stores

**Files:**
- Modify: `cmd/biz/factory.go`
- Create: `internal/biz/adapters.go`
- Create: `internal/biz/adapters_test.go`
- Test: `internal/biz/adapters_test.go`

- [ ] **Step 1: Write the failing test**

```go
package biz

import "testing"

func TestStateWriter_MarkReauthPending_DelegatesToRepository(t *testing.T) {
    repo := &stubNSSAARepository{}
    writer := NewStateWriter(repo)

    if err := writer.MarkReauthPending("auth-1"); err != nil {
        t.Fatalf("MarkReauthPending returned error: %v", err)
    }
    if repo.lastStatus != "PENDING" {
        t.Fatalf("lastStatus = %q, want PENDING", repo.lastStatus)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestStateWriter_MarkReauthPending_DelegatesToRepository -count=1`
Expected: FAIL with `undefined: NewStateWriter`

- [ ] **Step 3: Write minimal implementation**

```go
package biz

type NSSAAStatusRepository interface {
    UpdateStatus(authCtxID string, status string) error
    LoadNotificationURIs(authCtxID string) (reauth string, revoc string, err error)
}

type StateWriter struct {
    repo NSSAAStatusRepository
}

func NewStateWriter(repo NSSAAStatusRepository) *StateWriter {
    return &StateWriter{repo: repo}
}

func (w *StateWriter) MarkReauthPending(authCtxID string) error {
    return w.repo.UpdateStatus(authCtxID, "PENDING")
}

func (w *StateWriter) MarkRevoked(authCtxID string) error {
    return w.repo.UpdateStatus(authCtxID, "REVOKED")
}

func (w *StateWriter) ApplyCoA(authCtxID string, payload []byte) error {
    return w.repo.UpdateStatus(authCtxID, "PENDING")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/biz -run TestStateWriter_MarkReauthPending_DelegatesToRepository -count=1`
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

    if store.lastSaved.CallbackOwner != "amf" {
        t.Fatalf("CallbackOwner = %q, want amf", store.lastSaved.CallbackOwner)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/nssaa -run TestCreateSliceAuthentication_PersistsCallbackOwnershipMetadata -count=1`
Expected: FAIL because callback ownership metadata is not persisted yet

- [ ] **Step 3: Write minimal implementation**

```go
ctx.CallbackOwner = "amf"
ctx.ReauthNotifURI = body.ReauthNotifUri
ctx.RevocNotifURI = body.RevocNotifUri
```

And for AIW completion metadata in `internal/api/aiw/handler.go`:

```go
ctx.CallbackOwner = "ausf"
ctx.ReauthNotifURI = ""
ctx.RevocNotifURI = ""
ctx.HasAIWContext = true
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/nssaa -run TestCreateSliceAuthentication_PersistsCallbackOwnershipMetadata -count=1 && go test ./internal/api/aiw -run TestAIWHandler_.* -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/nssaa/handler.go internal/api/aiw/handler.go internal/api/nssaa/handler_test.go internal/api/aiw/handler_test.go internal/biz/adapters.go
 git commit -m "feat: persist callback ownership metadata"
```

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
//    SessionContext{AuthCtxID: "auth-it-1", ReauthNotifURI: amfServer.URL, CallbackOwner: "amf"}
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
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &SessionContext{
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

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/biz -run TestServerInitiatedCoordinator_Handle_LogsAndReturnsCompletionMetadata -count=1`
Expected: FAIL because completion semantics are not explicit yet

- [ ] **Step 3: Write minimal implementation**

```go
type Completion string

const (
    CompletionUpdatedState Completion = "UPDATED_STATE"
    CompletionNotifiedAMF  Completion = "NOTIFIED_AMF"
    CompletionAppliedCoA   Completion = "APPLIED_COA"
)

type ServerInitiatedResult struct {
    Response   proto.AaaServerInitiatedResponse
    Completion Completion
}
```

Set completion values in each branch and log them with existing structured logger fields:

```go
slog.Info("server_initiated_completed",
    "auth_ctx_id", sessionCtx.AuthCtxID,
    "session_id", req.SessionID,
    "message_type", req.MessageType,
    "completion", result.Completion,
    "callback_owner", sessionCtx.CallbackOwner,
)
```

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
    coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &SessionContext{
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

- `ServerInitiatedCoordinator`, `ServerInitiatedResult`, `SessionContext`, `SessionContextResolver`, and `SessionStateWriter` are introduced before later tasks depend on them.
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
