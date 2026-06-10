// Package resilience provides circuit breaker unit tests for NRM alarm integration.
// Spec: TS 33.501 §16, REQ-34
package resilience

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCircuitBreaker_FailureThresholdReached verifies that the circuit transitions
// to OPEN exactly when the failure threshold is reached.
func TestCircuitBreaker_FailureThresholdReached(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second, 3)

	// 4 failures — circuit still closed
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, StateClosed, cb.State(), "circuit must remain CLOSED below threshold")

	// 5th failure — threshold reached
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State(), "circuit must transition to OPEN at threshold")
	assert.False(t, cb.Allow(), "Allow() must return false when OPEN")
}

// TestCircuitBreaker_SuccessBelowThreshold verifies that successes below the
// success threshold in HALF_OPEN do not transition to CLOSED.
func TestCircuitBreaker_SuccessBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.State())

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// Transition to HALF_OPEN
	cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.State())

	// Record 2 successes — below threshold of 3
	cb.RecordSuccess()
	cb.RecordSuccess()
	assert.Equal(t, StateHalfOpen, cb.State(),
		"circuit must remain HALF_OPEN when successes are below threshold")
}

// TestCircuitBreaker_RecoveryTimeout verifies that after the recovery timeout
// elapses, a CLOSED circuit allows requests.
func TestCircuitBreaker_RecoveryTimeout(t *testing.T) {
	cb := NewCircuitBreaker(5, 15*time.Millisecond, 3)

	// Initially CLOSED — should allow
	assert.True(t, cb.Allow())
	assert.Equal(t, StateClosed, cb.State())

	// Record 4 failures — still closed
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, StateClosed, cb.State())

	// Record 5th — OPEN
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State())

	// Wait less than timeout — still blocked
	time.Sleep(5 * time.Millisecond)
	assert.False(t, cb.Allow(), "Allow() must return false before timeout")

	// Wait for full timeout
	time.Sleep(12 * time.Millisecond)
	assert.True(t, cb.Allow(), "Allow() must return true after recovery timeout")
	assert.Equal(t, StateHalfOpen, cb.State(), "state must transition to HALF_OPEN after timeout")
}

// TestCircuitBreaker_HalfOpenSuccess verifies that a successful request in
// HALF_OPEN transitions to CLOSED after the success threshold is reached.
func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.State())

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// Transition to HALF_OPEN
	cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.State())

	// Record 3 successes — transition to CLOSED
	cb.RecordSuccess()
	cb.RecordSuccess()
	cb.RecordSuccess()

	assert.Equal(t, StateClosed, cb.State(), "circuit must transition to CLOSED after 3 successes")
	assert.True(t, cb.Allow(), "Allow() must return true when CLOSED")
}

// TestCircuitBreaker_HalfOpenFailure verifies that a single failure in
// HALF_OPEN immediately transitions back to OPEN.
func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Wait for recovery timeout and transition to HALF_OPEN
	time.Sleep(15 * time.Millisecond)
	cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.State())

	// Single failure in HALF_OPEN
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State(),
		"single failure in HALF_OPEN must transition circuit back to OPEN")
}

// TestCircuitBreaker_StateReadout verifies that the circuit breaker exposes its
// current state for NRM monitoring integration (REQ-34).
func TestCircuitBreaker_StateReadout(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// CLOSED state readout
	assert.Equal(t, StateClosed, cb.State())
	assert.Equal(t, "CLOSED", cb.State().String())

	// OPEN state readout
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, StateOpen, cb.State())
	assert.Equal(t, "OPEN", cb.State().String())

	// HALF_OPEN state readout (via Allow after timeout).
	// Allow() transitions OPEN→HALF_OPEN atomically under lock; capture
	// state immediately so we don't race with another goroutine.
	time.Sleep(15 * time.Millisecond)
	_ = cb.Allow() // transitions to HALF_OPEN atomically
	assert.Equal(t, StateHalfOpen, cb.State())
	assert.Equal(t, "HALF_OPEN", cb.State().String())
}

// TestCircuitBreaker_ServerIdentification verifies that the circuit breaker
// registry associates a server identifier (host:port) with each breaker for
// alarm correlation in NRM (REQ-34).
func TestCircuitBreaker_ServerIdentification(t *testing.T) {
	registry := NewRegistry(5, 30*time.Second, 3)

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
	assert.Equal(t, StateOpen, cb1.State())
	assert.Equal(t, StateClosed, cb3.State(),
		"failure on one server must not affect circuit breaker for another server")
}

// TestCircuitBreaker_NoSpuriousTransitions is a regression test for the circuit
// breaker state-transition bug at the primitive layer.
//
// BUG: Higher-level callers (AMF notifier, etc.) may emit spurious transition
// metrics when no real state change occurred (e.g., CLOSED → CLOSED). This test
// verifies the primitive state machine is correct so callers can reliably detect
// genuine transitions.
//
// RESPONSIBILITY BOUNDARY:
//   - CircuitBreaker primitive: tracks state, performs transitions; does NOT
//     emit metrics.
//   - Caller responsibility: MUST only emit transition metrics when State()
//     actually changes. The caller must snapshot state before an operation,
//     compare with state after, and only emit if they differ.
//
// Example caller pattern (PSEUDOCODE):
//
//	before := cb.State()
//	_ = cb.Allow()
//	cb.RecordSuccess() // or RecordFailure()
//	after := cb.State()
//	if before != after {
//	    emitMetric("circuit_transition", before, after)
//	}
//
// This test exercises the full transition cycle to give callers a reliable
// ground truth for verifying their metric emission logic.
func TestCircuitBreaker_NoSpuriousTransitions(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond, 3)

	// --- CLOSED phase: no transitions expected ---

	// Allow() when CLOSED → remains CLOSED, no transition
	assert.Equal(t, StateClosed, cb.State())
	_ = cb.Allow()
	assert.Equal(t, StateClosed, cb.State(),
		"Allow() on CLOSED must not change state")

	// RecordSuccess() when CLOSED → remains CLOSED, no transition
	assert.Equal(t, StateClosed, cb.State())
	cb.RecordSuccess()
	assert.Equal(t, StateClosed, cb.State(),
		"RecordSuccess() on CLOSED must not change state")

	// RecordFailure() 4 times below threshold → remains CLOSED
	for i := 0; i < 4; i++ {
		assert.Equal(t, StateClosed, cb.State())
		cb.RecordFailure()
		assert.Equal(t, StateClosed, cb.State(),
			"RecordFailure() below threshold must not change state")
	}

	// --- CLOSED → OPEN transition ---

	// 5th RecordFailure() → transitions CLOSED → OPEN
	assert.Equal(t, StateClosed, cb.State())
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State(),
		"5th RecordFailure() must transition CLOSED → OPEN")

	// RecordFailure() when OPEN → remains OPEN (no transition)
	// In OPEN state, failures are ignored — circuit stays open.
	assert.Equal(t, StateOpen, cb.State())
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State(),
		"RecordFailure() on OPEN must not change state (failure is already open)")

	// Allow() when OPEN before timeout → remains OPEN, no transition
	assert.Equal(t, StateOpen, cb.State())
	_ = cb.Allow()
	assert.Equal(t, StateOpen, cb.State(),
		"Allow() on OPEN before timeout must not change state")

	// --- OPEN → HALF_OPEN transition ---

	// Wait for recovery timeout
	time.Sleep(15 * time.Millisecond)

	// Allow() after timeout → transitions OPEN → HALF_OPEN
	assert.Equal(t, StateOpen, cb.State())
	_ = cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.State(),
		"Allow() after timeout must transition OPEN → HALF_OPEN")

	// Allow() when HALF_OPEN → remains HALF_OPEN, no transition
	assert.Equal(t, StateHalfOpen, cb.State())
	_ = cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.State(),
		"Allow() on HALF_OPEN must not change state")

	// RecordFailure() when HALF_OPEN → transitions HALF_OPEN → OPEN
	assert.Equal(t, StateHalfOpen, cb.State())
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State(),
		"RecordFailure() on HALF_OPEN must transition HALF_OPEN → OPEN")

	// Re-enter HALF_OPEN to test success path
	time.Sleep(15 * time.Millisecond)
	_ = cb.Allow()
	assert.Equal(t, StateHalfOpen, cb.State())

	// --- HALF_OPEN → CLOSED transition ---

	// RecordSuccess() below threshold → remains HALF_OPEN
	assert.Equal(t, StateHalfOpen, cb.State())
	cb.RecordSuccess()
	assert.Equal(t, StateHalfOpen, cb.State(),
		"RecordSuccess() below threshold must not change state")

	// RecordSuccess() at threshold → transitions HALF_OPEN → CLOSED
	for i := 0; i < 3; i++ {
		before := cb.State()
		cb.RecordSuccess()
		after := cb.State()
		if before != after {
			break // transition happened
		}
	}
	assert.Equal(t, StateClosed, cb.State(),
		"RecordSuccess() at threshold must transition HALF_OPEN → CLOSED")

	// --- Back to CLOSED: confirm no spurious transitions ---

	// RecordSuccess() when CLOSED → remains CLOSED, no transition
	assert.Equal(t, StateClosed, cb.State())
	cb.RecordSuccess()
	assert.Equal(t, StateClosed, cb.State(),
		"RecordSuccess() on CLOSED must not change state")

	// RecordFailure() below threshold → remains CLOSED
	for i := 0; i < 4; i++ {
		assert.Equal(t, StateClosed, cb.State())
		cb.RecordFailure()
		assert.Equal(t, StateClosed, cb.State(),
			"RecordFailure() below threshold must not change state")
	}

	// Confirm closed state after all operations
	assert.Equal(t, StateClosed, cb.State())
}
