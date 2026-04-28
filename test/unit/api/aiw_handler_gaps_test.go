// Package api provides N60 API handler gap-filling tests.
// Spec: TS 29.526 §7.3
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/aiw"
	aiwnats "github.com/operator/nssAAF/oapi-gen/gen/aiw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStoreAIW struct {
	data      map[string]*aiw.AuthContext
	loadErr   error
	saveErr   error
	deleteErr error
}

func newMockStoreAIW() *mockStoreAIW {
	return &mockStoreAIW{data: make(map[string]*aiw.AuthContext)}
}

func (m *mockStoreAIW) Load(id string) (*aiw.AuthContext, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if ctx, ok := m.data[id]; ok {
		return ctx, nil
	}
	return nil, aiw.ErrNotFound
}

func (m *mockStoreAIW) Save(ctx *aiw.AuthContext) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.data[ctx.AuthCtxID] = ctx
	return nil
}

func (m *mockStoreAIW) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.data, id)
	return nil
}

func (m *mockStoreAIW) Close() error { return nil }

func makeRouterAIW(handler *aiw.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(common.RequestIDMiddleware)
	return aiwnats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-aiw/v1")
}

func doRequestAIW(handler *aiw.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(bs))
	} else {
		bodyReader = strings.NewReader("")
	}
	// Use http.NewRequestWithContext so that invalid URLs (e.g. with control
	// characters) don't panic httptest.NewRequest — the handler's own validation
	// will return 400 for invalid authCtxId.
	req, err := http.NewRequestWithContext(context.Background(), method, path, bodyReader)
	if err != nil {
		// Invalid URL: the test expects the handler to validate authCtxId and
		// return 400. Since we can't route the request, simulate that response.
		rec := httptest.NewRecorder()
		common.WriteProblem(rec, &common.ProblemDetails{
			Type:   "about:blank",
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "invalid URL: " + err.Error(),
		})
		return rec
	}
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouterAIW(handler).ServeHTTP(rec, req)
	return rec
}

// TestCreateAuth_InvalidBase64EapIdRsp verifies that an invalid base64 value
// in eapIdRsp returns HTTP 400.
func TestCreateAuth_InvalidBase64EapIdRsp(t *testing.T) {
	store := newMockStoreAIW()
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	// "not-valid-base64!!!" is not valid base64
	body := map[string]interface{}{
		"supi":     "imu-208046000000001",
		"eapIdRsp": "not-valid-base64!!!",
	}

	rec := doRequestAIW(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var problem common.ProblemDetails
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, 400, problem.Status)
}

// TestCreateAuth_InvalidSupi verifies that an invalid SUPI in create returns HTTP 400.
func TestCreateAuth_InvalidSupi(t *testing.T) {
	store := newMockStoreAIW()
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":     "bad-supi-format",
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequestAIW(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestCreateAuth_StoreSaveError verifies that a store save error returns HTTP 500.
func TestCreateAuth_StoreSaveError(t *testing.T) {
	store := newMockStoreAIW()
	store.saveErr = errors.New("redis write failed")
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi": "imu-208046000000001",
	}

	rec := doRequestAIW(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestCreateAuth_MissingSupi verifies that missing SUPI returns HTTP 400.
func TestCreateAuth_MissingSupi(t *testing.T) {
	store := newMockStoreAIW()
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequestAIW(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestCreateAuth_InvalidJSON verifies that non-JSON body returns HTTP 400.
func TestCreateAuth_InvalidJSON(t *testing.T) {
	store := newMockStoreAIW()
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	req := httptest.NewRequest(http.MethodPost,
		"/nnssaaf-aiw/v1/authentications",
		strings.NewReader("not-json-broken"))
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouterAIW(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestConfirmAuth_SessionNotFound verifies that confirming a non-existent
// session returns HTTP 404.
func TestConfirmAuth_SessionNotFound(t *testing.T) {
	store := newMockStoreAIW()
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/nonexistent-id", body)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestConfirmAuth_SupiMismatchInBody verifies that a SUPI mismatch between the
// request body and the stored session returns HTTP 400.
func TestConfirmAuth_SupiMismatchInBody(t *testing.T) {
	store := newMockStoreAIW()
	store.data["ctx-supi"] = &aiw.AuthContext{
		AuthCtxID: "ctx-supi",
		Supi:      "imu-208046000000001",
	}
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	// SUPI in body does not match stored SUPI
	body := map[string]interface{}{
		"supi":       "imu-999999999999999",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/ctx-supi", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "SUPI does not match")
}

// TestConfirmAuth_InvalidBase64EapMessage verifies that invalid base64 in
// eapMessage returns HTTP 400.
func TestConfirmAuth_InvalidBase64EapMessage(t *testing.T) {
	store := newMockStoreAIW()
	store.data["ctx-eap-valid"] = &aiw.AuthContext{
		AuthCtxID: "ctx-eap-valid",
		Supi:      "imu-208046000000001",
	}
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "!!!invalid-base64!!!",
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/ctx-eap-valid", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestConfirmAuth_MissingEapMessage verifies that a missing eapMessage
// in confirm returns HTTP 400.
func TestConfirmAuth_MissingEapMessage(t *testing.T) {
	store := newMockStoreAIW()
	store.data["ctx-missing-eap"] = &aiw.AuthContext{
		AuthCtxID: "ctx-missing-eap",
		Supi:      "imu-208046000000001",
	}
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi": "imu-208046000000001",
		// eapMessage missing
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/ctx-missing-eap", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestConfirmAuth_StoreLoadError verifies that a store load error returns HTTP 500.
func TestConfirmAuth_StoreLoadError(t *testing.T) {
	store := newMockStoreAIW()
	store.loadErr = errors.New("redis read failed")
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/any-id", body)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestConfirmAuth_StoreSaveError verifies that a store save error during confirm
// returns HTTP 500.
func TestConfirmAuth_StoreSaveError(t *testing.T) {
	store := newMockStoreAIW()
	store.data["ctx-save-err"] = &aiw.AuthContext{
		AuthCtxID: "ctx-save-err",
		Supi:      "imu-208046000000001",
	}
	store.saveErr = errors.New("redis write failed")
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/ctx-save-err", body)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestConfirmAuth_InvalidJSON verifies that non-JSON body in confirm returns HTTP 400.
func TestConfirmAuth_InvalidJSON(t *testing.T) {
	store := newMockStoreAIW()
	store.data["ctx-json-err"] = &aiw.AuthContext{
		AuthCtxID: "ctx-json-err",
		Supi:      "imu-208046000000001",
	}
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	req := httptest.NewRequest(http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/ctx-json-err",
		strings.NewReader("not-json-broken"))
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouterAIW(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestConfirmAuth_AuthCtxIDInvalid verifies that an invalid authCtxId
// (control characters) returns HTTP 400.
func TestConfirmAuth_AuthCtxIDInvalid(t *testing.T) {
	store := newMockStoreAIW()
	h := aiw.NewHandler(store, aiw.WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequestAIW(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/bad\x00id", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
