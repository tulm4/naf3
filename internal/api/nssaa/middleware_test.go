// middleware_test.go — Unit tests for NSSAA middleware
package nssaa

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddleware(t *testing.T) {
	var capturedCtx context.Context
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
	})

	middleware := AuthMiddleware(nextHandler)

	t.Run("no auth header allows request in phase1 stub", func(t *testing.T) {
		capturedCtx = nil
		req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.NotNil(t, capturedCtx, "request should pass through without auth")
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("non-Bearer scheme returns 401", func(t *testing.T) {
		capturedCtx = nil
		req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
		req.Header.Set(common.HeaderAuthorization, "Basic dXNlcjpwYXNz")
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Nil(t, capturedCtx, "request should not reach handler")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		assert.NoError(t, err)
		assert.Equal(t, "invalid authorization scheme", problem.Detail)
	})

	t.Run("empty Bearer token returns 401", func(t *testing.T) {
		capturedCtx = nil
		req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
		req.Header.Set(common.HeaderAuthorization, "Bearer ")
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Nil(t, capturedCtx)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		assert.NoError(t, err)
		assert.Equal(t, "missing bearer token", problem.Detail)
	})

	t.Run("valid Bearer token passes to next handler", func(t *testing.T) {
		capturedCtx = nil
		req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
		req.Header.Set(common.HeaderAuthorization, "Bearer some-token-value")
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.NotNil(t, capturedCtx, "request should reach next handler")
		token := GetBearerToken(capturedCtx)
		assert.Equal(t, "some-token-value", token)
	})

	t.Run("Bearer token with only whitespace returns 401", func(t *testing.T) {
		capturedCtx = nil
		req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
		req.Header.Set(common.HeaderAuthorization, "Bearer ")
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Nil(t, capturedCtx)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestGetBearerToken(t *testing.T) {
	t.Run("token present in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), contextKeyBearerToken{}, "my-secret-token")
		assert.Equal(t, "my-secret-token", GetBearerToken(ctx))
	})

	t.Run("token absent from context", func(t *testing.T) {
		ctx := context.Background()
		assert.Equal(t, "", GetBearerToken(ctx))
	})

	t.Run("wrong type stored returns empty", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), contextKeyBearerToken{}, 123)
		assert.Equal(t, "", GetBearerToken(ctx))
	})
}

func TestValidateAmfRequest(t *testing.T) {
	t.Run("empty amfInstanceId returns nil (optional field)", func(t *testing.T) {
		err := ValidateAmfRequest(context.Background(), "")
		assert.NoError(t, err)
	})

	t.Run("non-empty amfInstanceId returns nil in phase1 stub", func(t *testing.T) {
		err := ValidateAmfRequest(context.Background(), "amf-instance-001")
		assert.NoError(t, err)
	})
}

func TestValidateNssaiForGpsi(t *testing.T) {
	t.Run("always returns nil in phase1 stub", func(t *testing.T) {
		err := ValidateNssaiForGpsi(context.Background(), "52080460000001", types.Snssai{SST: 1, SD: "000001"})
		assert.NoError(t, err)
	})

	t.Run("empty GPSI also returns nil", func(t *testing.T) {
		err := ValidateNssaiForGpsi(context.Background(), "", types.Snssai{SST: 1})
		assert.NoError(t, err)
	})
}

func TestAuthResultFromStatus(t *testing.T) {
	tests := []struct {
		name   string
		status types.NssaaStatus
		want   *types.AuthResult
	}{
		{
			name:   "EAP_SUCCESS maps to EAP_SUCCESS",
			status: types.NssaaStatusEapSuccess,
			want:   ptr(types.AuthResultSuccess),
		},
		{
			name:   "EAP_FAILURE maps to EAP_FAILURE",
			status: types.NssaaStatusEapFailure,
			want:   ptr(types.AuthResultFailure),
		},
		{
			name:   "NssaaStatusPending returns nil",
			status: types.NssaaStatusPending,
			want:   nil,
		},
		{
			name:   "NssaaStatusNotExecuted returns nil",
			status: types.NssaaStatusNotExecuted,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AuthResultFromStatus(tt.status)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, *tt.want, *got)
			}
		})
	}
}

func TestSliceAuthConfirmationDataValidate(t *testing.T) {
	tests := []struct {
		name    string
		data    SliceAuthConfirmationData
		wantErr bool
	}{
		{
			name: "valid",
			data: SliceAuthConfirmationData{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: false,
		},
		{
			name: "valid with SD",
			data: SliceAuthConfirmationData{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1, SD: "000001"},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: false,
		},
		{
			name: "missing GPSI",
			data: SliceAuthConfirmationData{
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "invalid GPSI format",
			data: SliceAuthConfirmationData{
				Gpsi:       "12345",
				Snssai:     types.Snssai{SST: 1},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "invalid SD",
			data: SliceAuthConfirmationData{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1, SD: "GGGG"},
				EapMessage: "dXNlcgBleGFtcGxlLmNvbQ==",
			},
			wantErr: true,
		},
		{
			name: "missing EAP message",
			data: SliceAuthConfirmationData{
				Gpsi:   "52080460000001",
				Snssai: types.Snssai{SST: 1},
			},
			wantErr: true,
		},
		{
			name: "invalid EAP Base64",
			data: SliceAuthConfirmationData{
				Gpsi:       "52080460000001",
				Snssai:     types.Snssai{SST: 1},
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

func TestNewHandler_NSSAA(t *testing.T) {
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

func TestHandleConfirmSliceAuthentication_ValidationErrors(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})

	tests := []struct {
		name         string
		authCtxID    string
		body         string
		wantStatus   int
		wantErrCause string
	}{
		{
			name:         "malformed JSON body",
			authCtxID:    "valid-id",
			body:         `{`,
			wantStatus:   400,
			wantErrCause: "INVALID_PAYLOAD",
		},
		{
			name:         "missing GPSI in body",
			authCtxID:    "valid-id",
			body:         `{"snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid GPSI in body",
			authCtxID:    "valid-id",
			body:         `{"gpsi":"bad","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "missing EAP message",
			authCtxID:    "valid-id",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1}}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
		{
			name:         "invalid EAP Base64",
			authCtxID:    "valid-id",
			body:         `{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"!!!"}`,
			wantStatus:   400,
			wantErrCause: "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut,
				"/nnssaaf-nssaa/v1/slice-authentications/"+tt.authCtxID,
				bytes.NewBufferString(tt.body))
			req.Header.Set(common.HeaderXRequestID, "test-req-id")
			req.Header.Set(common.HeaderContentType, common.MediaTypeJSON)
			rec := httptest.NewRecorder()

			handler.HandleConfirmSliceAuthentication(rec, req, tt.authCtxID)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, common.MediaTypeProblemJSON, rec.Header().Get(common.HeaderContentType))

			var problem common.ProblemDetails
			err := json.Unmarshal(rec.Body.Bytes(), &problem)
			assert.NoError(t, err)
			assert.Contains(t, problem.Cause, tt.wantErrCause)
		})
	}
}

func TestRouter_ServeHTTP_NSSAA(t *testing.T) {
	handler := NewHandler(&Config{BaseURL: "https://nssAAF.operator.com"})
	router := NewRouter(handler)

	t.Run("POST /slice-authentications routes to create handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost,
			"/nnssaaf-nssaa/v1/slice-authentications",
			bytes.NewBufferString(`{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		// Phase 1 stub returns 404 for AAA not configured
		assert.Equal(t, 404, rec.Code)
	})

	t.Run("PUT /slice-authentications/{authCtxId} routes to confirm handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-nssaa/v1/slice-authentications/test-ctx-id",
			bytes.NewBufferString(`{"gpsi":"52080460000001","snssai":{"sst":1},"eapMessage":"dXNlcgBleGFtcGxlLmNvbQ=="}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		// Phase 1 stub returns 404 for auth context not found
		assert.Equal(t, 404, rec.Code)
	})

	t.Run("PUT /slice-authentications/ with invalid authCtxId returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/nnssaaf-nssaa/v1/slice-authentications/bad%0Aid",
			bytes.NewBufferString(`{}`))
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, 400, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		assert.NoError(t, err)
		assert.Equal(t, "INVALID_AUTH_CTX_ID", problem.Cause)
	})

	t.Run("unhandled path returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete,
			"/nnssaaf-nssaa/v1/slice-authentications/some-id",
			nil)
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, 404, rec.Code)
		var problem common.ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		assert.NoError(t, err)
		assert.Equal(t, "NOT_FOUND", problem.Cause)
	})

	t.Run("GET on POST path returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/nnssaaf-nssaa/v1/slice-authentications",
			nil)
		req.Header.Set(common.HeaderXRequestID, "test-req-id")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, 404, rec.Code)
	})
}

// ptr returns a pointer to the given value.
func ptr[T any](v T) *T { return &v }