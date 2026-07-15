package nrf

import (
	"encoding/json"
	"testing"
)

func TestNFProfileJSON(t *testing.T) {
	profile := NFProfile{
		NFInstanceID:   "test-instance-001",
		NFType:         NFTypeNSSAAF,
		NFStatus:       NFStatusRegistered,
		HeartBeatTimer: 300,
		InstanceName:   "nssAAF-gw-001",
		FQDN:           "nssAAF.operator.com",
		PLMNList: []PLMN{
			{MCC: "208", MNC: "001"},
		},
		NfServices: []NFService{
			{
				ServiceInstanceID: "nnssaaf-nssaa-001",
				ServiceName:       ServiceNameNnssaafNssaa,
				Versions:          []NFServiceVersion{{APIVersion: "v1"}},
				Scheme:            "https",
				NFServiceStatus:   NFServiceStatusRegistered,
				FQDN:              "nssAAF.operator.com",
				APIPrefix:         "https://nssAAF.operator.com/nnssaaf-nssaa/v1",
				AllowedNfTypes:    []string{"AMF"},
				Capacity:          1000,
				Priority:          100,
			},
		},
		NssaafInfo: &NssaafInfo{
			SupiRanges: []SupiRange{
				{
					Start:   "imsi-208010000000001",
					End:     "imsi-208019999999999",
					Pattern: "^imsi-20801[0-9]{8}$",
					Size:    "LARGE",
				},
			},
		},
	}

	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Verify critical fields are present
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded["nfInstanceId"] != "test-instance-001" {
		t.Errorf("nfInstanceId mismatch")
	}
	if decoded["nfType"] != "NSSAAF" {
		t.Errorf("nfType mismatch")
	}

	// Check nfServices is an array
	services, ok := decoded["nfServices"].([]interface{})
	if !ok {
		t.Fatalf("nfServices should be array")
	}
	if len(services) != 1 {
		t.Errorf("expected 1 service, got %d", len(services))
	}

	// Check nssaafInfo is present
	if decoded["nssaafInfo"] == nil {
		t.Errorf("nssaafInfo should be present")
	}
}
