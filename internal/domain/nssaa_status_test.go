package domain

import (
	"testing"
)

func TestTransitionTo(t *testing.T) {
	cases := []struct {
		name     string
		from     NssaaStatus
		event    AuthEvent
		expected NssaaStatus
		wantErr  bool
	}{
		// NOT_EXECUTED transitions
		{
			name:     "not_executed_to_pending_on_auth_started",
			from:     StatusNotExecuted,
			event:    EventAuthStarted,
			expected: StatusPending,
			wantErr:  false,
		},
		{
			name:     "not_executed_ignores_eap_round",
			from:     StatusNotExecuted,
			event:    EventEAPRound,
			expected: StatusNotExecuted,
			wantErr:  true,
		},
		{
			name:     "not_executed_ignores_aaa_complete",
			from:     StatusNotExecuted,
			event:    EventAAAComplete,
			expected: StatusNotExecuted,
			wantErr:  true,
		},
		{
			name:     "not_executed_ignores_aaa_failed",
			from:     StatusNotExecuted,
			event:    EventAAAFailed,
			expected: StatusNotExecuted,
			wantErr:  true,
		},

		// PENDING transitions
		{
			name:     "pending_to_success_on_aaa_complete",
			from:     StatusPending,
			event:    EventAAAComplete,
			expected: StatusSuccess,
			wantErr:  false,
		},
		{
			name:     "pending_to_failure_on_aaa_failed",
			from:     StatusPending,
			event:    EventAAAFailed,
			expected: StatusFailure,
			wantErr:  false,
		},
		{
			name:     "pending_remains_pending_on_eap_round",
			from:     StatusPending,
			event:    EventEAPRound,
			expected: StatusPending,
			wantErr:  false,
		},
		{
			name:     "pending_ignores_auth_started",
			from:     StatusPending,
			event:    EventAuthStarted,
			expected: StatusPending,
			wantErr:  true,
		},

		// Terminal state transitions (terminal states absorb all events)
		{
			name:     "success_absorbs_auth_started",
			from:     StatusSuccess,
			event:    EventAuthStarted,
			expected: StatusSuccess,
			wantErr:  false,
		},
		{
			name:     "success_absorbs_eap_round",
			from:     StatusSuccess,
			event:    EventEAPRound,
			expected: StatusSuccess,
			wantErr:  false,
		},
		{
			name:     "success_absorbs_aaa_complete",
			from:     StatusSuccess,
			event:    EventAAAComplete,
			expected: StatusSuccess,
			wantErr:  false,
		},
		{
			name:     "success_absorbs_aaa_failed",
			from:     StatusSuccess,
			event:    EventAAAFailed,
			expected: StatusSuccess,
			wantErr:  false,
		},
		{
			name:     "failure_absorbs_auth_started",
			from:     StatusFailure,
			event:    EventAuthStarted,
			expected: StatusFailure,
			wantErr:  false,
		},
		{
			name:     "failure_absorbs_eap_round",
			from:     StatusFailure,
			event:    EventEAPRound,
			expected: StatusFailure,
			wantErr:  false,
		},
		{
			name:     "failure_absorbs_aaa_complete",
			from:     StatusFailure,
			event:    EventAAAComplete,
			expected: StatusFailure,
			wantErr:  false,
		},
		{
			name:     "failure_absorbs_aaa_failed",
			from:     StatusFailure,
			event:    EventAAAFailed,
			expected: StatusFailure,
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TransitionTo(tc.from, tc.event)

			if tc.wantErr {
				if err == nil {
					t.Errorf("TransitionTo(%s, %s) = %s, want error",
						tc.from, tc.event, got)
				}
				return
			}

			if err != nil {
				t.Errorf("TransitionTo(%s, %s) returned unexpected error: %v",
					tc.from, tc.event, err)
				return
			}

			if got != tc.expected {
				t.Errorf("TransitionTo(%s, %s) = %s, want %s",
					tc.from, tc.event, got, tc.expected)
			}
		})
	}
}

func TestTransitionError(t *testing.T) {
	err := &TransitionError{From: StatusNotExecuted, To: "unknown"}

	if err.Error() == "" {
		t.Error("TransitionError.Error() should not be empty")
	}

	// Verify it implements error interface
	var _ error = err
}
