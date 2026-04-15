// Package types provides 3GPP data types for NSSAAF.
// Spec: TS 29.526 §7 (error responses), RFC 7807
package types

import (
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/api/common"
)

// 3GPP cause codes for NSSAAF error responses.
// Spec: TS 29.526 §7
const (
	CauseInvalidPayload         = "INVALID_PAYLOAD"
	CauseInvalidGpsi            = "INVALID_GPSI"
	CauseInvalidSupi            = "INVALID_SUPI"
	CauseInvalidSnssaiSst       = "INVALID_SNSSAI_SST"
	CauseInvalidSnssaiSd        = "INVALID_SNSSAI_SD"
	CauseInvalidEapPayload      = "INVALID_EAP_MESSAGE"
	CauseMissingEapPayload      = "MISSING_EAP_MESSAGE"
	CauseMissingGpsi            = "MISSING_GPSI"
	CauseMissingSupi            = "MISSING_SUPI"
	CauseMissingAmfInstanceID   = "MISSING_AMF_INSTANCE_ID"
	CauseInvalidAmfInstanceID   = "INVALID_AMF_INSTANCE_ID"
	CauseInvalidNotificationURI = "INVALID_NOTIFICATION_URI"
	CauseInvalidStatus          = "INVALID_STATUS"
	CauseAuthContextNotFound    = "AUTH_CONTEXT_NOT_FOUND"
	CauseAuthContextExpired     = "AUTH_CONTEXT_EXPIRED"
	CauseMaxEapRoundsExceeded   = "MAX_EAP_ROUNDS_EXCEEDED"
	CauseAuthConflict           = "AUTH_ALREADY_COMPLETED"
	CauseAaaServerNotConfigured = "AAA_SERVER_NOT_CONFIGURED"
	CauseAaaUnreachable         = "AAA_UNREACHABLE"
	CauseAaaUnavailable         = "AAA_UNAVAILABLE"
	CauseAaaTimeout             = "AAA_TIMEOUT"
	CauseAaaAuthRejected        = "AAA_AUTH_REJECTED"
	CauseInternalError          = "INTERNAL_ERROR"
	CauseInvalidAuthCtxID       = "INVALID_AUTH_CTX_ID"
	CauseMismatchedIdentity     = "MISMATCHED_IDENTITY"
)

// ValidationError represents an input validation failure.
// It implements the error interface and can be converted to ProblemDetails.
type ValidationError struct {
	HTTPStatus int
	Field      string
	Reason     string
	Cause      string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s — %s", e.Field, e.Reason)
}

// ToProblemDetails converts the ValidationError to an RFC 7807 ProblemDetails.
func (e *ValidationError) ToProblemDetails() *common.ProblemDetails {
	return common.ValidationProblem(e.Field, e.Reason)
}

// StatusCode returns the appropriate HTTP status code.
// Defaults to 400 if not set.
func (e *ValidationError) StatusCode() int {
	if e.HTTPStatus == 0 {
		return http.StatusBadRequest
	}
	return e.HTTPStatus
}

// NssaaError represents a domain-level error in the NSSAAF service.
// It wraps a cause code and optional detail for structured error responses.
type NssaaError struct {
	HTTPStatus int
	Err        error
	Cause      string
	Detail     string
}

func (e *NssaaError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("[%s] %s", e.Cause, e.Detail)
	}
	return e.Cause
}

// Unwrap returns the underlying error if present.
func (e *NssaaError) Unwrap() error { return e.Err }

// ToProblemDetails converts the NssaaError to an RFC 7807 ProblemDetails.
func (e *NssaaError) ToProblemDetails() *common.ProblemDetails {
	switch e.HTTPStatus {
	case http.StatusBadRequest:
		return common.NewProblem(e.HTTPStatus, e.Cause, e.Detail)
	case http.StatusForbidden:
		return common.ForbiddenProblem(e.Detail)
	case http.StatusNotFound:
		return common.NotFoundProblem(e.Detail)
	case http.StatusConflict:
		return common.ConflictProblem(e.Detail)
	case http.StatusGone:
		return common.GoneProblem(e.Detail)
	case http.StatusBadGateway:
		return common.BadGatewayProblem(e.Detail)
	case http.StatusServiceUnavailable:
		return common.ServiceUnavailableProblem(e.Detail)
	case http.StatusGatewayTimeout:
		return common.GatewayTimeoutProblem(e.Detail)
	default:
		return common.InternalServerProblem(e.Detail)
	}
}

// Common NSSAAF errors as sentinel errors.
var (
	ErrInvalidGpsi = &ValidationError{
		Field:      "gpsi",
		Reason:     "GPSI must match pattern ^5-?[0-9]{8,14}$ (TS 29.571 §5.4.4.3)",
		HTTPStatus: http.StatusBadRequest,
		Cause:      CauseInvalidGpsi,
	}

	ErrInvalidSupi = &ValidationError{
		Field:      "supi",
		Reason:     "SUPI must match pattern ^imu-[0-9]{15}$ (TS 29.571 §5.4.4.2)",
		HTTPStatus: http.StatusBadRequest,
		Cause:      CauseInvalidSupi,
	}

	ErrMissingGpsi = &ValidationError{
		Field:      "gpsi",
		Reason:     "GPSI is required (TS 23.502 §4.2.9.1)",
		HTTPStatus: http.StatusBadRequest,
		Cause:      CauseMissingGpsi,
	}

	ErrMissingSupi = &ValidationError{
		Field:      "supi",
		Reason:     "SUPI is required",
		HTTPStatus: http.StatusBadRequest,
		Cause:      CauseMissingSupi,
	}

	ErrAuthContextNotFound = &NssaaError{
		HTTPStatus: http.StatusNotFound,
		Cause:      CauseAuthContextNotFound,
		Detail:     "Authentication context not found",
	}

	ErrAuthContextExpired = &NssaaError{
		HTTPStatus: http.StatusGone,
		Cause:      CauseAuthContextExpired,
		Detail:     "Authentication context has expired",
	}

	ErrMaxEapRoundsExceeded = &NssaaError{
		HTTPStatus: http.StatusBadRequest,
		Cause:      CauseMaxEapRoundsExceeded,
		Detail:     "Maximum number of EAP rounds exceeded",
	}

	ErrAuthAlreadyCompleted = &NssaaError{
		HTTPStatus: http.StatusConflict,
		Cause:      CauseAuthConflict,
		Detail:     "Authentication has already completed",
	}

	ErrAaaServerNotConfigured = &NssaaError{
		HTTPStatus: http.StatusNotFound,
		Cause:      CauseAaaServerNotConfigured,
		Detail:     "No AAA server configuration found for this S-NSSAI",
	}

	ErrAaaUnreachable = &NssaaError{
		HTTPStatus: http.StatusBadGateway,
		Cause:      CauseAaaUnreachable,
		Detail:     "Cannot reach AAA server",
	}

	ErrAaaTimeout = &NssaaError{
		HTTPStatus: http.StatusGatewayTimeout,
		Cause:      CauseAaaTimeout,
		Detail:     "AAA server response timeout",
	}

	ErrAaaAuthRejected = &NssaaError{
		HTTPStatus: http.StatusForbidden,
		Cause:      CauseAaaAuthRejected,
		Detail:     "AAA server rejected authentication",
	}

	ErrMismatchedIdentity = &ValidationError{
		Field:      "gpsi",
		Reason:     "GPSI in request does not match the authenticated GPSI for this session",
		HTTPStatus: http.StatusBadRequest,
		Cause:      CauseMismatchedIdentity,
	}
)
