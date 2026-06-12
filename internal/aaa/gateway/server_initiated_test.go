package gateway

import (
	"testing"
	"time"
)

func TestServerInitiatedRegistry_RegisterAndWait(t *testing.T) {
	reg := NewServerInitiatedRegistry(500 * time.Millisecond)
	sessionID := "sess-123"
	messageType := "ASR"
	authCtxID := "auth-456"

	// Register a request
	ch, err := reg.Register(sessionID, authCtxID, messageType, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if ch == nil {
		t.Fatal("Register() returned nil channel")
	}
	if ch.AuthCtxID != authCtxID {
		t.Errorf("AuthCtxID = %q, want %q", ch.AuthCtxID, authCtxID)
	}
	if ch.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", ch.SessionID, sessionID)
	}
	if ch.MessageType != messageType {
		t.Errorf("MessageType = %q, want %q", ch.MessageType, messageType)
	}

	// Complete the request before timeout
	resp := &ServerInitiatedResponse{
		AuthCtxID:  authCtxID,
		ResultCode: 2001,
		Payload:    []byte("success"),
		ErrorCause: "",
	}

	// Start a goroutine to complete after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		reg.Complete(sessionID, messageType, resp)
	}()

	// Wait should return the response
	got := ch.Wait()
	if got == nil {
		t.Fatal("Wait() returned nil")
	}
	if got.ResultCode != 2001 {
		t.Errorf("ResultCode = %d, want %d", got.ResultCode, 2001)
	}
	if got.AuthCtxID != authCtxID {
		t.Errorf("AuthCtxID = %q, want %q", got.AuthCtxID, authCtxID)
	}
	if string(got.Payload) != "success" {
		t.Errorf("Payload = %q, want %q", got.Payload, "success")
	}
}

func TestServerInitiatedRegistry_Timeout(t *testing.T) {
	reg := NewServerInitiatedRegistry(500 * time.Millisecond)
	sessionID := "sess-timeout"
	messageType := "ASR"
	authCtxID := "auth-timeout"

	// Register with 100ms timeout
	ch, err := reg.Register(sessionID, authCtxID, messageType, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Wait should return timeout response
	got := ch.Wait()
	if got == nil {
		t.Fatal("Wait() returned nil")
	}
	if got.ResultCode != 3002 {
		t.Errorf("ResultCode = %d, want %d (UNABLE_TO_DELIVER)", got.ResultCode, 3002)
	}
	if got.ErrorCause != "timeout" {
		t.Errorf("ErrorCause = %q, want %q", got.ErrorCause, "timeout")
	}
}

func TestServerInitiatedRegistry_CompleteRemovesFromPending(t *testing.T) {
	reg := NewServerInitiatedRegistry(500 * time.Millisecond)
	sessionID := "sess-complete-remove"
	messageType := "ASR"
	authCtxID := "auth-complete"

	ch, err := reg.Register(sessionID, authCtxID, messageType, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Complete the request
	resp := &ServerInitiatedResponse{
		AuthCtxID:  authCtxID,
		ResultCode: 2001,
		Payload:    []byte("done"),
	}
	reg.Complete(sessionID, messageType, resp)

	// Wait on the channel should return immediately
	got := ch.Wait()
	if got == nil {
		t.Fatal("Wait() returned nil")
	}
	if got.ResultCode != 2001 {
		t.Errorf("ResultCode = %d, want %d", got.ResultCode, 2001)
	}

	// Trying to register the same key should now work (channel was removed)
	ch2, err := reg.Register(sessionID, authCtxID, messageType, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Register() second time error = %v", err)
	}
	if ch2 == nil {
		t.Fatal("Register() second time returned nil channel")
	}
}

func TestServerInitiatedRegistry_DuplicateRegistration(t *testing.T) {
	reg := NewServerInitiatedRegistry(500 * time.Millisecond)
	sessionID := "sess-duplicate"
	messageType := "ASR"
	authCtxID := "auth-dup"

	// First registration should succeed
	ch1, err := reg.Register(sessionID, authCtxID, messageType, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("First Register() error = %v", err)
	}

	// Second registration with same key should fail with UNABLE_TO_DELIVER
	ch2, err := reg.Register(sessionID, authCtxID, messageType, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Second Register() error = %v", err)
	}

	// The second registration should have received a duplicate error response
	got := ch2.Wait()
	if got == nil {
		t.Fatal("Wait() returned nil for duplicate registration")
	}
	if got.ResultCode != 3002 {
		t.Errorf("ResultCode = %d, want %d (UNABLE_TO_DELIVER)", got.ResultCode, 3002)
	}
	if got.ErrorCause != "duplicate" {
		t.Errorf("ErrorCause = %q, want %q", got.ErrorCause, "duplicate")
	}

	// First channel should still work
	ch1.Wait() // This will timeout since we didn't complete it
}
