// Package common provides HTTP header constants for the NSSAAF SBI interfaces.
// Spec: TS 29.500 §5 (HTTP/2, TLS requirements)
package common

const (
	// HeaderContentType is the standard HTTP Content-Type header.
	HeaderContentType = "Content-Type"
	// HeaderAuthorization is the standard HTTP Authorization header.
	HeaderAuthorization = "Authorization"
	// HeaderXRequestID is the 3GPP SBI correlation identifier.
	// All service requests MUST carry a unique X-Request-ID.
	// Spec: TS 29.500 §6.1
	HeaderXRequestID = "X-Request-ID"
	// HeaderXForwardedFor carries the original client IP through proxies.
	HeaderXForwardedFor = "X-Forwarded-For"
	// HeaderLocation is used in 201 Created responses to indicate
	// the resource URI.
	// Spec: RFC 7231 §7.1.2
	HeaderLocation = "Location"
	// HeaderRetryAfter indicates when the client MAY retry a request
	// after receiving a 503 response.
	HeaderRetryAfter = "Retry-After"
	// HeaderWWWAuthenticate is used in 401 responses to indicate
	// the authentication scheme required.
	HeaderWWWAuthenticate = "WWW-Authenticate"
	// Header3GPPRequestTimestamp carries the UE-side timestamp for
	// request correlation.
	// Spec: TS 29.500 §6.1
	Header3GPPRequestTimestamp = "3GPP-Request-Timestamp"
	// Header3GPPResponseTimestamp carries the NSSAAF-side timestamp
	// for response time measurement.
	// Spec: TS 29.500 §6.1
	Header3GPPResponseTimestamp = "3GPP-Response-Timestamp"
	// Header3GPPSessionID carries the NSSAAF session identifier for
	// correlation across multiple procedure steps.
	// Spec: TS 29.526 §7.1
	Header3GPPSessionID = "3GPP-Session-ID"
)

const (
	// MediaTypeJSON is the standard JSON media type.
	MediaTypeJSON = "application/json"
	// MediaTypeProblemJSON is the RFC 7807 Problem Details media type.
	MediaTypeProblemJSON = "application/problem+json"
	// MediaType3GPPNSSAA is the 3GPP-profiled JSON media type for NSSAA responses.
	// Spec: TS 29.526 §7.2
	MediaType3GPPNSSAA = "application/3gppNssaa+json"
	// MediaType3GPPAIW is the 3GPP-profiled JSON media type for AIW responses.
	// Spec: TS 29.526 §7.3
	MediaType3GPPAIW = "application/3gppAiw+json"
	// MediaTypeJSONVersion is used with version suffix per RFC 7231.
	MediaTypeJSONVersion = "application/json; charset=utf-8"
)

const (
	// OriginNRF is the placeholder NRF origin domain used in NF profile.
	OriginNRF = "https://nrf.operator.com"
	// OriginNSSAAF is the placeholder NSSAAF origin domain.
	OriginNSSAAF = "https://nssAAF.operator.com"
)
