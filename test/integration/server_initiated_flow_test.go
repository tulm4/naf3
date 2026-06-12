package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/aaa/gateway"
	"github.com/operator/nssAAF/internal/amf"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/biz"
	redisstore "github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/prometheus/client_golang/prometheus/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inMemoryReverseSessionStore struct {
	sessions map[string]*biz.NssaaSessionContext
}

func newInMemoryReverseSessionStore() *inMemoryReverseSessionStore {
	return &inMemoryReverseSessionStore{sessions: map[string]*biz.NssaaSessionContext{}}
}

func (s *inMemoryReverseSessionStore) LoadAuthContext(_ context.Context, authCtxID string) (*biz.NssaaSessionContext, error) {
	session, ok := s.sessions[authCtxID]
	if !ok {
		return nil, assert.AnError
	}
	copySession := *session
	return &copySession, nil
}

type noopStateWriter struct{}

func (noopStateWriter) MarkReauthPending(context.Context, string) error { return nil }
func (noopStateWriter) MarkRevoked(context.Context, string) error      { return nil }
func (noopStateWriter) ApplyCoA(context.Context, string, []byte) error { return nil }

type noopAIWLinker struct{}

func (noopAIWLinker) MarkAIWLinked(context.Context, string) error { return nil }

func TestServerInitiatedFlow_Reauth_CompletesThroughBizHTTPHandler(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	var amfCallbackObserved bool
	amfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amfCallbackObserved = true
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer amfServer.Close()

	corrStore := redisstore.NewSessionCorrelationStore(rdb, proto.DefaultPayloadTTL)
	err = corrStore.Save(context.Background(), "sess-it-1", proto.SessionCorrEntry{AuthCtxID: "auth-it-1"})
	require.NoError(t, err)

	persistentStore := newInMemoryReverseSessionStore()
	persistentStore.sessions["auth-it-1"] = &biz.NssaaSessionContext{
		AuthCtxID:      "auth-it-1",
		ReauthNotifURI: amfServer.URL + "/namf-callback/v1/reauth",
		CallbackOwner:  "amf",
	}

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	pool, err := redisstore.NewPool(context.Background(), redisstore.Config{Addrs: []string{mr.Addr()}})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	dlq := redisstore.NewDLQ(pool)
	notifier := amf.NewClient(factory, cbRegistry, dlq, config.CircuitBreakerConfig{}, resilience.RetryConfig{MaxAttempts: 1})

	resolver := biz.NewCorrelationResolver(rdb, persistentStore)
	coordinator := biz.NewServerInitiatedCoordinator(resolver, noopStateWriter{}, notifier, noopAIWLinker{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get(common.HeaderContentType) != "application/json" {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}

		var req proto.AaaServerInitiatedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := coordinator.Handle(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result.Response)
	}

	router := chi.NewRouter()
	router.Use(common.RequestIDMiddleware)
	router.HandleFunc("/aaa/server-initiated", handler)

	reqBody := []byte(`{
		"v":"1.0",
		"sessionId":"sess-it-1",
		"authCtxId":"auth-it-1",
		"transportType":"RADIUS",
		"messageType":"RAR",
		"payload":"AQID"
	}`)
	request := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader(reqBody))
	request.Header.Set(common.HeaderContentType, "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())
	assert.True(t, amfCallbackObserved, "expected AMF callback to be observed")
	assert.True(t, strings.Contains(recorder.Body.String(), "auth-it-1"), "response body missing auth context: %s", recorder.Body.String())
	assert.NotEmpty(t, recorder.Header().Get(common.HeaderXRequestID))
}

func TestServerInitiatedFlow_Reauth_RecordsCompletionMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.ServerInitiatedCompletions.WithLabelValues(string(proto.MessageTypeRAR), string(biz.CompletionAMFNotified)))
	TestServerInitiatedFlow_Reauth_CompletesThroughBizHTTPHandler(t)
	after := testutil.ToFloat64(metrics.ServerInitiatedCompletions.WithLabelValues(string(proto.MessageTypeRAR), string(biz.CompletionAMFNotified)))
	require.Equal(t, before+1, after)
}

// setupServerInitiatedTest sets up the common infrastructure for server-initiated tests.
// Returns the handler function, recorder, and teardown function.
func setupServerInitiatedTest(t *testing.T, messageType proto.MessageType, sessionID, authCtxID string, payload []byte) (*httptest.ResponseRecorder, func()) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	corrStore := redisstore.NewSessionCorrelationStore(rdb, proto.DefaultPayloadTTL)
	err = corrStore.Save(context.Background(), sessionID, proto.SessionCorrEntry{AuthCtxID: authCtxID})
	require.NoError(t, err)

	persistentStore := newInMemoryReverseSessionStore()
	persistentStore.sessions[authCtxID] = &biz.NssaaSessionContext{
		AuthCtxID:     authCtxID,
		CallbackOwner: "ausf", // Use ausf owner to avoid AMF dependency
	}

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	pool, err := redisstore.NewPool(context.Background(), redisstore.Config{Addrs: []string{mr.Addr()}})
	require.NoError(t, err)
	dlq := redisstore.NewDLQ(pool)
	notifier := amf.NewClient(factory, cbRegistry, dlq, config.CircuitBreakerConfig{}, resilience.RetryConfig{MaxAttempts: 1})

	resolver := biz.NewCorrelationResolver(rdb, persistentStore)
	coordinator := biz.NewServerInitiatedCoordinator(resolver, noopStateWriter{}, notifier, noopAIWLinker{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get(common.HeaderContentType) != "application/json" {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}

		var req proto.AaaServerInitiatedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := coordinator.Handle(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result.Response)
	}

	router := chi.NewRouter()
	router.Use(common.RequestIDMiddleware)
	router.HandleFunc("/aaa/server-initiated", handler)

	reqBody, _ := json.Marshal(&proto.AaaServerInitiatedRequest{
		Version:       "1.0",
		SessionID:     sessionID,
		AuthCtxID:     authCtxID,
		TransportType: proto.TransportRADIUS,
		MessageType:   messageType,
		Payload:       payload,
	})
	request := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader(reqBody))
	request.Header.Set(common.HeaderContentType, "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	return recorder, func() {
		mr.Close()
		_ = rdb.Close()
		_ = pool.Close()
	}
}

// TestServerInitiatedFlow_CoA_BizPodSuccess verifies that a CoA request is processed
// successfully through the Biz HTTP handler and returns the expected response.
func TestServerInitiatedFlow_CoA_BizPodSuccess(t *testing.T) {
	sessionID := "sess-coa-1"
	authCtxID := "auth-coa-1"
	payload := []byte{3, 0, 0, 16}

	recorder, teardown := setupServerInitiatedTest(t, proto.MessageTypeCoA, sessionID, authCtxID, payload)
	defer teardown()

	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, authCtxID, resp.AuthCtxID)
	assert.Equal(t, sessionID, resp.SessionID)
	assert.NotEmpty(t, resp.Payload, "CoA response should include payload")
}

// TestServerInitiatedFlow_ASR_BizPodSuccess verifies that an ASR request is processed
// successfully through the Biz HTTP handler and returns the expected response.
func TestServerInitiatedFlow_ASR_BizPodSuccess(t *testing.T) {
	sessionID := "sess-asr-1"
	authCtxID := "auth-asr-1"
	payload := []byte{1}

	recorder, teardown := setupServerInitiatedTest(t, proto.MessageTypeASR, sessionID, authCtxID, payload)
	defer teardown()

	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	var resp proto.AaaServerInitiatedResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, authCtxID, resp.AuthCtxID)
	assert.Equal(t, sessionID, resp.SessionID)
	assert.NotEmpty(t, resp.Payload, "ASR response should include payload")
}

// TestServerInitiatedFlow_CoA_Timeout verifies that when no response is received
// within the timeout period, the registry returns an UNABLE_TO_DELIVER response.
func TestServerInitiatedFlow_CoA_Timeout(t *testing.T) {
	// This test verifies the registry timeout behavior directly.
	// In a real flow, this would be triggered when the Biz Pod fails to respond
	// within the expected time window.
	registry := gateway.NewServerInitiatedRegistry(50 * time.Millisecond)

	// Register a pending request with short timeout
	ch, err := registry.Register("test-session-coa", "test-auth", "COA", 50*time.Millisecond)
	require.NoError(t, err)

	// Wait should return timeout response
	resp := ch.Wait()
	require.Equal(t, uint32(3002), resp.ResultCode, "expected UNABLE_TO_DELIVER on timeout")
	require.Equal(t, "timeout", resp.ErrorCause)
}

// TestServerInitiatedFlow_Registry_DuplicateRequest returns UNABLE_TO_DELIVER
// when a duplicate session+messageType is registered.
func TestServerInitiatedFlow_Registry_DuplicateRequest(t *testing.T) {
	registry := gateway.NewServerInitiatedRegistry(5 * time.Second)

	// First registration should succeed
	ch1, err := registry.Register("dup-session", "dup-auth", "COA", 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, ch1)

	// Second registration for same session+type should return duplicate response
	ch2, err := registry.Register("dup-session", "dup-auth", "COA", 5*time.Second)
	require.NoError(t, err)

	// Channel 2 should receive duplicate response asynchronously
	resp := ch2.Wait()
	require.Equal(t, uint32(3002), resp.ResultCode)
	require.Equal(t, "duplicate", resp.ErrorCause)

	// Channel 1 should still work
	registry.Complete("dup-session", "COA", &gateway.ServerInitiatedResponse{
		AuthCtxID:  "dup-auth",
		ResultCode: 0,
	})
}

// TestServerInitiatedFlow_CoA_RecordsCompletionMetric verifies that CoA completions
// are recorded in the metrics.
func TestServerInitiatedFlow_CoA_RecordsCompletionMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.ServerInitiatedCompletions.WithLabelValues(string(proto.MessageTypeCoA), string(biz.CompletionStateOnly)))
	TestServerInitiatedFlow_CoA_BizPodSuccess(t)
	after := testutil.ToFloat64(metrics.ServerInitiatedCompletions.WithLabelValues(string(proto.MessageTypeCoA), string(biz.CompletionStateOnly)))
	require.Equal(t, before+1, after)
}

// TestServerInitiatedFlow_ASR_RecordsCompletionMetric verifies that ASR completions
// are recorded in the metrics.
func TestServerInitiatedFlow_ASR_RecordsCompletionMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.ServerInitiatedCompletions.WithLabelValues(string(proto.MessageTypeASR), string(biz.CompletionStateOnly)))
	TestServerInitiatedFlow_ASR_BizPodSuccess(t)
	after := testutil.ToFloat64(metrics.ServerInitiatedCompletions.WithLabelValues(string(proto.MessageTypeASR), string(biz.CompletionStateOnly)))
	require.Equal(t, before+1, after)
}
