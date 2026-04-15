// Package aiw provides the Nnssaaf_AIW service operation handlers.
package aiw

import (
	"encoding/json"

	"github.com/operator/nssAAF/internal/types"
)

// AuthContext is the response body for POST /authentications (201 Created).
// Spec: TS 29.526 §7.3.2
type AuthContext struct {
	// Supi is the SUPI of the user.
	Supi string `json:"supi"`
	// EapMessage is the next EAP message from AAA-S.
	// May be null if no EAP exchange is needed.
	EapMessage *string `json:"eapMessage,omitempty"`
	// AuthCtxID is the opaque identifier for this authentication context.
	AuthCtxID string `json:"authCtxId"`
	// TttsInnerMethodContainer is used for EAP-TTLS inner method responses.
	TttsInnerMethodContainer *string `json:"ttlsInnerMethodContainer,omitempty"`
	// SupportedFeatures echoes back the client's supported features.
	SupportedFeatures string `json:"supportedFeatures,omitempty"`
}

// NewAuthContext creates a new AuthContext.
func NewAuthContext(supi, authCtxID string, eapMsg, ttlsContainer, features *string) *AuthContext {
	return &AuthContext{
		Supi:                     supi,
		AuthCtxID:                authCtxID,
		EapMessage:               eapMsg,
		TttsInnerMethodContainer: ttlsContainer,
		SupportedFeatures:        derefString(features),
	}
}

// AuthConfirmationResponse is the response body for PUT /authentications/{authCtxID}.
// Spec: TS 29.526 §7.3.3
type AuthConfirmationResponse struct {
	// Supi is the SUPI of the user.
	Supi string `json:"supi"`
	// EapMessage is the next EAP message from AAA-S. Null when terminal.
	EapMessage *string `json:"eapMessage,omitempty"`
	// AuthResult is the final authentication result.
	AuthResult *types.AuthResult `json:"authResult,omitempty"`
	// PvsInfo contains Privacy Violating Servers information.
	// Only populated on EAP_SUCCESS when applicable.
	// Spec: TS 33.501 §I.2.2.2
	PvsInfo []PvsInfo `json:"pvsInfo,omitempty"`
	// Msk is the Base64-encoded Master Session Key.
	// Only populated on EAP_SUCCESS.
	// Spec: RFC 5216 §2.1.4, TS 33.501 §I.2.2.2
	Msk *string `json:"msk,omitempty"`
	// SupportedFeatures echoes back the client's supported features.
	SupportedFeatures string `json:"supportedFeatures,omitempty"`
}

// MarshalJSON implements json.Marshaler with explicit field control.
func (r AuthConfirmationResponse) MarshalJSON() ([]byte, error) {
	m := make(map[string]any)
	m["supi"] = r.Supi
	if r.EapMessage != nil {
		m["eapMessage"] = *r.EapMessage
	}
	if r.AuthResult != nil {
		m["authResult"] = string(*r.AuthResult)
	}
	if len(r.PvsInfo) > 0 {
		m["pvsInfo"] = r.PvsInfo
	}
	if r.Msk != nil {
		m["msk"] = *r.Msk
	}
	if r.SupportedFeatures != "" {
		m["supportedFeatures"] = r.SupportedFeatures
	}
	return json.Marshal(m)
}

// NewAuthConfirmationResponse creates a response for an ongoing or completed AIW authentication.
func NewAuthConfirmationResponse(
	supi string,
	eapMsg *string,
	authResult *types.AuthResult,
	pvsInfo []PvsInfo,
	msk *string,
	features string,
) *AuthConfirmationResponse {
	return &AuthConfirmationResponse{
		Supi:              supi,
		EapMessage:        eapMsg,
		AuthResult:        authResult,
		PvsInfo:           pvsInfo,
		Msk:               msk,
		SupportedFeatures: features,
	}
}

// PvsInfo represents Privacy Violating Servers information.
// Spec: TS 33.501 §I.2.2.2
type PvsInfo struct {
	// ServerType identifies the type of privacy-violating server.
	ServerType string `json:"serverType"`
	// ServerID is the identifier of the server.
	ServerID string `json:"serverId"`
	// LocationInfo contains location-related privacy info.
	LocationInfo *LocationInfo `json:"locationInfo,omitempty"`
}

// LocationInfo contains location-related privacy information.
// Spec: TS 33.501 §I.2.2.2
type LocationInfo struct {
	CellID string `json:"cellId,omitempty"`
	TAC    uint32 `json:"tac,omitempty"`
}

// derefString safely dereferences a string pointer.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
