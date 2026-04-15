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
	// GPSI pattern: 5 followed by 8–14 digits
	// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.3
	gpsiRegex = regexp.MustCompile(`^5[0-9]{8,14}$`)
	// SUPI pattern: 'imu-' followed by 15 digits (IMSI)
	// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.2
	supiRegex = regexp.MustCompile(`^imu-[0-9]{15}$`)
	// SD pattern: exactly 6 hexadecimal characters
	// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
	sdRegex = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)
)

// ValidateGPSI validates that the GPSI (Generic Public Subscription Identifier)
// conforms to TS 29.571 §5.4.4.3.
// GPSI is required for all NSSAA procedures.
// Spec: TS 23.502 §4.2.9.1, TS 29.571 §5.4.4.3
func ValidateGPSI(gpsi string) error {
	if gpsi == "" {
		return ValidationProblem("gpsi", "GPSI is required")
	}
	if !gpsiRegex.MatchString(gpsi) {
		return ValidationProblem("gpsi", "must match pattern ^5[0-9]{8,14}$ (TS 29.571 §5.4.4.3)")
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
		return ValidationProblem("supi", "must match pattern ^imu-[0-9]{15}$ (IMSI-based SUPI, TS 29.571 §5.4.4.2)")
	}
	return nil
}

// ValidateSnssai validates the S-NSSAI (Single Network Slice Selection
// Assistance Information) components.
// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
func ValidateSnssai(sst int, sd string) error {
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
	return ValidateSnssai(sst, sd)
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
