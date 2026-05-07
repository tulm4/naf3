// Package domain provides domain-level business logic for NSSAAF.
// Spec: TS 29.571 §5.4.4.60
package domain

import (
	"fmt"

	"github.com/operator/nssAAF/internal/types"
)

// NssaaStatus represents the NSSAA authentication status.
// Type alias for the existing types.NssaaStatus.
type NssaaStatus = types.NssaaStatus

// Re-export status constants for domain package convenience.
const (
	// StatusNotExecuted means NSSAA has not been executed for this S-NSSAI yet.
	StatusNotExecuted = types.NssaaStatusNotExecuted
	// StatusPending means NSSAA is in progress (EAP exchange ongoing).
	StatusPending = types.NssaaStatusPending
	// StatusSuccess means EAP authentication completed successfully.
	StatusSuccess = types.NssaaStatusEapSuccess
	// StatusFailure means EAP authentication failed.
	StatusFailure = types.NssaaStatusEapFailure
)

// AuthEvent represents events that trigger NssaaStatus state transitions.
type AuthEvent int

const (
	// EventAuthStarted indicates the NSSAA procedure has been initiated.
	EventAuthStarted AuthEvent = iota
	// EventEAPRound indicates an intermediate EAP exchange round.
	EventEAPRound
	// EventAAAComplete indicates the AAA server responded with success.
	EventAAAComplete
	// EventAAAFailed indicates the AAA server responded with failure.
	EventAAAFailed
)

// TransitionError represents an invalid state transition attempt.
type TransitionError struct {
	From NssaaStatus
	To   string // "event_name" for invalid events
}

// Error implements the error interface.
func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid NSSAA status transition from %s with event: %s", e.From, e.To)
}

// TransitionTo validates and returns the next NssaaStatus based on current state and event.
// Spec: TS 29.571 §5.4.4.60, TS 23.502 §4.2.9
//
// State machine:
//   NOT_EXECUTED + EventAuthStarted → PENDING
//   PENDING + EventAAAComplete → EAP_SUCCESS
//   PENDING + EventAAAFailed → EAP_FAILURE
//   PENDING + EventEAPRound → PENDING (intermediate round, no error)
//   EAP_SUCCESS / EAP_FAILURE (terminal states) absorb all events (return current, nil)
func TransitionTo(current NssaaStatus, event AuthEvent) (NssaaStatus, error) {
	switch current {
	case StatusNotExecuted:
		if event == EventAuthStarted {
			return StatusPending, nil
		}
		return current, &TransitionError{From: current, To: event.String()}

	case StatusPending:
		switch event {
		case EventAAAComplete:
			return StatusSuccess, nil
		case EventAAAFailed:
			return StatusFailure, nil
		case EventEAPRound:
			return StatusPending, nil // intermediate round
		case EventAuthStarted:
			return current, &TransitionError{From: current, To: event.String()}
		default:
			return current, &TransitionError{From: current, To: event.String()}
		}

	case StatusSuccess, StatusFailure:
		// Terminal states absorb all events
		return current, nil

	default:
		return current, &TransitionError{From: current, To: event.String()}
	}
}

// String implements fmt.Stringer for AuthEvent.
func (e AuthEvent) String() string {
	switch e {
	case EventAuthStarted:
		return "EventAuthStarted"
	case EventEAPRound:
		return "EventEAPRound"
	case EventAAAComplete:
		return "EventAAAComplete"
	case EventAAAFailed:
		return "EventAAAFailed"
	default:
		return fmt.Sprintf("AuthEvent(%d)", int(e))
	}
}
