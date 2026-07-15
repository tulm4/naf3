package debug

import "testing"

func TestSanitize_HashesPIIKeys(t *testing.T) {
	in := map[string]any{
		"gpsi":               "msisdn-208046000000001",
		"supi":               "imsi-208046000000001",
		"msisdn":             "208046000000001",
		"user_name":          "alice",
		"calling_station_id": "5551234",
		"safe_field":         "keep-me",
	}
	out := sanitize(in)
	if out["gpsi"] == "msisdn-208046000000001" {
		t.Errorf("gpsi was not hashed: %v", out["gpsi"])
	}
	if out["safe_field"] != "keep-me" {
		t.Errorf("safe_field was modified: %v", out["safe_field"])
	}
}

func TestSanitize_RecursesIntoNestedMaps(t *testing.T) {
	in := map[string]any{
		"outer": map[string]any{
			"gpsi": "msisdn-208046000000001",
		},
	}
	out := sanitize(in)
	outer, ok := out["outer"].(map[string]any)
	if !ok {
		t.Fatal("outer was not preserved as map")
	}
	if outer["gpsi"] == "msisdn-208046000000001" {
		t.Error("nested gpsi was not hashed")
	}
}
