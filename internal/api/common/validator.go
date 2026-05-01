// Package common provides common validation utilities for 3GPP data types.
// Spec: TS 29.571 §5.4.4, TS 29.526
package common

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	// GPSI pattern per TS 29.571 §5.2.2:
	// Pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
	// Supports MSISDN-based, External Identifier-based, and catch-all formats
	// Spec: TS 29.571 §5.2.2
	gpsiRegex = regexp.MustCompile(`^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$`)
	// SUPI pattern: 'imsi-' followed by 5-15 digits (IMSI)
	// Per TS 23.003 §2.2, IMSI format: MCC (3) + MNC (2-3) + MSIN (variable)
	// Total length: 5-15 decimal digits
	// Spec: TS 23.003 §2.2, TS 29.571 §5.2.2
	supiRegex = regexp.MustCompile(`^imsi-[0-9]{5,15}$`)
	// SD pattern: exactly 6 hexadecimal characters
	// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
	sdRegex = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)
)

// ValidateGPSI validates that the GPSI (Generic Public Subscription Identifier)
// conforms to TS 29.571 §5.2.2.
// GPSI is required for all NSSAA procedures.
// Spec: TS 23.502 §4.2.9.1, TS 29.571 §5.2.2
func ValidateGPSI(gpsi string) error {
	if gpsi == "" {
		return ValidationProblem("gpsi", "GPSI is required")
	}
	if !gpsiRegex.MatchString(gpsi) {
		return ValidationProblem("gpsi", "must match pattern ^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$ (TS 29.571 §5.2.2)")
	}
	return nil
}

// ValidateSUPI validates that the SUPI (Subscription Permanent Identifier)
// conforms to TS 29.571 §5.4.4.2.
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.2
func ValidateSUPI(supi string) error {
	if supi == "" {
		return ValidationProblem("supi", "SUPI is required")
	}
	if !supiRegex.MatchString(supi) {
		return ValidationProblem("supi", "must match pattern ^imsi-[0-9]{5,15}$ (IMSI-based SUPI, TS 29.571 §5.2.2)")
	}
	return nil
}

// ValidateSnssai validates the S-NSSAI (Single Network Slice Selection
// Assistance Information) components.
// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
// The missing flag should be true when Snssai was not present in the request body.
func ValidateSnssai(sst int, sd string, missing bool) error {
	if missing {
		return ValidationProblem("snssai", "snssai is required (TS 29.526 §7.2.2)")
	}
	// Reject explicitly empty Snssai: both sst=0 and sd="" means empty object {} was sent.
	// This is different from missing (snssai not present at all).
	// Spec: TS 29.526 §7.2.2 requires at least sst or sd.
	if sst == 0 && sd == "" {
		return ValidationProblem("snssai", "snssai.sst or snssai.sd must be provided")
	}
	// SST range: 0–255 (standard values 1–128, operator-specific 129–255)
	// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
	if sst < 0 || sst > 255 {
		return ValidationProblem("snssai.sst", "SST must be in range 0-255 (TS 29.571 §5.4.4.60)")
	}
	// SD is optional (empty string means default SD), but if present must be 6 hex chars
	// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
	if sd != "" && !sdRegex.MatchString(sd) {
		return ValidationProblem("snssai.sd", "SD must be exactly 6 hex characters if present (TS 29.571 §5.4.4.60)")
	}
	return nil
}

// ValidateURI validates that a URI string is well-formed.
// Spec: RFC 3986, TS 29.571 §5.4.4.15
func ValidateURI(uri string) error {
	if uri == "" {
		return ValidationProblem("uri", "URI is required")
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return ValidationProblem("uri", "malformed URI: "+err.Error())
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ValidationProblem("uri", "URI must include scheme and host")
	}
	return nil
}

// ValidateAuthCtxID validates that an AuthCtxId (authentication context
// identifier) is non-empty and safe for use as a resource key.
// Spec: TS 29.526 §7.2.2
func ValidateAuthCtxID(id string) error {
	if strings.TrimSpace(id) == "" {
		return ValidationProblem("authCtxId", "authCtxId is required")
	}
	// AuthCtxId is an opaque string assigned by NSSAAF; allow any non-empty string
	// but reject control characters
	for _, r := range id {
		if r < 0x20 || r == 0x7F {
			return ValidationProblem("authCtxId", "authCtxId contains invalid control characters")
		}
	}
	return nil
}

// ValidateNssai validates a list of S-NSSAIs for an NSSAA request.
// Returns an aggregated error listing all validation failures.
// Spec: TS 29.526 §7.2.2, TS 23.502 §4.2.9
func ValidateNssai(sst int, sd string) error {
	return ValidateSnssai(sst, sd, false)
}

// ValidatePlmnID validates a PLMN ID (MCC + MNC).
// Spec: TS 23.003 §2.10, TS 29.571 §5.4.4.6
func ValidatePlmnID(mcc, mnc string) error {
	if len(mcc) != 3 {
		return ValidationProblem("plmnId.mcc", "MCC must be exactly 3 digits (TS 29.571 §5.4.4.6)")
	}
	if len(mnc) != 2 && len(mnc) != 3 {
		return ValidationProblem("plmnId.mnc", "MNC must be 2 or 3 digits (TS 29.571 §5.4.4.6)")
	}
	for _, c := range mcc + mnc {
		if c < '0' || c > '9' {
			return ValidationProblem("plmnId", "PLMN ID must contain only digits")
		}
	}
	return nil
}

// FormatError returns a formatted validation error string for debugging.
func FormatError(field, reason string) string {
	return fmt.Sprintf("validation error: %s — %s", field, reason)
}
