package debug

import "github.com/operator/nssAAF/internal/logging"

// piiKeys is the set of map keys that must be replaced with a hash before
// any debug event is written. Defense-in-depth: call sites should already
// pass hashed values, but a stray raw GPSI/SUPI in a Detail map must never
// reach Redis.
var piiKeys = map[string]struct{}{
	"gpsi":               {},
	"supi":               {},
	"imsi":               {},
	"msisdn":             {},
	"user_name":          {},
	"calling_station_id": {},
}

// sanitize returns a copy of m with any PII-keyed value replaced by
// logging.HashGPSI of that value. Recurses into nested map[string]any.
func sanitize(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, isPII := piiKeys[k]; isPII {
			if s, ok := v.(string); ok && s != "" {
				out[k] = logging.HashGPSI(s)
				continue
			}
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = sanitize(nested)
			continue
		}
		out[k] = v
	}
	return out
}
