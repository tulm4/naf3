package redis

import (
	"context"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/operator/nssAAF/internal/proto"
	goredis "github.com/redis/go-redis/v9"
)

func TestSessionCorrelationStore_Save_SetsExpectedTTL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	store := NewSessionCorrelationStore(rdb, proto.DefaultPayloadTTL)
	err = store.Save(context.Background(), "sess-1", proto.SessionCorrEntry{AuthCtxID: "auth-1"})
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	ttl := mr.TTL(proto.SessionCorrKey("sess-1"))
	if ttl <= 0 {
		t.Fatalf("TTL = %v, want > 0", ttl)
	}
}
