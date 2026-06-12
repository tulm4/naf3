package gateway

import (
	"testing"
	"time"
)

func TestDiameterHandler_ASR_WaitsForBizPodResponse(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("test-session", "ASR", &ServerInitiatedResponse{
			AuthCtxID:  "test-auth",
			ResultCode: 2001,
		})
	}()

	ch, _ := registry.Register("test-session", "test-auth", "ASR", 5*time.Second)
	resp := ch.Wait()

	if resp.ResultCode != 2001 {
		t.Errorf("expected ResultCode 2001, got %d", resp.ResultCode)
	}
}

func TestDiameterHandler_ASR_TimeoutReturnsUnableToDeliver(t *testing.T) {
	registry := NewServerInitiatedRegistry(100 * time.Millisecond)
	ch, _ := registry.Register("test-session", "test-auth", "ASR", 100*time.Millisecond)

	resp := ch.Wait()
	if resp.ResultCode != 3002 {
		t.Errorf("expected ResultCode 3002, got %d", resp.ResultCode)
	}
	if resp.ErrorCause != "timeout" {
		t.Errorf("expected ErrorCause 'timeout', got %s", resp.ErrorCause)
	}
}

// Tests for DiameterHandler ASR wait-for-Biz response behavior.

func TestDiameterHandler_STR_ForwardsToBizPod(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("session-1", "STR", &ServerInitiatedResponse{
			AuthCtxID:  "auth-1",
			ResultCode: 2001,
		})
	}()

	ch, _ := registry.Register("session-1", "auth-1", "STR", 5*time.Second)
	resp := ch.Wait()

	if resp.ResultCode != 2001 {
		t.Errorf("expected ResultCode 2001, got %d", resp.ResultCode)
	}
}

func TestDiameterHandler_STR_TimeoutReturnsUnableToDeliver(t *testing.T) {
	registry := NewServerInitiatedRegistry(100 * time.Millisecond)
	ch, _ := registry.Register("session-1", "auth-1", "STR", 100*time.Millisecond)

	resp := ch.Wait()
	if resp.ResultCode != 3002 {
		t.Errorf("expected ResultCode 3002, got %d", resp.ResultCode)
	}
	if resp.ErrorCause != "timeout" {
		t.Errorf("expected ErrorCause 'timeout', got %s", resp.ErrorCause)
	}
}
