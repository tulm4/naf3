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
