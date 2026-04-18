// Package specs provides shared types generated from TS29571_CommonData.yaml.
// These types are used by both the NSSAA and AIW generated packages.
package specs

// Snssai mirrors TS29571_CommonData.yaml#/components/schemas/Snssai.
// Spec: TS 29.571 §5.4.60
type Snssai struct {
	// Sst: Slice/Service Type, range 0–255.
	Sst uint8 `json:"sst"`
	// Sd: Slice Differentiator, 6 hex chars, optional.
	Sd string `json:"sd,omitempty"`
}

// Gpsi mirrors TS29571_CommonData.yaml#/components/schemas/Gpsi.
// Spec: TS 29.571 §5.4.4.3
type Gpsi string

// Supi mirrors TS29571_CommonData.yaml#/components/schemas/Supi.
// Spec: TS 29.571 §5.4.4.2
type Supi string

// SupportedFeatures mirrors TS29571_CommonData.yaml#/components/schemas/SupportedFeatures.
type SupportedFeatures string

// Uri mirrors TS29571_CommonData.yaml#/components/schemas/Uri.
type Uri string

// NfInstanceId mirrors TS29571_CommonData.yaml#/components/schemas/NfInstanceId.
type NfInstanceId string

// AuthStatus mirrors TS29571_CommonData.yaml#/components/schemas/AuthStatus.
// Spec: TS 29.571 §5.4.4.60
type AuthStatus string

const (
	AuthStatusEAPSUCCESS AuthStatus = "EAP_SUCCESS"
	AuthStatusEAPFAILURE AuthStatus = "EAP_FAILURE"
	AuthStatusPENDING    AuthStatus = "PENDING"
)

// ServerAddressingInfo mirrors TS29571_CommonData.yaml#/components/schemas/ServerAddressingInfo.
type ServerAddressingInfo struct {
	Ipv4Addresses []Ipv4Addr `json:"ipv4Addresses,omitempty"`
	Ipv6Addresses []Ipv6Addr `json:"ipv6Addresses,omitempty"`
	FqdnList      []Fqdn     `json:"fqdnList,omitempty"`
}

// Ipv4Addr mirrors TS29571_CommonData.yaml#/components/schemas/Ipv4Addr.
type Ipv4Addr string

// Ipv6Addr mirrors TS29571_CommonData.yaml#/components/schemas/Ipv6Addr.
type Ipv6Addr string

// Fqdn mirrors TS29571_CommonData.yaml#/components/schemas/Fqdn.
type Fqdn string
