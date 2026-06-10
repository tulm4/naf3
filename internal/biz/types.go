package biz

import (
	"context"
	"fmt"

	"github.com/operator/nssAAF/internal/proto"
)

// NssaaSessionContext contains the persisted reverse-path state required to
// process AAA server-initiated callbacks.
type NssaaSessionContext struct {
	AuthCtxID      string
	SessionID      string
	GPSI           string
	Supi           string
	ReauthNotifURI string
	RevocNotifURI  string
	CallbackOwner  string
	HasAIWContext  bool
}

// Completion describes how a server-initiated message was completed.
type Completion string

const (
	CompletionStateOnly   Completion = "STATE_ONLY"
	CompletionAMFNotified Completion = "AMF_NOTIFIED"
	CompletionAIWLinked   Completion = "AIW_LINKED"
)

// ServerInitiatedResult is the Biz-side result for a server-initiated AAA message.
type ServerInitiatedResult struct {
	Response   proto.AaaServerInitiatedResponse
	Completion Completion
}

// Validate checks whether the server-initiated result has the minimum data
// required to send a transport-level response back to the AAA Gateway.
func (r ServerInitiatedResult) Validate() error {
	if len(r.Response.Payload) == 0 {
		return fmt.Errorf("response payload is required")
	}
	return nil
}

// SessionContextResolver resolves server-initiated state from correlation and/or
// durable persistence.
type SessionContextResolver interface {
	Resolve(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error)
}

// PersistentContextLookup loads the durable auth context when Redis correlation
// is absent or incomplete.
type PersistentContextLookup interface {
	LoadAuthContext(ctx context.Context, authCtxID string) (*NssaaSessionContext, error)
}

// SessionStateWriter persists reverse-path state transitions triggered by AAA.
type SessionStateWriter interface {
	MarkReauthPending(ctx context.Context, authCtxID string) error
	MarkRevoked(ctx context.Context, authCtxID string) error
	ApplyCoA(ctx context.Context, authCtxID string, payload []byte) error
}

// AMFNotifier sends server-initiated callbacks to the AMF when the persisted
// auth context is owned by AMF.
type AMFNotifier interface {
	SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error
	SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error
}

// AIWLinker updates AIW-owned reverse-flow state after AAA callbacks.
type AIWLinker interface {
	MarkAIWLinked(ctx context.Context, authCtxID string) error
}
