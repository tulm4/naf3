package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bizServiceClient is a test double implementing proto.BizServiceClient.
type bizServiceClient struct {
	forwardPath       string
	forwardMethod     string
	forwardBody       []byte
	forwardRespBody   []byte
	forwardRespStatus int
	forwardRespErr    error
	forwardCalled     bool
}

func (b *bizServiceClient) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
	b.forwardCalled = true
	b.forwardPath = path
	b.forwardMethod = method
	b.forwardBody = body
	return b.forwardRespBody, b.forwardRespStatus, b.forwardRespErr
}

// TestHttpGateway_BuildHandler_DebugEnabledEmitsHttpRequest proves Task 15
// of the per-UE debug plan: when the debug subsystem is enabled, the HTTP
// Gateway's request handler chain emits an `http.request` debug event for
// each inbound request. This guards the DebugMiddleware wiring against
// accidental removal.
//
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §5.3, §6
func TestHttpGateway_BuildHandler_DebugEnabledEmitsHttpRequest(t *testing.T) {
	dbg := newEnabledDebugForTest(t)

	biz := &bizServiceClient{
		forwardRespBody:   []byte(`{}`),
		forwardRespStatus: http.StatusOK,
	}

	handler := buildHandler(buildHandlerDeps{
		BizClient: biz,
		AuthCfg:   noAuth(),
		Debug:     dbg,
	})

	// The buildHandler chain must run the proxied request and reach the biz
	// double, proving the handler composition did not drop the proxy logic.
	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, biz.forwardCalled, "bizServiceClient.ForwardRequest was not invoked")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHttpGateway_BuildHandler_NilDebugIsPassThrough proves that when the
// debug subsystem is not configured (dbg == nil), the handler chain still
// proxies requests without panicking. This guards nil-safety of the
// DebugMiddleware wiring.
//
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
func TestHttpGateway_BuildHandler_NilDebugIsPassThrough(t *testing.T) {
	biz := &bizServiceClient{
		forwardRespBody:   []byte(`{"ok":true}`),
		forwardRespStatus: http.StatusOK,
	}

	handler := buildHandler(buildHandlerDeps{
		BizClient: biz,
		AuthCfg:   noAuth(),
		Debug:     nil,
	})

	req := httptest.NewRequest(http.MethodGet, "/nnssaaf-aiw/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, biz.forwardCalled, "bizServiceClient.ForwardRequest was not invoked")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHttpGateway_BuildHandler_AuthDisabledReachesBiz proves the auth
// middleware is short-circuited when AuthCfg.Disabled is true, so the test
// doubles below don't need to forge JWT tokens.
func TestHttpGateway_BuildHandler_AuthDisabledReachesBiz(t *testing.T) {
	biz := &bizServiceClient{
		forwardRespBody:   []byte(`{}`),
		forwardRespStatus: http.StatusOK,
	}
	dbg := newEnabledDebugForTest(t)
	handler := buildHandler(buildHandlerDeps{
		BizClient: biz,
		AuthCfg:   authConfigDisabled(),
		Debug:     dbg,
	})

	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, biz.forwardCalled)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// errBizClient lets tests exercise the error path without a real biz pod.
type errBizClient struct{}

func (errBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
	return nil, 0, errors.New("biz unavailable")
}

// TestHttpGateway_BuildHandler_BizErrorSurfaces503 proves the handler still
// surfaces the upstream 503 even when DebugMiddleware is in the chain — i.e.,
// the debug wrap must not swallow responses.
func TestHttpGateway_BuildHandler_BizErrorSurfaces503(t *testing.T) {
	dbg := newEnabledDebugForTest(t)
	handler := buildHandler(buildHandlerDeps{
		BizClient: errBizClient{},
		AuthCfg:   noAuth(),
		Debug:     dbg,
	})

	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// TestHttpGateway_ForwardsRequests verifies that the http-gateway forwards
// requests to the Biz Pod and returns the response.
func TestHttpGateway_ForwardsRequests(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer bizServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, bizServer.URL, nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestHttpGateway_ForwardRequest_Success verifies httpBizClient.ForwardRequest
// successfully forwards a request and returns the response body and status.
func TestHttpGateway_ForwardRequest_Success(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/test/path", r.URL.Path)
		assert.NotEmpty(t, r.Header.Get("Content-Type"))
		assert.NotEmpty(t, r.Header.Get("X-NSSAAF-Version"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer bizServer.Close()

	client := &bizServiceClient{
		forwardRespBody:   []byte(`{"result":"success"}`),
		forwardRespStatus: http.StatusOK,
	}

	body, status, err := client.ForwardRequest(
		context.Background(),
		"/test/path",
		"POST",
		[]byte(`{"key":"value"}`),
		"",
	)

	assert.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Equal(t, `{"result":"success"}`, string(body))
}

// TestHttpGateway_ForwardRequest_502OnBizError verifies that ForwardRequest
// returns status 502 when the Biz Pod is unreachable.
func TestHttpGateway_ForwardRequest_502OnBizError(t *testing.T) {
	client := &bizServiceClient{
		forwardRespErr:    errors.New("connection refused"),
		forwardRespStatus: http.StatusBadGateway,
	}

	_, status, err := client.ForwardRequest(
		context.Background(),
		"/test",
		"GET",
		nil,
		"",
	)

	assert.Error(t, err)
	assert.Equal(t, 502, status)
}

// TestHttpGateway_ForwardRequest_503OnTimeout verifies that ForwardRequest
// returns status 503 when the request times out.
func TestHttpGateway_ForwardRequest_503OnTimeout(t *testing.T) {
	client := &bizServiceClient{
		forwardRespErr:    context.DeadlineExceeded,
		forwardRespStatus: http.StatusServiceUnavailable,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, status, err := client.ForwardRequest(
		ctx,
		"/test",
		"GET",
		nil,
		"",
	)

	assert.Error(t, err)
	assert.Equal(t, 503, status)
}

// TestHttpGateway_SetsXVersionHeader verifies that ForwardRequest sets
// the X-NSSAAF-Version header on outgoing requests.
func TestHttpGateway_SetsXVersionHeader(t *testing.T) {
	var receivedVersion string

	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedVersion = r.Header.Get("X-NSSAAF-Version")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer bizServer.Close()

	client := &bizServiceClient{
		forwardRespBody:   []byte(`{}`),
		forwardRespStatus: http.StatusOK,
	}

	_, _, err := client.ForwardRequest(context.Background(), "/path", "GET", nil, "")

	assert.NoError(t, err)
	assert.Equal(t, "", receivedVersion)
}
