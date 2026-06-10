package biz

import (
	"context"
	"testing"

	"github.com/operator/nssAAF/internal/storage"
)

type stubNssaaStore struct {
	sessions map[string]*storage.NssaaSession
}

func newStubNssaaStore() *stubNssaaStore {
	return &stubNssaaStore{sessions: map[string]*storage.NssaaSession{}}
}

func (s *stubNssaaStore) Load(_ context.Context, id string) (*storage.NssaaSession, error) {
	session, ok := s.sessions[id]
	if !ok {
		return nil, storage.ErrSessionNotFound
	}
	clone := *session
	return &clone, nil
}

func (s *stubNssaaStore) Save(_ context.Context, session *storage.NssaaSession) error {
	clone := *session
	s.sessions[session.AuthCtxID] = &clone
	return nil
}

func (s *stubNssaaStore) Delete(_ context.Context, id string) error {
	delete(s.sessions, id)
	return nil
}

func (s *stubNssaaStore) Close() error { return nil }

type stubAiwStore struct {
	sessions map[string]*storage.AiwSession
}

func newStubAiwStore() *stubAiwStore {
	return &stubAiwStore{sessions: map[string]*storage.AiwSession{}}
}

func (s *stubAiwStore) Load(_ context.Context, id string) (*storage.AiwSession, error) {
	session, ok := s.sessions[id]
	if !ok {
		return nil, storage.ErrSessionNotFound
	}
	clone := *session
	return &clone, nil
}

func (s *stubAiwStore) Save(_ context.Context, session *storage.AiwSession) error {
	clone := *session
	s.sessions[session.AuthCtxID] = &clone
	return nil
}

func TestReverseFlowStateWriter_MarkReauthPending_DelegatesToStore(t *testing.T) {
	store := newStubNssaaStore()
	store.sessions["auth-1"] = &storage.NssaaSession{AuthCtxID: "auth-1", Status: "EAP_SUCCESS"}
	writer := NewReverseFlowStateWriter(store)

	if err := writer.MarkReauthPending(context.Background(), "auth-1"); err != nil {
		t.Fatalf("MarkReauthPending returned error: %v", err)
	}
	if store.sessions["auth-1"].Status != "PENDING" {
		t.Fatalf("status = %q, want PENDING", store.sessions["auth-1"].Status)
	}
}

func TestNssaaSessionResolver_LoadAuthContext_MapsPersistedFields(t *testing.T) {
	store := newStubNssaaStore()
	store.sessions["auth-2"] = &storage.NssaaSession{
		AuthCtxID:      "auth-2",
		GPSI:           "msisdn-12345",
		ReauthURI:      "http://amf/reauth",
		RevocURI:       "http://amf/revoke",
		CallbackOwner:  "amf",
		HasAIWContext:  true,
	}

	resolver := NewNssaaSessionResolver(store)
	ctx, err := resolver.LoadAuthContext(context.Background(), "auth-2")
	if err != nil {
		t.Fatalf("LoadAuthContext returned error: %v", err)
	}
	if ctx.CallbackOwner != "amf" {
		t.Fatalf("callback owner = %q, want amf", ctx.CallbackOwner)
	}
	if !ctx.HasAIWContext {
		t.Fatalf("HasAIWContext = false, want true")
	}
}

func TestAIWCompletionLinker_MarkAIWLinked_SetsCompletedAt(t *testing.T) {
	store := newStubAiwStore()
	store.sessions["auth-3"] = &storage.AiwSession{AuthCtxID: "auth-3", Status: "PENDING"}
	linker := NewAIWCompletionLinker(store)

	if err := linker.MarkAIWLinked(context.Background(), "auth-3"); err != nil {
		t.Fatalf("MarkAIWLinked returned error: %v", err)
	}
	if store.sessions["auth-3"].CompletedAt == nil {
		t.Fatalf("CompletedAt is nil, want timestamp")
	}
}
