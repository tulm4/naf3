package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/operator/nssAAF/internal/proto"
	goredis "github.com/redis/go-redis/v9"
)

// SessionCorrelationStore persists reverse-path correlation entries in Redis.
type SessionCorrelationStore struct {
	rdb goredis.Cmdable
	ttl time.Duration
}

// NewSessionCorrelationStore creates a session correlation store with the given TTL.
func NewSessionCorrelationStore(rdb goredis.Cmdable, ttl time.Duration) *SessionCorrelationStore {
	return &SessionCorrelationStore{rdb: rdb, ttl: ttl}
}

// Save writes a correlation entry keyed by session ID.
func (s *SessionCorrelationStore) Save(ctx context.Context, sessionID string, entry proto.SessionCorrEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal session correlation: %w", err)
	}
	if err := s.rdb.Set(ctx, proto.SessionCorrKey(sessionID), data, s.ttl).Err(); err != nil {
		return fmt.Errorf("save session correlation %s: %w", sessionID, err)
	}
	return nil
}

// Load retrieves a correlation entry keyed by session ID.
func (s *SessionCorrelationStore) Load(ctx context.Context, sessionID string) (*proto.SessionCorrEntry, error) {
	data, err := s.rdb.Get(ctx, proto.SessionCorrKey(sessionID)).Bytes()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("load session correlation %s: %w", sessionID, err)
	}
	var entry proto.SessionCorrEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("decode session correlation %s: %w", sessionID, err)
	}
	return &entry, nil
}
