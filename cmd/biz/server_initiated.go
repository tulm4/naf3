// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"fmt"

	"github.com/operator/nssAAF/internal/biz"
	"github.com/operator/nssAAF/internal/proto"
)

// CoordinatorHandle is the subset of biz.ServerInitiatedCoordinator.Handle
// that the proto.ServerInitiatedHandler adapter calls.
// Extracted to an interface so tests can inject mock coordinators.
type CoordinatorHandle interface {
	Handle(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*biz.ServerInitiatedResult, error)
}

// NewServerInitiatedHandler wraps a coordinator with the proto.ServerInitiatedHandler
// interface expected by the HTTP gateway layer.
// Spec: docs/superpowers/specs/... §4.5
//
// The coordinator already handles:
//   - Reverse-path context resolution (Redis + PostgreSQL)
//   - AMF notification for RAR/ASR
//   - AIW completion linking for AUSF-owned sessions
//   - Session state persistence (MarkReauthPending, MarkRevoked, ApplyCoA)
//
// This adapter dispatches per message type and maps errors to result codes.
func NewServerInitiatedHandler(coordinator CoordinatorHandle) proto.ServerInitiatedHandler {
	return &serverInitiatedHandlerImpl{coordinator: coordinator}
}

type serverInitiatedHandlerImpl struct {
	coordinator CoordinatorHandle
}

func (h *serverInitiatedHandlerImpl) HandleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
	result, err := h.coordinator.Handle(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("handle re-auth: %w", err)
	}
	return &result.Response, nil
}

func (h *serverInitiatedHandlerImpl) HandleRevocation(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
	result, err := h.coordinator.Handle(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("handle revocation: %w", err)
	}
	return &result.Response, nil
}

func (h *serverInitiatedHandlerImpl) HandleCoA(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
	result, err := h.coordinator.Handle(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("handle coa: %w", err)
	}
	return &result.Response, nil
}
