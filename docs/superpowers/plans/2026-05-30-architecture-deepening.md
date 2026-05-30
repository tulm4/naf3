# Architecture Deepening — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deepen three shallow modules: (1) codify the AAARouter seam with RoutingContext, (2) add WriteThroughStore bridge for session persistence, (3) extract NFClientFactory to deduplicate NF client wiring.

**Architecture:** Incremental — each candidate is independent and verified in isolation. Candidates 2 and 3 can run in parallel after Candidate 1 completes.

**Tech Stack:** Go 1.22+, go-redis, pgx, Prometheus, OpenTelemetry, go-diameter

---

## File Structure Overview

```
Files to CREATE (9 new files):
- internal/eap/aaa_router.go
- internal/radius/aaa_router.go
- internal/radius/aaa_router_test.go
- internal/diameter/aaa_router.go
- internal/diameter/aaa_router_test.go
- internal/storage/bridge.go
- internal/storage/bridge_test.go
- internal/nfclient/factory.go
- internal/nfclient/factory_test.go

Files to MODIFY (6 files):
- internal/radius/client.go: add decodeSnssaiKey helper
- internal/eap/engine.go: update forwardToAAA call site, add NewEngineWithWriteThrough
- internal/eap/engine_test.go: update mockAAAClient signature
- internal/udm/udm.go: use factory
- internal/ausf/client.go: use factory
- internal/nrf/client.go: use factory
- cmd/biz/main.go: update NF client constructor calls
```

---

## Task 1: Codify AAARouter Seam with RoutingContext

**Files:**
- Create: `internal/eap/aaa_router.go`
- Create: `internal/radius/aaa_router.go`
- Create: `internal/radius/aaa_router_test.go`
- Create: `internal/diameter/aaa_router.go`
- Create: `internal/diameter/aaa_router_test.go`
- Modify: `internal/eap/engine.go` (forwardToAAA)
- Modify: `internal/eap/engine_test.go` (mockAAAClient)

### Task 1.1: Add decodeSnssaiKey helper to radius package

**Files:**
- Modify: `internal/radius/client.go` (add helper at end of file)

- [ ] **Step 1: Read end of internal/radius/client.go**

```bash
tail -10 internal/radius/client.go
```

Expected: last function is `HasMessageAuthenticator`. Add helper after it.

- [ ] **Step 2: Add decodeSnssaiKey helper**

Add at end of `internal/radius/client.go`:

```go
// decodeSnssaiKey parses a composite S-NSSAI key into SST and SD components.
// Input formats: "sst" or "sst-sd" (e.g. "1", "1-000001", "2-abc123").
// Returns sst=0, sd="" for invalid input.
func decodeSnssaiKey(key string) (sst uint8, sd string) {
    if key == "" {
        return 0, ""
    }
    parts := strings.SplitN(key, "-", 2)
    sstVal, err := strconv.ParseUint(parts[0], 10, 8)
    if err != nil {
        return 0, ""
    }
    if len(parts) == 1 {
        return uint8(sstVal), ""
    }
    return uint8(sstVal), parts[1]
}
```

Add `strings` and `strconv` to imports if not present.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/radius/...
```

Expected: no errors.

### Task 1.2: Create internal/eap/aaa_router.go

**Files:**
- Create: `internal/eap/aaa_router.go`

- [ ] **Step 1: Create the file**

```bash
touch internal/eap/aaa_router.go
```

- [ ] **Step 2: Write the deepened AAARouter interface**

Write to `internal/eap/aaa_router.go`:

```go
// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

// RoutingContext is the structured metadata needed to route and encode an EAP message
// to AAA-S. It crosses the AAARouter seam so that tests can assert on routing
// decisions without parsing opaque wire bytes.
//
// Key differences from raw Session fields:
//   - GPSI is hashed before RADIUS encoding (per TS 33.501 PII requirements).
//     Diameter sends unhashed GPSI as User-Name AVP.
//   - S-NSSAI is decoded from the Session.SnssaiKey composite key.
type RoutingContext struct {
    GPSI     string // subscriber identity (protocol-dependent: hashed for RADIUS, raw for Diameter)
    Sst      uint8  // S-NSSAI Slice Service Type
    Sd       string // S-NSSAI Slice Differentiator (empty string if not configured)
    AuthCtxID string // NSSAAF auth context ID (for correlation)
}

// AAARouter is the seam between the EAP engine and the AAA protocol.
// Protocol adapters (RADIUS, Diameter) implement this by encoding RoutingContext
// into protocol-specific attributes/AVPs.
type AAARouter interface {
    // RoutingContext extracts the structured routing metadata from a session.
    // Extracted once per call site, passed to SendEAP so the adapter can
    // encode it without re-extracting from the session.
    RoutingContext(session *Session) RoutingContext

    // SendEAP forwards an EAP payload to AAA-S and returns the response.
    // The routing parameter carries all context needed for attribute/AVP encoding,
    // so the adapter can be tested on message structure without a live AAA-S.
    SendEAP(ctx context.Context, session *Session, routing RoutingContext, eapPayload []byte) ([]byte, error)
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/eap/...
```

Expected: no errors.

### Task 1.3: Create internal/radius/aaa_router.go

**Files:**
- Create: `internal/radius/aaa_router.go`

- [ ] **Step 1: Create the file and write the adapter**

Write to `internal/radius/aaa_router.go`:

```go
// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "log/slog"

    "github.com/operator/nssAAF/internal/eap"
)

// RadiusAAARouter adapts radius.Client to the eap.AAARouter interface.
// It encodes RoutingContext into RADIUS VSAs per RFC 3579 and 3GPP TS 29.561 §16.
type RadiusAAARouter struct {
    client *Client
}

// NewRadiusAAARouter creates a RadiusAAARouter with the given RADIUS client.
func NewRadiusAAARouter(client *Client) *RadiusAAARouter {
    return &RadiusAAARouter{client: client}
}

// RoutingContext implements eap.AAARouter.
// RADIUS requires hashed GPSI per TS 33.501 PII requirements.
func (r *RadiusAAARouter) RoutingContext(session *eap.Session) eap.RoutingContext {
    gpsihash := hashGPSI(session.Gpsi)
    sst, sd := decodeSnssaiKey(session.SnssaiKey)
    return eap.RoutingContext{
        GPSI:     gpsihash,
        Sst:      sst,
        Sd:       sd,
        AuthCtxID: session.AuthCtxID,
    }
}

// hashGPSI returns SHA-256(gpsi)[:16] as a hex string.
// This is the same hash used in radius.Client.SendEAP.
func hashGPSI(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return hex.EncodeToString(h[:16])
}

// SendEAP implements eap.AAARouter.
func (r *RadiusAAARouter) SendEAP(ctx context.Context, session *eap.Session, routing eap.RoutingContext, eapPayload []byte) ([]byte, error) {
    return r.client.SendEAP(ctx, routing.GPSI, eapPayload, routing.Sst, routing.Sd)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/radius/...
```

Expected: no errors.

### Task 1.4: Create internal/radius/aaa_router_test.go

**Files:**
- Create: `internal/radius/aaa_router_test.go`

- [ ] **Step 1: Write the tests**

Write to `internal/radius/aaa_router_test.go`:

```go
// Package radius provides RADIUS client for AAA protocol interworking.
package radius

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/operator/nssAAF/internal/eap"
)

func TestRadiusAAARouter_RoutingContext_HashesGPSI(t *testing.T) {
    client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1", ServerPort: 1812}, slog.Default())
    require.NoError(t, err)
    router := NewRadiusAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com"})
    // SHA-256("alice@example.com")[:16] hex = ff8d9819fc0e12bf0d24892e45987e24
    expected := "ff8d9819fc0e12bf0d24892e45987e24"
    assert.Equal(t, expected, routing.GPSI)
}

func TestRadiusAAARouter_RoutingContext_DecodesSnssai_SstOnly(t *testing.T) {
    client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1"}, slog.Default())
    require.NoError(t, err)
    router := NewRadiusAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", SnssaiKey: "1"})
    assert.Equal(t, uint8(1), routing.Sst)
    assert.Equal(t, "", routing.Sd)
}

func TestRadiusAAARouter_RoutingContext_DecodesSnssai_SstAndSd(t *testing.T) {
    client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1"}, slog.Default())
    require.NoError(t, err)
    router := NewRadiusAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", SnssaiKey: "1-000001"})
    assert.Equal(t, uint8(1), routing.Sst)
    assert.Equal(t, "000001", routing.Sd)
}

func TestRadiusAAARouter_RoutingContext_AuthCtxID(t *testing.T) {
    client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1"}, slog.Default())
    require.NoError(t, err)
    router := NewRadiusAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", AuthCtxID: "auth-123"})
    assert.Equal(t, "auth-123", routing.AuthCtxID)
}
```

- [ ] **Step 2: Run the tests**

```bash
go test ./internal/radius/... -v -run TestRadiusAAARouter
```

Expected: all 4 tests pass.

### Task 1.5: Create internal/diameter/aaa_router.go

**Files:**
- Create: `internal/diameter/aaa_router.go`

- [ ] **Step 1: Create the file and write the adapter**

Write to `internal/diameter/aaa_router.go`:

```go
// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
    "context"
    "strconv"
    "strings"

    "github.com/operator/nssAAF/internal/eap"
)

// DiameterAAARouter adapts diameter.Client to the eap.AAARouter interface.
// It encodes RoutingContext into Diameter AVPs per RFC 4072 and TS 29.561 §17.
type DiameterAAARouter struct {
    client *Client
}

// NewDiameterAAARouter creates a DiameterAAARouter with the given Diameter client.
func NewDiameterAAARouter(client *Client) *DiameterAAARouter {
    return &DiameterAAARouter{client: client}
}

// RoutingContext implements eap.AAARouter.
// Diameter sends unhashed GPSI as User-Name AVP per RFC 4072.
func (d *DiameterAAARouter) RoutingContext(session *eap.Session) eap.RoutingContext {
    sst, sd := decodeSnssaiKey(session.SnssaiKey)
    return eap.RoutingContext{
        GPSI:     session.Gpsi, // Diameter sends unhashed GPSI
        Sst:      sst,
        Sd:       sd,
        AuthCtxID: session.AuthCtxID,
    }
}

// decodeSnssaiKey parses "sst" or "sst-sd" format.
func decodeSnssaiKey(key string) (sst uint8, sd string) {
    if key == "" {
        return 0, ""
    }
    parts := strings.SplitN(key, "-", 2)
    sstVal, err := strconv.ParseUint(parts[0], 10, 8)
    if err != nil {
        return 0, ""
    }
    if len(parts) == 1 {
        return uint8(sstVal), ""
    }
    return uint8(sstVal), parts[1]
}

// SendEAP implements eap.AAARouter.
func (d *DiameterAAARouter) SendEAP(ctx context.Context, session *eap.Session, routing eap.RoutingContext, eapPayload []byte) ([]byte, error) {
    return d.client.SendDER(ctx, routing.AuthCtxID, routing.GPSI, eapPayload, routing.Sst, routing.Sd)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/diameter/...
```

Expected: no errors.

### Task 1.6: Create internal/diameter/aaa_router_test.go

**Files:**
- Create: `internal/diameter/aaa_router_test.go`

- [ ] **Step 1: Write the tests**

Write to `internal/diameter/aaa_router_test.go`:

```go
// Package diameter provides Diameter client for AAA protocol interworking.
package diameter

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/operator/nssAAF/internal/eap"
)

func TestDiameterAAARouter_RoutingContext_UnhashedGPSI(t *testing.T) {
    client, err := NewClient(Config{OriginHost: "nssAAF", OriginRealm: "test"}, slog.Default())
    require.NoError(t, err)
    router := NewDiameterAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com"})
    // Diameter sends unhashed GPSI
    assert.Equal(t, "alice@example.com", routing.GPSI)
}

func TestDiameterAAARouter_RoutingContext_DecodesSnssai(t *testing.T) {
    client, err := NewClient(Config{OriginHost: "nssAAF", OriginRealm: "test"}, slog.Default())
    require.NoError(t, err)
    router := NewDiameterAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", SnssaiKey: "2-abc123"})
    assert.Equal(t, uint8(2), routing.Sst)
    assert.Equal(t, "abc123", routing.Sd)
}

func TestDiameterAAARouter_RoutingContext_AuthCtxID(t *testing.T) {
    client, err := NewClient(Config{OriginHost: "nssAAF", OriginRealm: "test"}, slog.Default())
    require.NoError(t, err)
    router := NewDiameterAAARouter(client)

    routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", AuthCtxID: "auth-456"})
    assert.Equal(t, "auth-456", routing.AuthCtxID)
}
```

- [ ] **Step 2: Run the tests**

```bash
go test ./internal/diameter/... -v -run TestDiameterAAARouter
```

Expected: all 3 tests pass.

### Task 1.7: Update forwardToAAA in internal/eap/engine.go

**Files:**
- Modify: `internal/eap/engine.go`

- [ ] **Step 1: Read the forwardToAAA function**

Find the `forwardToAAA` method (around line 371):

```bash
sed -n '369,393p' internal/eap/engine.go
```

Expected output:

```go
func (e *Engine) forwardToAAA(ctx context.Context, session *Session, eapPayload []byte) ([]byte, error) {
    if e.aaaClient == nil {
        return nil, errors.New("aaa client not configured")
    }

    e.logger.Debug("eap_forward_to_aaa",
        "auth_ctx_id", session.AuthCtxID,
        "snssai_key", session.SnssaiKey,
        "method", session.Method,
        "rounds", session.Rounds,
    )

    response, err := e.aaaClient.SendEAP(ctx, session, eapPayload)
    if err != nil {
        e.logger.Error("eap_aaa_error",
            "auth_ctx_id", session.AuthCtxID,
            "error", err,
        )
        return nil, fmt.Errorf("aaa client error: %w", err)
    }

    return response, nil
}
```

- [ ] **Step 2: Update the SendEAP call to extract and pass RoutingContext**

Replace the `e.aaaClient.SendEAP(ctx, session, eapPayload)` call with:

```go
    routing := e.aaaClient.RoutingContext(session)
    response, err := e.aaaClient.SendEAP(ctx, session, routing, eapPayload)
```

The rest of the function stays the same.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/eap/...
```

Expected: compile error — `mockAAAClient` doesn't implement new `SendEAP` signature. This is expected.

### Task 1.8: Update mockAAAClient in internal/eap/engine_test.go

**Files:**
- Modify: `internal/eap/engine_test.go`

- [ ] **Step 1: Read the mockAAAClient struct and its SendEAP method**

```bash
sed -n '26,85p' internal/eap/engine_test.go
```

Expected: `mockAAAClient` struct followed by `SendEAP` method.

- [ ] **Step 2: Update the mockAAAClient.SendEAP signature**

Find the `func (m *mockAAAClient) SendEAP` method (line 46). Replace its signature from:

```go
func (m *mockAAAClient) SendEAP(ctx context.Context, session *Session, eapPayload []byte) ([]byte, error) {
```

To:

```go
func (m *mockAAAClient) SendEAP(ctx context.Context, session *Session, routing RoutingContext, eapPayload []byte) ([]byte, error) {
```

- [ ] **Step 3: Add RoutingContext method to mockAAAClient**

After the `SendEAP` method (after line ~80), add:

```go
// RoutingContext implements eap.AAARouter.
func (m *mockAAAClient) RoutingContext(session *Session) RoutingContext {
    return RoutingContext{
        GPSI:     session.Gpsi, // Return unhashed — test assertions use raw GPSI
        Sst:      1,
        Sd:       "000001",
        AuthCtxID: session.AuthCtxID,
    }
}
```

- [ ] **Step 4: Verify existing tests still compile and pass**

```bash
go test ./internal/eap/... -v -count=1 2>&1 | head -50
```

Expected: all existing tests pass. Look for `TestProcessFirstResponse`, `TestProcessEAPSuccess`, `TestProcessEAPFailure` in the output.

### Task 1.9: Verify all of Task 1

- [ ] **Run the full test suite for affected packages**

```bash
go build ./internal/eap/... ./internal/radius/... ./internal/diameter/... && \
go test ./internal/eap/... -count=1 && \
go test ./internal/radius/... -count=1 && \
go test ./internal/diameter/... -count=1
```

Expected: all pass.

---

## Task 2: WriteThroughStore Bridge

**Files:**
- Create: `internal/storage/bridge.go`
- Create: `internal/storage/bridge_test.go`
- Modify: `internal/eap/engine.go` (add NewEngineWithWriteThrough constructor)

### Task 2.1: Create internal/storage/bridge.go

**Files:**
- Create: `internal/storage/bridge.go`

- [ ] **Step 1: Write the bridge types**

Write to `internal/storage/bridge.go`:

```go
// Package storage provides data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package storage

import (
    "context"
    "log/slog"
    "strconv"
    "strings"
    "time"

    "github.com/operator/nssAAF/internal/eap"
    "github.com/operator/nssAAF/internal/types"
)

// AuthSession is the Postgres-persisted version of an EAP session.
// It carries AMF metadata, notification URIs, and failure details that are not
// needed for EAP engine runtime state but are required for audit and compliance.
type AuthSession struct {
    AuthCtxID        string
    GPSI             string
    Supi             string
    SnssaiSST        uint8
    SnssaiSD         string
    AMFInstanceID    string
    AMFIP            *string
    AMFRegion        string
    AAAConfigID      *string
    EAPSessionState  []byte
    NssaaStatus      types.NssaaStatus
    EAPRounds        int
    MaxEAPRounds     int
    FailureReason    string
    FailureCause     string
    ReauthNotifURI   string
    RevocNotifURI   string
    CreatedAt        time.Time
    UpdatedAt        time.Time
    ExpiresAt        time.Time
    CompletedAt      *time.Time
    TerminatedAt     *time.Time
}

// SessionPersistence is the interface for long-term session audit.
// Postgres satisfies this. Tests use a nop adapter.
type SessionPersistence interface {
    SaveAuthSession(ctx context.Context, s *AuthSession) error
    GetAuthSession(ctx context.Context, authCtxID string) (*AuthSession, error)
}

// nopPersistence is a no-op SessionPersistence for tests.
type nopPersistence struct{}

func (n *nopPersistence) SaveAuthSession(_ context.Context, _ *AuthSession) error { return nil }
func (n *nopPersistence) GetAuthSession(_ context.Context, _ string) (*AuthSession, error) {
    return nil, ErrSessionNotFound
}

// WriteThroughStore composes a SessionStore (TTL cache) with a SessionPersistence
// (long-term audit). Reads come from the cache; writes go to both.
// The persistence write is fire-and-forget — failures are logged but do not fail
// the cache write. This matches the production requirement: a Redis failure must
// not block the EAP exchange, and a Postgres failure must not block the cache.
type WriteThroughStore struct {
    cache   eap.SessionStore
    persist SessionPersistence
    logger  *slog.Logger
}

// NewWriteThroughStore creates a write-through store.
func NewWriteThroughStore(cache eap.SessionStore, persist SessionPersistence, logger *slog.Logger) *WriteThroughStore {
    return &WriteThroughStore{
        cache:   cache,
        persist: persist,
        logger:  logger,
    }
}

// Get implements eap.SessionStore. Reads always from the cache.
func (w *WriteThroughStore) Get(ctx context.Context, authCtxID string) (*eap.Session, error) {
    return w.cache.Get(ctx, authCtxID)
}

// Put implements eap.SessionStore. Writes to both cache (blocking) and audit (fire-and-forget).
func (w *WriteThroughStore) Put(ctx context.Context, session *eap.Session) error {
    if err := w.cache.Put(ctx, session); err != nil {
        return err
    }
    authSess := toAuthSession(session)
    if err := w.persist.SaveAuthSession(ctx, authSess); err != nil {
        w.logger.Warn("audit_write_failed",
            "auth_ctx_id", session.AuthCtxID,
            "error", err,
        )
    }
    return nil
}

// Delete implements eap.SessionStore. Cache-only delete.
func (w *WriteThroughStore) Delete(ctx context.Context, authCtxID string) error {
    return w.cache.Delete(ctx, authCtxID)
}

// Size implements eap.SessionStore.
func (w *WriteThroughStore) Size() int {
    return w.cache.Size()
}

// toAuthSession converts an EAP session to a Postgres-persisted AuthSession.
// S-NSSAI is decoded from the Session.SnssaiKey composite key.
func toAuthSession(s *eap.Session) *AuthSession {
    sst, sd := decodeSnssaiKey(s.SnssaiKey)
    return &AuthSession{
        AuthCtxID:       s.AuthCtxID,
        GPSI:            s.Gpsi,
        Supi:            s.Supi,
        SnssaiSST:       sst,
        SnssaiSD:        sd,
        EAPSessionState: nil, // engine state not persisted to Postgres
        EAPRounds:       s.Rounds,
        MaxEAPRounds:    s.MaxRounds,
        CreatedAt:       s.CreatedAt,
        UpdatedAt:       s.LastActivity,
    }
}

// decodeSnssaiKey parses "sst" or "sst-sd" format.
func decodeSnssaiKey(key string) (sst uint8, sd string) {
    if key == "" {
        return 0, ""
    }
    parts := strings.SplitN(key, "-", 2)
    sstVal, err := strconv.ParseUint(parts[0], 10, 8)
    if err != nil {
        return 0, ""
    }
    if len(parts) == 1 {
        return uint8(sstVal), ""
    }
    return uint8(sstVal), parts[1]
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/storage/...
```

Expected: no errors.

### Task 2.2: Create internal/storage/bridge_test.go

**Files:**
- Create: `internal/storage/bridge_test.go`

- [ ] **Step 1: Write the tests**

Write to `internal/storage/bridge_test.go`:

```go
// Package storage provides data persistence layer for NSSAAF.
package storage

import (
    "context"
    "errors"
    "log/slog"
    "os"
    "sync"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/operator/nssAAF/internal/eap"
)

type mockPersistence struct {
    mu    sync.Mutex
    saved *AuthSession
    getFn func(string) (*AuthSession, error)
    err   error
}

func (m *mockPersistence) SaveAuthSession(_ context.Context, s *AuthSession) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.saved = s
    return m.err
}

func (m *mockPersistence) GetAuthSession(_ context.Context, authCtxID string) (*AuthSession, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.getFn != nil {
        return m.getFn(authCtxID)
    }
    return nil, ErrSessionNotFound
}

func TestWriteThroughStore_Put_BothStoresUpdated(t *testing.T) {
    cache := eap.NewTestSessionManager(time.Minute)
    persist := &mockPersistence{}
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
    store := NewWriteThroughStore(cache, persist, logger)

    session := eap.NewSession("auth-1", "alice@example.com")
    err := store.Put(context.Background(), session)
    require.NoError(t, err)

    persist.mu.Lock()
    saved := persist.saved
    persist.mu.Unlock()
    require.NotNil(t, saved)
    assert.Equal(t, "auth-1", saved.AuthCtxID)
    assert.Equal(t, "alice@example.com", saved.GPSI)
}

func TestWriteThroughStore_Put_CacheFailure_Propagates(t *testing.T) {
    cache := &failingSessionStore{putErr: errors.New("redis connection lost")}
    persist := &mockPersistence{}
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
    store := NewWriteThroughStore(cache, persist, logger)

    err := store.Put(context.Background(), eap.NewSession("auth-1", "alice@example.com"))
    assert.Error(t, err)

    persist.mu.Lock()
    assert.Nil(t, persist.saved) // persistence should not be called if cache fails
    persist.mu.Unlock()
}

func TestWriteThroughStore_Put_PersistenceFailure_Logged_NotPropagated(t *testing.T) {
    cache := eap.NewTestSessionManager(time.Minute)
    persist := &mockPersistence{err: errors.New("postgres connection lost")}
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
    store := NewWriteThroughStore(cache, persist, logger)

    err := store.Put(context.Background(), eap.NewSession("auth-1", "alice@example.com"))
    assert.NoError(t, err) // cache succeeded, persistence failure is logged, not propagated
}

func TestWriteThroughStore_Get_ReadsFromCache(t *testing.T) {
    cache := eap.NewTestSessionManager(time.Minute)
    persist := &mockPersistence{}
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
    store := NewWriteThroughStore(cache, persist, logger)

    session := eap.NewSession("auth-1", "alice@example.com")
    _ = cache.Put(context.Background(), session)

    got, err := store.Get(context.Background(), "auth-1")
    require.NoError(t, err)
    assert.Equal(t, "auth-1", got.AuthCtxID)
}

func TestWriteThroughStore_Delete_CacheOnly(t *testing.T) {
    cache := eap.NewTestSessionManager(time.Minute)
    persist := &mockPersistence{}
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
    store := NewWriteThroughStore(cache, persist, logger)

    session := eap.NewSession("auth-1", "alice@example.com")
    _ = cache.Put(context.Background(), session)

    err := store.Delete(context.Background(), "auth-1")
    require.NoError(t, err)

    _, err = store.Get(context.Background(), "auth-1")
    assert.Error(t, err)
}

// failingSessionStore is a SessionStore that fails on Put.
type failingSessionStore struct {
    putErr error
}

func (f *failingSessionStore) Get(_ context.Context, _ string) (*eap.Session, error) {
    return nil, eap.ErrSessionNotFound
}
func (f *failingSessionStore) Put(_ context.Context, _ *eap.Session) error {
    return f.putErr
}
func (f *failingSessionStore) Delete(_ context.Context, _ string) error { return nil }
func (f *failingSessionStore) Size() int                            { return 0 }
```

- [ ] **Step 2: Run the tests**

```bash
go test ./internal/storage/... -v -run TestWriteThroughStore
```

Expected: all 5 tests pass.

### Task 2.3: Add NewEngineWithWriteThrough constructor

**Files:**
- Modify: `internal/eap/engine.go`

- [ ] **Step 1: Read the end of engine.go to find where to add the constructor**

```bash
tail -20 internal/eap/engine.go
```

Expected: last function is `authResultFromEapResult`.

- [ ] **Step 2: Add NewEngineWithWriteThrough constructor**

Add after `authResultFromEapResult` (at end of file):

```go
// NewEngineWithWriteThrough creates an EAP engine backed by a write-through store
// (TTL cache + long-term audit). Use this for production with both Redis and Postgres.
func NewEngineWithWriteThrough(cfg Config, aaaClient AAARouter, cache eap.SessionStore, persist storage.SessionPersistence, logger *slog.Logger) *Engine {
    if cfg.MaxRounds == 0 {
        cfg.MaxRounds = DefaultMaxRounds
    }
    if cfg.RoundTimeout == 0 {
        cfg.RoundTimeout = DefaultRoundTimeout
    }
    if cfg.SessionTTL == 0 {
        cfg.SessionTTL = DefaultSessionTTL
    }
    if cfg.FragmentTTLSeconds == 0 {
        cfg.FragmentTTLSeconds = 60
    }

    return &Engine{
        cfg:         cfg,
        sessions:    storage.NewWriteThroughStore(cache, persist, logger),
        fragmentMgr: NewFragmentManager(cfg.FragmentTTLSeconds),
        aaaClient:   aaaClient,
        logger:      &defaultLogger{logger},
    }
}
```

Add import for `storage` package:

```go
import (
    // ... existing imports ...
    "github.com/operator/nssAAF/internal/storage"
)
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/eap/...
```

Expected: no errors.

### Task 2.4: Verify all of Task 2

- [ ] **Run the full test suite**

```bash
go build ./internal/storage/... ./internal/eap/... && \
go test ./internal/storage/... -count=1 && \
go test ./internal/eap/... -count=1
```

Expected: all pass.

---

## Task 3: NFClientFactory Extraction

**Files:**
- Create: `internal/nfclient/factory.go`
- Create: `internal/nfclient/factory_test.go`
- Modify: `internal/nrf/client.go`
- Modify: `internal/udm/udm.go`
- Modify: `internal/ausf/client.go`
- Modify: `cmd/biz/main.go`

### Task 3.1: Create internal/nfclient/factory.go

**Files:**
- Create: `internal/nfclient/factory.go`

- [ ] **Step 1: Create the directory and file**

```bash
mkdir -p internal/nfclient
touch internal/nfclient/factory.go
```

- [ ] **Step 2: Write the factory**

Write to `internal/nfclient/factory.go`:

```go
// Package nfclient provides common infrastructure for NF (Network Function) HTTP clients:
// OTel-instrumented transport, circuit breaker guards, and error normalization.
package nfclient

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/operator/nssAAF/internal/resilience"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Factory wires common NF client infrastructure: OTel-instrumented HTTP transport,
// circuit breaker guards, and timeout management. Each NF client calls factory.Do()
// instead of duplicating the wiring.
type Factory struct {
    cbRegistry *resilience.Registry
    transport  http.RoundTripper
    timeout    time.Duration
}

// NewFactory creates a factory with shared transport and registry.
func NewFactory(cbRegistry *resilience.Registry) *Factory {
    return &Factory{
        cbRegistry: cbRegistry,
        transport:  otelhttp.NewTransport(http.DefaultTransport),
        timeout:    30 * time.Second,
    }
}

// WithTimeout returns a copy of f with a custom default timeout.
func (f *Factory) WithTimeout(timeout time.Duration) *Factory {
    return &Factory{cbRegistry: f.cbRegistry, transport: f.transport, timeout: timeout}
}

// Do executes an HTTP request with circuit breaker guard and OTel instrumentation.
// Returns (statusCode, responseBody, error).
// The caller provides method, path, and body; factory owns transport + CB + OTel.
func (f *Factory) Do(ctx context.Context, baseURL, method, path string, body []byte) (int, []byte, error) {
    if f.cbRegistry != nil {
        cb := f.cbRegistry.Get(baseURL)
        if !cb.Allow() {
            return 0, nil, fmt.Errorf("nfclient: circuit breaker open for %s", baseURL)
        }
    }

    url := baseURL + path
    var bodyReader io.Reader
    if body != nil {
        bodyReader = bytes.NewReader(body)
    }

    req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
    if err != nil {
        f.recordFailure(baseURL)
        return 0, nil, fmt.Errorf("nfclient: create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{
        Transport: f.transport,
        Timeout:   f.timeout,
    }
    resp, err := client.Do(req)
    if err != nil {
        f.recordFailure(baseURL)
        return 0, nil, fmt.Errorf("nfclient: do request: %w", err)
    }
    defer resp.Body.Close()

    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return 0, nil, fmt.Errorf("nfclient: read body: %w", err)
    }

    if resp.StatusCode >= 400 {
        f.recordFailure(baseURL)
    } else {
        f.recordSuccess(baseURL)
    }

    return resp.StatusCode, respBody, nil
}

func (f *Factory) recordFailure(baseURL string) {
    if f.cbRegistry != nil {
        f.cbRegistry.Get(baseURL).RecordFailure()
    }
}

func (f *Factory) recordSuccess(baseURL string) {
    if f.cbRegistry != nil {
        f.cbRegistry.Get(baseURL).RecordSuccess()
    }
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/nfclient/...
```

Expected: no errors.

### Task 3.2: Create internal/nfclient/factory_test.go

**Files:**
- Create: `internal/nfclient/factory_test.go`

- [ ] **Step 1: Write the factory tests**

Write to `internal/nfclient/factory_test.go`:

```go
// Package nfclient provides common NF client infrastructure.
package nfclient

import (
    "context"
    "errors"
    "io"
    "net/http"
    "strings"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/operator/nssAAF/internal/resilience"
)

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
    return f(req)
}

func TestFactory_DoesCircuitBreak(t *testing.T) {
    cb := resilience.NewCircuitBreaker(1, time.Hour, 1)
    cb.RecordFailure() // open the breaker immediately

    factory := NewFactory(&resilience.Registry{})
    factory.cbRegistry = cb

    _, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
    assert.ErrorContains(t, err, "circuit breaker open")
}

func TestFactory_RecordsSuccess(t *testing.T) {
    rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
    })
    factory := &Factory{transport: rt, timeout: time.Second}

    _, _, err := factory.Do(context.Background(), "http://nrf:8080", http.MethodGet, "/test", nil)
    assert.NoError(t, err)
}

func TestFactory_RecordsFailure_OnNon2xx(t *testing.T) {
    rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("error"))}, nil
    })
    factory := &Factory{transport: rt, timeout: time.Second}

    _, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
    assert.Error(t, err)
    assert.ErrorContains(t, err, "500")
}

func TestFactory_ReturnsStatusCode(t *testing.T) {
    rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found"))}, nil
    })
    factory := &Factory{transport: rt, timeout: time.Second}

    status, body, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
    require.NoError(t, err)
    assert.Equal(t, 404, status)
    assert.Equal(t, "not found", string(body))
}

func TestFactory_NilRegistry_NoPanic(t *testing.T) {
    factory := NewFactory(nil)
    factory.cbRegistry = nil

    _, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
    assert.NoError(t, err) // no CB means no guard, so this succeeds
}

func TestFactory_WithTimeout(t *testing.T) {
    factory := NewFactory(nil).WithTimeout(5 * time.Second)
    assert.Equal(t, 5*time.Second, factory.timeout)
}
```

- [ ] **Step 2: Run the tests**

```bash
go test ./internal/nfclient/... -v
```

Expected: all 6 tests pass.

### Task 3.3: Refactor internal/udm/udm.go to use factory

**Files:**
- Modify: `internal/udm/udm.go`

- [ ] **Step 1: Read the current Client struct and NewClient**

```bash
sed -n '1,60p' internal/udm/udm.go
```

Expected: `Client` struct with `baseURL`, `nrfClient`, `httpClient`, `cbRegistry` fields.

- [ ] **Step 2: Replace the Client struct fields and NewClient**

Find and replace the `Client` struct definition and `NewClient` function:

From:
```go
type Client struct {
    baseURL    string
    nrfClient  *nrf.Client
    httpClient *http.Client
    cbRegistry *resilience.Registry
}

func NewClient(cfg config.UDMConfig, nrfClient *nrf.Client, cbRegistry *resilience.Registry) *Client {
    return &Client{
        baseURL:    cfg.BaseURL,
        nrfClient: nrfClient,
        httpClient: &http.Client{
            Timeout:   cfg.Timeout,
            Transport: otelhttp.NewTransport(http.DefaultTransport),
        },
        cbRegistry: cbRegistry,
    }
}
```

To:
```go
type Client struct {
    baseURL   string
    nrfClient *nrf.Client
    factory   *nfclient.Factory
}

func NewClient(cfg config.UDMConfig, factory *nfclient.Factory, nrfClient *nrf.Client) *Client {
    return &Client{
        baseURL:   cfg.BaseURL,
        nrfClient: nrfClient,
        factory:   factory,
    }
}

func (c *Client) doRequest(ctx context.Context, baseURL, method, path string, body []byte) (int, []byte, error) {
    return c.factory.Do(ctx, baseURL, method, path, body)
}

func (c *Client) discoverBaseURL(ctx context.Context, supi string) (string, error) {
    if c.baseURL != "" {
        return c.baseURL, nil
    }
    if c.nrfClient == nil {
        return "", errors.New("udm: no baseURL and no NRF client configured")
    }
    plmn := extractPLMNFromSupi(supi)
    return c.nrfClient.DiscoverUDM(ctx, plmn)
}
```

Add `nfclient` import and remove `resilience` and `otelhttp` imports from the import block.

- [ ] **Step 3: Refactor GetAuthContext to use factory**

Replace the `GetAuthContext` method body. Find the start of the method body (after `func (c *Client) GetAuthContext`) and replace the circuit-breaker ceremony with a call to `doRequest`.

The key change: remove all `cb` variable checks, `cb.Allow()` calls, and `cb.Record*()` calls. Replace the `httpClient.Do(req)` call with `c.doRequest(ctx, baseURL, http.MethodGet, path, nil)`.

Before (lines ~56-117 — look for the pattern of `cbRegistry.Get`, `cb.Allow`, `httpClient.Do`, `cb.Record*`):
```go
var cb *resilience.CircuitBreaker
if c.cbRegistry != nil {
    cb = c.cbRegistry.Get(baseURL)
    if !cb.Allow() {
        return nil, fmt.Errorf("udm: circuit breaker open for %s", baseURL)
    }
}
// ... httpClient.Do(req) ...
if resp.StatusCode == http.StatusNotFound {
    if cb != nil { cb.RecordFailure() }
    // ...
}
if resp.StatusCode != http.StatusOK {
    if cb != nil { cb.RecordFailure() }
    // ...
}
if cb != nil { cb.RecordSuccess() }
```

After — the method body becomes:
```go
baseURL, err := c.discoverBaseURL(ctx, supi)
if err != nil {
    return nil, err
}
path := "/nudm-uem/v1/subscribers/" + supi + "/auth-contexts"
status, body, err := c.doRequest(ctx, baseURL, http.MethodGet, path, nil)
if err != nil {
    return nil, err
}
if status == http.StatusNotFound {
    return nil, fmt.Errorf("udm: subscriber %s not found", supi)
}
if status != http.StatusOK {
    return nil, fmt.Errorf("udm: unexpected status %d", status)
}
var result struct {
    AuthContexts []AuthSubscription `json:"authContexts"`
}
if err := json.Unmarshal(body, &result); err != nil {
    return nil, fmt.Errorf("udm: decode response: %w", err)
}
if len(result.AuthContexts) == 0 {
    return nil, fmt.Errorf("udm: no auth contexts found for %s", supi)
}
return &result.AuthContexts[0], nil
```

- [ ] **Step 4: Refactor UpdateAuthContext the same way**

Same pattern: remove `cb` variable and ceremony, use `c.doRequest`.

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/udm/...
```

Expected: no errors.

### Task 3.4: Refactor internal/ausf/client.go to use factory

**Files:**
- Modify: `internal/ausf/client.go`

- [ ] **Step 1: Read the current Client struct and NewClient**

```bash
sed -n '1,50p' internal/ausf/client.go
```

- [ ] **Step 2: Replace Client struct and NewClient**

From:
```go
type Client struct {
    baseURL    string
    httpClient *http.Client
    cbRegistry *resilience.Registry
}

func NewClient(cfg config.AUSFConfig, cbRegistry *resilience.Registry) *Client {
    return &Client{
        baseURL: cfg.BaseURL,
        httpClient: &http.Client{
            Timeout:   cfg.Timeout,
            Transport: otelhttp.NewTransport(http.DefaultTransport),
        },
        cbRegistry: cbRegistry,
    }
}
```

To:
```go
type Client struct {
    baseURL string
    factory *nfclient.Factory
}

func NewClient(cfg config.AUSFConfig, factory *nfclient.Factory) *Client {
    return &Client{
        baseURL: cfg.BaseURL,
        factory: factory,
    }
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
    return c.factory.Do(ctx, c.baseURL, method, path, body)
}
```

- [ ] **Step 3: Refactor ForwardMSK to use factory**

Replace the `cb` ceremony with `c.doRequest`. The method body becomes:
```go
if c.baseURL == "" {
    return fmt.Errorf("ausf: baseURL not configured")
}
payload := map[string]interface{}{
    "authCtxId": authCtxID,
    "msk":       msk,
}
body, err := json.Marshal(payload)
if err != nil {
    return fmt.Errorf("ausf: marshal msk: %w", err)
}
status, respBody, err := c.doRequest(ctx, http.MethodPost, "/nnssaaaf-aiw/v1/msk", body)
if err != nil {
    return fmt.Errorf("ausf: forward msk: %w", err)
}
if status >= 400 {
    return fmt.Errorf("ausf: unexpected status %d", status)
}
return nil
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/ausf/...
```

Expected: no errors.

### Task 3.5: Refactor internal/nrf/client.go to use factory

**Files:**
- Modify: `internal/nrf/client.go`

- [ ] **Step 1: Read the current Client struct and NewClient**

```bash
sed -n '1,110p' internal/nrf/client.go
```

- [ ] **Step 2: Replace Client struct, NewClient, and add doRequest**

From:
```go
type Client struct {
    baseURL      string
    httpClient   *http.Client
    nfInstanceID string
    cache        *NRFDiscoveryCache
    registered   atomic.Bool
    cbRegistry   *resilience.Registry
}

func NewClient(cfg config.NRFConfig, cbRegistry *resilience.Registry) *Client {
    cacheTTL := cfg.CacheTTL
    if cacheTTL == 0 {
        cacheTTL = 5 * time.Minute
    }
    return &Client{
        baseURL: cfg.BaseURL,
        httpClient: &http.Client{
            Timeout:   cfg.DiscoverTimeout,
            Transport: otelhttp.NewTransport(http.DefaultTransport),
        },
        nfInstanceID: fmt.Sprintf("nssAAF-instance-%d", time.Now().UnixNano()),
        cache: &NRFDiscoveryCache{
            ttl: cacheTTL,
        },
        cbRegistry: cbRegistry,
    }
}
```

To:
```go
type Client struct {
    baseURL      string
    nfInstanceID string
    cache        *NRFDiscoveryCache
    registered   atomic.Bool
    factory      *nfclient.Factory
}

func NewClient(cfg config.NRFConfig, factory *nfclient.Factory) *Client {
    cacheTTL := cfg.CacheTTL
    if cacheTTL == 0 {
        cacheTTL = 5 * time.Minute
    }
    return &Client{
        baseURL:      cfg.BaseURL,
        nfInstanceID: fmt.Sprintf("nssAAF-instance-%d", time.Now().UnixNano()),
        cache: &NRFDiscoveryCache{
            ttl: cacheTTL,
        },
        factory: factory,
    }
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
    return c.factory.Do(ctx, c.baseURL, method, path, body)
}
```

- [ ] **Step 3: Refactor Register, Heartbeat, DiscoverUDM, DiscoverAMF, Deregister**

In each method, remove the `cb` variable and ceremony. Replace `c.httpClient.Do(req)` with `c.doRequest(ctx, http.MethodXxx, path, body)`.

For `Register`: path is `/nnrf-disc/v1/nf-instances`, method is `POST`.
For `Heartbeat`: path is `/nnrf-disc/v1/nf-instances/`+c.nfInstanceID, method is `PUT`.
For `DiscoverUDM`: path is `/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem`, method is `GET`.
For `DiscoverAMF`: path is `/nnrf-disc/v1/nf-instances/`+amfID, method is `GET`.
For `Deregister`: path is `/nnrf-disc/v1/nf-instances/`+c.nfInstanceID, method is `DELETE`.

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/nrf/...
```

Expected: no errors.

### Task 3.6: Update cmd/biz/main.go constructor calls

**Files:**
- Modify: `cmd/biz/main.go`

- [ ] **Step 1: Find all NF client constructor calls**

```bash
grep -n "nrf.NewClient\|udm.NewClient\|ausf.NewClient" cmd/biz/main.go
```

Expected output shows line numbers for each call.

- [ ] **Step 2: Read the context around each constructor call**

```bash
grep -n "NewFactory\|cbRegistry\|resilience.Registry" cmd/biz/main.go | head -20
```

This shows where the registry is created and where factory needs to be inserted.

- [ ] **Step 3: Add factory creation and update constructor calls**

Find where `cbRegistry` is created (likely `resilience.NewRegistry(...)`) and add factory creation after it:

```go
// After cbRegistry creation:
factory := nfclient.NewFactory(cbRegistry)

// Update constructor calls:
// Before:
nrfClient := nrf.NewClient(cfg.NRF, cbRegistry)
udmClient := udm.NewClient(cfg.UDM, cbRegistry, nrfClient)
ausfClient := ausf.NewClient(cfg.AUSF, cbRegistry)

// After:
nrfClient := nrf.NewClient(cfg.NRF, factory)
udmClient := udm.NewClient(cfg.UDM, factory, nrfClient)
ausfClient := ausf.NewClient(cfg.AUSF, factory)
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./cmd/biz/...
```

Expected: no errors.

### Task 3.7: Verify all of Task 3

- [ ] **Run the full test suite**

```bash
go build ./cmd/... ./internal/nrf/... ./internal/udm/... ./internal/ausf/... ./internal/nfclient/... && \
go test ./internal/nfclient/... -count=1 && \
go test ./internal/nrf/... -count=1 && \
go test ./internal/udm/... -count=1 && \
go test ./internal/ausf/... -count=1
```

Expected: all pass.

---

## Task 4: Final Verification

- [ ] **Full build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Full test suite**

```bash
go test ./... -count=1 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] **Count lines changed**

```bash
echo "=== New files ===" && \
wc -l internal/eap/aaa_router.go internal/radius/aaa_router.go internal/diameter/aaa_router.go \
      internal/storage/bridge.go internal/nfclient/factory.go && \
echo "=== Test files ===" && \
wc -l internal/radius/aaa_router_test.go internal/diameter/aaa_router_test.go \
      internal/storage/bridge_test.go internal/nfclient/factory_test.go
```

Expected: roughly 100 lines for adapter + interface files, ~150 lines for tests.

---

## Self-Review Checklist

- [ ] **Spec coverage**: Each candidate's interface is fully defined. Candidate 1 adds 3 new files + 2 modified. Candidate 2 adds 2 new files + 1 modified. Candidate 5 adds 2 new files + 4 modified.
- [ ] **Placeholder scan**: No "TBD", "TODO", or "fill in later" in code blocks. All method signatures are complete.
- [ ] **Type consistency**: `RoutingContext` struct defined once in `internal/eap/aaa_router.go`. `SessionPersistence` interface defined once in `internal/storage/bridge.go`. `Factory` defined once in `internal/nfclient/factory.go`. All usages match these definitions.
- [ ] **Test names**: Each test has a descriptive name matching its assertion (`TestRadiusAAARouter_RoutingContext_HashesGPSI`, `TestWriteThroughStore_Put_PersistenceFailure_Logged_NotPropagated`).
- [ ] **Error modes documented**: Each interface method documents its error modes in comments.
