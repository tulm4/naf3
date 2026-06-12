// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Task 1.1
package gateway

import (
	"sync"
	"time"
)

// Result codes for server-initiated responses.
const (
	ResultCodeSuccess          uint32 = 2001 // DIAMETER_SUCCESS
	ResultCodeUnableToDeliver uint32 = 3002 // UNABLE_TO_DELIVER
)

// ServerInitiatedResponse holds the result of a server-initiated message processing.
// Task 1.1
type ServerInitiatedResponse struct {
	AuthCtxID  string
	ResultCode uint32 // 2001=SUCCESS, 5xxx=ERROR
	Payload    []byte
	ErrorCause string
}

// ResponseChannel represents a pending server-initiated request waiting for a response.
// Task 1.1
type ResponseChannel struct {
	AuthCtxID   string
	SessionID   string
	MessageType string
	Response    chan *ServerInitiatedResponse
	CreatedAt   time.Time
	ExpiresAt   time.Time
	registry    *ServerInitiatedRegistry // back-reference for cleanup on timeout
}

// Wait blocks until a response is received or the timeout expires.
// Returns a timeout response (ResultCode=3002 UNABLE_TO_DELIVER) if expired.
// Task 1.1
func (rc *ResponseChannel) Wait() *ServerInitiatedResponse {
	select {
	case resp := <-rc.Response:
		return resp
	case <-time.After(time.Until(rc.ExpiresAt)):
		// Clean up registry entry to prevent memory leak
		if rc.registry != nil {
			rc.registry.Expire(rc.SessionID, rc.MessageType)
		}
		return &ServerInitiatedResponse{
			ResultCode: ResultCodeUnableToDeliver,
			ErrorCause: "timeout",
		}
	}
}

// ServerInitiatedRegistry manages pending server-initiated requests.
// Task 1.1
type ServerInitiatedRegistry struct {
	pending map[string]*ResponseChannel
	mu      sync.RWMutex
	timeout time.Duration
}

// NewServerInitiatedRegistry creates a new registry with the specified default timeout.
func NewServerInitiatedRegistry(defaultTimeout time.Duration) *ServerInitiatedRegistry {
	return &ServerInitiatedRegistry{
		pending: make(map[string]*ResponseChannel),
		timeout: defaultTimeout,
	}
}

// registryKey generates the map key for a session and message type.
func registryKey(sessionID, messageType string) string {
	return sessionID + ":" + messageType
}

// Register creates a new pending request and returns its ResponseChannel.
// Task 1.1
func (r *ServerInitiatedRegistry) Register(sessionID, authCtxID, messageType string, timeout time.Duration) (*ResponseChannel, error) {
	key := registryKey(sessionID, messageType)

	if timeout == 0 {
		timeout = r.timeout
	}

	rc := &ResponseChannel{
		AuthCtxID:   authCtxID,
		SessionID:   sessionID,
		MessageType: messageType,
		Response:    make(chan *ServerInitiatedResponse, 1),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
		registry:    r, // back-reference for cleanup on timeout
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.pending[key]; exists {
		// Return error response for duplicate registration
		go func() {
			rc.Response <- &ServerInitiatedResponse{
				ResultCode: ResultCodeUnableToDeliver,
				ErrorCause: "duplicate",
			}
		}()
		return rc, nil
	}

	r.pending[key] = rc
	return rc, nil
}

// Complete delivers a response to the pending request and removes it from the registry.
// Task 1.1
func (r *ServerInitiatedRegistry) Complete(sessionID, messageType string, resp *ServerInitiatedResponse) {
	key := registryKey(sessionID, messageType)

	r.mu.Lock()
	defer r.mu.Unlock()

	rc, exists := r.pending[key]
	if !exists {
		return
	}

	delete(r.pending, key)

	select {
	case rc.Response <- resp:
	default:
		// Channel already had a value (timeout already returned), ignore
	}
}

// Expire removes a pending request from the registry without delivering a response.
// Called when Wait() times out to prevent memory leaks.
// Task 1.1
func (r *ServerInitiatedRegistry) Expire(sessionID, messageType string) {
	key := registryKey(sessionID, messageType)

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.pending, key)
}
