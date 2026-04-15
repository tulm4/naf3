// handler_test.go — Unit tests for NSSAA API handlers
// Spec: TS 29.526 §7.2
package nssaa

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSliceAuthInfo_InvalidJSON(t *testing.T) {
	_, err := ParseSliceAuthInfo([]byte(`{`))
	assert.Error(t, err)
}

func TestParseSliceAuthInfo_InvalidSST(t *testing.T) {
	// SST overflow (256 > 255) cannot be tested via struct literal.
	// Instead, test that the JSON parser accepts the value and validation rejects it.
	_, err := ParseSliceAuthInfo([]byte(`{"gpsi":"52080460000001","snssai":{"sst":256},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`))
	assert.Error(t, err)
}

func TestParseSliceAuthInfo_InvalidSD(t *testing.T) {
	// Parse succeeds for valid JSON; Validate catches the bad SD.
	info, err := ParseSliceAuthInfo([]byte(`{"gpsi":"52080460000001","snssai":{"sst":1,"sd":"GGGG"},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`))
	require.NoError(t, err)
	errs := info.Validate()
	assert.NotEmpty(t, errs)
}

func TestSliceAuthInfoValidate(t *testing.T) {
	tests := []struct {
		name    string
		info    SliceAuthInfo
		wantErr bool
	}{
		{
			name: "valid",
			info: SliceAuthInfo{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1, SD: "000001"},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: false,
		},
		{
			name: "valid without SD",
			info: SliceAuthInfo{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: false,
		},
		{
			name: "missing GPSI",
			info: SliceAuthInfo{
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "invalid GPSI",
			info: SliceAuthInfo{
				Gpsi:       "12345",
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "invalid SD",
			info: SliceAuthInfo{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1, SD: "GGGGGG"},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "missing EAP message",
			info: SliceAuthInfo{
				Gpsi:   "52080460000001",
				Snssai: types.Snssai{SST: 1},
			},
			wantErr: true,
		},
		{
			name: "invalid Base64 EAP message",
			info: SliceAuthInfo{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "not-valid-base64!!!",
			},
			wantErr: true,
		},
		{
			name: "invalid reauth URI scheme",
			info: SliceAuthInfo{
				Gpsi:           "52080460000001",
				Snssai:         types.Snssai{SST: 1},
				EapMessage:     "dXNlcgBleGFtcGxlLmNvbQ==",
				ReauthNotifURI: "http://amf.operator.com/callback",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.info.Validate()
			if tt.wantErr {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}
		})
	}
}

func TestParseSliceAuthConfirmationData(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "valid",
			json:    `{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			json:    `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := ParseSliceAuthConfirmationData([]byte(tt.json))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "52080460000001", data.Gpsi)
		})
	}
}

func TestHandleCreateSliceAuthentication(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})

	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantErrCause string
	}{
		{
			name:         "invalid JSON",
			body:         `{`,
			wantStatus:   400,
			wantErrCause: "INVALID_PAYLOAD",
		},
		{
			name:         "missing GPSI",
			body:         `{"snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid GPSI",
			body:         `{"gpsi":"123","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid SST",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":256},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "INVALID_PAYLOAD", // JSON parser rejects uint8 overflow
		},
		{
			name:         "invalid SD",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1,"sd":"GGGG"},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "missing EAP message",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1}}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid Base64 EAP message",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"!!!"}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid notification URI",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ==","reauthNotifUri":"http://insecure.com"}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "AAA server not configured (phase1 stub)",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   404,
			wantErrCause: "NOT_FOUND", // ErrAaaServerNotConfigured.ToProblemDetails() returns StatusNotFound
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,
				"/nnssaaf-nssaa/v1/slice-authentications",
				bytes.NewBufferString(tt.body))
			req.Header.Set(common.HeaderXRequestID, "test-req-id")
			req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
			rec := httptest.NewRecorder()

			handler.HandleCreateSliceAuthentication(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code, "status code mismatch")
			assert.Equal(t, common.MediaTypeProblemJSON, rec.Header().Get(common.HeaderContentType))

			var problem common.ProblemDetails
			err := json.Unmarshal(rec.Body.Bytes(), &problem)
			require.NoError(t, err)
			assert.Contains(t, problem.Cause, tt.wantErrCause)
		})
	}
}

func TestHandleConfirmSliceAuthentication_InvalidAuthCtxId(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})

	// Test empty authCtxId (passed directly, not via URL path)
	t.Run("empty authCtxId", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-nssaa/v1/slice-authentications/placeholder",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		handler.HandleConfirmSliceAuthentication(rec, req, "")

		assert.Equal(t, 400, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, "INVALID_AUTH_CTX_ID", problem.Cause)
	})

	// Test whitespace-only authCtxId
	t.Run("whitespace only authCtxId", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-nssaa/v1/slice-authentications/placeholder",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		handler.HandleConfirmSliceAuthentication(rec, req, "   ")

		assert.Equal(t, 400, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, "INVALID_AUTH_CTX_ID", problem.Cause)
	})

	// Test authCtxId with control character
	t.Run("authCtxId with newline", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-nssaa/v1/slice-authentications/placeholder",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		handler.HandleConfirmSliceAuthentication(rec, req, "auth\nctx")

		assert.Equal(t, 400, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, "INVALID_AUTH_CTX_ID", problem.Cause)
	})
}

func TestGenerateAuthCtxID(t *testing.T) {
	id1 := GenerateAuthCtxID()
	id2 := GenerateAuthCtxID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "each call should generate a unique ID")
}

func TestBuildLocation(t *testing.T) {
	loc := buildLocation("https://nssAAF.operator.com", "abc123")
	assert.Equal(t, "https://nssAAF.operator.com/nnssaaf-nssaa/v1/slice-authentications/abc123", loc)

	loc2 := buildLocation("https://nssAAF.operator.com/", "abc123")
	assert.Equal(t, "https://nssAAF.operator.com/nnssaaf-nssaa/v1/slice-authentications/abc123", loc2)
}

func TestFormatValidationErrors(t *testing.T) {
	single := formatValidationErrors([]error{assert.AnError})
	assert.Equal(t, assert.AnError.Error(), single)

	multi := formatValidationErrors([]error{assert.AnError, io.EOF})
	assert.Contains(t, multi, "; ")
}
