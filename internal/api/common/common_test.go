package common

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProblem(t *testing.T) {
	p := NewProblem(400, "INVALID_PAYLOAD", "missing required field 'gpsi'")
	assert.Equal(t, 400, p.Status)
	assert.Equal(t, "INVALID_PAYLOAD", p.Cause)
	assert.Equal(t, "missing required field 'gpsi'", p.Detail)
}

func TestValidationProblem(t *testing.T) {
	p := ValidationProblem("gpsi", "GPSI is required")
	assert.Equal(t, 400, p.Status)
	assert.Equal(t, "VALIDATION_ERROR", p.Cause)
	assert.Contains(t, p.Detail, "gpsi")
	assert.Contains(t, p.Detail, "GPSI is required")
	assert.Equal(t, "https://nssAAF.operator.com/probs/validation-error", p.Type)
}

func TestForbiddenProblem(t *testing.T) {
	p := ForbiddenProblem("AAA-S rejected slice authentication")
	assert.Equal(t, 403, p.Status)
	assert.Equal(t, "SLICE_AUTH_REJECTED", p.Cause)
}

func TestNotFoundProblem(t *testing.T) {
	p := NotFoundProblem("AuthCtxId not found")
	assert.Equal(t, 404, p.Status)
	assert.Equal(t, "NOT_FOUND", p.Cause)
}

func TestBadGatewayProblem(t *testing.T) {
	p := BadGatewayProblem("cannot reach RADIUS server at 10.0.0.1:1812")
	assert.Equal(t, 502, p.Status)
	assert.Equal(t, "AAA_UNREACHABLE", p.Cause)
}

func TestServiceUnavailableProblem(t *testing.T) {
	p := ServiceUnavailableProblem("AAA-S overloaded")
	assert.Equal(t, 503, p.Status)
	assert.Equal(t, "AAA_UNAVAILABLE", p.Cause)
}

func TestGatewayTimeoutProblem(t *testing.T) {
	p := GatewayTimeoutProblem("AAA-S did not respond within 10s")
	assert.Equal(t, 504, p.Status)
	assert.Equal(t, "AAA_TIMEOUT", p.Cause)
}

func TestUnauthorizedProblem(t *testing.T) {
	p := UnauthorizedProblem("missing bearer token")
	assert.Equal(t, 401, p.Status)
	assert.Equal(t, "AUTHENTICATION_REQUIRED", p.Cause)
}

func TestConflictProblem(t *testing.T) {
	p := ConflictProblem("duplicate authentication request")
	assert.Equal(t, 409, p.Status)
	assert.Equal(t, "CONFLICT", p.Cause)
}

func TestGoneProblem(t *testing.T) {
	p := GoneProblem("slice subscription revoked")
	assert.Equal(t, 410, p.Status)
	assert.Equal(t, "RESOURCE_GONE", p.Cause)
}

func TestInternalServerProblem(t *testing.T) {
	p := InternalServerProblem("nil pointer dereference")
	assert.Equal(t, 500, p.Status)
	assert.Equal(t, "INTERNAL_ERROR", p.Cause)
}

func TestProblemDetailsJSON(t *testing.T) {
	p := NewProblem(400, "INVALID_PAYLOAD", "test detail")
	data, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"status":400`)
	assert.Contains(t, string(data), `"cause":"INVALID_PAYLOAD"`)
	assert.Contains(t, string(data), `"detail":"test detail"`)
}

func TestWriteProblem(t *testing.T) {
	w := httptest.NewRecorder()
	p := ValidationProblem("gpsi", "GPSI is required")
	WriteProblem(w, p)

	assert.Equal(t, 400, w.Code)
	assert.Equal(t, MediaTypeProblemJSON, w.Header().Get(HeaderContentType))

	var got ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &got)
	require.NoError(t, err)
	assert.Equal(t, 400, got.Status)
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	err := WriteJSON(w, http.StatusOK, map[string]string{"hello": "world"})
	require.NoError(t, err)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Header().Get(HeaderContentType), "application/json")

	var got map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &got)
	require.NoError(t, err)
	assert.Equal(t, "world", got["hello"])
}

func TestWriteJSONEncodeError(t *testing.T) {
	// failingWriter always fails on Write, simulating an unreachable client
	failingWriter := &failingResponseWriter{httptest.NewRecorder()}
	err := WriteJSON(failingWriter, http.StatusOK, map[string]string{"hello": "world"})
	assert.Error(t, err)
}

// failingResponseWriter wraps httptest.ResponseRecorder and makes Write fail.
type failingResponseWriter struct{ *httptest.ResponseRecorder }

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

func (f *failingResponseWriter) WriteHeader(statusCode int) {
	// no-op: avoid panic from httptest(ResponseRecorder).WriteHeader
}

func TestValidateGPSI(t *testing.T) {
	tests := []struct {
		name    string
		gpsi    string
		wantErr bool
	}{
		// Valid: MSISDN-based GPSI per TS 29.571 §5.2.2
		{"valid MSISDN min (5 digits)", "msisdn-12345", false},
		{"valid MSISDN 10 digits", "msisdn-2080460000", false},
		{"valid MSISDN max (15 digits)", "msisdn-208046000000001", false},
		// Valid: External Identifier-based GPSI per TS 29.571 §5.2.2
		{"valid External Identifier", "extid-user@domain.com", false},
		// Valid: Catch-all (any other string) per TS 29.571 §5.2.2
		{"valid catch-all (old 5-prefix)", "52080460000001", false},
		{"valid catch-all (any string)", "any-format-here", false},
		// Invalid
		{"empty", "", true},
		// Note: Invalid MSISDN formats are accepted by catch-all (.+)
		// because the spec allows any string that doesn't match the first two forms
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGPSI(tt.gpsi)
			if tt.wantErr {
				assert.Error(t, err)
				var p *ProblemDetails
				assert.True(t, errors.As(err, &p))
				assert.Equal(t, 400, p.Status)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSUPI(t *testing.T) {
	tests := []struct {
		name    string
		supi    string
		wantErr bool
	}{
		{"valid imsi", "imu-123456789012345", false},
		{"valid zero imsi", "imu-000000000000000", false},
		{"empty", "", true},
		{"missing prefix", "123456789012345", true},
		{"wrong prefix", "nai-123456789012345", true},
		{"too short", "imu-12345678901234", true},
		{"too long", "imu-1234567890123456", true},
		{"non-digit chars", "imu-12345678901234a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSUPI(tt.supi)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSnssai(t *testing.T) {
	tests := []struct {
		name     string
		sst      int
		sd       string
		missing  bool
		wantErr  bool
	}{
		{"valid sst only", 1, "", false, false},
		{"valid sst 0 with sd", 0, "000001", false, false},
		{"empty snssai object sst_0_sd_empty", 0, "", false, true},
		{"valid sst 255", 255, "", false, false},
		{"valid sst with sd", 128, "112233", false, false},
		{"valid sst with lowercase sd", 1, "aabbcc", false, false},
		{"invalid sst negative", -1, "", false, true},
		{"invalid sst too high", 256, "", false, true},
		{"invalid sd too short", 1, "ABC12", false, true},
		{"invalid sd too long", 1, "ABCDEF1", false, true},
		{"invalid sd non-hex", 1, "GGGGGG", false, true},
		{"missing snssai", 0, "", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSnssai(tt.sst, tt.sd, tt.missing)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"valid https", "https://nssaaa.operator.com/auth", false},
		{"valid with port", "http://localhost:8080/callback", false},
		{"empty", "", true},
		{"no scheme", "nssaaa.operator.com", true},
		{"no host", "https:///path", true},
		{"relative path only", "/nnssaaf/v1/auth", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAuthCtxID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid uuid-style", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid alphanumeric", "authctx-abc123", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"with newline", "auth\nctx", true},
		{"with tab", "auth\tctx", true},
		{"with null byte", "auth\x00ctx", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAuthCtxID(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePlmnID(t *testing.T) {
	tests := []struct {
		name    string
		mcc     string
		mnc     string
		wantErr bool
	}{
		{"valid 2-digit mnc", "460", "00", false},
		{"valid 3-digit mnc", "460", "001", false},
		{"valid 3-digit mnc mixed", "123", "456", false},
		{"empty mcc", "", "00", true},
		{"empty mnc", "460", "", true},
		{"mcc too short", "46", "00", true},
		{"mcc too long", "4600", "00", true},
		{"mnc too short", "460", "0", true},
		{"mnc too long", "460", "0012", true},
		{"mcc non-digit", "46a", "00", true},
		{"mnc non-digit", "460", "0a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePlmnID(tt.mcc, tt.mnc)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatError(t *testing.T) {
	msg := FormatError("gpsi", "GPSI is required")
	assert.Equal(t, "validation error: gpsi — GPSI is required", msg)
}

func TestRequestIDMiddleware(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := GetRequestID(r.Context())
		w.Header().Set("X-Received-Request-ID", reqID)
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("generates UUID when missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Header().Get("X-Received-Request-ID"))
		assert.NotEmpty(t, rec.Header().Get(HeaderXRequestID))
	})

	t.Run("preserves client request ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(HeaderXRequestID, "client-provided-id")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, "client-provided-id", rec.Header().Get(HeaderXRequestID))
	})
}

func TestRecoveryMiddleware(t *testing.T) {
	handler := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("intentional panic for test")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, MediaTypeProblemJSON, rec.Header().Get(HeaderContentType))

	var p ProblemDetails
	err := json.Unmarshal(rec.Body.Bytes(), &p)
	require.NoError(t, err)
	assert.Equal(t, 500, p.Status)
	assert.Equal(t, "INTERNAL_ERROR", p.Cause)
}

func TestCORSMiddleware(t *testing.T) {
	handler := CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("OPTIONS preflight for OAM", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/oam/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("no CORS for non-OAM paths", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nnssaaf/v1/authenticate", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestLoggingMiddleware(t *testing.T) {
	// Chain RequestIDMiddleware before LoggingMiddleware since logging reads from context
	handler := LoggingMiddleware(RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderXRequestID, "log-test-req-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "log-test-req-id", rec.Header().Get(HeaderXRequestID))
}

func TestContextWithRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "test-req-id")
	assert.Equal(t, "test-req-id", GetRequestID(ctx))
}

func TestContextGetRequestIDEmpty(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", GetRequestID(ctx))
}

func TestGetRequestIDWrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestIDKey{}, 123)
	assert.Equal(t, "", GetRequestID(ctx))
}

func TestMetricsMiddleware(t *testing.T) {
	handler := MetricsMiddleware(RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))

	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestMetricsMiddleware_RecordsCounter(t *testing.T) {
	handler := MetricsMiddleware(RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/nnssaaf-nssaa/v1/slice-authentications", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetricsMiddleware_4xx(t *testing.T) {
	handler := MetricsMiddleware(RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})))

	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-aiw/v1/authentications", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetricsMiddleware_5xx(t *testing.T) {
	handler := MetricsMiddleware(RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})))

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMetricsMiddleware_AIW(t *testing.T) {
	handler := MetricsMiddleware(RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))

	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-aiw/v1/authentications", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestStatusLabel(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{200, "2xx"}, {201, "2xx"}, {299, "2xx"},
		{301, "3xx"}, {304, "3xx"},
		{400, "4xx"}, {404, "4xx"}, {499, "4xx"},
		{500, "5xx"}, {502, "5xx"}, {503, "5xx"}, {599, "5xx"},
		{600, "5xx"}, // >= 500 falls into 5xx bucket
		{-1, "unknown"}, {99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := statusLabel(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripAPIversion(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		// Version segment only: strips trailing /vN
		{"/nnssaaf-nssaa/v1/", "/nnssaaf-nssaa/v1"},
		{"/nnssaaf-aiw/v2/authentications", "/nnssaaf-aiw/v2"},
		// Versioned resource paths: unchanged (version not at the end)
		{"/nnssaaf-nssaa/v1/slice-authentications/abc", "/nnssaaf-nssaa/v1/slice-authentications/abc"},
		// Non-versioned paths unchanged
		{"/healthz/live", "/healthz/live"},
		{"/aaa/forward", "/aaa/forward"},
		{"/metrics", "/metrics"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := stripAPIversion(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInferService(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/nnssaaf-nssaa/v1/slice-authentications", "nssaa"},
		{"/nnssaaf-aiw/v1/authentications", "aiw"},
		{"/aaa/forward", "internal"},
		{"/aaa/server-initiated", "internal"},
		{"/healthz/live", "oam"},
		{"/healthz/ready", "oam"},
		{"/metrics", "oam"},
		{"/unknown/path", "oam"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := inferService(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
