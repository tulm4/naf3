// Package messages provides strongly-typed Diameter message structs for NSSAAF.
package messages

// NSSAI-Configuration AVP (Code 3100, Vendor 10415).
// Spec: TS 29.571 §5.4.4.61
type NSSAIConfiguration struct {
	ConfiguredNSSAI []SNSSAI
	RequestedNSSAI []SNSSAI
}

// PLMN-Id AVP (Code 1467, Vendor 10415).
// Spec: TS 29.571 §5.4.4.30
type PLMNId struct {
	MCC string // Mobile Country Code (3 digits)
	MNC string // Mobile Network Code (2-3 digits)
}

// NSSAA-Authorization-Information AVP (Code 3104, Vendor 10415).
// Spec: TS 29.571 §5.4.4.61
type NSSAAuthorizationInformation struct {
	SNSSAI               SNSSAI
	AuthorizationResult   AuthorizationResult
	AuthorizationGracePeriod uint32
}

// Authorization-Result values.
type AuthorizationResult uint32

const (
	AuthorizationResultSliceAuthorized   AuthorizationResult = 0
	AuthorizationResultSliceNotAuthorized AuthorizationResult = 1
)

// Rejected-SNSSAI-Cause values.
type RejectedSNSSAIResult uint32

const (
	SNSSAICauseSNSSAINotAvailable    RejectedSNSSAIResult = 0
	SNSSAICauseSNSSAINotSubscribed   RejectedSNSSAIResult = 1
	SNSSAICauseSNSSAIChanged         RejectedSNSSAIResult = 2
	SNSSAICauseSliceAuthFailed       RejectedSNSSAIResult = 3
)
