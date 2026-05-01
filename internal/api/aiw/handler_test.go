package aiw

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/common"
	aiwnats "github.com/operator/nssAAF/oapi-gen/gen/aiw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStore struct {
	data      map[string]*AuthContext
	loadErr   error
	saveErr   error
	deleteErr error
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string]*AuthContext)}
}

func (m *mockStore) Load(id string) (*AuthContext, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if ctx, ok := m.data[id]; ok {
		return ctx, nil
	}
	return nil, ErrNotFound
}

func (m *mockStore) Save(ctx *AuthContext) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.data[ctx.AuthCtxID] = ctx
	return nil
}

func (m *mockStore) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.data, id)
	return nil
}

func (m *mockStore) Close() error {
	return nil
}

func makeRouter(handler *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(common.RequestIDMiddleware)
	return aiwnats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-aiw/v1")
}

func doRequest(handler *Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(bs))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouter(handler).ServeHTTP(rec, req)
	return rec
}

// ─── CreateAuthenticationContext tests ────────────────────────────────────────

func TestCreateAuthenticationContext_OK(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":     "imsi-208046000000001",
		"eapIdRsp": "dXNlckBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(common.HeaderLocation))
	assert.Contains(t, rec.Header().Get(common.HeaderLocation), "/authentications/")
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var resp aiwnats.AuthContext
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "imsi-208046000000001", string(resp.Supi))
	assert.NotEmpty(t, resp.AuthCtxId)
}

func TestCreateAuthenticationContext_WithOptionalFields(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":                     "imsi-208046000000001",
		"eapIdRsp":                 "dXNlckBleGFtcGxlLmNvbQ==",
		"ttlsInnerMethodContainer": "aGVsbG8=", // base64 "hello"
		"supportedFeatures":        "a1b2c3",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp aiwnats.AuthContext
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.EapMessage)
}

func TestCreateAuthenticationContext_InvalidSupi(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":     "invalid-supi",
		"eapIdRsp": "dXNlckBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var problem common.ProblemDetails
	err := json.Unmarshal(rec.Body.Bytes(), &problem)
	require.NoError(t, err)
	assert.Equal(t, 400, problem.Status)
	assert.Contains(t, problem.Detail, "supi")
}

func TestCreateAuthenticationContext_StoreSaveError(t *testing.T) {
	store := newMockStore()
	store.saveErr = errors.New("write failed")
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi": "imsi-208046000000001",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCreateAuthenticationContext_InvalidJSON(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	req := httptest.NewRequest(http.MethodPost,
		"/nnssaaf-aiw/v1/authentications",
		strings.NewReader("broken json"))
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouter(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ─── ConfirmAuthentication tests ────────────────────────────────────────────────

func TestConfirmAuthentication_OK(t *testing.T) {
	store := newMockStore()
	store.data["auth-ctx-001"] = &AuthContext{
		AuthCtxID: "auth-ctx-001",
		Supi:      "imsi-208046000000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imsi-208046000000001",
		"eapMessage": "dGVzdA==", // base64 "test"
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/auth-ctx-001", body)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var resp aiwnats.AuthConfirmationResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "imsi-208046000000001", string(resp.Supi))
	assert.NotNil(t, resp.EapMessage)
	assert.Nil(t, resp.AuthResult) // Phase 1: continue (null)
}

func TestConfirmAuthentication_NotFound(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imsi-208046000000001",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/nonexistent-id", body)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestConfirmAuthentication_SupiMismatch(t *testing.T) {
	store := newMockStore()
	store.data["auth-ctx-002"] = &AuthContext{
		AuthCtxID: "auth-ctx-002",
		Supi:      "imsi-208046000000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imsi-999999999999999", // different SUPI
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/auth-ctx-002", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "SUPI does not match")
}

func TestConfirmAuthentication_InvalidSupi(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "bad-supi",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/any-id", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfirmAuthentication_MissingEapMessage(t *testing.T) {
	store := newMockStore()
	store.data["auth-ctx-003"] = &AuthContext{
		AuthCtxID: "auth-ctx-003",
		Supi:      "imsi-208046000000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi": "imsi-208046000000001",
		// eapMessage missing
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/auth-ctx-003", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfirmAuthentication_EmptyEapMessage(t *testing.T) {
	store := newMockStore()
	store.data["auth-ctx-004"] = &AuthContext{
		AuthCtxID: "auth-ctx-004",
		Supi:      "imsi-208046000000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imsi-208046000000001",
		"eapMessage": "",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/auth-ctx-004", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfirmAuthentication_StoreLoadError(t *testing.T) {
	store := newMockStore()
	store.loadErr = errors.New("store read failed")
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"supi":       "imsi-208046000000001",
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/any-id", body)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestConfirmAuthentication_InvalidJSON(t *testing.T) {
	store := newMockStore()
	store.data["auth-ctx-005"] = &AuthContext{
		AuthCtxID: "auth-ctx-005",
		Supi:      "imsi-208046000000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	req := httptest.NewRequest(http.MethodPut,
		"/nnssaaf-aiw/v1/authentications/auth-ctx-005",
		strings.NewReader("not-json"))
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouter(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ─── InMemoryStore tests ───────────────────────────────────────────────────

func TestInMemoryStore(t *testing.T) {
	store := NewInMemoryStore()

	ctx := &AuthContext{AuthCtxID: "id-001", Supi: "imsi-208046000000001"}
	err := store.Save(ctx)
	require.NoError(t, err)

	loaded, err := store.Load("id-001")
	require.NoError(t, err)
	assert.Equal(t, "imsi-208046000000001", loaded.Supi)

	_, err = store.Load("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)

	err = store.Delete("id-001")
	require.NoError(t, err)
	_, err = store.Load("id-001")
	assert.ErrorIs(t, err, ErrNotFound)

	assert.NoError(t, store.Close())
}

// ─── Handler implements ServerInterface ────────────────────────────────────

func TestHandler_ImplementsServerInterface(t *testing.T) {
	// Compile-time check: Handler must implement aiwnats.ServerInterface.
	var _ aiwnats.ServerInterface = (*Handler)(nil)
}
