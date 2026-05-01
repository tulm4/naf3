package nssaa

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/common"
	nssaanats "github.com/operator/nssAAF/oapi-gen/gen/nssaa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStore struct {
	data      map[string]*AuthCtx
	loadErr   error
	saveErr   error
	deleteErr error
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string]*AuthCtx)}
}

func (m *mockStore) Load(id string) (*AuthCtx, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if ctx, ok := m.data[id]; ok {
		return ctx, nil
	}
	return nil, ErrNotFound
}

func (m *mockStore) Save(ctx *AuthCtx) error {
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
	return nssaanats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-nssaa/v1")
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

// ─── CreateSliceAuthenticationContext tests ─────────────────────────────────

func TestCreateSliceAuthenticationContext_OK(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(common.HeaderLocation))
	assert.Contains(t, rec.Header().Get(common.HeaderLocation), "/slice-authentications/")
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var resp nssaanats.SliceAuthContext
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "520804600000001", string(resp.Gpsi))
	assert.Equal(t, uint8(1), resp.Snssai.Sst)
	assert.Equal(t, "000001", resp.Snssai.Sd)
	assert.NotEmpty(t, resp.AuthCtxId)
	assert.NotNil(t, resp.EapMessage)

	require.Len(t, store.data, 1)
	for _, ctx := range store.data {
		assert.Equal(t, "520804600000001", ctx.GPSI)
		assert.Equal(t, uint8(1), ctx.SnssaiSST)
		assert.Equal(t, "000001", ctx.SnssaiSD)
	}
}

func TestCreateSliceAuthenticationContext_WithoutSD(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 128},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

	require.Equal(t, http.StatusCreated, rec.Code)
	var resp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, uint8(128), resp.Snssai.Sst)
	assert.Empty(t, resp.Snssai.Sd)
}

func TestCreateSliceAuthenticationContext_InvalidGPSI(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	// Empty GPSI is invalid per TS 29.571 §5.2.2
	body := map[string]interface{}{
		"gpsi":     "",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var problem common.ProblemDetails
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, 400, problem.Status)
	assert.Contains(t, problem.Detail, "gpsi")
}

func TestCreateSliceAuthenticationContext_InvalidSST(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 300},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSliceAuthenticationContext_InvalidSD(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "GGGGGG"},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSliceAuthenticationContext_MissingEapIdRsp(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":   "520804600000001",
		"snssai": map[string]interface{}{"sst": 1},
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSliceAuthenticationContext_EmptyEapIdRsp(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSliceAuthenticationContext_StoreSaveError(t *testing.T) {
	store := newMockStore()
	store.saveErr = errors.New("store write failed")
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCreateSliceAuthenticationContext_InvalidJSON(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	req := httptest.NewRequest(http.MethodPost,
		"/nnssaaf-nssaa/v1/slice-authentications",
		strings.NewReader("not-json{"))
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouter(h).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSliceAuthenticationContext_GPSIWithDash(t *testing.T) {
	// The common GPSI validator (TS 29.571 §5.2.2) uses ^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$
	// which accepts MSISDN-based, External Identifier-based, and catch-all formats.
	// GPSI "52080460000001" is valid as catch-all.
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":     "52080460000001",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusCreated, rec.Code)
}

// ─── ConfirmSliceAuthentication tests ───────────────────────────────────────

func TestConfirmSliceAuthentication_OK(t *testing.T) {
	store := newMockStore()
	store.data["test-auth-ctx-001"] = &AuthCtx{
		AuthCtxID: "test-auth-ctx-001",
		GPSI:      "520804600000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/test-auth-ctx-001", body)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var resp nssaanats.SliceAuthConfirmationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "520804600000001", string(resp.Gpsi))
	assert.NotNil(t, resp.EapMessage)
	assert.Nil(t, resp.AuthResult)
}

func TestConfirmSliceAuthentication_NotFound(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/nonexistent-id", body)

	require.Equal(t, http.StatusNotFound, rec.Code)
	var problem common.ProblemDetails
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, 404, problem.Status)
}

func TestConfirmSliceAuthentication_GPSIMismatch(t *testing.T) {
	store := newMockStore()
	store.data["test-auth-ctx-002"] = &AuthCtx{
		AuthCtxID: "test-auth-ctx-002",
		GPSI:      "520804600000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":       "599999999999999",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/test-auth-ctx-002", body)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var problem common.ProblemDetails
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Contains(t, problem.Detail, "GPSI does not match")
}

func TestConfirmSliceAuthentication_InvalidGPSI(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	// Empty GPSI is invalid per TS 29.571 §5.2.2
	body := map[string]interface{}{
		"gpsi":       "",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/test-ctx", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfirmSliceAuthentication_MissingEapMessage(t *testing.T) {
	store := newMockStore()
	store.data["ctx-003"] = &AuthCtx{AuthCtxID: "ctx-003", GPSI: "520804600000001"}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":   "520804600000001",
		"snssai": map[string]interface{}{"sst": 1},
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/ctx-003", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfirmSliceAuthentication_EmptyEapMessage(t *testing.T) {
	store := newMockStore()
	store.data["ctx-004"] = &AuthCtx{AuthCtxID: "ctx-004", GPSI: "520804600000001"}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/ctx-004", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfirmSliceAuthentication_StoreLoadError(t *testing.T) {
	store := newMockStore()
	store.loadErr = errors.New("store read failed")
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	body := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}

	rec := doRequest(h, http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/any-id", body)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestConfirmSliceAuthentication_InvalidJSON(t *testing.T) {
	store := newMockStore()
	store.data["ctx-005"] = &AuthCtx{AuthCtxID: "ctx-005", GPSI: "520804600000001"}
	h := NewHandler(store, WithAPIRoot("https://nssAAF.example.com"))

	req := httptest.NewRequest(http.MethodPut,
		"/nnssaaf-nssaa/v1/slice-authentications/ctx-005",
		strings.NewReader("not-json"))
	req.Header.Set(common.HeaderXRequestID, "test-req-id")
	req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	rec := httptest.NewRecorder()
	makeRouter(h).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// ─── InMemoryStore tests ─────────────────────────────────────────────────────

func TestInMemoryStore(t *testing.T) {
	store := NewInMemoryStore()

	ctx := &AuthCtx{AuthCtxID: "id-001", GPSI: "520804600000001"}
	require.NoError(t, store.Save(ctx))

	loaded, err := store.Load("id-001")
	require.NoError(t, err)
	assert.Equal(t, "520804600000001", loaded.GPSI)

	_, err = store.Load("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, store.Delete("id-001"))
	_, err = store.Load("id-001")
	assert.ErrorIs(t, err, ErrNotFound)

	assert.NoError(t, store.Close())
}

// ─── Interface checks ─────────────────────────────────────────────────────

func TestHandler_ImplementsServerInterface(t *testing.T) {
	var _ nssaanats.ServerInterface = (*Handler)(nil)
}
