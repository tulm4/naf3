package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRateLimitRequests_BackwardCompatibleLabels(t *testing.T) {
	RateLimitRequests.WithLabelValues("nssaa", "limited").Inc()
	RateLimitRequests.WithLabelValues("aiw", "limited").Inc()

	assert.Equal(t, 1.0, testutil.ToFloat64(RateLimitRequests.WithLabelValues("nssaa", "limited")))
	assert.Equal(t, 1.0, testutil.ToFloat64(RateLimitRequests.WithLabelValues("aiw", "limited")))
}

func TestRateLimitDecisionRequests_RecordsDecisionLabels(t *testing.T) {
	for _, tc := range []struct {
		service string
		scope   string
		result  string
	}{
		{service: "nssaa", scope: "amf", result: "allowed"},
		{service: "nssaa", scope: "amf", result: "limited"},
		{service: "nssaa", scope: "amf", result: "error"},
		{service: "aiw", scope: "supi", result: "allowed"},
		{service: "aiw", scope: "supi", result: "limited"},
		{service: "aiw", scope: "supi", result: "error"},
	} {
		RateLimitDecisionRequests.WithLabelValues(tc.service, tc.scope, tc.result).Inc()
		assert.Equal(t, 1.0, testutil.ToFloat64(RateLimitDecisionRequests.WithLabelValues(tc.service, tc.scope, tc.result)))
	}
}
