package biz

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/proto"
)

type stubSessionLookup struct {
	ctx *NssaaSessionContext
}

func (s stubSessionLookup) LoadAuthContext(_ context.Context, authCtxID string) (*NssaaSessionContext, error) {
	out := *s.ctx
	out.AuthCtxID = authCtxID
	return &out, nil
}

func TestCorrelationResolver_Resolve_UsesRedisThenPersistentContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	pool, err := redis.NewPool(context.Background(), redis.Config{Addrs: []string{mr.Addr()}})
	if err != nil {
		t.Fatalf("redis pool: %v", err)
	}
	defer func() { _ = pool.Close() }()

	corrStore := redis.NewSessionCorrelationStore(pool.Client(), time.Minute)
	if err := corrStore.Save(context.Background(), "sess-123", proto.SessionCorrEntry{
		AuthCtxID: "auth-123",
		PodID:     "biz-a",
		Sst:       1,
		Sd:        "000001",
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	resolver := NewCorrelationResolver(pool.Client(), stubSessionLookup{ctx: &NssaaSessionContext{
		ReauthNotifURI: "http://amf/reauth",
		RevocNotifURI:  "http://amf/revoke",
		CallbackOwner:  "amf",
	}})

	got, err := resolver.Resolve(context.Background(), "sess-123", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.AuthCtxID != "auth-123" {
		t.Fatalf("AuthCtxID = %q, want auth-123", got.AuthCtxID)
	}
	if got.ReauthNotifURI != "http://amf/reauth" {
		t.Fatalf("ReauthNotifURI = %q", got.ReauthNotifURI)
	}
}
