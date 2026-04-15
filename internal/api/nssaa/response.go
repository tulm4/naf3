// Package nssaa provides the Nnssaaf_NSSAA service operation handlers.
package nssaa

import (
	"encoding/json"

	"github.com/operator/nssAAF/internal/types"
)

// SliceAuthContext is the response body for POST /slice-authentications (201 Created).
// Spec: TS 29.526 §7.2.2
type SliceAuthContext struct {
	// Gpsi is the GPSI of the UE.
	Gpsi string `json:"gpsi"`
	// Snssai is the S-NSSAI for this authentication.
	Snssai types.Snssai `json:"snssai"`
	// EapMessage is the next EAP message from AAA-S to forward to the UE.
	// May be null if the authentication is already complete.
	EapMessage *string `json:"eapMessage,omitempty"`
	// AuthCtxID is the opaque identifier for this authentication context.
	// Assigned by NSSAAF. Format is an opaque string (UUIDv7 recommended).
	// Spec: TS 29.526 §7.2.2
	AuthCtxID string `json:"authCtxId"`
}

// MarshalJSON implements json.Marshaler with explicit field control.
func (r SliceAuthContext) MarshalJSON() ([]byte, error) {
	m := make(map[string]any)
	m["gpsi"] = r.Gpsi
	m["snssai"] = r.Snssai
	m["authCtxId"] = r.AuthCtxID
	if r.EapMessage != nil {
		m["eapMessage"] = *r.EapMessage
	}
	return json.Marshal(m)
}

// SliceAuthConfirmationResponse is the response body for PUT /slice-authentications/{authCtxID}.
// Spec: TS 29.526 §7.2.3
type SliceAuthConfirmationResponse struct {
	// Gpsi is the GPSI of the UE.
	Gpsi string `json:"gpsi"`
	// Snssai is the S-NSSAI for this authentication.
	Snssai types.Snssai `json:"snssai"`
	// EapMessage is the next EAP message from AAA-S to forward to the UE.
	// Null when authResult is set (terminal state).
	EapMessage *string `json:"eapMessage,omitempty"`
	// AuthResult is the final authentication result.
	// Set to "EAP_SUCCESS" or "EAP_FAILURE" when the authentication is complete.
	// Null during multi-round EAP exchanges.
	AuthResult *types.AuthResult `json:"authResult,omitempty"`
}

// MarshalJSON implements json.Marshaler with explicit field control.
func (r SliceAuthConfirmationResponse) MarshalJSON() ([]byte, error) {
	m := make(map[string]any)
	m["gpsi"] = r.Gpsi
	m["snssai"] = r.Snssai
	if r.EapMessage != nil {
		m["eapMessage"] = *r.EapMessage
	}
	if r.AuthResult != nil {
		m["authResult"] = string(*r.AuthResult)
	}
	return json.Marshal(m)
}

// NewSliceAuthContext creates a new SliceAuthContext.
func NewSliceAuthContext(gpsi string, snssai types.Snssai, authCtxID string, eapMsg *string) *SliceAuthContext {
	return &SliceAuthContext{
		Gpsi:       gpsi,
		Snssai:     snssai,
		AuthCtxID:  authCtxID,
		EapMessage: eapMsg,
	}
}

// NewSliceAuthConfirmationResponse creates a response for an ongoing or completed EAP exchange.
func NewSliceAuthConfirmationResponse(
	gpsi string,
	snssai types.Snssai,
	eapMsg *string,
	authResult *types.AuthResult,
) *SliceAuthConfirmationResponse {
	return &SliceAuthConfirmationResponse{
		Gpsi:       gpsi,
		Snssai:     snssai,
		EapMessage: eapMsg,
		AuthResult: authResult,
	}
}

// AuthResultFromStatus converts an NssaaStatus to an AuthResult.
func AuthResultFromStatus(s types.NssaaStatus) *types.AuthResult {
	switch s {
	case types.NssaaStatusEapSuccess:
		r := types.AuthResultSuccess
		return &r
	case types.NssaaStatusEapFailure:
		r := types.AuthResultFailure
		return &r
	default:
		return nil
	}
}
