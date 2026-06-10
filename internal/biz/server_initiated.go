package biz

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/proto"
)

// ServerInitiatedCoordinator handles AAA server-initiated reverse flows inside Biz.
type ServerInitiatedCoordinator struct {
	resolver SessionContextResolver
	writer   SessionStateWriter
	notifier AMFNotifier
	aiwLink  AIWLinker
}

// NewServerInitiatedCoordinator creates a reverse-path coordinator.
func NewServerInitiatedCoordinator(resolver SessionContextResolver, writer SessionStateWriter, notifier AMFNotifier, aiwLink AIWLinker) *ServerInitiatedCoordinator {
	return &ServerInitiatedCoordinator{
		resolver: resolver,
		writer:   writer,
		notifier: notifier,
		aiwLink:  aiwLink,
	}
}

// Handle processes a server-initiated AAA message and returns the raw payload
// that the AAA Gateway should forward back to AAA-S.
func (c *ServerInitiatedCoordinator) Handle(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*ServerInitiatedResult, error) {
	sessionCtx, err := c.resolver.Resolve(ctx, req.SessionID, req.AuthCtxID)
	if err != nil {
		return nil, fmt.Errorf("resolve reverse-path context: %w", err)
	}
	if sessionCtx.CallbackOwner == "" {
		return nil, fmt.Errorf("callback owner is required for server-initiated handling")
	}

	switch req.MessageType {
	case proto.MessageTypeRAR:
		return c.handleRAR(ctx, req, sessionCtx)
	case proto.MessageTypeASR:
		return c.handleASR(ctx, req, sessionCtx)
	case proto.MessageTypeCoA:
		return c.handleCoA(ctx, req, sessionCtx)
	default:
		return nil, fmt.Errorf("unsupported message type: %s", req.MessageType)
	}
}

func (c *ServerInitiatedCoordinator) handleRAR(ctx context.Context, req *proto.AaaServerInitiatedRequest, sessionCtx *NssaaSessionContext) (*ServerInitiatedResult, error) {
	if err := c.writer.MarkReauthPending(ctx, sessionCtx.AuthCtxID); err != nil {
		return nil, fmt.Errorf("mark reauth pending: %w", err)
	}

	completion := CompletionStateOnly
	if sessionCtx.CallbackOwner == "amf" && sessionCtx.ReauthNotifURI != "" {
		if err := c.notifier.SendReAuthNotification(ctx, sessionCtx.ReauthNotifURI, sessionCtx.AuthCtxID, req.Payload); err != nil {
			return nil, fmt.Errorf("send reauth notification: %w", err)
		}
		completion = CompletionAMFNotified
	}
	if sessionCtx.CallbackOwner == "ausf" && sessionCtx.HasAIWContext {
		if c.aiwLink == nil {
			return nil, fmt.Errorf("aiw linker not configured")
		}
		if err := c.aiwLink.MarkAIWLinked(ctx, sessionCtx.AuthCtxID); err != nil {
			return nil, fmt.Errorf("mark aiw linked: %w", err)
		}
		completion = CompletionAIWLinked
	}

	result := &ServerInitiatedResult{
		Response: proto.AaaServerInitiatedResponse{
			Version:   proto.CurrentVersion,
			SessionID: req.SessionID,
			AuthCtxID: sessionCtx.AuthCtxID,
			Payload:   []byte{2, 0, 0, 12},
		},
		Completion: completion,
	}
	metrics.ServerInitiatedCompletions.WithLabelValues(string(req.MessageType), string(result.Completion)).Inc()

	slog.Info("server_initiated_completed",
		"auth_ctx_id", sessionCtx.AuthCtxID,
		"session_id", req.SessionID,
		"message_type", req.MessageType,
		"completion", result.Completion,
		"callback_owner", sessionCtx.CallbackOwner,
	)

	return result, result.Validate()
}

func (c *ServerInitiatedCoordinator) handleASR(ctx context.Context, req *proto.AaaServerInitiatedRequest, sessionCtx *NssaaSessionContext) (*ServerInitiatedResult, error) {
	if err := c.writer.MarkRevoked(ctx, sessionCtx.AuthCtxID); err != nil {
		return nil, fmt.Errorf("mark revoked: %w", err)
	}

	completion := CompletionStateOnly
	if sessionCtx.CallbackOwner == "amf" && sessionCtx.RevocNotifURI != "" {
		if err := c.notifier.SendRevocationNotification(ctx, sessionCtx.RevocNotifURI, sessionCtx.AuthCtxID, req.Payload); err != nil {
			return nil, fmt.Errorf("send revocation notification: %w", err)
		}
		completion = CompletionAMFNotified
	}
	if sessionCtx.CallbackOwner == "ausf" && sessionCtx.HasAIWContext {
		if c.aiwLink == nil {
			return nil, fmt.Errorf("aiw linker not configured")
		}
		if err := c.aiwLink.MarkAIWLinked(ctx, sessionCtx.AuthCtxID); err != nil {
			return nil, fmt.Errorf("mark aiw linked: %w", err)
		}
		completion = CompletionAIWLinked
	}

	result := &ServerInitiatedResult{
		Response: proto.AaaServerInitiatedResponse{
			Version:   proto.CurrentVersion,
			SessionID: req.SessionID,
			AuthCtxID: sessionCtx.AuthCtxID,
			Payload:   []byte{1},
		},
		Completion: completion,
	}
	metrics.ServerInitiatedCompletions.WithLabelValues(string(req.MessageType), string(result.Completion)).Inc()

	slog.Info("server_initiated_completed",
		"auth_ctx_id", sessionCtx.AuthCtxID,
		"session_id", req.SessionID,
		"message_type", req.MessageType,
		"completion", result.Completion,
		"callback_owner", sessionCtx.CallbackOwner,
	)

	return result, result.Validate()
}

func (c *ServerInitiatedCoordinator) handleCoA(ctx context.Context, req *proto.AaaServerInitiatedRequest, sessionCtx *NssaaSessionContext) (*ServerInitiatedResult, error) {
	if err := c.writer.ApplyCoA(ctx, sessionCtx.AuthCtxID, req.Payload); err != nil {
		return nil, fmt.Errorf("apply coa: %w", err)
	}

	completion := CompletionStateOnly
	if sessionCtx.CallbackOwner == "ausf" && sessionCtx.HasAIWContext {
		if c.aiwLink == nil {
			return nil, fmt.Errorf("aiw linker not configured")
		}
		if err := c.aiwLink.MarkAIWLinked(ctx, sessionCtx.AuthCtxID); err != nil {
			return nil, fmt.Errorf("mark aiw linked: %w", err)
		}
		completion = CompletionAIWLinked
	}

	result := &ServerInitiatedResult{
		Response: proto.AaaServerInitiatedResponse{
			Version:   proto.CurrentVersion,
			SessionID: req.SessionID,
			AuthCtxID: sessionCtx.AuthCtxID,
			Payload:   []byte{2, 0, 0, 12},
		},
		Completion: completion,
	}
	metrics.ServerInitiatedCompletions.WithLabelValues(string(req.MessageType), string(result.Completion)).Inc()

	slog.Info("server_initiated_completed",
		"auth_ctx_id", sessionCtx.AuthCtxID,
		"session_id", req.SessionID,
		"message_type", req.MessageType,
		"completion", result.Completion,
		"callback_owner", sessionCtx.CallbackOwner,
	)

	return result, result.Validate()
}
