package nrf

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// YAMLProfile represents the NFProfile configuration in YAML format.
type YAMLProfile struct {
	InstanceID    string                 `yaml:"instanceId"`
	InstanceName  string                 `yaml:"instanceName"`
	FQDN          string                 `yaml:"fqdn"`
	Locality      string                 `yaml:"locality"`
	NFSetID       string                 `yaml:"nfSetId"`
	IPv4Addresses []string               `yaml:"ipv4Addresses"`
	PLMNList      []PLMN                 `yaml:"plmnList"`
	SNSSAIList    []Snssai               `yaml:"snssais"`
	NSSAAServices map[string]YAMLService `yaml:"nfServices"`
	NSSAAFInfo    *YAMLNSSAAFInfo        `yaml:"nssaafInfo"`
	CustomInfo    *CustomInfo            `yaml:"customInfo"`
}

// YAMLService represents a service configuration in YAML.
type YAMLService struct {
	ServiceInstanceID string   `yaml:"serviceInstanceId"`
	APIPrefix         string   `yaml:"apiPrefix"`
	AllowedNfTypes    []string `yaml:"allowedNfTypes"`
	Capacity          int      `yaml:"capacity"`
	Priority          int      `yaml:"priority"`
	SupportedFeatures string   `yaml:"supportedFeatures"`
}

// YAMLNSSAAFInfo represents NSSAAF-specific info in YAML.
type YAMLNSSAAFInfo struct {
	SupiRanges                     []SupiRange            `yaml:"supiRanges"`
	InternalGroupIdentifiersRanges []InternalGroupIdRange `yaml:"internalGroupIdentifiersRanges"`
}

// LoadProfileFromYAML reads NFProfile configuration from a YAML file.
func LoadProfileFromYAML(path string) (*YAMLProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading profile config: %w", err)
	}

	var profile YAMLProfile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parsing profile config: %w", err)
	}

	if profile.InstanceID == "" {
		return nil, fmt.Errorf("instanceId is required in NFProfile config")
	}

	return &profile, nil
}

// BuildNFProfile converts YAML configuration to a 3GPP-compliant NFProfile.
// Spec: TS 29.510 §6.1.6.2.2
func BuildNFProfile(yamlProfile *YAMLProfile, heartbeatTimer int) *NFProfile {
	profile := &NFProfile{
		NFInstanceID:   yamlProfile.InstanceID,
		NFType:         NFTypeNSSAAF,
		NFStatus:       NFStatusRegistered,
		HeartBeatTimer: heartbeatTimer,
		InstanceName:   yamlProfile.InstanceName,
		FQDN:           yamlProfile.FQDN,
		Locality:       yamlProfile.Locality,
		NFSetID:        yamlProfile.NFSetID,
		PLMNList:       yamlProfile.PLMNList,
		SNSSAIList:     yamlProfile.SNSSAIList,
		CustomInfo:     yamlProfile.CustomInfo,
	}

	for name, svc := range yamlProfile.NSSAAServices {
		nfSvc := NFService{
			ServiceInstanceID: svc.ServiceInstanceID,
			ServiceName:       name,
			Versions:          []NFServiceVersion{{APIVersion: "v1"}},
			Scheme:            "https",
			NFServiceStatus:   NFServiceStatusRegistered,
			FQDN:              yamlProfile.FQDN,
			APIPrefix:         "https://" + yamlProfile.FQDN + svc.APIPrefix,
			Capacity:          svc.Capacity,
			Priority:          svc.Priority,
			SupportedFeatures: svc.SupportedFeatures,
			AllowedNfTypes:    svc.AllowedNfTypes,
		}

		for _, addr := range yamlProfile.IPv4Addresses {
			nfSvc.IPEndPoints = append(nfSvc.IPEndPoints, IPEndPoint{
				IPv4Address: addr,
				Port:        443,
				Transport:   "TCP",
			})
		}

		profile.NfServices = append(profile.NfServices, nfSvc)
	}

	if yamlProfile.NSSAAFInfo != nil {
		profile.NssaafInfo = &NssaafInfo{
			SupiRanges:                     yamlProfile.NSSAAFInfo.SupiRanges,
			InternalGroupIdentifiersRanges: yamlProfile.NSSAAFInfo.InternalGroupIdentifiersRanges,
		}
	}

	return profile
}
