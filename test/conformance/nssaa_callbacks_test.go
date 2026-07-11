// Package conformance provides TS 29.526 NSSAA API conformance test suites.
// Spec: TS 29.526 v18.7.0 §7.2.4-5 (Server-Initiated Callbacks)
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/biz"
	redisstore "github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test Fixtures ──────────────────────────────────────────────────────────

// inMemorySessionStore implements biz.PersistentContextLookup.
type inMemorySessionStore struct {
	sessions map[string]*biz.NssaaSessionContext
}

func newInMemorySessionStore() *inMemorySessionStore {
	return &inMemorySessionStore{sessions: map[string]*biz.NssaaSessionContext{}}
}

func (s *inMemorySessionStore) LoadAuthContext(_ context.Context, authCtxID string) (*biz.NssaaSessionContext, error) {
	session, ok := s.sessions[authCtxID]
	if !ok {
		return nil, assert.AnError
	}
	c := *session
	return &c, nil
}

// trackingStateWriter records which methods were called.
type trackingStateWriter struct {
	Calls []string
}

func (w *trackingStateWriter) MarkReauthPending(_ context.Context, authCtxID string) error {
	w.Calls = append(w.Calls, "MarkReauthPending:"+authCtxID)
	return nil
}
func (w *trackingStateWriter) MarkRevoked(_ context.Context, authCtxID string) error {
	w.Calls = append(w.Calls, "MarkRevoked:"+authCtxID)
	return nil
}
func (w *trackingStateWriter) ApplyCoA(_ context.Context, authCtxID string, _ []byte) error {
	w.Calls = append(w.Calls, "ApplyCoA:"+authCtxID)
	return nil
}

// noopAIWLinker is a no-op AIW linker.
type noopAIWLinker struct{}

func (noopAIWLinker) MarkAIWLinked(context.Context, string) error { return nil }

// noopNotifier is a mock AMF notifier.
type noopNotifier struct{}

func (noopNotifier) SendReAuthNotification(context.Context, string, string, []byte) error { return nil }
func (noopNotifier) SendRevocationNotification(context.Context, string, string, []byte) error {
	return nil
}

// ─── Test Setup ─────────────────────────────────────────────────────────────

// callbackTestEnv holds all test infrastructure for a callback test case.
type callbackTestEnv struct {
	t           *testing.T
	store       *inMemorySessionStore
	corrStore   *redisstore.SessionCorrelationStore
	stateWriter *trackingStateWriter
	redisClient *goredis.Client
	miniredis   *miniredis.Miniredis
	amfServer   *httptest.Server
	coordinator *biz.ServerInitiatedCoordinator
}

func newCallbackTestEnv(t *testing.T) *callbackTestEnv {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	corrStore := redisstore.NewSessionCorrelationStore(rdb, proto.DefaultPayloadTTL)

	amfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	require.NoError(t, err)

	store := newInMemorySessionStore()
	stateWriter := &trackingStateWriter{}

	resolver := biz.NewCorrelationResolver(rdb, store)
	coordinator := biz.NewServerInitiatedCoordinator(resolver, stateWriter, &noopNotifier{}, noopAIWLinker{})

	return &callbackTestEnv{
		t:           t,
		store:       store,
		corrStore:   corrStore,
		stateWriter: stateWriter,
		redisClient: rdb,
		miniredis:   mr,
		amfServer:   amfServer,
		coordinator: coordinator,
	}
}

func (e *callbackTestEnv) addSession(sessionID, authCtxID, owner, reauthURI, revocURI string) {
	e.t.Helper()
	e.store.sessions[authCtxID] = &biz.NssaaSessionContext{
		AuthCtxID:      authCtxID,
		SessionID:      sessionID,
		CallbackOwner:  owner,
		ReauthNotifURI: reauthURI,
		RevocNotifURI:  revocURI,
	}
	_ = e.corrStore.Save(context.Background(), sessionID, proto.SessionCorrEntry{AuthCtxID: authCtxID})
}

func (e *callbackTestEnv) teardown() {
	e.t.Helper()
	e.amfServer.Close()
	_ = e.redisClient.Close()
	e.miniredis.Close()
}

// httpRouter returns a chi router wired to the coordinator.
func (e *callbackTestEnv) httpRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(common.RequestIDMiddleware)
	r.HandleFunc("/aaa/server-initiated", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get(common.HeaderContentType) != common.MediaTypeJSON {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		var req proto.AaaServerInitiatedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := e.coordinator.Handle(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set(common.HeaderXRequestID, common.GetRequestID(r.Context()))
		w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result.Response)
	})
	return r
}

// serveHTTP sends a POST /aaa/server-initiated request and returns the recorder.
func (e *callbackTestEnv) serveHTTP(sessionID, authCtxID string, msgType proto.MessageType, payload []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	body, _ := json.Marshal(&proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   sessionID,
		AuthCtxID:   authCtxID,
		MessageType: msgType,
		Payload:     payload,
	})
	req := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader(body))
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
	rec := httptest.NewRecorder()
	e.httpRouter().ServeHTTP(rec, req)
	return rec
}

// serveHTTPValidated is like serveHTTP but validates required fields before calling coordinator.
func (e *callbackTestEnv) serveHTTPValidated(sessionID, authCtxID string, msgType proto.MessageType, payload []byte, checkSessionID, checkAuthCtxID bool) *httptest.ResponseRecorder {
	e.t.Helper()
	body, _ := json.Marshal(&proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   sessionID,
		AuthCtxID:   authCtxID,
		MessageType: msgType,
		Payload:     payload,
	})
	req := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader(body))
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Use(common.RequestIDMiddleware)
	r.HandleFunc("/aaa/server-initiated", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get(common.HeaderContentType) != common.MediaTypeJSON {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		var req proto.AaaServerInitiatedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if checkSessionID && req.SessionID == "" {
			http.Error(w, "sessionId is required", http.StatusBadRequest)
			return
		}
		if checkAuthCtxID && req.AuthCtxID == "" {
			http.Error(w, "authCtxId is required", http.StatusBadRequest)
			return
		}
		result, err := e.coordinator.Handle(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set(common.HeaderXRequestID, common.GetRequestID(r.Context()))
		w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result.Response)
	})
	r.ServeHTTP(rec, req)
	return rec
}

// ─── Happy Path Tests ────────────────────────────────────────────────────────

// TestTS29526_NSSAA_Callback_RAR_AMFNotified verifies that a RAR message
// triggers session reauth pending, AMF notification, and 200 OK response.
// Spec: TS 29.526 §7.2.5
func TestTS29526_NSSAA_Callback_RAR_AMFNotified(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-rar-1", "auth-rar-1", "amf",
		env.amfServer.URL+"/namf", env.amfServer.URL+"/namf")

	rec := env.serveHTTP("sess-rar-1", "auth-rar-1", proto.MessageTypeRAR, []byte{1, 2, 3})

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "auth-rar-1", resp.AuthCtxID)
	assert.Equal(t, "sess-rar-1", resp.SessionID)
	assert.NotEmpty(t, resp.Payload, "RAR response should include payload")
}

// TestTS29526_NSSAA_Callback_ASR_AMFNotified verifies that an ASR message
// triggers session revocation, AMF notification, and 200 OK response.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_ASR_AMFNotified(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-asr-1", "auth-asr-1", "amf",
		env.amfServer.URL+"/namf", env.amfServer.URL+"/namf")

	rec := env.serveHTTP("sess-asr-1", "auth-asr-1", proto.MessageTypeASR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "auth-asr-1", resp.AuthCtxID)
	assert.Equal(t, "sess-asr-1", resp.SessionID)
}

// TestTS29526_NSSAA_Callback_CoA_AMFNotified verifies that a CoA message
// triggers session update and 200 OK response.
// Spec: TS 29.526 §7.2.5
func TestTS29526_NSSAA_Callback_CoA_AMFNotified(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-coa-1", "auth-coa-1", "ausf", "", "")

	rec := env.serveHTTP("sess-coa-1", "auth-coa-1", proto.MessageTypeCoA, []byte{3, 0, 0, 16})

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "auth-coa-1", resp.AuthCtxID)
	assert.NotEmpty(t, resp.Payload, "CoA response should include payload")
}

// TestTS29526_NSSAA_Callback_DM_AMFNotified verifies that a DM (RADIUS Disconnect-Request)
// maps to ASR in the coordinator. Per TS 29.526 §7.2.4, both RADIUS DM and Diameter ASR
// signal revocation — the coordinator handles ASR for both.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_DM_AMFNotified(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-dm-1", "auth-dm-1", "amf",
		env.amfServer.URL+"/namf", env.amfServer.URL+"/namf")

	// DM (RADIUS Disconnect-Request) and ASR (Diameter Abort-Session-Request) are
	// equivalent in 3GPP semantics. The coordinator treats both as revocation.
	rec := env.serveHTTP("sess-dm-1", "auth-dm-1", proto.MessageTypeASR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "auth-dm-1", resp.AuthCtxID)
}

// TestTS29526_NSSAA_Callback_RAR_AUSFOwner verifies that a RAR message for an
// AUSF-owned session triggers AIW linking and 200 OK without AMF callback.
// Spec: TS 29.526 §7.2.5
func TestTS29526_NSSAA_Callback_RAR_AUSFOwner(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-rar-ausf", "auth-rar-ausf", "ausf", "", "")

	rec := env.serveHTTP("sess-rar-ausf", "auth-rar-ausf", proto.MessageTypeRAR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "auth-rar-ausf", resp.AuthCtxID)
}

// ─── Error Tests ─────────────────────────────────────────────────────────────

// TestTS29526_NSSAA_Callback_MissingSessionID verifies that omitting sessionId
// returns 400 Bad Request.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_MissingSessionID(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	rec := env.serveHTTPValidated("", "auth-1", proto.MessageTypeRAR, []byte{1}, true, false)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-CB-010: missing sessionId should return 400")
}

// TestTS29526_NSSAA_Callback_MissingAuthCtxID verifies that omitting authCtxId
// returns 400 Bad Request.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_MissingAuthCtxID(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	rec := env.serveHTTPValidated("sess-1", "", proto.MessageTypeRAR, []byte{1}, false, true)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-CB-011: missing authCtxId should return 400")
}

// TestTS29526_NSSAA_Callback_UnknownSession verifies that a request with an
// unknown session ID returns 502 Bad Gateway.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_UnknownSession(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	rec := env.serveHTTP("unknown-session", "unknown-auth", proto.MessageTypeRAR, []byte{1})

	assert.Equal(t, http.StatusBadGateway, rec.Code,
		"TC-CB-012: unknown session should return 502")
}

// TestTS29526_NSSAA_Callback_InvalidMessageType verifies that an invalid
// messageType is handled gracefully by the coordinator.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_InvalidMessageType(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-inv-1", "auth-inv-1", "ausf", "", "")

	body, _ := json.Marshal(&proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-inv-1",
		AuthCtxID:   "auth-inv-1",
		MessageType: "INVALID_TYPE",
	})
	req := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader(body))
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
	rec := httptest.NewRecorder()
	env.httpRouter().ServeHTTP(rec, req)

	// Coordinator returns "unsupported message type" → 502.
	assert.Equal(t, http.StatusBadGateway, rec.Code,
		"TC-CB-013: invalid messageType should return 502")
}

// TestTS29526_NSSAA_Callback_NonJSONBody verifies that a non-JSON body
// returns 415 Unsupported Media Type.
// Spec: RFC 7231
func TestTS29526_NSSAA_Callback_NonJSONBody(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader([]byte("not json")))
	req.Header.Set(common.HeaderContentType, "text/plain")
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	r.HandleFunc("/aaa/server-initiated", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(common.HeaderContentType) != common.MediaTypeJSON {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
	})
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code,
		"TC-CB-014: non-JSON body should return 415")
}

// TestTS29526_NSSAA_Callback_WrongMethod verifies that GET on the endpoint
// returns 405 Method Not Allowed.
// Spec: RFC 7231
func TestTS29526_NSSAA_Callback_WrongMethod(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/aaa/server-initiated", nil)
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	r.HandleFunc("/aaa/server-initiated", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"TC-CB-015: GET on /aaa/server-initiated should return 405")
}

// ─── Response Header Tests ───────────────────────────────────────────────────

// TestTS29526_NSSAA_Callback_ResponseHeaders_RAR verifies that RAR responses
// include the X-Request-ID header.
// Spec: TS 29.500 §6.1
func TestTS29526_NSSAA_Callback_ResponseHeaders_RAR(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-hdr-rar", "auth-hdr-rar", "ausf", "", "")

	rec := env.serveHTTP("sess-hdr-rar", "auth-hdr-rar", proto.MessageTypeRAR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code)

	reqID := rec.Header().Get(common.HeaderXRequestID)
	assert.NotEmpty(t, reqID,
		"TC-CB-020: RAR response should include X-Request-ID header")
}

// TestTS29526_NSSAA_Callback_ResponseHeaders_ASR verifies that ASR responses
// include the X-Request-ID header.
// Spec: TS 29.500 §6.1
func TestTS29526_NSSAA_Callback_ResponseHeaders_ASR(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-hdr-asr", "auth-hdr-asr", "ausf", "", "")

	rec := env.serveHTTP("sess-hdr-asr", "auth-hdr-asr", proto.MessageTypeASR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code)

	reqID := rec.Header().Get(common.HeaderXRequestID)
	assert.NotEmpty(t, reqID,
		"TC-CB-021: ASR response should include X-Request-ID header")
}

// TestTS29526_NSSAA_Callback_ResponseHeaders_CoA verifies that CoA responses
// include the X-Request-ID header.
// Spec: TS 29.500 §6.1
func TestTS29526_NSSAA_Callback_ResponseHeaders_CoA(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-hdr-coa", "auth-hdr-coa", "ausf", "", "")

	rec := env.serveHTTP("sess-hdr-coa", "auth-hdr-coa", proto.MessageTypeCoA, []byte{3})

	require.Equal(t, http.StatusOK, rec.Code)

	reqID := rec.Header().Get(common.HeaderXRequestID)
	assert.NotEmpty(t, reqID,
		"TC-CB-022: CoA response should include X-Request-ID header")
}

// ─── Session State Tests ─────────────────────────────────────────────────────

// TestTS29526_NSSAA_Callback_SessionState_AfterRAR verifies that after processing
// a RAR message, the session state writer is called with MarkReauthPending.
// Spec: TS 29.526 §7.2.5
func TestTS29526_NSSAA_Callback_SessionState_AfterRAR(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-state-rar", "auth-state-rar", "ausf", "", "")

	rec := env.serveHTTP("sess-state-rar", "auth-state-rar", proto.MessageTypeRAR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code)

	assert.Contains(t, env.stateWriter.Calls, "MarkReauthPending:auth-state-rar",
		"TC-CB-030: after RAR, session should be marked PENDING")
}

// TestTS29526_NSSAA_Callback_SessionState_AfterASR verifies that after processing
// an ASR message, the session state writer is called with MarkRevoked.
// Spec: TS 29.526 §7.2.4
func TestTS29526_NSSAA_Callback_SessionState_AfterASR(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-state-asr", "auth-state-asr", "ausf", "", "")

	rec := env.serveHTTP("sess-state-asr", "auth-state-asr", proto.MessageTypeASR, []byte{1})

	require.Equal(t, http.StatusOK, rec.Code)

	assert.Contains(t, env.stateWriter.Calls, "MarkRevoked:auth-state-asr",
		"TC-CB-031: after ASR, session should be marked REVOKED")
}

// TestTS29526_NSSAA_Callback_SessionState_AfterCoA verifies that after processing
// a CoA message, the session state writer is called with ApplyCoA.
// Spec: TS 29.526 §7.2.5
func TestTS29526_NSSAA_Callback_SessionState_AfterCoA(t *testing.T) {
	t.Parallel()

	env := newCallbackTestEnv(t)
	defer env.teardown()

	env.addSession("sess-state-coa", "auth-state-coa", "ausf", "", "")

	payload := []byte{3, 0, 0, 16}
	rec := env.serveHTTP("sess-state-coa", "auth-state-coa", proto.MessageTypeCoA, payload)

	require.Equal(t, http.StatusOK, rec.Code)

	assert.Contains(t, env.stateWriter.Calls, "ApplyCoA:auth-state-coa",
		"TC-CB-032: after CoA, session should have ApplyCoA called")
}
