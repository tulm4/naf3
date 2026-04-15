// request.go — AIW API request types
// Spec: TS 29.526 §7.3
package aiw

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/types"
)

// AuthInfo is the request body for POST /authentications.
// Spec: TS 29.526 §7.3.2
type AuthInfo struct {
	// Supi is the Subscription Permanent Identifier of the user.
	// Required. Must match pattern ^imu-[0-9]{15}$.
	// Spec: TS 29.571 §5.4.4.2
	Supi string `json:"supi"`

	// EapIdRsp is the Base64-encoded EAP Identity Response from the UE.
	// Optional for initial request. If absent, NSSAAF returns an initial EAP request.
	// Spec: RFC 3748
	EapIdRsp string `json:"eapIdRsp,omitempty"`

	// TttsInnerMethodContainer is used for EAP-TTLS inner method.
	// Optional. Base64-encoded.
	// Spec: TS 29.526 §7.3.2
	TtlsInnerMethodContainer string `json:"ttlsInnerMethodContainer,omitempty"`

	// SupportedFeatures is a string encoding optional features supported by the AUSF.
	// Spec: TS 29.526 §7.3.2
	SupportedFeatures string `json:"supportedFeatures,omitempty"`
}

// Validate checks that all required fields are present and well-formed.
func (r *AuthInfo) Validate() []error {
	var errs []error

	// SUPI is required
	if err := types.Supi(r.Supi).Validate(); err != nil {
		errs = append(errs, err)
	}

	// EapIdRsp is optional, but if present must be valid Base64
	if r.EapIdRsp != "" {
		if _, err := base64.StdEncoding.DecodeString(r.EapIdRsp); err != nil {
			errs = append(errs, &types.ValidationError{
				Field:      "eapIdRsp",
				Reason:     "EAP message must be valid Base64 (RFC 3748)",
				HTTPStatus: http.StatusBadRequest,
				Cause:      types.CauseInvalidEapPayload,
			})
		}
	}

	// TttsInnerMethodContainer: if present, must be valid Base64
	if r.TtlsInnerMethodContainer != "" {
		if _, err := base64.StdEncoding.DecodeString(r.TtlsInnerMethodContainer); err != nil {
			errs = append(errs, &types.ValidationError{
				Field:      "ttlsInnerMethodContainer",
				Reason:     "TTLS inner method container must be valid Base64",
				HTTPStatus: http.StatusBadRequest,
				Cause:      types.CauseInvalidEapPayload,
			})
		}
	}

	return errs
}

// ParseAuthInfo parses an AuthInfo from JSON.
func ParseAuthInfo(data []byte) (*AuthInfo, error) {
	var info AuthInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}
	return &info, nil
}

// ConfirmAuthData is the request body for PUT /authentications/{authCtxId}.
// Spec: TS 29.526 §7.3.3
type ConfirmAuthData struct {
	// Supi is the SUPI of the user.
	Supi string `json:"supi"`

	// EapMessage is the Base64-encoded EAP response from the UE.
	EapMessage string `json:"eapMessage,omitempty"`

	// SupportedFeatures is a string encoding optional features.
	SupportedFeatures string `json:"supportedFeatures,omitempty"`
}

// Validate checks that the required fields are well-formed.
func (r *ConfirmAuthData) Validate() []error {
	var errs []error

	if err := types.Supi(r.Supi).Validate(); err != nil {
		errs = append(errs, err)
	}

	// EapMessage is required
	if r.EapMessage == "" {
		errs = append(errs, &types.ValidationError{
			Field:      "eapMessage",
			Reason:     "EAP message is required",
			HTTPStatus: http.StatusBadRequest,
			Cause:      types.CauseMissingEapPayload,
		})
	} else if _, err := base64.StdEncoding.DecodeString(r.EapMessage); err != nil {
		errs = append(errs, &types.ValidationError{
			Field:      "eapMessage",
			Reason:     "EAP message must be valid Base64 (RFC 3748)",
			HTTPStatus: http.StatusBadRequest,
			Cause:      types.CauseInvalidEapPayload,
		})
	}

	return errs
}

// ParseConfirmAuthData parses a ConfirmAuthData from JSON.
func ParseConfirmAuthData(data []byte) (*ConfirmAuthData, error) {
	var authData ConfirmAuthData
	if err := json.Unmarshal(data, &authData); err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}
	return &authData, nil
}
