// Package nssaa provides the Nnssaaf_NSSAA service operation handlers.
package nssaa

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/types"
)

// SliceAuthInfo is the request body for POST /slice-authentications.
// Spec: TS 29.526 §7.2.2
type SliceAuthInfo struct {
	// Gpsi is the Generic Public Subscription Identifier of the UE.
	// Required. Must match pattern ^5-?[0-9]{8,14}$.
	// Spec: TS 23.502 §4.2.9.1, TS 29.571 §5.4.4.3
	Gpsi string `json:"gpsi"`

	// Snssai identifies the network slice for authentication.
	// Required.
	Snssai types.Snssai `json:"snssai"`

	// EapMessage is the Base64-encoded EAP Identity Response from the UE.
	// Required. Spec: RFC 3748
	EapMessage string `json:"eapMessage"`

	// AmfInstanceID is the AMF instance identifier.
	// Optional but recommended for audit logging.
	// Spec: TS 29.526 §7.2.2
	AmfInstanceID string `json:"amfInstanceId,omitempty"`

	// ReauthNotifURI is the callback URI for re-authentication notifications.
	// Optional. Must be a valid HTTPS URI if present.
	// Spec: TS 29.526 §7.2.2
	ReauthNotifURI string `json:"reauthNotifUri,omitempty"`

	// RevocNotifURI is the callback URI for slice revocation notifications.
	// Optional. Must be a valid HTTPS URI if present.
	// Spec: TS 29.526 §7.2.2
	RevocNotifURI string `json:"revocNotifUri,omitempty"`
}

// Validate checks that all required fields are present and well-formed.
// Spec: TS 29.526 §7.2.2
func (r *SliceAuthInfo) Validate() []error {
	var errs []error

	// GPSI is required
	if err := types.Gpsi(r.Gpsi).Validate(); err != nil {
		errs = append(errs, err)
	}

	// S-NSSAI is required
	if err := r.Snssai.Validate(); err != nil {
		errs = append(errs, err)
	}

	// EAP message is required and must be valid Base64
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

	// ReauthNotifURI: if present, must be valid HTTPS URI
	if r.ReauthNotifURI != "" {
		if err := common.ValidateURI(r.ReauthNotifURI); err != nil {
			var pd *common.ProblemDetails
			if errors.As(err, &pd) {
				errs = append(errs, &types.ValidationError{
					Field:      "reauthNotifUri",
					Reason:     pd.Detail,
					HTTPStatus: http.StatusBadRequest,
					Cause:      types.CauseInvalidNotificationURI,
				})
			}
		} else if !hasHTTPScheme(r.ReauthNotifURI) {
			errs = append(errs, &types.ValidationError{
				Field:      "reauthNotifUri",
				Reason:     "notification URI must use HTTPS scheme",
				HTTPStatus: http.StatusBadRequest,
				Cause:      types.CauseInvalidNotificationURI,
			})
		}
	}

	// RevocNotifURI: same validation
	if r.RevocNotifURI != "" {
		if err := common.ValidateURI(r.RevocNotifURI); err != nil {
			var pd *common.ProblemDetails
			if errors.As(err, &pd) {
				errs = append(errs, &types.ValidationError{
					Field:      "revocNotifUri",
					Reason:     pd.Detail,
					HTTPStatus: http.StatusBadRequest,
					Cause:      types.CauseInvalidNotificationURI,
				})
			}
		} else if !hasHTTPScheme(r.RevocNotifURI) {
			errs = append(errs, &types.ValidationError{
				Field:      "revocNotifUri",
				Reason:     "notification URI must use HTTPS scheme",
				HTTPStatus: http.StatusBadRequest,
				Cause:      types.CauseInvalidNotificationURI,
			})
		}
	}

	return errs
}

// hasHTTPScheme reports true if the URI uses the https scheme.
func hasHTTPScheme(uri string) bool {
	return len(uri) >= 8 && uri[0:8] == "https://"
}

// SliceAuthConfirmationData is the request body for PUT /slice-authentications/{authCtxID}.
// Spec: TS 29.526 §7.2.3
type SliceAuthConfirmationData struct {
	// Gpsi is the GPSI of the UE. Must match the GPSI in the session.
	Gpsi string `json:"gpsi"`

	// Snssai is the S-NSSAI for this authentication.
	Snssai types.Snssai `json:"snssai"`

	// EapMessage is the Base64-encoded EAP response from the UE.
	// Required.
	EapMessage string `json:"eapMessage"`
}

// Validate checks that all required fields are present and well-formed.
func (r *SliceAuthConfirmationData) Validate() []error {
	var errs []error

	if err := types.Gpsi(r.Gpsi).Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := r.Snssai.Validate(); err != nil {
		errs = append(errs, err)
	}
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

// ParseSliceAuthInfo parses a SliceAuthInfo from a JSON request body.
func ParseSliceAuthInfo(payload []byte) (*SliceAuthInfo, error) {
	var info SliceAuthInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}
	return &info, nil
}

// ParseSliceAuthConfirmationData parses a SliceAuthConfirmationData from a JSON request body.
func ParseSliceAuthConfirmationData(payload []byte) (*SliceAuthConfirmationData, error) {
	var data SliceAuthConfirmationData
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}
	return &data, nil
}
