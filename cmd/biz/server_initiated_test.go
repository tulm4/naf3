// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/operator/nssAAF/internal/biz"
	"github.com/operator/nssAAF/internal/proto"
)

// mockCoordinatorErr is a mock that always returns an error.
type mockCoordinatorErr struct {
	err error
}

func (m *mockCoordinatorErr) Handle(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*biz.ServerInitiatedResult, error) {
	return nil, m.err
}

// mockCoordinatorOK is a mock that returns a success result.
type mockCoordinatorOK struct{}

func (m *mockCoordinatorOK) Handle(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*biz.ServerInitiatedResult, error) {
	return &biz.ServerInitiatedResult{
		Response: proto.AaaServerInitiatedResponse{
			Version:     proto.CurrentVersion,
			SessionID:   req.SessionID,
			AuthCtxID:   req.AuthCtxID,
			MessageType: req.MessageType,
			ResultCode:  proto.ResultCodeSuccess,
			Payload:     []byte{2, 0, 0, 12},
		},
		Completion: biz.CompletionStateOnly,
	}, nil
}

func TestHandleReAuth_SessionNotFound(t *testing.T) {
	notFoundErr := errors.New("session not found")
	handler := NewServerInitiatedHandler(&mockCoordinatorErr{err: notFoundErr})

	req := &proto.AaaServerInitiatedRequest{
		SessionID:   "session-123",
		AuthCtxID:   "auth-456",
		MessageType: proto.MessageTypeRAR,
	}

	_, err := handler.HandleReAuth(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "handle re-auth") {
		t.Fatalf("expected 'handle re-auth' in error, got: %v", err)
	}
}

func TestHandleReAuth_Success(t *testing.T) {
	handler := NewServerInitiatedHandler(&mockCoordinatorOK{})

	req := &proto.AaaServerInitiatedRequest{
		SessionID:   "session-123",
		AuthCtxID:   "auth-456",
		MessageType: proto.MessageTypeRAR,
		Payload:     []byte{1, 2, 3},
	}

	resp, err := handler.HandleReAuth(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionID != req.SessionID {
		t.Fatalf("SessionID = %q, want %q", resp.SessionID, req.SessionID)
	}
	if resp.AuthCtxID != req.AuthCtxID {
		t.Fatalf("AuthCtxID = %q, want %q", resp.AuthCtxID, req.AuthCtxID)
	}
	if resp.MessageType != req.MessageType {
		t.Fatalf("MessageType = %q, want %q", resp.MessageType, req.MessageType)
	}
	if resp.ResultCode != proto.ResultCodeSuccess {
		t.Fatalf("ResultCode = %d, want %d (SUCCESS)", resp.ResultCode, proto.ResultCodeSuccess)
	}
}

func TestHandleRevocation_SessionNotFound(t *testing.T) {
	notFoundErr := errors.New("session not found")
	handler := NewServerInitiatedHandler(&mockCoordinatorErr{err: notFoundErr})

	req := &proto.AaaServerInitiatedRequest{
		SessionID:   "session-123",
		AuthCtxID:   "auth-456",
		MessageType: proto.MessageTypeASR,
	}

	_, err := handler.HandleRevocation(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "handle revocation") {
		t.Fatalf("expected 'handle revocation' in error, got: %v", err)
	}
}

func TestHandleRevocation_Success(t *testing.T) {
	handler := NewServerInitiatedHandler(&mockCoordinatorOK{})

	req := &proto.AaaServerInitiatedRequest{
		SessionID:   "session-789",
		AuthCtxID:   "auth-abc",
		MessageType: proto.MessageTypeASR,
	}

	resp, err := handler.HandleRevocation(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionID != req.SessionID {
		t.Fatalf("SessionID = %q, want %q", resp.SessionID, req.SessionID)
	}
	if resp.ResultCode != proto.ResultCodeSuccess {
		t.Fatalf("ResultCode = %d, want %d (SUCCESS)", resp.ResultCode, proto.ResultCodeSuccess)
	}
}

func TestHandleCoA_Success(t *testing.T) {
	handler := NewServerInitiatedHandler(&mockCoordinatorOK{})

	req := &proto.AaaServerInitiatedRequest{
		SessionID:   "session-coa",
		AuthCtxID:   "auth-coa",
		MessageType: proto.MessageTypeCoA,
		Payload:     []byte{0x02, 0x00, 0x00, 0x0c},
	}

	resp, err := handler.HandleCoA(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.MessageType != proto.MessageTypeCoA {
		t.Fatalf("MessageType = %q, want %q", resp.MessageType, proto.MessageTypeCoA)
	}
	if resp.ResultCode != proto.ResultCodeSuccess {
		t.Fatalf("ResultCode = %d, want %d (SUCCESS)", resp.ResultCode, proto.ResultCodeSuccess)
	}
}

func TestHandleCoA_CoordinatorError(t *testing.T) {
	coaErr := errors.New("coa processing failed")
	handler := NewServerInitiatedHandler(&mockCoordinatorErr{err: coaErr})

	req := &proto.AaaServerInitiatedRequest{
		SessionID:   "session-coa",
		AuthCtxID:   "auth-coa",
		MessageType: proto.MessageTypeCoA,
	}

	_, err := handler.HandleCoA(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for CoA failure")
	}
	if !strings.Contains(err.Error(), "handle coa") {
		t.Fatalf("expected 'handle coa' in error, got: %v", err)
	}
}
