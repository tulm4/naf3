// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/nssaa"
	cacheredis "github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/storage/postgres"
	nssaanats "github.com/operator/nssAAF/oapi-gen/gen/nssaa"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultTestDBURL    = "postgres://nssaa:nssaa@localhost:5432/nssaa"
	defaultTestRedisURL = "redis://localhost:6379"
)

func testDBURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return defaultTestDBURL
}

func testRedisURL() string {
	if u := os.Getenv("TEST_REDIS_URL"); u != "" {
		return u
	}
	return defaultTestRedisURL
}

// redisAddr returns just "host:port" suitable for go-redis Options.Addr.
func redisAddr() string {
	u := testRedisURL()
	// Strip scheme if present (e.g., redis://localhost:6379 → localhost:6379)
	u = strings.TrimPrefix(u, "redis://")
	u = strings.TrimPrefix(u, "rediss://")
	if idx := strings.Index(u, "@"); idx != -1 {
		u = u[idx+1:]
	}
	if idx := strings.Index(u, "/"); idx != -1 {
		u = u[:idx]
	}
	return u
}

func skipIfNoDB(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" || os.Getenv("TEST_REDIS_URL") == "" {
		t.Skip("TEST_DATABASE_URL or TEST_REDIS_URL not set — skipping integration test")
	}
}

func skipIfNoRedis(t *testing.T) {
	if _, present := os.LookupEnv("TEST_REDIS_URL"); !present {
		t.Skip("TEST_REDIS_URL not set — skipping integration test")
	}
}

// ─── Test fixtures ────────────────────────────────────────────────────────────

// storeWithCache wraps a postgres.Store and propagates saves to Redis cache.
type storeWithCache struct {
	pg    *postgres.Store
	cache *cacheredis.SessionCache
}

func (s *storeWithCache) Load(id string) (*nssaa.AuthCtx, error) {
	return s.pg.Load(id)
}

func (s *storeWithCache) Save(ctx *nssaa.AuthCtx) error {
	if err := s.pg.Save(ctx); err != nil {
		return err
	}
	if s.cache != nil {
		cacheCtx := context.Background()
		entry := &cacheredis.SessionCacheEntry{
			SnssaiSST:   ctx.SnssaiSST,
			SnssaiSD:    ctx.SnssaiSD,
			NssaaStatus: "PENDING",
			EAPRounds:   0,
			Method:      "EAP-TLS",
		}
		_ = s.cache.Set(cacheCtx, ctx.AuthCtxID, entry)
	}
	return nil
}

func (s *storeWithCache) Delete(id string) error {
	if s.cache != nil {
		_ = s.cache.Delete(context.Background(), id)
	}
	return s.pg.Delete(id)
}

func (s *storeWithCache) Close() error { return s.pg.Close() }

func openTestPool(t *testing.T) *postgres.Pool {
	skipIfNoDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := postgres.Config{DSN: testDBURL()}
	pool, err := postgres.NewPool(ctx, cfg)
	require.NoError(t, err, "failed to connect to test database")
	return pool
}

func runMigrations(t *testing.T, pool *postgres.Pool) {
	migrator := postgres.NewMigrator(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := migrator.Migrate(ctx)
	require.NoError(t, err, "failed to run migrations")
}

func openTestRedis(t *testing.T) *goredis.Client {
	skipIfNoRedis(t)
	client := goredis.NewClient(&goredis.Options{
		Addr: redisAddr(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Ping(ctx).Err()
	require.NoError(t, err, "failed to connect to test Redis")
	return client
}

// nssaaRouter builds a chi router wired to the NSSAA handler with the given store.
func nssaaRouter(store nssaa.AuthCtxStore) http.Handler {
	handler := nssaa.NewHandler(store)
	r := chi.NewRouter()
	r.Use(common.RequestIDMiddleware)
	return nssaanats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-nssaa/v1")
}

func doNSSAARequest(router http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
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

func TestIntegration_NSSAA_CreateSession(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &storeWithCache{pg: postgres.NewSessionStore(pool, enc)}
	router := nssaaRouter(store)

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

	require.Equal(t, http.StatusCreated, rec.Code, "create session: %s", rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get(common.HeaderLocation))
	assert.Contains(t, rec.Header().Get(common.HeaderLocation), "/slice-authentications/")
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var resp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "520804600000001", string(resp.Gpsi))
	assert.Equal(t, uint8(1), resp.Snssai.Sst)
	assert.Equal(t, "000001", resp.Snssai.Sd)
	assert.NotEmpty(t, resp.AuthCtxId)
}

// ─── Test: GPSI encrypted at rest in PostgreSQL ────────────────────────────────

func TestIntegration_NSSAA_CreateSession_GPSIStoredEncrypted(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	pgStore := postgres.NewSessionStore(pool, enc)
	store := &storeWithCache{pg: pgStore}
	router := nssaaRouter(store)

	gpsi := "520804600000001"
	body := map[string]interface{}{
		"gpsi":     gpsi,
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}

	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	authCtxID := resp.AuthCtxId

	// Verify GPSI is NOT stored in plaintext via direct DB query.
	var storedGPSI string
	err = pool.QueryRow(context.Background(),
		"SELECT gpsi FROM slice_auth_sessions WHERE auth_ctx_id = $1",
		authCtxID,
	).Scan(&storedGPSI)
	require.NoError(t, err)
	assert.NotEqual(t, gpsi, storedGPSI, "GPSI should be encrypted, not stored as plaintext")
	assert.NotEmpty(t, storedGPSI)

	// Verify the stored value can be decrypted back to the original GPSI.
	ciphertext, err := base64.StdEncoding.DecodeString(storedGPSI)
	require.NoError(t, err, "stored GPSI should be base64-encoded ciphertext")
	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, gpsi, string(decrypted), "encrypted GPSI should decrypt back to original value")
}

// ─── Test: ConfirmSession → 200 ───────────────────────────────────────────────

func TestIntegration_NSSAA_ConfirmSession(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &storeWithCache{pg: postgres.NewSessionStore(pool, enc)}
	router := nssaaRouter(store)

	// Create session first.
	createBody := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dXNlcgBleGFtcGxlLmNvbQ==",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// Confirm session.
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}
	path := "/nnssaaf-nssaa/v1/slice-authentications/" + createResp.AuthCtxId
	rec = doNSSAARequest(router, http.MethodPut, path, confirmBody)

	require.Equal(t, http.StatusOK, rec.Code, "confirm session: %s", rec.Body.String())
	assert.Equal(t, "test-req-id", rec.Header().Get(common.HeaderXRequestID))

	var confirmResp nssaanats.SliceAuthConfirmationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &confirmResp))
	assert.Equal(t, "520804600000001", string(confirmResp.Gpsi))
}

// ─── Test: GetSession via store ───────────────────────────────────────────────

func TestIntegration_NSSAA_GetSession(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	pgStore := postgres.NewSessionStore(pool, enc)
	store := &storeWithCache{pg: pgStore}
	router := nssaaRouter(store)

	// Create a session via the handler.
	createBody := map[string]interface{}{
		"gpsi":     "520804600000002",
		"snssai":   map[string]interface{}{"sst": 2, "sd": "000002"},
		"eapIdRsp": "dXNlcjI=",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// GET the session via store (GET handler not implemented in Phase 1).
	loaded, err := pgStore.Load(createResp.AuthCtxId)
	require.NoError(t, err)
	assert.Equal(t, "520804600000002", loaded.GPSI)
	assert.Equal(t, uint8(2), loaded.SnssaiSST)
	assert.Equal(t, "000002", loaded.SnssaiSD)
}

// ─── Test: GetSession → 404 NotFound ─────────────────────────────────────────
// GET handler not implemented in Phase 1 — verify store returns ErrNotFound.

func TestIntegration_NSSAA_GetSession_NotFound(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := postgres.NewSessionStore(pool, enc)

	// Verify store returns ErrNotFound for nonexistent session.
	_, err = store.Load("nonexistent-uuid-12345")
	require.Error(t, err)
	assert.True(t, errors.Is(err, nssaa.ErrNotFound))
}

// ─── Test: Session cached in Redis after creation ─────────────────────────────

func TestIntegration_NSSAA_SessionInRedis(t *testing.T) {
	skipIfNoRedis(t)
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	redisClient := openTestRedis(t)
	defer redisClient.Close()
	cache := cacheredis.NewSessionCache(redisClient, 5*time.Minute)

	store := &storeWithCache{
		pg:    postgres.NewSessionStore(pool, enc),
		cache: cache,
	}
	router := nssaaRouter(store)

	body := map[string]interface{}{
		"gpsi":     "520804600000003",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000003"},
		"eapIdRsp": "dXNlcjM=",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusCreated, rec.Code)
	var resp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Verify session is cached in Redis.
	ctx := context.Background()
	key := "nssaa:session:" + resp.AuthCtxId
	exists, err := redisClient.Exists(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, exists, int64(0), "session should be cached in Redis after creation")
}

// ─── Test: ConfirmSession → 400 invalid base64 ────────────────────────────────

func TestIntegration_NSSAA_ConfirmSession_InvalidBase64(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &storeWithCache{pg: postgres.NewSessionStore(pool, enc)}
	router := nssaaRouter(store)

	// Create session.
	createBody := map[string]interface{}{
		"gpsi":     "520804600000004",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000004"},
		"eapIdRsp": "dXNlcjQ=",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// Confirm with invalid eapMessage (not valid base64).
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600000004",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000004"},
		"eapMessage": "not-valid-base64!!!",
	}
	path := "/nnssaaf-nssaa/v1/slice-authentications/" + createResp.AuthCtxId
	rec = doNSSAARequest(router, http.MethodPut, path, confirmBody)

	require.Equal(t, http.StatusBadRequest, rec.Code, "invalid eapMessage base64 should return 400")

	var problem common.ProblemDetails
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, 400, problem.Status)
}

// ─── Test: ConfirmSession → 400 GPSI mismatch ───────────────────────────────

func TestIntegration_NSSAA_ConfirmSession_GPSIMismatch(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &storeWithCache{pg: postgres.NewSessionStore(pool, enc)}
	router := nssaaRouter(store)

	// Create session with GPSI 520804600000005.
	createBody := map[string]interface{}{
		"gpsi":     "520804600000005",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000005"},
		"eapIdRsp": "dXNlcjU=",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// Confirm with a different GPSI.
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600009999", // different GPSI
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000005"},
		"eapMessage": "dGVzdA==",
	}
	path := "/nnssaaf-nssaa/v1/slice-authentications/" + createResp.AuthCtxId
	rec = doNSSAARequest(router, http.MethodPut, path, confirmBody)

	require.Equal(t, http.StatusBadRequest, rec.Code, "GPSI mismatch should return 400")
	assert.Contains(t, rec.Body.String(), "GPSI does not match")
}

// ─── Test: ConfirmSession → 400 Snssai mismatch ─────────────────────────────

func TestIntegration_NSSAA_ConfirmSession_SnssaiMismatch(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &storeWithCache{pg: postgres.NewSessionStore(pool, enc)}
	router := nssaaRouter(store)

	// Create session with SST=1.
	createBody := map[string]interface{}{
		"gpsi":     "520804600000006",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000006"},
		"eapIdRsp": "dXNlcjY=",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", createBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	var createResp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createResp))

	// Confirm with different SNSSAI (SST=2).
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600000006",
		"snssai":     map[string]interface{}{"sst": 2, "sd": "000006"}, // different SST
		"eapMessage": "dGVzdA==",
	}
	path := "/nnssaaf-nssaa/v1/slice-authentications/" + createResp.AuthCtxId
	rec = doNSSAARequest(router, http.MethodPut, path, confirmBody)

	require.Equal(t, http.StatusBadRequest, rec.Code, "Snssai mismatch should return 400")
}

// ─── Test: 10 concurrent session creates all succeed ─────────────────────────

func TestIntegration_NSSAA_ConcurrentSessions(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	store := &storeWithCache{pg: postgres.NewSessionStore(pool, enc)}
	router := nssaaRouter(store)

	const n = 10
	results := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// GPSI pattern: ^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$
// Note: Spec allows any non-empty string as catch-all; only whitespace-only is rejected.
			// Use "52080460" (8 digits) + 2-digit suffix = 10 digits total.
			gpsi := fmt.Sprintf("52080460%02d", idx)
			body := map[string]interface{}{
				"gpsi":     gpsi,
				"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
				"eapIdRsp": "dGVzdA==",
			}
			results[idx] = doNSSAARequest(router, http.MethodPost,
				"/nnssaaf-nssaa/v1/slice-authentications", body)
		}(i)
	}
	wg.Wait()

	successes := 0
	for i := 0; i < n; i++ {
		if results[i] != nil && results[i].Code == http.StatusCreated {
			successes++
		} else if results[i] != nil {
			log.Printf("DEBUG: request %d: status=%d body=%s", i, results[i].Code, results[i].Body.String())
		} else {
			log.Printf("DEBUG: request %d: nil response", i)
		}
	}
	assert.Equal(t, n, successes, "all %d concurrent creates should succeed", n)
}

// ─── Test: Session expires after configured TTL ───────────────────────────────

func TestIntegration_NSSAA_SessionExpiry(t *testing.T) {
	skipIfNoRedis(t)
	pool := openTestPool(t)
	defer pool.Close()
	runMigrations(t, pool)

	enc, err := postgres.NewEncryptor(make([]byte, 32))
	require.NoError(t, err)
	redisClient := openTestRedis(t)
	defer redisClient.Close()
	cache := cacheredis.NewSessionCache(redisClient, 1*time.Second) // 1s TTL for fast expiry test

	store := &storeWithCache{
		pg:    postgres.NewSessionStore(pool, enc),
		cache: cache,
	}
	router := nssaaRouter(store)

	body := map[string]interface{}{
		"gpsi":     "520804600000099",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000099"},
		"eapIdRsp": "dGVzdA==",
	}
	rec := doNSSAARequest(router, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)
	require.Equal(t, http.StatusCreated, rec.Code)
	var resp nssaanats.SliceAuthContext
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Wait for TTL to expire.
	time.Sleep(1100 * time.Millisecond)

	// Verify Redis cache expired.
	ctx := context.Background()
	exists, err := cache.Exists(ctx, resp.AuthCtxId)
	require.NoError(t, err)
	assert.False(t, exists, "session cache should be expired after TTL")
}
