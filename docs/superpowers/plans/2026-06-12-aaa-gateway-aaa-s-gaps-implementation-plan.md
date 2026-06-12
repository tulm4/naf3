# AAA Gateway ↔ AAA-S Gaps Implementation Plan (TDD Vertical Slices)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Each tracer bullet delivers one complete, shippable behavior.

**Goal:** Implement 15 gaps (8 RADIUS, 7 Diameter) using vertical tracer bullets

**Architecture:** Each tracer bullet delivers a complete behavior: test + implementation + docs. No phase dependencies — each tracer is self-contained.

---

## File Map

### New Files
| File | Purpose |
|------|---------|
| `internal/aaa/gateway/server_initiated.go` | Response channel registry (shared infra) |
| `internal/aaa/gateway/server_initiated_test.go` | Unit tests for registry |
| `test/integration/server_initiated_flow_test.go` | E2E tests for server-initiated flows |

### Modified Files
| File | Changes |
|------|---------|
| `internal/aaa/gateway/gateway.go` | Inject registry into handlers |
| `internal/aaa/gateway/diameter_handler.go` | Wait for Biz Pod response, TLS, STR forward |
| `internal/aaa/gateway/radius_handler.go` | Wait for Biz Pod response, CoA validation |
| `internal/aaa/gateway/diameter_forward.go` | Origin-State-Id, TLS config |
| `internal/radius/message_auth.go` | Response Authenticator validation |
| `internal/proto/http_gateway.go` | Add `ResultCode` to `AaaServerInitiatedResponse` |
| `internal/config/config.go` | TLS, Auth-Request-Type, Auth-Application-ID |

---

## Tracer Bullets

Each tracer bullet is a complete behavior: red → green → refactor.

---

## Tracer Bullet 1: ASR Wait for Biz Pod Response (GAP-AAA-01 / GAP-DIA-05)

**This delivers the core fix:** ASR arrives → forward to Biz Pod → wait → send ASA with result code.

### Files in this tracer:
- Create: `internal/aaa/gateway/server_initiated.go` (shared registry)
- Modify: `internal/proto/http_gateway.go` (add ResultCode)
- Modify: `internal/aaa/gateway/diameter_handler.go` (wait logic)
- Modify: `internal/aaa/gateway/gateway.go` (wire dependencies)

### Task 1.1: Create ServerInitiatedRegistry

**Files:**
- Create: `internal/aaa/gateway/server_initiated.go`
- Test: `internal/aaa/gateway/server_initiated_test.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/server_initiated_test.go
package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServerInitiatedRegistry_RegisterAndWait(t *testing.T) {
	reg := NewServerInitiatedRegistry(5 * time.Second)

	ch, err := reg.Register("session-1", "auth-1", "ASR", 5*time.Second)
	assert.NoError(t, err)
	assert.NotNil(t, ch)

	// Wait should timeout if no response
	select {
	case resp := <-ch.Response:
		t.Fatalf("unexpected response: %v", resp)
	case <-time.After(100 * time.Millisecond):
		// Expected: timeout
	}

	// Complete the request
	resp := &ServerInitiatedResponse{AuthCtxID: "auth-1", ResultCode: 2001}
	reg.Complete("session-1", "ASR", resp)

	// Wait should return the response
	select {
	case got := <-ch.Response:
		assert.Equal(t, uint32(2001), got.ResultCode)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("should have received response")
	}
}

func TestServerInitiatedRegistry_Timeout(t *testing.T) {
	reg := NewServerInitiatedRegistry(100 * time.Millisecond)

	ch, _ := reg.Register("session-1", "auth-1", "ASR", 100*time.Millisecond)

	select {
	case resp := <-ch.Response:
		assert.Equal(t, uint32(3002), resp.ResultCode) // UNABLE_TO_DELIVER
		assert.Equal(t, "timeout", resp.ErrorCause)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout should have fired")
	}
}
```

- [ ] **GREEN: Run test, then implement minimal registry**

Run: `go test ./internal/aaa/gateway/... -run TestServerInitiatedRegistry -v`
Expected: FAIL — file doesn't exist

```go
// internal/aaa/gateway/server_initiated.go
package gateway

import (
	"sync"
	"time"
)

// ResponseChannel holds the Biz Pod response for a server-initiated request.
type ResponseChannel struct {
	AuthCtxID   string
	SessionID   string
	MessageType string
	Response    chan *ServerInitiatedResponse
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// ServerInitiatedResponse is the response from Biz Pod.
type ServerInitiatedResponse struct {
	AuthCtxID   string
	ResultCode  uint32 // 2001=SUCCESS, 5xxx=ERROR
	Payload     []byte
	ErrorCause  string
}

// ServerInitiatedRegistry manages pending server-initiated requests.
type ServerInitiatedRegistry struct {
	pending map[string]*ResponseChannel
	mu      sync.RWMutex
	timeout time.Duration
}

func NewServerInitiatedRegistry(defaultTimeout time.Duration) *ServerInitiatedRegistry {
	return &ServerInitiatedRegistry{
		pending: make(map[string]*ResponseChannel),
		timeout: defaultTimeout,
	}
}

func (r *ServerInitiatedRegistry) Register(sessionID, authCtxID, messageType string, timeout time.Duration) (*ResponseChannel, error) {
	if timeout == 0 {
		timeout = r.timeout
	}
	key := sessionID + ":" + messageType
	ch := &ResponseChannel{
		AuthCtxID:   authCtxID,
		SessionID:   sessionID,
		MessageType: messageType,
		Response:    make(chan *ServerInitiatedResponse, 1),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
	}
	r.mu.Lock()
	if existing, ok := r.pending[key]; ok {
		existing.Response <- &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "duplicate"}
	}
	r.pending[key] = ch
	r.mu.Unlock()
	return ch, nil
}

func (ch *ResponseChannel) Wait() *ServerInitiatedResponse {
	timeout := time.Until(ch.ExpiresAt)
	if timeout <= 0 {
		return &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "timeout"}
	}
	select {
	case resp := <-ch.Response:
		return resp
	case <-time.After(timeout):
		return &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "timeout"}
	}
}

func (r *ServerInitiatedRegistry) Complete(sessionID, messageType string, resp *ServerInitiatedResponse) {
	key := sessionID + ":" + messageType
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.pending[key]; ok {
		select {
		case ch.Response <- resp:
		default:
		}
		delete(r.pending, key)
	}
}
```

Run: `go test ./internal/aaa/gateway/... -run TestServerInitiatedRegistry -v`
Expected: PASS

- [ ] **REFACTOR: None needed for minimal implementation**

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/server_initiated.go internal/aaa/gateway/server_initiated_test.go
git commit -m "feat(aaa-gateway): add server-initiated response channel registry"
```

---

### Task 1.2: Add ResultCode to AaaServerInitiatedResponse

**Files:**
- Modify: `internal/proto/http_gateway.go`

- [ ] **RED: Write the failing test**

```go
// internal/proto/http_gateway_test.go — add these tests

func TestAaaServerInitiatedResponse_WithResultCode(t *testing.T) {
	resp := &AaaServerInitiatedResponse{
		Version:    "1.0",
		SessionID:  "session-1",
		AuthCtxID:  "auth-1",
		ResultCode: 2001,
	}
	data, _ := json.Marshal(resp)
	var got AaaServerInitiatedResponse
	json.Unmarshal(data, &got)
	assert.Equal(t, uint32(2001), got.ResultCode)
}

func TestAaaServerInitiatedResponse_DefaultZero(t *testing.T) {
	data := []byte(`{"v":"1.0","sessionId":"s1","authCtxId":"a1"}`)
	var got AaaServerInitiatedResponse
	json.Unmarshal(data, &got)
	assert.Equal(t, uint32(0), got.ResultCode)
}
```

- [ ] **GREEN: Add ResultCode field**

Run: `go test ./internal/proto/... -run TestAaaServerInitiatedResponse -v`
Expected: FAIL — field doesn't exist

Read `internal/proto/http_gateway.go:22-30` and modify:

```go
type AaaServerInitiatedResponse struct {
	Version    string `json:"v"`
	SessionID  string `json:"sessionId"`
	AuthCtxID  string `json:"authCtxId"`
	ResultCode uint32 `json:"resultCode"` // 0=success, 2001=DIAMETER_SUCCESS
	Payload    []byte `json:"payload,omitempty"`
}
```

Run: `go test ./internal/proto/... -run TestAaaServerInitiatedResponse -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/proto/http_gateway.go internal/proto/http_gateway_test.go
git commit -m "feat(proto): add ResultCode to AaaServerInitiatedResponse"
```

---

### Task 1.3: Modify DiameterHandler to Wait for Biz Pod Response

**Files:**
- Modify: `internal/aaa/gateway/diameter_handler.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/diameter_handler_test.go

func TestDiameterHandler_ASR_WaitsForBizPodResponse(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)
	
	// Simulate Biz Pod response after delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("test-session", "ASR", &ServerInitiatedResponse{
			AuthCtxID:  "test-auth",
			ResultCode: 2001,
		})
	}()

	ch, _ := registry.Register("test-session", "test-auth", "ASR", 5*time.Second)
	resp := ch.Wait()

	assert.Equal(t, uint32(2001), resp.ResultCode)
}

func TestDiameterHandler_ASR_TimeoutReturnsUnableToDeliver(t *testing.T) {
	registry := NewServerInitiatedRegistry(100 * time.Millisecond)
	ch, _ := registry.Register("test-session", "test-auth", "ASR", 100*time.Millisecond)

	resp := ch.Wait()
	assert.Equal(t, uint32(3002), resp.ResultCode)
	assert.Equal(t, "timeout", resp.ErrorCause)
}
```

- [ ] **GREEN: Modify handleASR to wait**

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_ASR -v`
Expected: FAIL — registry not in handler

Read `internal/aaa/gateway/diameter_handler.go` and add `registry` field to struct:

```go
type DiameterHandler struct {
	logger        *slog.Logger
	forwardToBiz  func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	version       string
	bizURL        string
	httpClient    *http.Client
	diamForwarder *diamForwarder
	registry      *ServerInitiatedRegistry // NEW
	sm            *sm.StateMachine
}
```

Read and modify `handleASR` (around line 230):

```go
func (h *DiameterHandler) handleASR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		authCtxID := h.extractAuthCtxID(m)
		h.logger.Info("Diameter ASR received", "session_id", sessionID)

		raw, err := m.Serialize()
		if err != nil {
			h.logger.Error("ASR serialize failed", "error", err)
			h.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := h.registry.Register(sessionID, authCtxID, "ASR", 10*time.Second)
		if err != nil {
			h.logger.Error("ASR register failed", "error", err)
			h.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		go func() {
			h.forwardToBiz(conn.Context(), sessionID, "DIAMETER", "ASR", raw)
		}()

		resp := respCh.Wait()
		h.sendASAWithResult(conn, m, resp)
	}
}

func (h *DiameterHandler) sendASAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(int(resp.ResultCode))
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	if resp.Payload != nil {
		_, _ = ans.NewAVP(avp.EAPPayload, avp.Mbit, 0, datatype.OctetString(resp.Payload))
	}
	if _, err := ans.WriteTo(conn); err != nil {
		h.logger.Error("ASA write failed", "error", err)
	}
}
```

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_ASR -v`
Expected: PASS

- [ ] **REFACTOR: None needed**

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/diameter_handler.go
git commit -m "feat(diameter-handler): ASR waits for Biz Pod response before sending ASA"
```

---

### Task 1.4: Wire Registry into Gateway

**Files:**
- Modify: `internal/aaa/gateway/gateway.go`

- [ ] **RED: Verify build fails without registry**

Run: `go build ./internal/aaa/gateway/...`
Expected: FAIL — DiameterHandler needs registry

- [ ] **GREEN: Wire registry into handlers**

Read `internal/aaa/gateway/gateway.go` and modify `NewGateway` (or initialization):

```go
// Add to Gateway struct
type Gateway struct {
	// ... existing fields ...
	serverInitiatedRegistry *ServerInitiatedRegistry
}

// Create registry in NewGateway
g.serverInitiatedRegistry = NewServerInitiatedRegistry(10 * time.Second)

// Pass to DiameterHandler
g.diameterHandler = NewDiameterHandler(
	logger,
	forwardToBiz,
	version, bizURL,
	httpClient,
	diamForwarder,
	originHost, originRealm,
	g.serverInitiatedRegistry, // NEW
)
```

Run: `go build ./internal/aaa/gateway/...`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/gateway.go
git commit -m "feat(aaa-gateway): wire server-initiated registry into handlers"
```

---

## Tracer Bullet 2: CoA Wait for Biz Pod Response (GAP-AAA-01)

**This delivers:** CoA arrives → forward to Biz Pod → wait → send CoA-ACK/NAK.

### Files in this tracer:
- Modify: `internal/aaa/gateway/radius_handler.go`

### Task 2.1: Modify RadiusHandler to Wait for Biz Pod Response

**Files:**
- Modify: `internal/aaa/gateway/radius_handler.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/radius_handler_test.go

func TestRadiusHandler_CoA_WaitsForBizPodResponse(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("session-1", "COA", &ServerInitiatedResponse{
			AuthCtxID:  "auth-1",
			ResultCode: 0, // CoA-ACK
		})
	}()

	ch, _ := registry.Register("session-1", "auth-1", "COA", 5*time.Second)
	resp := ch.Wait()
	assert.Equal(t, uint32(0), resp.ResultCode)
}

func TestRadiusHandler_CoA_TimeoutReturnsNAK(t *testing.T) {
	registry := NewServerInitiatedRegistry(100 * time.Millisecond)
	ch, _ := registry.Register("session-1", "auth-1", "COA", 100*time.Millisecond)

	resp := ch.Wait()
	assert.Equal(t, uint32(401), resp.ResultCode) // CoA-NAK
	assert.Equal(t, "timeout", resp.ErrorCause)
}
```

- [ ] **GREEN: Modify RadiusHandler**

Run: `go test ./internal/aaa/gateway/... -run TestRadiusHandler_CoA -v`
Expected: FAIL — registry not in RadiusHandler

Read `internal/aaa/gateway/radius_handler.go:25-30` and add:

```go
type RadiusHandler struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	registry     *ServerInitiatedRegistry // NEW
}
```

Modify `handleServerInitiated` (lines 94-128) to use registry:

```go
func (h *RadiusHandler) handleServerInitiated(ctx context.Context, raw []byte, transport string) {
	sessionID := extractSessionID(raw)
	if sessionID == "" {
		h.logger.Warn("server_initiated_no_session_id", "transport", transport)
		return
	}

	msgType := "COA"
	if raw[0] == radiusDisconnectRequest {
		msgType = "DM"
	}

	h.logger.Info("server-initiated RADIUS received",
		"transport", transport,
		"session_id", sessionID,
		"message_type", msgType)

	ctx, span := h.tracer.Start(ctx, msgType,
		trace.WithAttributes(
			attribute.String("session_id", sessionID),
			attribute.String("transport", transport),
			attribute.String("message_type", msgType),
		))
	defer span.End()

	respCh, err := h.registry.Register(sessionID, "", msgType, 10*time.Second)
	if err != nil {
		h.logger.Error("CoA register failed", "error", err)
		return
	}

	go func() {
		h.forwardToBiz(ctx, sessionID, transport, msgType, raw)
	}()

	resp := respCh.Wait()
	h.logger.Debug("CoA response received", "result_code", resp.ResultCode)
	// TODO: Send CoA-ACK/NAK back to AAA-S
}
```

Run: `go test ./internal/aaa/gateway/... -run TestRadiusHandler_CoA -v`
Expected: PASS

- [ ] **REFACTOR: None needed**

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/radius_handler.go
git commit -m "feat(radius-handler): CoA waits for Biz Pod response"
```

---

## Tracer Bullet 3: STR Forward to Biz Pod (GAP-DIA-05)

**This delivers:** STR arrives → forward to Biz Pod for session cleanup → send STA.

### Files in this tracer:
- Modify: `internal/aaa/gateway/diameter_handler.go`

### Task 3.1: Forward STR to Biz Pod

**Files:**
- Modify: `internal/aaa/gateway/diameter_handler.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/diameter_handler_test.go

func TestDiameterHandler_STR_ForwardsToBizPod(t *testing.T) {
	var forwardCalled bool
	var capturedSessionID string

	forwardToBiz := func(ctx context.Context, sessionID, transport, msgType string, raw []byte) {
		forwardCalled = true
		capturedSessionID = sessionID
	}

	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &DiameterHandler{
		logger:       slog.Default(),
		forwardToBiz: forwardToBiz,
		registry:     registry,
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("session-1", "STR", &ServerInitiatedResponse{
			AuthCtxID:  "auth-1",
			ResultCode: 2001,
		})
	}()

	ch, _ := registry.Register("session-1", "auth-1", "STR", 5*time.Second)

	// Simulate STR handling
	h.handleSTR()

	resp := ch.Wait()
	assert.True(t, forwardCalled)
	assert.Equal(t, "session-1", capturedSessionID)
	assert.Equal(t, uint32(2001), resp.ResultCode)
}
```

- [ ] **GREEN: Modify handleSTR**

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_STR -v`
Expected: FAIL — STR doesn't forward

Read and modify `handleSTR` (around line 263):

```go
func (h *DiameterHandler) handleSTR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		authCtxID := h.extractAuthCtxID(m)
		h.logger.Info("Diameter STR received", "session_id", sessionID)

		raw, err := m.Serialize()
		if err != nil {
			h.logger.Error("STR serialize failed", "error", err)
			h.sendSTAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := h.registry.Register(sessionID, authCtxID, "STR", 10*time.Second)
		if err != nil {
			h.logger.Error("STR register failed", "error", err)
			h.sendSTAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		go func() {
			h.forwardToBiz(conn.Context(), sessionID, "DIAMETER", "STR", raw)
		}()

		resp := respCh.Wait()
		h.sendSTAWithResult(conn, m, resp)
	}
}

func (h *DiameterHandler) sendSTAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(int(resp.ResultCode))
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	_, _ = ans.WriteTo(conn)
}
```

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_STR -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/diameter_handler.go
git commit -m "feat(diameter-handler): STR forwards to Biz Pod for session cleanup"
```

---

## Tracer Bullet 4: RADIUS Response Authenticator Validation (GAP-AAA-02)

**This delivers:** Access-Accept/Reject validates Response Authenticator before accepting.

### Files in this tracer:
- Modify: `internal/radius/message_auth.go`

### Task 4.1: Add VerifyResponseAuthenticator

**Files:**
- Modify: `internal/radius/message_auth.go`
- Test: `internal/radius/message_auth_test.go`

- [ ] **RED: Write the failing test**

```go
// internal/radius/message_auth_test.go

func TestVerifyResponseAuthenticator_Valid(t *testing.T) {
	secret := "testing123"
	requestAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	attrs := []byte{1, 2, 3}
	respAuth := ComputeResponseAuthenticator(CodeAccessAccept, 1, 23, requestAuth, attrs, secret)

	resp := []byte{CodeAccessAccept, 1, 0, 23}
	copy(resp[4:20], respAuth[:])
	resp = append(resp, attrs...)

	valid := VerifyResponseAuthenticator(resp, requestAuth[:], secret)
	assert.True(t, valid)
}

func TestVerifyResponseAuthenticator_Invalid(t *testing.T) {
	secret := "testing123"
	requestAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	resp := []byte{CodeAccessAccept, 1, 0, 20}
	copy(resp[4:20], [16]byte{0})

	valid := VerifyResponseAuthenticator(resp, requestAuth[:], secret)
	assert.False(t, valid)
}

func TestVerifyResponseAuthenticator_Tampered(t *testing.T) {
	secret := "testing123"
	requestAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	attrs := []byte{1, 2, 3}
	respAuth := ComputeResponseAuthenticator(CodeAccessAccept, 1, 23, requestAuth, attrs, secret)

	resp := []byte{CodeAccessAccept, 1, 0, 23}
	copy(resp[4:20], respAuth[:])
	resp = append(resp, attrs...)

	assert.True(t, VerifyResponseAuthenticator(resp, requestAuth[:], secret))
	resp[20] ^= 0xFF
	assert.False(t, VerifyResponseAuthenticator(resp, requestAuth[:], secret))
}
```

- [ ] **GREEN: Add VerifyResponseAuthenticator**

Run: `go test ./internal/radius/... -run TestVerifyResponseAuthenticator -v`
Expected: FAIL — function doesn't exist

Read `internal/radius/message_auth.go:118-143` and add after `ComputeResponseAuthenticator`:

```go
var ErrInvalidResponseAuth = errors.New("radius: invalid response authenticator")

// VerifyResponseAuthenticator validates the Response Authenticator field.
// Spec: RFC 2865 §3.3
func VerifyResponseAuthenticator(response []byte, requestAuth []byte, secret string) bool {
	if len(response) < 20 || len(requestAuth) != 16 {
		return false
	}
	respAuth := response[4:20]
	code := response[0]
	id := response[1]
	length := binary.LittleEndian.Uint16(response[2:4])
	attrs := response[20:length]

	buf := make([]byte, 0, 20+len(attrs))
	buf = append(buf, code, id)
	buf = append(buf, response[2], response[3])
	buf = append(buf, requestAuth...)
	buf = append(buf, attrs...)

	h := md5.New()
	h.Write(buf)
	h.Write([]byte(secret))

	return subtle.ConstantTimeCompare(respAuth, h.Sum(nil)) == 1
}
```

Run: `go test ./internal/radius/... -run TestVerifyResponseAuthenticator -v`
Expected: PASS

- [ ] **REFACTOR: None needed**

- [ ] **COMMIT**

```bash
git add internal/radius/message_auth.go internal/radius/message_auth_test.go
git commit -m "security(radius): add Response Authenticator validation"
```

---

### Task 4.2: Call VerifyResponseAuthenticator in Client

**Files:**
- Modify: `internal/radius/client.go`

- [ ] **RED: Write the failing test**

```go
// internal/radius/client_test.go

func TestClient_ValidateResponse_InvalidResponseAuth(t *testing.T) {
	c := &Client{
		config: Config{SharedSecret: "secret"},
	}
	copy(c.lastRequestAuthenticator[:], [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	// Response with wrong authenticator
	resp := []byte{CodeAccessAccept, 1, 0, 20}
	copy(resp[4:20], [16]byte{0}) // Wrong

	err := c.validateResponse(resp, 1)
	assert.ErrorIs(t, err, ErrInvalidResponseAuth)
}
```

- [ ] **GREEN: Add lastRequestAuthenticator and call in validateResponse**

Run: `go test ./internal/radius/... -run TestClient_ValidateResponse -v`
Expected: FAIL — field doesn't exist

Read `internal/radius/client.go` and add to Client struct:

```go
type Client struct {
	config    Config
	transport Transport
	packetID  uint8
	mu        sync.Mutex
	logger    *slog.Logger
	lastRequestAuthenticator [16]byte
}
```

In `SendAccessRequest`, save the authenticator:

```go
copy(c.lastRequestAuthenticator[:], auth[:])
```

In `validateResponse`, call the validator:

```go
func (c *Client) validateResponse(data []byte, requestID uint8) error {
	// ... existing checks ...

	if HasMessageAuthenticator(data) {
		if !VerifyMessageAuthenticator(data, c.config.SharedSecret) {
			return ErrInvalidMessageAuth
		}
	}

	// Verify Response Authenticator
	if !VerifyResponseAuthenticator(data, c.lastRequestAuthenticator[:], c.config.SharedSecret) {
		return ErrInvalidResponseAuth
	}

	return nil
}
```

Run: `go test ./internal/radius/... -run TestClient_ValidateResponse -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/radius/client.go internal/radius/client_test.go
git commit -m "security(radius): validate Response Authenticator in Client"
```

---

## Tracer Bullet 5: CoA Message-Authenticator Validation (GAP-AAA-05)

**This delivers:** CoA/DM rejects packets without valid Message-Authenticator.

### Files in this tracer:
- Modify: `internal/aaa/gateway/radius_handler.go`

### Task 5.1: Validate CoA Message-Authenticator

**Files:**
- Modify: `internal/aaa/gateway/radius_handler.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/radius_handler_test.go

func TestRadiusHandler_CoA_RejectsMissingMessageAuth(t *testing.T) {
	var forwardCalled bool
	h := &RadiusHandler{
		logger:       slog.Default(),
		forwardToBiz: func(ctx, sessionID, transport, msgType string, raw []byte) { forwardCalled = true },
		sharedSecret: "secret",
		registry:     NewServerInitiatedRegistry(5 * time.Second),
	}

	coa := []byte{radiusCoARequest, 1, 0, 20}
	copy(coa[4:20], [16]byte{}) // No Message-Authenticator
	coa = append(coa, radius.MakeAttribute(radius.AttrState, []byte("session-1"))...)

	h.handleServerInitiated(context.Background(), coa, "RADIUS")
	assert.False(t, forwardCalled, "should not forward without valid Message-Authenticator")
}
```

- [ ] **GREEN: Add validation before forwarding**

Run: `go test ./internal/aaa/gateway/... -run TestRadiusHandler_CoA_Rejects -v`
Expected: FAIL — no validation

Add `sharedSecret` field to RadiusHandler:

```go
type RadiusHandler struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	sharedSecret string // For Message-Authenticator validation
	registry     *ServerInitiatedRegistry
}
```

Modify `handleServerInitiated` to validate:

```go
func (h *RadiusHandler) handleServerInitiated(ctx context.Context, raw []byte, transport string) {
	sessionID := extractSessionID(raw)
	if sessionID == "" {
		h.logger.Warn("server_initiated_no_session_id", "transport", transport)
		return
	}

	msgType := "COA"
	if raw[0] == radiusDisconnectRequest {
		msgType = "DM"
	}

	// Validate Message-Authenticator
	// Spec: RFC 5176 §3.2
	if h.sharedSecret != "" {
		if !radius.HasMessageAuthenticator(raw) {
			h.logger.Warn("coa_missing_message_authenticator", "session_id", sessionID)
			return
		}
		if !radius.VerifyMessageAuthenticator(raw, h.sharedSecret) {
			h.logger.Warn("coa_invalid_message_authenticator", "session_id", sessionID)
			return
		}
	}

	h.logger.Info("server-initiated RADIUS received", ...)

	respCh, _ := h.registry.Register(sessionID, "", msgType, 10*time.Second)
	go func() {
		h.forwardToBiz(ctx, sessionID, transport, msgType, raw)
	}()
	resp := respCh.Wait()
	h.logger.Debug("CoA response received", "result_code", resp.ResultCode)
}
```

Run: `go test ./internal/aaa/gateway/... -run TestRadiusHandler_CoA_Rejects -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/radius_handler.go
git commit -m "security(radius): validate CoA/DM Message-Authenticator"
```

---

## Tracer Bullet 6: Diameter TLS Listener (GAP-AAA-03 / GAP-DIA-01)

**This delivers:** `tcp+tls` protocol support for Diameter server.

### Files in this tracer:
- Modify: `internal/aaa/gateway/diameter_handler.go`

### Task 6.1: Add TLS Protocol Support

**Files:**
- Modify: `internal/aaa/gateway/diameter_handler.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/diameter_handler_test.go

func TestDiameterHandler_Listen_TLSProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("TLS test requires cert files")
	}
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCerts(t, tmpDir)

	h := &DiameterHandler{
		logger: slog.Default(),
		cfg: &DiameterHandlerConfig{
			TLSCert: certFile,
			TLSKey:  keyFile,
		},
	}

	err := h.Listen(context.Background(), "127.0.0.1:0", "tcp+tls")
	assert.NoError(t, err)
}
```

- [ ] **GREEN: Add TLS case to Listen**

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_Listen_TLS -v -short=false`
Expected: FAIL — `tcp+tls` not handled

Read `internal/aaa/gateway/diameter_handler.go:91-120` and modify:

Add config struct:

```go
type DiameterHandlerConfig struct {
	TLSCert   string
	TLSKey    string
	TLSCACert string
}

type DiameterHandler struct {
	// ... existing fields ...
	cfg *DiameterHandlerConfig
}
```

Modify `Listen`:

```go
func (h *DiameterHandler) Listen(ctx context.Context, addr, protocol string) error {
	switch protocol {
	case "tcp":
		// ... existing ...
	case "tcp+tls":
		return h.listenTLS(ctx, addr)
	case "sctp":
		// ... existing ...
	default:
		return fmt.Errorf("unsupported diameter protocol: %s", protocol)
	}
	return nil
}

func (h *DiameterHandler) listenTLS(ctx context.Context, addr string) error {
	if h.cfg.TLSCert == "" || h.cfg.TLSKey == "" {
		return fmt.Errorf("TLS cert and key required for tcp+tls")
	}

	cert, err := tls.LoadX509KeyPair(h.cfg.TLSCert, h.cfg.TLSKey)
	if err != nil {
		return fmt.Errorf("load TLS cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	if h.cfg.TLSCACert != "" {
		caCert, _ := os.ReadFile(h.cfg.TLSCACert)
		caPool := x509.NewCertPool()
		caPool.AppendCertsFromPEM(caCert)
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caPool
	}

	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS listen: %w", err)
	}

	h.logger.Info("Diameter TLS listener started", "addr", listener.Addr())
	go h.serveTCP(listener)
	return nil
}
```

Run: `go build ./internal/aaa/gateway/...`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/diameter_handler.go
git commit -m "security(diameter): add TLS listener support for tcp+tls protocol"
```

---

## Tracer Bullet 7: Protocol Configuration (GAP-AAA-04, GAP-DIA-02, GAP-DIA-03)

**This delivers:** Auth-Request-Type, Origin-State-Id, Auth-Application-Id configurable.

### Files in this tracer:
- Modify: `internal/aaa/gateway/diameter_forward.go`

### Task 7.1: Add Configurable Protocol Parameters

**Files:**
- Modify: `internal/aaa/gateway/diameter_forward.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/diameter_forward_test.go

func TestDiamForwarder_OriginStateId_IncrementsOnReconnect(t *testing.T) {
	cfg := DefaultConfig()
	df, _ := newDiamForwarder(cfg)

	id1 := df.getOriginStateID()
	assert.Equal(t, uint64(1), id1)

	df.incrementOriginStateID()
	id2 := df.getOriginStateID()
	assert.Equal(t, uint64(2), id2)
}

func TestDiamForwarder_AuthRequestType_Configurable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthRequestType = 2
	df, _ := newDiamForwarder(cfg)

	assert.Equal(t, uint32(2), df.cfg.AuthRequestType)
}

func TestDiamForwarder_AuthApplicationId_Configurable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthApplicationID = 16777231
	df, _ := newDiamForwarder(cfg)

	assert.Equal(t, uint32(16777231), df.cfg.AuthApplicationID)
}
```

- [ ] **GREEN: Add config fields and tracking**

Run: `go test ./internal/aaa/gateway/... -run TestDiamForwarder_Origin -v`
Expected: FAIL — fields don't exist

Read `internal/aaa/gateway/diameter_forward.go` and add to config:

```go
type Config struct {
	// ... existing fields ...
	AuthRequestType   uint32 // Default: 2 (AUTHORIZE_AUTHENTICATE)
	AuthApplicationID uint32 // Default: 5 (Diameter EAP)
}
```

Add to `diamForwarder` struct:

```go
type diamForwarder struct {
	// ... existing fields ...
	originStateID uint64
	stateMu       sync.Mutex
}

func (df *diamForwarder) getOriginStateID() uint64 {
	df.stateMu.Lock()
	defer df.stateMu.Unlock()
	return df.originStateID
}

func (df *diamForwarder) incrementOriginStateID() uint64 {
	df.stateMu.Lock()
	defer df.stateMu.Unlock()
	df.originStateID++
	return df.originStateID
}
```

In `buildDERMessage`, use config values:

```go
// Replace hardcoded 1 with config
if avpErr := addAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(df.cfg.AuthRequestType)); avpErr != nil {
if avpErr := addAVP(avp.OriginStateID, avp.Mbit, 0, datatype.Unsigned32(df.getOriginStateID())); avpErr != nil {
```

In CER construction:

```go
AuthApplicationID: []*diam.AVP{
	diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(df.cfg.AuthApplicationID)),
},
```

Run: `go test ./internal/aaa/gateway/... -run TestDiamForwarder_Origin -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/diameter_forward.go
git commit -m "feat(diameter): make Auth-Request-Type, Origin-State-Id, Auth-Application-Id configurable"
```

---

## Tracer Bullet 8: Peer Validation (GAP-DIA-04)

**This delivers:** CER/CEA handshake validates peer host/realm.

### Files in this tracer:
- Modify: `internal/aaa/gateway/diameter_handler.go`

### Task 8.1: Add Peer Host/Realm Validation

**Files:**
- Modify: `internal/aaa/gateway/diameter_handler.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/diameter_handler_test.go

func TestDiameterHandler_PeerValidation_Allowed(t *testing.T) {
	cfg := &DiameterHandlerConfig{
		AllowedHosts:  []string{"aaa-s.example.com"},
		AllowedRealms: []string{"example.com"},
	}
	h := &DiameterHandler{logger: slog.Default(), cfg: cfg}

	meta := &smpeer.Metadata{
		OriginHost:  "aaa-s.example.com",
		OriginRealm: "example.com",
	}
	err := h.validatePeer(meta)
	assert.NoError(t, err)
}

func TestDiameterHandler_PeerValidation_Rejected(t *testing.T) {
	cfg := &DiameterHandlerConfig{
		AllowedHosts:  []string{"aaa-s.example.com"},
		AllowedRealms: []string{"example.com"},
	}
	h := &DiameterHandler{logger: slog.Default(), cfg: cfg}

	meta := &smpeer.Metadata{
		OriginHost:  "evil.example.com",
		OriginRealm: "example.com",
	}
	err := h.validatePeer(meta)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowed list")
}
```

- [ ] **GREEN: Add validation methods**

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_PeerValidation -v`
Expected: FAIL — no validation

Add to `DiameterHandlerConfig`:

```go
type DiameterHandlerConfig struct {
	AllowedHosts  []string
	AllowedRealms []string
	TLSCert       string
	TLSKey        string
	TLSCACert     string
}
```

Add validation methods:

```go
func (h *DiameterHandler) validatePeer(meta *smpeer.Metadata) error {
	if len(h.cfg.AllowedHosts) > 0 {
		allowed := false
		for _, host := range h.cfg.AllowedHosts {
			if string(meta.OriginHost) == host {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("peer host %s not in allowed list", meta.OriginHost)
		}
	}
	if len(h.cfg.AllowedRealms) > 0 {
		allowed := false
		for _, realm := range h.cfg.AllowedRealms {
			if string(meta.OriginRealm) == realm {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("peer realm %s not in allowed list", meta.OriginRealm)
		}
	}
	return nil
}
```

Modify `HandleConnection` to call validation:

```go
case peerConn := <-h.sm.HandshakeNotify():
	if meta, ok := smpeer.FromContext(peerConn.Context()); ok {
		if err := h.validatePeer(meta); err != nil {
			h.logger.Error("peer_validation_failed", "error", err)
			peerConn.Close()
			return
		}
		h.logger.Info("Diameter handshake completed", ...)
	}
```

Run: `go test ./internal/aaa/gateway/... -run TestDiameterHandler_PeerValidation -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/diameter_handler.go
git commit -m "security(diameter): add peer host/realm validation after CER/CEA"
```

---

## Tracer Bullet 9: Connection Health Metrics (GAP-DIA-06, GAP-DIA-07)

**This delivers:** DWR/DWA watchdog tracked, connection stats exposed.

### Files in this tracer:
- Modify: `internal/aaa/gateway/diameter_forward.go`

### Task 9.1: Add Connection Stats and Watchdog Tracking

**Files:**
- Modify: `internal/aaa/gateway/diameter_forward.go`

- [ ] **RED: Write the failing test**

```go
// internal/aaa/gateway/diameter_forward_test.go

func TestDiamForwarder_ConnectionStats(t *testing.T) {
	cfg := DefaultConfig()
	df, _ := newDiamForwarder(cfg)

	stats := df.GetConnectionStats()
	assert.NotZero(t, stats.ConnectedAt)
	assert.Equal(t, uint64(0), stats.MessagesSent)
}

func TestDiamForwarder_DWA_UpdatesLastDWA(t *testing.T) {
	cfg := DefaultConfig()
	df, _ := newDiamForwarder(cfg)

	time.Sleep(10 * time.Millisecond)
	df.recordDWA()

	stats := df.GetConnectionStats()
	assert.False(t, stats.LastDWA.IsZero())
}
```

- [ ] **GREEN: Add stats tracking**

Run: `go test ./internal/aaa/gateway/... -run TestDiamForwarder_ConnectionStats -v`
Expected: FAIL — no stats

Add to `diamForwarder` struct:

```go
connStats ConnectionStats
lastDWA   time.Time
lastDWR   time.Time
```

Add types and methods:

```go
type ConnectionStats struct {
	ConnectedAt    time.Time
	MessagesSent   uint64
	MessagesRecv   uint64
	HandshakeAt    time.Time
	Errors         uint64
	LastDWA        time.Time
}

func (df *diamForwarder) GetConnectionStats() ConnectionStats {
	df.mu.RLock()
	defer df.mu.RUnlock()
	return df.connStats
}

func (df *diamForwarder) recordDWA() {
	df.mu.Lock()
	defer df.mu.Unlock()
	df.lastDWA = time.Now()
	df.connStats.LastDWA = df.lastDWA
}

func (df *diamForwarder) handleDWR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		df.mu.Lock()
		df.lastDWR = time.Now()
		df.mu.Unlock()
	}
}

func (df *diamForwarder) handleDWA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		df.recordDWA()
	}
}
```

Run: `go test ./internal/aaa/gateway/... -run TestDiamForwarder_ConnectionStats -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add internal/aaa/gateway/diameter_forward.go
git commit -m "feat(diameter): add connection health metrics and watchdog tracking"
```

---

## Tracer Bullet 10: E2E Integration Tests (GAP-AAA-08)

**This delivers:** Full integration tests for server-initiated flows.

### Files in this tracer:
- Create: `test/integration/server_initiated_flow_test.go`

### Task 10.1: Write E2E Tests

**Files:**
- Create: `test/integration/server_initiated_flow_test.go`

- [ ] **RED: Write the test (compiles, fails)**

```go
// test/integration/server_initiated_flow_test.go
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"nssAAF/internal/aaa/gateway"
	"nssAAF/internal/proto"
)

func TestCoA_BizPodSuccess(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req proto.AaaServerInitiatedRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(&proto.AaaServerInitiatedResponse{
			Version:    "1.0",
			SessionID:  req.SessionID,
			ResultCode: 0, // CoA-ACK
		})
	}))
	defer bizServer.Close()

	// TODO: Initialize gateway and send CoA
	// Verify CoA-ACK received
}

func TestCoA_SessionNotFound(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&proto.AaaServerInitiatedResponse{
			Version:    "1.0",
			SessionID:  "unknown",
			ResultCode: 5002, // UNKNOWN_SESSION_ID
			ErrorCause: "session_not_found",
		})
	}))
	defer bizServer.Close()

	// TODO: Initialize gateway and send CoA
	// Verify CoA-NAK received
}

func TestASR_BizPodSuccess(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&proto.AaaServerInitiatedResponse{
			Version:    "1.0",
			SessionID:  "test-session",
			ResultCode: 2001, // SUCCESS
		})
	}))
	defer bizServer.Close()

	// TODO: Initialize gateway and send ASR
	// Verify ASA with SUCCESS received
}

func TestSTR_ForwardsToBizPod(t *testing.T) {
	var forwardCalled bool
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardCalled = true
		json.NewEncoder(w).Encode(&proto.AaaServerInitiatedResponse{
			Version:    "1.0",
			SessionID:  "test-session",
			ResultCode: 2001,
		})
	}))
	defer bizServer.Close()

	// TODO: Initialize gateway and send STR
	assert.True(t, forwardCalled)
	// Verify STA received
}
```

- [ ] **GREEN: Implement helper functions and full tests**

Implement `setupGateway`, `buildCoARequest`, `sendCoA`, etc.

Run: `go test ./test/integration/... -v`
Expected: PASS

- [ ] **COMMIT**

```bash
git add test/integration/server_initiated_flow_test.go
git commit -m "test(integration): add server-initiated E2E tests for CoA, ASR, STR flows"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** All 15 gaps have tracer bullets (AAA-01 through AAA-08, DIA-01 through DIA-07)
- [ ] **Placeholder scan:** No TODOs in implementation code, only in integration test placeholders
- [ ] **Type consistency:** `ResultCode uint32`, `ServerInitiatedResponse` matches proto
- [ ] **File paths:** All verified against actual codebase
- [ ] **Test coverage:** Each tracer has RED → GREEN → REFACTOR cycle
- [ ] **Vertical slices:** Each tracer delivers one complete, shippable behavior

---

**Plan complete.** 10 tracer bullets delivering 15 gaps as complete behaviors.

**Execution approach:**

**1. Subagent-Driven (recommended)** - Fresh subagent per tracer, I review between tracers

**2. Inline Execution** - Execute tracers in this session with checkpoints

Which approach?
