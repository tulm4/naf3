package nrf

import (
	"os"
	"testing"
)

func TestLoadProfileFromYAML(t *testing.T) {
	content := `
instanceId: "550e8400-e29b-41d4-a716-446655440000"
instanceName: "nssAAF-gw-001"
fqdn: "nssAAF.operator.com"
locality: "dc-1"
nfSetId: "nssAAF-set-001"

ipv4Addresses:
  - "10.0.1.50"
  - "10.0.2.50"

plmnList:
  - mcc: "208"
    mnc: "001"

nssaafInfo:
  supiRanges:
    - start: "imsi-208010000000001"
      end: "imsi-208019999999999"
      pattern: "^imsi-20801[0-9]{8}$"
      size: "LARGE"

nfServices:
  nnssaaf-nssaa:
    serviceInstanceId: "nnssaaf-nssaa-001"
    apiPrefix: "/nnssaaf-nssaa/v1"
    allowedNfTypes: ["AMF"]
    capacity: 1000
    priority: 100
`
	tmp, err := os.CreateTemp("", "nf-profile-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())

	if _, werr := tmp.WriteString(content); werr != nil {
		t.Fatal(werr)
	}
	tmp.Close()

	profile, err := LoadProfileFromYAML(tmp.Name())
	if err != nil {
		t.Fatalf("LoadProfileFromYAML failed: %v", err)
	}

	if profile.InstanceID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("InstanceID mismatch: got %s", profile.InstanceID)
	}

	if profile.InstanceName != "nssAAF-gw-001" {
		t.Errorf("InstanceName mismatch")
	}

	if len(profile.NSSAAServices) != 1 {
		t.Errorf("expected 1 service, got %d", len(profile.NSSAAServices))
	}

	if _, ok := profile.NSSAAServices["nnssaaf-nssaa"]; !ok {
		t.Errorf("Service nnssaaf-nssaa not found")
	}

	if profile.NSSAAFInfo == nil || len(profile.NSSAAFInfo.SupiRanges) != 1 {
		t.Errorf("NSSAAFInfo.SupiRanges should have 1 entry")
	}
}

func TestBuildNFProfile(t *testing.T) {
	yamlProfile := &YAMLProfile{
		InstanceID:    "550e8400-e29b-41d4-a716-446655440000",
		InstanceName:  "nssAAF-gw-001",
		FQDN:          "nssAAF.operator.com",
		Locality:      "dc-1",
		IPv4Addresses: []string{"10.0.1.50", "10.0.2.50"},
		PLMNList: []PLMN{
			{MCC: "208", MNC: "001"},
		},
		NSSAAServices: map[string]YAMLService{
			"nnssaaf-nssaa": {
				ServiceInstanceID: "nnssaaf-nssaa-001",
				APIPrefix:         "/nnssaaf-nssaa/v1",
				AllowedNfTypes:    []string{"AMF"},
				Capacity:          1000,
				Priority:          100,
			},
		},
	}

	profile := BuildNFProfile(yamlProfile, 300)

	if profile.NFInstanceID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("NFInstanceID mismatch")
	}

	if profile.NFType != NFTypeNSSAAF {
		t.Errorf("NFType should be NSSAAF")
	}

	if profile.NFStatus != NFStatusRegistered {
		t.Errorf("NFStatus should be REGISTERED")
	}

	if profile.HeartBeatTimer != 300 {
		t.Errorf("HeartBeatTimer mismatch")
	}

	if len(profile.NfServices) != 1 {
		t.Errorf("expected 1 service")
	}

	svc := profile.NfServices[0]
	if len(svc.IPEndPoints) != 2 {
		t.Errorf("expected 2 IP endpoints")
	}
}
