package storage

import "context"

// NssaaStore manages NSSAA slice authentication sessions.
type NssaaStore interface {
	Load(ctx context.Context, id string) (*NssaaSession, error)
	Save(ctx context.Context, session *NssaaSession) error
	Delete(ctx context.Context, id string) error
	Close() error
}

// AiwStore manages AIW authentication sessions.
type AiwStore interface {
	Load(ctx context.Context, id string) (*AiwSession, error)
	Save(ctx context.Context, session *AiwSession) error
	Delete(ctx context.Context, id string) error
	Close() error
}
