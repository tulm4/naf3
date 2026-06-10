package biz

import (
	"context"
	"testing"

	"github.com/operator/nssAAF/internal/proto"
)

type stubResolver struct {
	ctx *NssaaSessionContext
}

func (s stubResolver) Resolve(_ context.Context, sessionID, authCtxID string) (*NssaaSessionContext, error) {
	out := *s.ctx
	out.SessionID = sessionID
	out.AuthCtxID = authCtxID
	return &out, nil
}

type stubWriter struct {
	reauthAuthCtxIDs  []string
	revokedAuthCtxIDs []string
	coaPayloads       [][]byte
}

func (s *stubWriter) MarkReauthPending(_ context.Context, authCtxID string) error {
	s.reauthAuthCtxIDs = append(s.reauthAuthCtxIDs, authCtxID)
	return nil
}

func (s *stubWriter) MarkRevoked(_ context.Context, authCtxID string) error {
	s.revokedAuthCtxIDs = append(s.revokedAuthCtxIDs, authCtxID)
	return nil
}

func (s *stubWriter) ApplyCoA(_ context.Context, authCtxID string, payload []byte) error {
	s.coaPayloads = append(s.coaPayloads, append([]byte(nil), payload...))
	return nil
}

type stubNotifier struct {
	reauthCalls     int
	revocationCalls int
}

func (s *stubNotifier) SendReAuthNotification(_ context.Context, uri, authCtxID string, payload []byte) error {
	s.reauthCalls++
	return nil
}

func (s *stubNotifier) SendRevocationNotification(_ context.Context, uri, authCtxID string, payload []byte) error {
	s.revocationCalls++
	return nil
}

type stubAIWLinker struct {
	linkedAuthCtxIDs []string
}

func (s *stubAIWLinker) MarkAIWLinked(_ context.Context, authCtxID string) error {
	s.linkedAuthCtxIDs = append(s.linkedAuthCtxIDs, authCtxID)
	return nil
}

func TestServerInitiatedResult_RequiresResponsePayload(t *testing.T) {
	result := ServerInitiatedResult{
		Response: proto.AaaServerInitiatedResponse{
			Version:   proto.CurrentVersion,
			SessionID: "sess-1",
			AuthCtxID: "auth-1",
		},
	}

	if err := result.Validate(); err == nil {
		t.Fatalf("expected validation error when response payload is empty")
	}
}

func TestServerInitiatedCoordinator_Handle_Reauth_UpdatesStateAndNotifiesAMF(t *testing.T) {
	writer := &stubWriter{}
	notifier := &stubNotifier{}
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
		CallbackOwner:  "amf",
		ReauthNotifURI: "http://amf/reauth",
	}}, writer, notifier, nil)

	result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-1",
		AuthCtxID:   "auth-1",
		MessageType: proto.MessageTypeRAR,
		Payload:     []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.reauthAuthCtxIDs) != 1 || writer.reauthAuthCtxIDs[0] != "auth-1" {
		t.Fatalf("reauth state updates = %#v", writer.reauthAuthCtxIDs)
	}
	if notifier.reauthCalls != 1 {
		t.Fatalf("reauth notifications = %d, want 1", notifier.reauthCalls)
	}
	if result.Completion != CompletionAMFNotified {
		t.Fatalf("completion = %s, want %s", result.Completion, CompletionAMFNotified)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("result.Validate() error = %v", err)
	}
}

func TestServerInitiatedCoordinator_Handle_Revocation_UpdatesStateAndNotifiesAMF(t *testing.T) {
	writer := &stubWriter{}
	notifier := &stubNotifier{}
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
		CallbackOwner: "amf",
		RevocNotifURI: "http://amf/revoke",
	}}, writer, notifier, nil)

	result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-2",
		AuthCtxID:   "auth-2",
		MessageType: proto.MessageTypeASR,
		Payload:     []byte{9, 9, 9},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.revokedAuthCtxIDs) != 1 || writer.revokedAuthCtxIDs[0] != "auth-2" {
		t.Fatalf("revoked auth contexts = %#v", writer.revokedAuthCtxIDs)
	}
	if notifier.revocationCalls != 1 {
		t.Fatalf("revocation notifications = %d, want 1", notifier.revocationCalls)
	}
	if result.Completion != CompletionAMFNotified {
		t.Fatalf("completion = %s, want %s", result.Completion, CompletionAMFNotified)
	}
}

func TestServerInitiatedCoordinator_Handle_CoA_UpdatesStateOnly(t *testing.T) {
	writer := &stubWriter{}
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
		CallbackOwner: "amf",
	}}, writer, &stubNotifier{}, nil)

	result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-3",
		AuthCtxID:   "auth-3",
		MessageType: proto.MessageTypeCoA,
		Payload:     []byte{4, 5, 6},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.coaPayloads) != 1 {
		t.Fatalf("coa payloads = %#v", writer.coaPayloads)
	}
	if result.Completion != CompletionStateOnly {
		t.Fatalf("completion = %s, want %s", result.Completion, CompletionStateOnly)
	}
}

func TestServerInitiatedCoordinator_Handle_AIWOwnedFlow_LinksAIWContext(t *testing.T) {
	writer := &stubWriter{}
	linker := &stubAIWLinker{}
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
		CallbackOwner: "ausf",
		HasAIWContext: true,
	}}, writer, &stubNotifier{}, linker)

	result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-4",
		AuthCtxID:   "auth-4",
		MessageType: proto.MessageTypeRAR,
		Payload:     []byte{1},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(linker.linkedAuthCtxIDs) != 1 || linker.linkedAuthCtxIDs[0] != "auth-4" {
		t.Fatalf("linked auth contexts = %#v", linker.linkedAuthCtxIDs)
	}
	if result.Completion != CompletionAIWLinked {
		t.Fatalf("completion = %s, want %s", result.Completion, CompletionAIWLinked)
	}
}

func TestServerInitiatedCoordinator_Handle_ASR_AIWLinked(t *testing.T) {
	writer := &stubWriter{}
	linker := &stubAIWLinker{}
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
		CallbackOwner: "ausf",
		HasAIWContext: true,
	}}, writer, &stubNotifier{}, linker)

	result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-asr-aiw",
		AuthCtxID:   "auth-asr-aiw",
		MessageType: proto.MessageTypeASR,
		Payload:     []byte{7},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(linker.linkedAuthCtxIDs) != 1 || linker.linkedAuthCtxIDs[0] != "auth-asr-aiw" {
		t.Fatalf("linked auth contexts = %#v", linker.linkedAuthCtxIDs)
	}
	if result.Completion != CompletionAIWLinked {
		t.Fatalf("completion = %s, want %s", result.Completion, CompletionAIWLinked)
	}
}

func TestServerInitiatedCoordinator_Handle_CoA_AIWLinked(t *testing.T) {
	writer := &stubWriter{}
	linker := &stubAIWLinker{}
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{
		CallbackOwner: "ausf",
		HasAIWContext: true,
	}}, writer, &stubNotifier{}, linker)

	result, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-coa-aiw",
		AuthCtxID:   "auth-coa-aiw",
		MessageType: proto.MessageTypeCoA,
		Payload:     []byte{8},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(linker.linkedAuthCtxIDs) != 1 || linker.linkedAuthCtxIDs[0] != "auth-coa-aiw" {
		t.Fatalf("linked auth contexts = %#v", linker.linkedAuthCtxIDs)
	}
	if result.Completion != CompletionAIWLinked {
		t.Fatalf("completion = %s, want %s", result.Completion, CompletionAIWLinked)
	}
}

func TestServerInitiatedCoordinator_Handle_RejectsWhenCallbackOwnerMissing(t *testing.T) {
	coordinator := NewServerInitiatedCoordinator(stubResolver{ctx: &NssaaSessionContext{}}, &stubWriter{}, &stubNotifier{}, nil)

	_, err := coordinator.Handle(context.Background(), &proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "sess-no-owner",
		AuthCtxID:   "auth-no-owner",
		MessageType: proto.MessageTypeRAR,
		Payload:     []byte{1},
	})
	if err == nil {
		t.Fatalf("expected error when callback owner is missing")
	}
}
