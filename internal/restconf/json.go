// Package restconf provides a RESTCONF server (RFC 8040) for the NRM.
package restconf

import (
	"net/http"
	"time"
)

// mediaType is the RFC 8040 JSON media type for yang.data responses.
const mediaType = "application/yang.data+json"

// AlarmInfo represents an alarm for the RESTCONF API.
type AlarmInfo struct {
	AlarmID               string     `json:"alarm-id"`
	AlarmType             string     `json:"alarm-type"`
	ProbableCause         string     `json:"probable-cause"`
	SpecificProblem       string     `json:"specific-problem,omitempty"`
	Severity              string     `json:"severity"`
	PerceivedSeverity     string     `json:"perceived-severity,omitempty"`
	BackupObject          string     `json:"backup-object,omitempty"`
	CorrelatedAlarms      []string   `json:"correlated-alarms,omitempty"`
	ProposedRepairActions string     `json:"proposed-repair-actions,omitempty"`
	EventTime             time.Time  `json:"event-time"`
	Acked                 bool       `json:"acked,omitempty"`
	AckedBy               string     `json:"acked-by,omitempty"`
	AckedAt               *time.Time `json:"acked-at,omitempty"`
}

// SetJSONHeaders sets the RFC 8040 required Content-Type header.
func SetJSONHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", mediaType)
}

// WrapWithModule wraps a data struct with the YANG module prefix.
// E.g. {"3gpp-nssaaf-nrm:nssaa-function": {...}}
//
// Spec: RFC 8040 §5.3.1 — YANG JSON encoding.
func WrapWithModule(data interface{}, modulePrefix, container string) map[string]interface{} {
	key := modulePrefix + ":" + container
	return map[string]interface{}{key: data}
}

// NssaaFunctionEntry represents a single NSSAAFFunction instance.
// Fields match the YANG model in internal/nrm/model.go.
type NssaaFunctionEntry struct {
	ManagedElementID     string        `json:"managed-element-id"`
	ManagedElementTypeID string        `json:"managed-element-type-id,omitempty"`
	UserDefinedData      string        `json:"user-defined-data,omitempty"`
	PLMNInfoList         []string      `json:"p-l-m-n-info-list,omitempty"`
	SBIFQDN              string        `json:"s-b-i-f-q-d-n,omitempty"`
	CNSIIdList           []string      `json:"c-n-s-i-id-list,omitempty"`
	CommModelList        []string      `json:"comm-model-list,omitempty"`
	NssaaInfo            *NssaaInfo    `json:"nssaa-info,omitempty"`
	EpN58                []EndpointN58 `json:"ep-n58,omitempty"`
	EpN59                []EndpointN59 `json:"ep-n59,omitempty"`
}

// NssaaInfo holds NSSAAF-specific information.
type NssaaInfo struct {
	SupiRanges            []string `json:"supi-ranges,omitempty"`
	InternalGroupIDRanges []string `json:"internal-group-id-ranges,omitempty"`
	SupportedSecurityAlgo []string `json:"supported-security-algo,omitempty"`
}

// EndpointN58 represents the N58 (AMF-NSSAAF) interface endpoint.
type EndpointN58 struct {
	EndpointID   string `json:"endpoint-id"`
	LocalAddress string `json:"local-address,omitempty"`
}

// EndpointN59 represents the N59 (NSSAAF-UDM) interface endpoint.
type EndpointN59 struct {
	EndpointID   string `json:"endpoint-id"`
	LocalAddress string `json:"local-address,omitempty"`
}

// NewNssaaFunctionData returns a YANG JSON response for GET nssaa-function.
func NewNssaaFunctionData(entries []NssaaFunctionEntry) map[string]interface{} {
	list := make([]interface{}, len(entries))
	for i := range entries {
		list[i] = entries[i]
	}
	return WrapWithModule(
		map[string]interface{}{"nssaa-function": list},
		"3gpp-nssaaf-nrm",
		"nssaa-function",
	)
}

// NewAlarmData returns a YANG JSON response for GET alarms.
func NewAlarmData(alarms []*AlarmInfo) map[string]interface{} {
	list := make([]interface{}, len(alarms))
	for i := range alarms {
		list[i] = alarms[i]
	}
	return WrapWithModule(
		map[string]interface{}{"alarm": list},
		"3gpp-nssaaf-nrm",
		"alarms",
	)
}

// ErrorResponse represents an RFC 8040 §3.2.2 error response in JSON format.
type ErrorResponse struct {
	Errors ErrorBody `json:"ietf-restconf:errors"`
}

// ErrorBody holds the error entries per RFC 8040.
type ErrorBody struct {
	Error []ErrorEntry `json:"error"`
}

// ErrorEntry is a single error per RFC 8040 §3.2.2.
type ErrorEntry struct {
	ErrorType     string `json:"error-type,omitempty"`
	ErrorTag      string `json:"error-tag,omitempty"`
	ErrorSeverity string `json:"error-severity,omitempty"`
	Path          string `json:"path,omitempty"`
	Message       string `json:"error-message"`
}

// NewErrorResponse creates an RFC 8040 §3.2.2 error response.
func NewErrorResponse(status int, message string) ErrorResponse {
	tag := tagFromStatus(status)
	severity := severityFromStatus(status)
	return ErrorResponse{
		Errors: ErrorBody{
			Error: []ErrorEntry{
				{
					ErrorType:     "application",
					ErrorTag:      tag,
					ErrorSeverity: severity,
					Message:       message,
				},
			},
		},
	}
}

// tagFromStatus maps HTTP status to RFC 8040 error-tag.
func tagFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "malformed-message"
	case http.StatusNotFound:
		return "invalid-value"
	case http.StatusMethodNotAllowed:
		return "operation-not-supported"
	case http.StatusNotAcceptable:
		return "invalid-value"
	case http.StatusUnsupportedMediaType:
		return "malformed-message"
	case http.StatusInternalServerError:
		return "operation-failed"
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "resource-denied"
	default:
		return "operation-failed"
	}
}

// severityFromStatus maps HTTP status to error-severity.
func severityFromStatus(_ int) string {
	return "error"
}
