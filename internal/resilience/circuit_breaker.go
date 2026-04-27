// Package resilience provides high-availability patterns: circuit breakers,
// retries, load balancing, and failover mechanisms.
package resilience

import (
	"sync"
	"time"
)

// State represents the circuit breaker state machine.
// Spec: TS 33.501 §16 — circuit breaker pattern for AAA server failure isolation
const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// State is the state of a circuit breaker.
type State int

// String implements fmt.Stringer.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker implements a per-host:port circuit breaker.
// REQ-11: CLOSED → OPEN (5 consecutive failures) → HALF_OPEN (30s recovery) → CLOSED (3 successes)
// D-03: Registry keyed by "host:port"
type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failures         int
	successes        int
	lastFailure      time.Time
	openedAt         time.Time
	failureThreshold int
	recoveryTimeout  time.Duration
	successThreshold int
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
// Default: failureThreshold=5, recoveryTimeout=30s, successThreshold=3.
func NewCircuitBreaker(failureThreshold int, recoveryTimeout, successThreshold time.Duration) *CircuitBreaker {
	if failureThreshold == 0 {
		failureThreshold = 5
	}
	if recoveryTimeout == 0 {
		recoveryTimeout = 30 * time.Second
	}
	if successThreshold == 0 {
		successThreshold = 3
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		recoveryTimeout:  recoveryTimeout,
		successThreshold: int(successThreshold),
	}
}

// Allow returns true if the circuit breaker allows a request.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) >= cb.recoveryTimeout {
			cb.state = StateHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = StateClosed
			cb.failures = 0
		}
	case StateClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = time.Now()
	cb.failures++

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.failureThreshold {
			cb.state = StateOpen
			cb.openedAt = time.Now()
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.openedAt = time.Now()
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Registry manages named circuit breakers keyed by "host:port".
// D-03: CircuitBreakerRegistry keyed by "host:port".
type Registry struct {
	mu                      sync.RWMutex
	breakers                map[string]*CircuitBreaker
	defaultFailureThreshold int
	defaultRecoveryTimeout  time.Duration
	defaultSuccessThreshold int
}

// NewRegistry creates a circuit breaker registry with defaults.
func NewRegistry(failureThreshold int, recoveryTimeout, successThreshold time.Duration) *Registry {
	if failureThreshold == 0 {
		failureThreshold = 5
	}
	if recoveryTimeout == 0 {
		recoveryTimeout = 30 * time.Second
	}
	if successThreshold == 0 {
		successThreshold = 3
	}
	return &Registry{
		breakers:                make(map[string]*CircuitBreaker),
		defaultFailureThreshold: failureThreshold,
		defaultRecoveryTimeout:  recoveryTimeout,
		defaultSuccessThreshold: int(successThreshold),
	}
}

// Get returns the circuit breaker for a given key, creating it if needed.
func (r *Registry) Get(key string) *CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[key]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if cb, ok := r.breakers[key]; ok {
		return cb
	}
	cb = NewCircuitBreaker(r.defaultFailureThreshold, r.defaultRecoveryTimeout, time.Duration(r.defaultSuccessThreshold))
	r.breakers[key] = cb
	return cb
}
