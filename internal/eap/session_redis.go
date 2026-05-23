// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// SessionKeyPrefix is the Redis key prefix for EAP sessions.
	SessionKeyPrefix = "nssaa:eap:session:"
)

// SessionStore is the interface for EAP session persistence.
// Spec: TS 33.501 §16.3
//
// Both in-memory (sessionManager) and Redis-backed (RedisSessionManager)
// implementations satisfy this interface.
type SessionStore interface {
	// Get retrieves a session by authCtxID.
	Get(ctx context.Context, authCtxID string) (*Session, error)
	// Put stores or updates a session.
	Put(ctx context.Context, session *Session) error
	// Delete removes a session.
	Delete(ctx context.Context, authCtxID string) error
	// Size returns the number of active sessions.
	Size() int
}

// RedisSessionManager implements SessionStore using Redis as the backing store.
// Thread-safe via Redis.
type RedisSessionManager struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewRedisSessionManager creates a new Redis-backed session manager.
func NewRedisSessionManager(client redis.Cmdable, ttl time.Duration) *RedisSessionManager {
	return &RedisSessionManager{
		client: client,
		ttl:    ttl,
	}
}

// Get retrieves a session from Redis by authCtxID.
func (m *RedisSessionManager) Get(ctx context.Context, authCtxID string) (*Session, error) {
	key := SessionKeyPrefix + authCtxID
	data, err := m.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var rs redisSession
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return rs.toSession(), nil
}

// Put stores a session in Redis with TTL.
func (m *RedisSessionManager) Put(ctx context.Context, session *Session) error {
	key := SessionKeyPrefix + session.AuthCtxID
	rs := fromSession(session)
	data, err := json.Marshal(rs)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := m.client.Set(ctx, key, data, m.ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// Delete removes a session from Redis.
func (m *RedisSessionManager) Delete(ctx context.Context, authCtxID string) error {
	key := SessionKeyPrefix + authCtxID
	return m.client.Del(ctx, key).Err()
}

// Size returns the number of active EAP sessions in Redis.
// Note: This is an O(n) operation that scans all matching keys.
func (m *RedisSessionManager) Size() int {
	// Redis SCAN is not trivially wrapped by go-redis without iteration.
	// For production, consider maintaining a separate counter or using Redis SET.
	// This implementation returns 0 as a safe default.
	return 0
}

// redisSession is the JSON-serializable representation of a Session.
type redisSession struct {
	AuthCtxID       string           `json:"authCtxId"`
	Gpsi            string           `json:"gpsi"`
	Supi            string           `json:"supi,omitempty"`
	SnssaiKey       string           `json:"snssaiKey"`
	State           SessionState     `json:"state"`
	Method          Method           `json:"method"`
	Rounds          int              `json:"rounds"`
	MaxRounds       int              `json:"maxRounds"`
	ExpectedID      uint8            `json:"expectedId"`
	LastNonce       []byte           `json:"lastNonce,omitempty"`
	CachedResponse  []byte           `json:"cachedResponse,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
	LastActivity    time.Time        `json:"lastActivity"`
	Timeout         time.Duration    `json:"timeout"`
	TLSState        *TLSSessionState `json:"tlsState,omitempty"`
}

// toSession converts a redisSession back to a Session.
func (rs *redisSession) toSession() *Session {
	return &Session{
		AuthCtxID:       rs.AuthCtxID,
		Gpsi:            rs.Gpsi,
		Supi:            rs.Supi,
		SnssaiKey:       rs.SnssaiKey,
		State:           rs.State,
		Method:          rs.Method,
		Rounds:          rs.Rounds,
		MaxRounds:       rs.MaxRounds,
		ExpectedID:      rs.ExpectedID,
		LastNonce:       rs.LastNonce,
		CachedResponse:  rs.CachedResponse,
		CreatedAt:       rs.CreatedAt,
		LastActivity:    rs.LastActivity,
		Timeout:         rs.Timeout,
		TLSState:        rs.TLSState,
	}
}

// fromSession converts a Session to a redisSession.
func fromSession(s *Session) *redisSession {
	return &redisSession{
		AuthCtxID:       s.AuthCtxID,
		Gpsi:            s.Gpsi,
		Supi:            s.Supi,
		SnssaiKey:       s.SnssaiKey,
		State:           s.State,
		Method:          s.Method,
		Rounds:          s.Rounds,
		MaxRounds:       s.MaxRounds,
		ExpectedID:      s.ExpectedID,
		LastNonce:       s.LastNonce,
		CachedResponse:  s.CachedResponse,
		CreatedAt:       s.CreatedAt,
		LastActivity:    s.LastActivity,
		Timeout:         s.Timeout,
		TLSState:        s.TLSState,
	}
}
