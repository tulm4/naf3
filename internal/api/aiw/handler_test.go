// handler_test.go — Unit tests for AIW API handlers
// Spec: TS 29.526 §7.3
package aiw

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

func TestParseAuthInfo(t *testing.T) {
	// ParseAuthInfo only validates JSON structure, not business rules.
	// It returns an error only if the JSON is malformed.
	tests := []struct {
		name     string
		json     string
		wantSupi string
		wantErr  bool
	}{
		{
			name:     "valid with all fields",
			json:     `{"supi":"imu-208046000000001","eapIdRsp":"dXNlcgBleGFtcGxlLmNvbQ==","supportedFeatures":"3GPP-R18-AIW"}`,
			wantSupi: "imu-208046000000001",
			wantErr: false,
		},
		{
			name:     "valid with only required fields",
			json:     `{"supi":"imu-208046000000001"}`,
			wantSupi: "imu-208046000000001",
			wantErr: false,
		},
		{
			name:    "invalid JSON syntax",
			json:    `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseAuthInfo([]byte(tt.json))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSupi, info.Supi)
		})
	}
}

func TestAuthInfoValidate(t *testing.T) {
	tests := []struct {
		name    string
		info    AuthInfo
		wantErr bool
	}{
		{
			name: "valid",
			info: AuthInfo{
				Supi:              "imu-208046000000001",
				EapIdRsp:          "dXNlcgBleGFtcGxlLmNvbQ==",
				SupportedFeatures: "3GPP-R18-AIW",
			},
			wantErr: false,
		},
		{
			name: "valid without EAP message",
			info: AuthInfo{
				Supi: "imu-208046000000001",
			},
			wantErr: false,
		},
		{
			name: "missing SUPI",
			info: AuthInfo{
				EapIdRsp: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "invalid SUPI",
			info: AuthInfo{
				Supi: "invalid-format",
			},
			wantErr: true,
		},
		{
			name: "invalid EAP Base64",
			info: AuthInfo{
				Supi:    "imu-208046000000001",
				EapIdRsp: "!!!",
			},
			wantErr: true,
		},
		{
			name: "invalid TTLS container Base64",
			info: AuthInfo{
				Supi:                    "imu-208046000000001",
				TtlsInnerMethodContainer: "!!!",
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

func TestParseConfirmAuthData(t *testing.T) {
	// ParseConfirmAuthData only validates JSON structure, not business rules.
	tests := []struct {
		name     string
		json     string
		wantSupi string
		wantErr  bool
	}{
		{
			name:     "valid",
			json:     `{"supi":"imu-208046000000001","eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantSupi: "imu-208046000000001",
			wantErr:  false,
		},
		{
			name:    "invalid JSON syntax",
			json:    `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := ParseConfirmAuthData([]byte(tt.json))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSupi, data.Supi)
		})
	}
}

func TestHandleCreateAuthentication(t *testing.T) {
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
			name:         "missing SUPI",
			body:         `{"eapIdRsp":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid SUPI",
			body:         `{"supi":"123","eapIdRsp":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid EAP Base64",
			body:         `{"supi":"imu-208046000000001","eapIdRsp":"!!!"}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "AAA not configured (phase1 stub)",
			body:         `{"supi":"imu-208046000000001"}`,
			wantStatus:   404,
			wantErrCause: "NOT_FOUND", // ErrAaaServerNotConfigured.ToProblemDetails() returns NotFoundProblem
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,
				"/nnssaaf-aiw/v1/authentications",
				bytes.NewBufferString(tt.body))
			req.Header.Set(common.HeaderXRequestID, "test-req-id")
			req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
			rec := httptest.NewRecorder()

			handler.HandleCreateAuthentication(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, common.MediaTypeProblemJSON, rec.Header().Get(common.HeaderContentType))

			var problem common.ProblemDetails
			err := json.Unmarshal(rec.Body.Bytes(), &problem)
			require.NoError(t, err)
			assert.Contains(t, problem.Cause, tt.wantErrCause)
		})
	}
}

func TestHandleConfirmAuthentication_InvalidAuthCtxId(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})

	// Test empty authCtxId (passed directly, not via URL path)
	t.Run("empty authCtxId", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-aiw/v1/authentications/valid-id",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		handler.HandleConfirmAuthentication(rec, req, "")

		assert.Equal(t, 400, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, "INVALID_AUTH_CTX_ID", problem.Cause)
	})

	// Test authCtxId with control character
	t.Run("authCtxId with control character", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-aiw/v1/authentications/valid-id",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		handler.HandleConfirmAuthentication(rec, req, "auth\nctx")

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
	assert.NotEqual(t, id1, id2)
}

func TestBuildLocation(t *testing.T) {
	loc := buildLocation("https://nssAAF.operator.com", "abc123")
	assert.Equal(t, "https://nssAAF.operator.com/nnssaaf-aiw/v1/authentications/abc123", loc)

	loc2 := buildLocation("https://nssAAF.operator.com/", "abc123")
	assert.Equal(t, "https://nssAAF.operator.com/nnssaaf-aiw/v1/authentications/abc123", loc2)
}

func TestConfirmAuthDataValidate(t *testing.T) {
	tests := []struct {
		name    string
		data    ConfirmAuthData
		wantErr bool
	}{
		{
			name: "valid",
			data: ConfirmAuthData{
				Supi:       "imu-208046000000001",
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: false,
		},
		{
			name: "valid with supported features",
			data: ConfirmAuthData{
				Supi:              "imu-208046000000001",
				EapMessage:        "dXNlcgBleGFtcGxlLmNvbQ==",
				SupportedFeatures: "3GPP-R18-AIW",
			},
			wantErr: false,
		},
		{
			name: "missing SUPI",
			data: ConfirmAuthData{
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "invalid SUPI",
			data: ConfirmAuthData{
				Supi:       "invalid-format",
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "missing EAP message",
			data: ConfirmAuthData{
				Supi: "imu-208046000000001",
			},
			wantErr: true,
		},
		{
			name: "invalid EAP Base64",
			data: ConfirmAuthData{
				Supi:       "imu-208046000000001",
				EapMessage: "!!!",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.data.Validate()
			if tt.wantErr {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}
		})
	}
}

func TestFormatAiwErrors(t *testing.T) {
	single := formatAiwErrors([]error{assert.AnError})
	assert.Equal(t, assert.AnError.Error(), single)

	multi := formatAiwErrors([]error{assert.AnError, io.EOF})
	assert.Contains(t, multi, "; ")

	empty := formatAiwErrors([]error{})
	assert.Equal(t, "unknown validation error", empty)
}

func TestAuthConfirmationResponseMarshalJSON(t *testing.T) {
	tests := []struct {
		name       string
		resp       AuthConfirmationResponse
		wantFields []string
		omitFields []string
	}{
		{
			name: "only required fields",
			resp: AuthConfirmationResponse{
				Supi: "imu-208046000000001",
			},
			wantFields: []string{`"supi":"imu-208046000000001"`},
			omitFields: []string{"eapMessage", "authResult", "pvsInfo", "msk", "supportedFeatures"},
		},
		{
			name: "all fields populated",
			resp: AuthConfirmationResponse{
				Supi:              "imu-208046000000001",
				EapMessage:        strPtr("eap-data"),
				AuthResult:        authResultPtr(types.AuthResultSuccess),
				PvsInfo:           []PvsInfo{{ServerType: "location", ServerID: "srv-1"}},
				Msk:               strPtr("bXNrLWtleS10ZXN0"),
				SupportedFeatures: "3GPP-R18-AIW",
			},
			wantFields: []string{
				`"supi":"imu-208046000000001"`,
				`"eapMessage":"eap-data"`,
				`"authResult":"EAP_SUCCESS"`,
				`"pvsInfo"`,
				`"msk":"bXNrLWtleS10ZXN0"`,
				`"supportedFeatures":"3GPP-R18-AIW"`,
			},
			omitFields: []string{},
		},
		{
			name: "nil EAP message omitted",
			resp: AuthConfirmationResponse{
				Supi:       "imu-208046000000001",
				EapMessage: nil,
			},
			omitFields: []string{"eapMessage"},
		},
		{
			name: "nil auth result omitted",
			resp: AuthConfirmationResponse{
				Supi:       "imu-208046000000001",
				AuthResult: nil,
			},
			omitFields: []string{"authResult"},
		},
		{
			name: "nil msk omitted",
			resp: AuthConfirmationResponse{
				Supi: "imu-208046000000001",
				Msk:  nil,
			},
			omitFields: []string{"msk"},
		},
		{
			name: "empty pvsInfo omitted",
			resp: AuthConfirmationResponse{
				Supi:     "imu-208046000000001",
				PvsInfo:  nil,
			},
			omitFields: []string{"pvsInfo"},
		},
		{
			name: "failure result",
			resp: AuthConfirmationResponse{
				Supi:       "imu-208046000000001",
				AuthResult: authResultPtr(types.AuthResultFailure),
			},
			wantFields: []string{`"authResult":"EAP_FAILURE"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.resp.MarshalJSON()
			require.NoError(t, err)
			jsonStr := string(data)
			for _, f := range tt.wantFields {
				assert.Contains(t, jsonStr, f)
			}
			for _, f := range tt.omitFields {
				assert.NotContains(t, jsonStr, `"`+f+`"`)
			}
		})
	}
}

func TestNewHandler(t *testing.T) {
	t.Run("nil config defaults to safe values", func(t *testing.T) {
		h := NewHandler(nil)
		assert.Equal(t, "https://nssAAF.operator.com", h.cfg.BaseURL)
	})

	t.Run("nil config still sets baseURL", func(t *testing.T) {
		h := NewHandler(nil)
		assert.NotNil(t, h.cfg)
	})

	t.Run("empty baseURL defaults", func(t *testing.T) {
		h := NewHandler(&Config{})
		assert.Equal(t, "https://nssAAF.operator.com", h.cfg.BaseURL)
	})

	t.Run("explicit baseURL preserved", func(t *testing.T) {
		h := NewHandler(&Config{BaseURL: "https://custom.operator.com"})
		assert.Equal(t, "https://custom.operator.com", h.cfg.BaseURL)
	})
}

func TestHandleConfirmAuthentication_ValidationErrors(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})

	tests := []struct {
		name           string
		authCtxID      string
		body           string
		wantStatus     int
		wantErrCause   string
	}{
		{
			name:         "malformed JSON body",
			authCtxID:    "valid-id",
			body:         `{`,
			wantStatus:   400,
			wantErrCause: "INVALID_PAYLOAD",
		},
		{
			name:         "missing SUPI in body",
			authCtxID:    "valid-id",
			body:         `{"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid SUPI in body",
			authCtxID:    "valid-id",
			body:         `{"supi":"bad","eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "missing EAP message",
			authCtxID:    "valid-id",
			body:         `{"supi":"imu-208046000000001"}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid EAP Base64",
			authCtxID:    "valid-id",
			body:         `{"supi":"imu-208046000000001","eapMessage":"!!!"}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut,
				"/nnssaaf-aiw/v1/authentications/"+tt.authCtxID,
				bytes.NewBufferString(tt.body))
			req.Header.Set(common.HeaderXRequestID, "test-req-id")
			req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
			rec := httptest.NewRecorder()

			handler.HandleConfirmAuthentication(rec, req, tt.authCtxID)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, common.MediaTypeProblemJSON, rec.Header().Get(common.HeaderContentType))

			var problem common.ProblemDetails
			err := json.Unmarshal(rec.Body.Bytes(), &problem)
			require.NoError(t, err)
			assert.Contains(t, problem.Cause, tt.wantErrCause)
		})
	}
}

func TestRouter_ServeHTTP_AIW(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})
	router := NewRouter(handler)

	t.Run("POST /authentications routes to create handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost,
			"/nnssaaf-aiw/v1/authentications",
			bytes.NewBufferString(`{"supi":"imu-208046000000001"}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		// Phase 1 stub returns 404 for AAA not configured
		assert.Equal(t, 404, rec.Code)
	})

	t.Run("PUT /authentications/{authCtxId} routes to confirm handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-aiw/v1/authentications/test-ctx-id",
			bytes.NewBufferString(`{"supi":"imu-208046000000001","eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		// Phase 1 stub returns 404 for auth context not found
		assert.Equal(t, 404, rec.Code)
	})

	t.Run("PUT /authentications/ with invalid authCtxId returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-aiw/v1/authentications/bad%0Aid",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, 400, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, "INVALID_AUTH_CTX_ID", problem.Cause)
	})

	t.Run("unhandled path returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete,
			"/nnssaaf-aiw/v1/authentications/some-id",
			nil)
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, 404, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, "NOT_FOUND", problem.Cause)
	})

	t.Run("GET on POST path returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/nnssaaf-aiw/v1/authentications",
			nil)
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, 404, rec.Code)
	})
}

// Helper: returns a pointer to a string.
func strPtr(s string) *string { return &s }

// Helper: returns a pointer to an AuthResult.
func authResultPtr(r types.AuthResult) *types.AuthResult { return &r }
