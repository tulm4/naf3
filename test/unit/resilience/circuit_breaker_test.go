// Package resilience_test provides circuit breaker unit tests for NRM alarm integration.
// Spec: TS 33.501 §16, REQ-34
package resilience_test

import (
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
	"github.com/stretchr/testify/assert"
)

// TestCircuitBreaker_FailureThresholdReached verifies that the circuit transitions
// to OPEN exactly when the failure threshold is reached.
func TestCircuitBreaker_FailureThresholdReached(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 100*time.Millisecond, 3)

	// 4 failures — circuit still closed
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, resilience.StateClosed, cb.State(), "circuit must remain CLOSED below threshold")

	// 5th failure — threshold reached
	cb.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb.State(), "circuit must transition to OPEN at threshold")
	assert.False(t, cb.Allow(), "Allow() must return false when OPEN")
}

// TestCircuitBreaker_SuccessBelowThreshold verifies that successes below the
// success threshold in HALF_OPEN do not transition to CLOSED.
func TestCircuitBreaker_SuccessBelowThreshold(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, resilience.StateOpen, cb.State())

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// Transition to HALF_OPEN
	cb.Allow()
	assert.Equal(t, resilience.StateHalfOpen, cb.State())

	// Record 2 successes — below threshold of 3
	cb.RecordSuccess()
	cb.RecordSuccess()
	assert.Equal(t, resilience.StateHalfOpen, cb.State(),
		"circuit must remain HALF_OPEN when successes are below threshold")
}

// TestCircuitBreaker_RecoveryTimeout verifies that after the recovery timeout
// elapses, a CLOSED circuit allows requests.
func TestCircuitBreaker_RecoveryTimeout(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 15*time.Millisecond, 3)

	// Initially CLOSED — should allow
	assert.True(t, cb.Allow())
	assert.Equal(t, resilience.StateClosed, cb.State())

	// Record 4 failures — still closed
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, resilience.StateClosed, cb.State())

	// Record 5th — OPEN
	cb.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb.State())

	// Wait less than timeout — still blocked
	time.Sleep(5 * time.Millisecond)
	assert.False(t, cb.Allow(), "Allow() must return false before timeout")

	// Wait for full timeout
	time.Sleep(12 * time.Millisecond)
	assert.True(t, cb.Allow(), "Allow() must return true after recovery timeout")
	assert.Equal(t, resilience.StateHalfOpen, cb.State(), "state must transition to HALF_OPEN after timeout")
}

// TestCircuitBreaker_HalfOpenSuccess verifies that a successful request in
// HALF_OPEN transitions to CLOSED after the success threshold is reached.
func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, resilience.StateOpen, cb.State())

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// Transition to HALF_OPEN
	cb.Allow()
	assert.Equal(t, resilience.StateHalfOpen, cb.State())

	// Record 3 successes — transition to CLOSED
	cb.RecordSuccess()
	cb.RecordSuccess()
	cb.RecordSuccess()

	assert.Equal(t, resilience.StateClosed, cb.State(), "circuit must transition to CLOSED after 3 successes")
	assert.True(t, cb.Allow(), "Allow() must return true when CLOSED")
}

// TestCircuitBreaker_HalfOpenFailure verifies that a single failure in
// HALF_OPEN immediately transitions back to OPEN.
func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Wait for recovery timeout and transition to HALF_OPEN
	time.Sleep(15 * time.Millisecond)
	cb.Allow()
	assert.Equal(t, resilience.StateHalfOpen, cb.State())

	// Single failure in HALF_OPEN
	cb.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb.State(),
		"single failure in HALF_OPEN must transition circuit back to OPEN")
}

// TestCircuitBreaker_StateReadout verifies that the circuit breaker exposes its
// current state for NRM monitoring integration (REQ-34).
func TestCircuitBreaker_StateReadout(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 100*time.Millisecond, 3)

	// CLOSED state readout
	assert.Equal(t, resilience.StateClosed, cb.State())
	assert.Equal(t, "CLOSED", cb.State().String())

	// OPEN state readout
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, resilience.StateOpen, cb.State())
	assert.Equal(t, "OPEN", cb.State().String())

	// HALF_OPEN state readout (via Allow after timeout)
	time.Sleep(120 * time.Millisecond)
	cb.Allow()
	assert.Equal(t, resilience.StateHalfOpen, cb.State())
	assert.Equal(t, "HALF_OPEN", cb.State().String())
}

// TestCircuitBreaker_ServerIdentification verifies that the circuit breaker
// registry associates a server identifier (host:port) with each breaker for
// alarm correlation in NRM (REQ-34).
func TestCircuitBreaker_ServerIdentification(t *testing.T) {
	registry := resilience.NewRegistry(5, 30*time.Second, 3)

	// Get circuit breaker for a specific server
	cb1 := registry.Get("aaa-server-1:1812")
	cb2 := registry.Get("aaa-server-1:1812")

	// Same key returns the same instance
	assert.Same(t, cb1, cb2,
		"same server identifier must return the same circuit breaker instance")

	// Different server returns different instance
	cb3 := registry.Get("aaa-server-2:1812")
	assert.NotSame(t, cb1, cb3,
		"different server identifiers must return different circuit breaker instances")

	// State of one server does not affect another
	cb1.RecordFailure()
	cb1.RecordFailure()
	cb1.RecordFailure()
	cb1.RecordFailure()
	cb1.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb1.State())
	assert.Equal(t, resilience.StateClosed, cb3.State(),
		"failure on one server must not affect circuit breaker for another server")
}
