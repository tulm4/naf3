package resilience

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second, 3)
	assert.Equal(t, StateClosed, cb.State(), "circuit breaker should start in CLOSED state")
	assert.True(t, cb.Allow(), "Allow() should return true in CLOSED state")
}

func TestCircuitBreaker_TransitionToOpen(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second, 3)

	// Record 5 failures
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	assert.Equal(t, StateOpen, cb.State(), "circuit breaker should transition to OPEN after 5 failures")
	assert.False(t, cb.Allow(), "Allow() should return false when OPEN (before recovery timeout)")
}

func TestCircuitBreaker_TransitionToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.State())

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	assert.True(t, cb.Allow(), "Allow() should return true after recovery timeout")
	assert.Equal(t, StateHalfOpen, cb.State(), "state should be HALF_OPEN after timeout")
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // Trigger transition to HALF_OPEN

	// Record 3 successes
	for i := 0; i < 3; i++ {
		cb.RecordSuccess()
	}

	assert.Equal(t, StateClosed, cb.State(), "circuit breaker should transition to CLOSED after 3 successes in HALF_OPEN")
	assert.True(t, cb.Allow(), "Allow() should return true when CLOSED")
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // Trigger transition to HALF_OPEN

	// Record a failure in HALF_OPEN
	cb.RecordFailure()

	assert.Equal(t, StateOpen, cb.State(), "single failure in HALF_OPEN should trip circuit back to OPEN")
}

func TestCircuitBreaker_RecordSuccessInClosed(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second, 3)

	// Record some successes
	cb.RecordSuccess()
	cb.RecordSuccess()

	assert.Equal(t, StateClosed, cb.State(), "successes in CLOSED should keep state CLOSED")
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_AllowInOpen_BeforeTimeout(t *testing.T) {
	cb := NewCircuitBreaker(5, 100*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Try before timeout
	assert.False(t, cb.Allow(), "Allow() should return false when OPEN (before timeout)")
}

func TestCircuitBreaker_AllowInOpen_AfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// Allow should transition to HALF_OPEN and return true
	assert.True(t, cb.Allow(), "Allow() should return true when OPEN (after timeout)")
	assert.Equal(t, StateHalfOpen, cb.State())
}

func TestCircuitBreaker_Registry(t *testing.T) {
	registry := NewRegistry(5, 30*time.Second, 3)

	// Get same key twice should return same circuit breaker
	cb1 := registry.Get("aaa-server:1812")
	cb2 := registry.Get("aaa-server:1812")
	assert.Same(t, cb1, cb2, "same key should return same circuit breaker instance")

	// Get different key should return different circuit breaker
	cb3 := registry.Get("aaa-server:1813")
	assert.NotSame(t, cb1, cb3, "different key should return different circuit breaker instance")
}

func TestCircuitBreaker_String(t *testing.T) {
	assert.Equal(t, "CLOSED", StateClosed.String())
	assert.Equal(t, "OPEN", StateOpen.String())
	assert.Equal(t, "HALF_OPEN", StateHalfOpen.String())
	assert.Equal(t, "UNKNOWN", State(99).String())
}
