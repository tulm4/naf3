package nrf

// NF Type constants
const (
	NFTypeNSSAAF = "NSSAAF"
	NFTypeAMF    = "AMF"
	NFTypeAUSF   = "AUSF"
	NFTypeUDM    = "UDM"
)

// NF Status constants
const (
	NFStatusRegistered     = "REGISTERED"
	NFStatusSuspended      = "SUSPENDED"
	NFStatusUndiscoverable = "UNDISCOVERABLE"
)

// NF Service Status constants
const (
	NFServiceStatusRegistered  = "REGISTERED"
	NFServiceStatusRequired    = "REQUIRED"
	NFServiceStatusUnavailable = "UNAVAILABLE"
)

// Service name constants
const (
	ServiceNameNnssaafNssaa = "nnssaaf-nssaa"
	ServiceNameNnssaafAiw   = "nnssaaf-aiw"
	ServiceNameNudmUem      = "nudm-uem"
	ServiceNameNudmUau      = "nudm-uau"
)

// NFProfile represents the NSSAAF NF profile for NRF registration.
// Spec: TS 29.510 §6.1.6.2.2
type NFProfile struct {
	NFInstanceID   string      `json:"nfInstanceId"`
	NFType         string      `json:"nfType"`
	NFStatus       string      `json:"nfStatus"`
	HeartBeatTimer int         `json:"heartBeatTimer"`
	Load           int         `json:"load,omitempty"`
	InstanceName   string      `json:"nfInstanceName,omitempty"`
	FQDN           string      `json:"fqdn,omitempty"`
	Locality       string      `json:"locality,omitempty"`
	NFSetID        string      `json:"nfSetId,omitempty"`
	PLMNList       []PLMN      `json:"plmnList,omitempty"`
	SNSSAIList     []Snssai    `json:"sNssais,omitempty"`
	NfServices     []NFService `json:"nfServices,omitempty"`
	NssaafInfo     *NssaafInfo `json:"nssaafInfo,omitempty"`
	CustomInfo     *CustomInfo `json:"customInfo,omitempty"`
}

// PLMN represents a Public Land Mobile Network identifier.
type PLMN struct {
	MCC string `json:"mcc"`
	MNC string `json:"mnc"`
}

// Snssai represents a Single Network Slice Selection Assistance Information.
type Snssai struct {
	SST int    `json:"sst"`
	SD  string `json:"sd,omitempty"`
}

// NFService represents a network function service offered by NSSAAF.
// Spec: TS 29.510 §6.1.6.2.3
type NFService struct {
	ServiceInstanceID string             `json:"serviceInstanceId"`
	ServiceName       string             `json:"serviceName"`
	Versions          []NFServiceVersion `json:"versions"`
	Scheme            string             `json:"scheme"`
	NFServiceStatus   string             `json:"nfServiceStatus"`
	FQDN              string             `json:"fqdn,omitempty"`
	APIPrefix         string             `json:"apiPrefix,omitempty"`
	IPEndPoints       []IPEndPoint       `json:"ipEndPoints,omitempty"`
	Capacity          int                `json:"capacity,omitempty"`
	Priority          int                `json:"priority,omitempty"`
	SupportedFeatures string             `json:"supportedFeatures,omitempty"`
	AllowedNfTypes    []string           `json:"allowedNfTypes,omitempty"`
	AllowedNfDomains  []string           `json:"allowedNfDomains,omitempty"`
}

// NFServiceVersion represents a supported API version.
type NFServiceVersion struct {
	APIVersion string `json:"apiVersion"`
}

// IPEndPoint represents an IP endpoint for a service.
// Spec: TS 29.510 §6.1.6.2.5
type IPEndPoint struct {
	IPv4Address string `json:"ipv4Address,omitempty"`
	IPv6Address string `json:"ipv6Address,omitempty"`
	Transport   string `json:"transport,omitempty"`
	Port        int    `json:"port,omitempty"`
}

// NssaafInfo represents NSSAAF-specific information in NFProfile.
// Spec: TS 29.510 §6.1.6.2.104
type NssaafInfo struct {
	SupiRanges                     []SupiRange            `json:"supiRanges,omitempty"`
	InternalGroupIdentifiersRanges []InternalGroupIdRange `json:"internalGroupIdentifiersRanges,omitempty"`
}

// SupiRange represents a range of SUPI values.
// Spec: TS 29.571 §5.4.4.60
type SupiRange struct {
	Start   string `json:"start"`
	End     string `json:"end,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Size    string `json:"size,omitempty"`
}

// InternalGroupIdRange represents a range of internal group identifiers.
type InternalGroupIdRange struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

// CustomInfo holds NSSAAF-specific custom information.
type CustomInfo struct {
	SupportedAaaProtocols []string `json:"supportedAaaProtocols,omitempty"`
	MaxEapRounds          int      `json:"maxEapRounds,omitempty"`
	EapTimeoutSeconds     int      `json:"eapTimeoutSeconds,omitempty"`
}
