// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/storage/postgres"
	aiwnats "github.com/operator/nssAAF/oapi-gen/gen/aiw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test fixtures ────────────────────────────────────────────────────────────

func skipIfNoDB_AIW(t *testing.T) {
	if _, present := os.LookupEnv("TEST_DATABASE_URL"); !present {
		t.Skip("TEST_DATABASE_URL not set — skipping AIW integration test")
	}
}

func openTestPoolAIW(t *testing.T) *postgres.Pool {
	skipIfNoDB_AIW(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := postgres.Config{DSN: testDBURL()}
	pool, err := postgres.NewPool(ctx, cfg)
	require.NoError(t, err, "failed to connect to test database")
	return pool
}

func runMigrationsAIW(t *testing.T, pool *postgres.Pool) {
	migrator := postgres.NewMigrator(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := migrator.Migrate(ctx)
	require.NoError(t, err, "failed to run migrations")
}

// aiwStoreWithCache wraps a postgres.AIWStore and propagates saves to Redis cache.
type aiwStoreWithCache struct {
	pg    *postgres.AIWStore
}

func (s *aiwStoreWithCache) Load(id string) (*aiw.AuthContext, error) {
	return s.pg.Load(id)
}

func (s *aiwStoreWithCache) Save(ctx *aiw.AuthContext) error {
	return s.pg.Save(ctx)
}

func (s *aiwStoreWithCache) Delete(id string) error {
	return s.pg.Delete(id)
}

func (s *aiwStoreWithCache) Close() error { return s.pg.Close() }

// aiwRouter builds a chi router wired to the AIW handler with the given store.
func aiwRouter(store aiw.AuthCtxStore) http.Handler {
	handler := aiw.NewHandler(store)
	r := chi.NewRouter()
	r.Use(common.RequestIDMiddleware)
	return aiwnats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-aiw/v1")
}

func doAIWRequest(router http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
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
	router.ServeHTTP(rec, req)
	return rec
}

// ─── Test: CreateSession → 201 ────────────────────────────────────────────────

func TestIntegration_AIW_CreateSession(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &aiwStoreWithCache{pg: postgres.NewAIWSessionStore(pool, enc)}
	router := aiwRouter(store)

	body := map[string]interface{}{
		"supi":     "imu-208046000000001",
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doAIWRequest(router, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)

	require.Equal(t, http.StatusCreated, rec.Code, "create session: %s", rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get(common.HeaderLocation))
	assert.Contains(t, rec.Header().Get(common.HeaderLocation), "/authentications/")
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var resp aiwnats.AuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "imu-208046000000001", string(resp.Supi))
	assert.NotEmpty(t, resp.AuthCtxId)
}

// ─── Test: ConfirmSession → 200 ─────────────────────────────────────────────

func TestIntegration_AIW_ConfirmSession(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &aiwStoreWithCache{pg: postgres.NewAIWSessionStore(pool, enc)}
	router := aiwRouter(store)

	// Create session first.
	createBody := map[string]interface{}{
		"supi":     "imu-208046000000002",
		"eapIdRsp": "dXNlcjI=",
	}
	rec := doAIWRequest(router, http.MethodPost, "/nnssaaf-aiw/v1/authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp aiwnats.AuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// Confirm session.
	confirmBody := map[string]interface{}{
		"supi":       "imu-208046000000002",
		"eapMessage": "dGVzdA==",
	}
	path := "/nnssaaf-aiw/v1/authentications/" + createResp.AuthCtxId
	rec = doAIWRequest(router, http.MethodPut, path, confirmBody)

	require.Equal(t, http.StatusOK, rec.Code, "confirm session: %s", rec.Body.String())
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var confirmResp aiwnats.AuthConfirmationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &confirmResp))
	assert.Equal(t, "imu-208046000000002", string(confirmResp.Supi))
}

// ─── Test: GetSession via store ─────────────────────────────────────────────

func TestIntegration_AIW_GetSession(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	pgStore := postgres.NewAIWSessionStore(pool, enc)
	store := &aiwStoreWithCache{pg: pgStore}
	router := aiwRouter(store)

	// Create a session via the handler.
	createBody := map[string]interface{}{
		"supi":     "imu-208046000000003",
		"eapIdRsp": "dXNlcjM=",
	}
	rec := doAIWRequest(router, http.MethodPost, "/nnssaaf-aiw/v1/authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp aiwnats.AuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// GET the session via store (GET handler not implemented in Phase 1).
	loaded, err := pgStore.Load(createResp.AuthCtxId)
	require.NoError(t, err)
	assert.Equal(t, "imu-208046000000003", loaded.Supi)
}

// ─── Test: GetSession → 404 NotFound ───────────────────────────────────────
// GET handler not implemented in Phase 1 — verify store returns ErrNotFound.

func TestIntegration_AIW_GetSession_NotFound(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := postgres.NewAIWSessionStore(pool, enc)

	// Verify store returns ErrNotFound for nonexistent session.
	_, err = store.Load("nonexistent-uuid-99999")
	require.Error(t, err)
	assert.True(t, errors.Is(err, aiw.ErrNotFound))
}

// ─── Test: Session stored in PostgreSQL ──────────────────────────────────────
// Verifies AIW sessions are persisted to PostgreSQL after creation.

func TestIntegration_AIW_SessionInRedis(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	pgStore := postgres.NewAIWSessionStore(pool, enc)
	store := &aiwStoreWithCache{pg: pgStore}
	router := aiwRouter(store)

	body := map[string]interface{}{
		"supi":     "imu-208046000000004",
		"eapIdRsp": "dXNlcjQ=",
	}
	rec := doAIWRequest(router, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)
	require.Equal(t, http.StatusCreated, rec.Code)
	var resp aiwnats.AuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Verify session exists in PostgreSQL.
	loaded, err := pgStore.Load(resp.AuthCtxId)
	require.NoError(t, err)
	assert.Equal(t, "imu-208046000000004", loaded.Supi)
}

// ─── Test: Invalid SUPI → 400 ─────────────────────────────────────────────

func TestIntegration_AIW_InvalidSupi(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &aiwStoreWithCache{pg: postgres.NewAIWSessionStore(pool, enc)}
	router := aiwRouter(store)

	body := map[string]interface{}{
		"supi":     "bad-supi",
		"eapIdRsp": "dGVzdA==",
	}

	rec := doAIWRequest(router, http.MethodPost, "/nnssaaf-aiw/v1/authentications", body)

	require.Equal(t, http.StatusBadRequest, rec.Code, "invalid SUPI should return 400")

	var problem common.ProblemDetails
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, 400, problem.Status)
	assert.Contains(t, problem.Detail, "supi")
}

// ─── Test: SUPI mismatch in body → 400 ─────────────────────────────────────

func TestIntegration_AIW_SupiMismatch(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &aiwStoreWithCache{pg: postgres.NewAIWSessionStore(pool, enc)}
	router := aiwRouter(store)

	// Create session with SUPI imu-208046000000005.
	createBody := map[string]interface{}{
		"supi":     "imu-208046000000005",
		"eapIdRsp": "dXNlcjU=",
	}
	rec := doAIWRequest(router, http.MethodPost, "/nnssaaf-aiw/v1/authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp aiwnats.AuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// Confirm with a different SUPI.
	confirmBody := map[string]interface{}{
		"supi":       "imu-999999999999999", // different SUPI
		"eapMessage": "dGVzdA==",
	}
	path := "/nnssaaf-aiw/v1/authentications/" + createResp.AuthCtxId
	rec = doAIWRequest(router, http.MethodPut, path, confirmBody)

	require.Equal(t, http.StatusBadRequest, rec.Code, "SUPI mismatch should return 400")
	assert.Contains(t, rec.Body.String(), "SUPI does not match")
}

// ─── Test: 10 concurrent session creates all succeed ─────────────────────────

func TestIntegration_AIW_ConcurrentSessions(t *testing.T) {
	pool := openTestPoolAIW(t)
	defer pool.Close()
	runMigrationsAIW(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &aiwStoreWithCache{pg: postgres.NewAIWSessionStore(pool, enc)}
	router := aiwRouter(store)

	const n = 10
	results := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			supi := fmt.Sprintf("imu-208%015d", idx)
			body := map[string]interface{}{
				"supi":     supi,
				"eapIdRsp": "dGVzdA==",
			}
			results[idx] = doAIWRequest(router, http.MethodPost,
				"/nnssaaf-aiw/v1/authentications", body)
		}(i)
	}
	wg.Wait()

	successes := 0
	for i := 0; i < n; i++ {
		if results[i] != nil && results[i].Code == http.StatusCreated {
			successes++
		}
	}
	assert.Equal(t, n, successes, "all %d concurrent creates should succeed", n)
}
