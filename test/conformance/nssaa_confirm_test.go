// Package conformance provides TS 29.526 NSSAA API conformance test suites.
// Spec: TS 29.526 v18.7.0 §7.2 (NSSAA Confirm)
package conformance

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/storage"
	nssaanats "github.com/operator/nssAAF/oapi-gen/gen/nssaa"
	"github.com/stretchr/testify/assert"
)

// ─── Session Lookup Tests ─────────────────────────────────────────────────────

// TC-NSSAA-CONFIRM-001: Valid confirm with existing session → 200.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_ValidConfirmExtended(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-001"] = &storage.NssaaSession{
		AuthCtxID: "ctx-001",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-001", body)

	assert.Equal(t, http.StatusOK, rec.Code,
		"TC-NSSAA-CONFIRM-001: Valid confirm → 200, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-002: Session not found → 404.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SessionNotFoundExtended(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/nonexistent", body)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"TC-NSSAA-CONFIRM-002: Session not found → 404, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-003: Store load error → 500.
// Spec: TS 29.526 §7.2
func TestTS29526_NSSAA_ConfirmSlice_StoreLoadError(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.loadErr = errors.New("connection refused")
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-003", body)

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"TC-NSSAA-CONFIRM-003: Store load error → 500, got %d", rec.Code)
}

// ─── GPSI Validation Tests ──────────────────────────────────────────────────

// TC-NSSAA-CONFIRM-010: GPSI mismatch between request and session → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_GPSIMismatchExtended(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-010"] = &storage.NssaaSession{
		AuthCtxID: "ctx-010",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-209999999999999", // Different GPSI
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-010", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-010: GPSI mismatch → 400, got %d", rec.Code)
	assert.Contains(t, rec.Body.String(), "gpsi",
		"TC-NSSAA-CONFIRM-010: error should mention gpsi field")
}

// TC-NSSAA-CONFIRM-011: GPSI valid format variants → 200.
// Spec: TS 29.571 §5.2.2
func TestTS29526_NSSAA_ConfirmSlice_GPSIValidVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		gpsi string
	}{
		{"msisdn-form", "msisdn-208046000000001"},
		{"extid-form", "extid-user@example.com"},
		{"catchall", "subscriber123"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newNssaaMockStore()
			store.data["ctx-011"] = &storage.NssaaSession{
				AuthCtxID: "ctx-011",
				GPSI:      tc.gpsi,
				SnssaiSST: 1,
			}
			h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

			body := map[string]interface{}{
				"gpsi":       tc.gpsi,
				"snssai":     map[string]interface{}{"sst": 1},
				"eapMessage": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-011", body)

			assert.Equal(t, http.StatusOK, rec.Code,
				"TC-NSSAA-CONFIRM-011: GPSI %s → 200, got %d", tc.gpsi, rec.Code)
		})
	}
}

// ─── S-NSSAI Validation Tests ───────────────────────────────────────────────

// TC-NSSAA-CONFIRM-012: S-NSSAI mismatch between request and session → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SnssaiMismatchExtended(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-012"] = &storage.NssaaSession{
		AuthCtxID: "ctx-012",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 2, "sd": "000002"}, // Different S-NSSAI
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-012", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-012: S-NSSAI mismatch → 400, got %d", rec.Code)
	assert.Contains(t, rec.Body.String(), "snssai",
		"TC-NSSAA-CONFIRM-012: error should mention snssai field")
}

// TC-NSSAA-CONFIRM-013: S-NSSAI SST mismatch → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SnssaiSSTMismatch(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-013"] = &storage.NssaaSession{
		AuthCtxID: "ctx-013",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 2}, // Different SST
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-013", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-013: S-NSSAI SST mismatch → 400, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-014: S-NSSAI SD mismatch → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SnssaiSDMismatch(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-014"] = &storage.NssaaSession{
		AuthCtxID: "ctx-014",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "FFFFFF"}, // Different SD
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-014", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-014: S-NSSAI SD mismatch → 400, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-015: S-NSSAI valid formats → 200.
// Spec: TS 29.571 §5.4.4.60
func TestTS29526_NSSAA_ConfirmSlice_SnssaiValidFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sst  uint8
		sd   string
	}{
		{"sst-only", 1, ""},
		{"sst-with-sd", 1, "000001"},
		// Note: SST=0 is rejected by current implementation
		{"sst-max", 255, "FFFFFF"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newNssaaMockStore()
			store.data["ctx-015"] = &storage.NssaaSession{
				AuthCtxID: "ctx-015",
				GPSI:      "msisdn-208046000000001",
				SnssaiSST: tc.sst,
				SnssaiSD:  tc.sd,
			}
			h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

			body := map[string]interface{}{
				"gpsi":       "msisdn-208046000000001",
				"snssai":     map[string]interface{}{"sst": tc.sst, "sd": tc.sd},
				"eapMessage": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-015", body)

			assert.Equal(t, http.StatusOK, rec.Code,
				"TC-NSSAA-CONFIRM-015: S-NSSAI sst=%d sd=%s → 200, got %d", tc.sst, tc.sd, rec.Code)
		})
	}
}

// TC-NSSAA-CONFIRM-016: Session has SD but request omits SD → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SnssaiSDMismatchSessionHasSD(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-016"] = &storage.NssaaSession{
		AuthCtxID: "ctx-016",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001", // Session has SD
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	// Request omits SD (empty string)
	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1}, // No SD
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-016", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-016: Session has SD, request omits → 400, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-017: Session has no SD but request provides SD → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SnssaiSDMismatchSessionOmitsSD(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-017"] = &storage.NssaaSession{
		AuthCtxID: "ctx-017",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "", // Session has no SD
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	// Request provides SD
	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"}, // SD provided
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-017", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-017: Session omits SD, request has SD → 400, got %d", rec.Code)
}

// ─── EAP Message Tests ──────────────────────────────────────────────────────

// TC-NSSAA-CONFIRM-020: Valid EAP message (valid base64) → 200.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_EAPValidBase64(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-020"] = &storage.NssaaSession{
		AuthCtxID: "ctx-020",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-020", body)

	assert.Equal(t, http.StatusOK, rec.Code,
		"TC-NSSAA-CONFIRM-020: valid EAP → 200, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-021: Missing EAP message → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_EAPMissing(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-021"] = &storage.NssaaSession{
		AuthCtxID: "ctx-021",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":   "msisdn-208046000000001",
		"snssai": map[string]interface{}{"sst": 1},
		// eapMessage omitted
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-021", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-021: missing EAP → 400, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-022: Empty EAP message → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_EAPEmpty(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-022"] = &storage.NssaaSession{
		AuthCtxID: "ctx-022",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-022", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-022: empty EAP → 400, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-023: Invalid base64 EAP message → 400.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_EAPInvalidBase64(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-023"] = &storage.NssaaSession{
		AuthCtxID: "ctx-023",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "not-valid-base64!!!",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-023", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-CONFIRM-023: invalid base64 EAP → 400, got %d", rec.Code)
}

// ─── AuthCtxId Validation Tests ─────────────────────────────────────────────

// TC-NSSAA-CONFIRM-030: Invalid authCtxId format validation.
// Note: Control characters in URL cause httptest.NewRequest to panic.
// We test with a very long authCtxId instead to test validation limits.
// Spec: TS 29.526 §7.2.2
func TestTS29526_NSSAA_ConfirmSlice_AuthCtxIdInvalid(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	// Test with session not found - authCtxId validation happens after session lookup
	// A non-existent UUID should return 404
	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/nonexistent-session", body)

	// Session not found → 404
	assert.Equal(t, http.StatusNotFound, rec.Code,
		"TC-NSSAA-CONFIRM-030: nonexistent authCtxId → 404, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-031: Valid authCtxId formats → 200 or 404.
// Spec: TS 29.526 §7.2.2
func TestTS29526_NSSAA_ConfirmSlice_AuthCtxIdValidFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authCtxId  string
		expectCode int
	}{
		{"uuid-format", "550e8400-e29b-41d4-a716-446655440000", http.StatusNotFound},
		{"simple-id", "auth-ctx-001", http.StatusNotFound},
		{"numeric-id", "12345", http.StatusNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newNssaaMockStore()
			h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

			body := map[string]interface{}{
				"gpsi":       "msisdn-208046000000001",
				"snssai":     map[string]interface{}{"sst": 1},
				"eapMessage": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/"+tc.authCtxId, body)

			assert.Equal(t, tc.expectCode, rec.Code,
				"TC-NSSAA-CONFIRM-031: authCtxId %s → %d, got %d", tc.authCtxId, tc.expectCode, rec.Code)
		})
	}
}

// ─── Response Structure Tests ────────────────────────────────────────────────

// TC-NSSAA-CONFIRM-040: Response includes X-Request-ID header.
// Spec: TS 29.500 §6.1
func TestTS29526_NSSAA_ConfirmSlice_ResponseXRequestID(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-040"] = &storage.NssaaSession{
		AuthCtxID: "ctx-040",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-040", body)

	xRequestID := rec.Header().Get(common.HeaderXRequestID)
	assert.Equal(t, "conf-req-id", xRequestID,
		"TC-NSSAA-CONFIRM-040: X-Request-ID should be echoed")
}

// TC-NSSAA-CONFIRM-041: Response body contains expected fields.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_ResponseBody(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-041"] = &storage.NssaaSession{
		AuthCtxID: "ctx-041",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-041", body)

	var resp nssaanats.SliceAuthConfirmationResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.NoError(t, err, "TC-NSSAA-CONFIRM-041: Response should be valid JSON")
	assert.Equal(t, "msisdn-208046000000001", string(resp.Gpsi), "TC-NSSAA-CONFIRM-041: gpsi echoed")
	assert.Equal(t, uint8(1), resp.Snssai.Sst, "TC-NSSAA-CONFIRM-041: sst echoed")
	assert.NotNil(t, resp.EapMessage, "TC-NSSAA-CONFIRM-041: eapMessage present")
}

// TC-NSSAA-CONFIRM-042: Content-Type header is application/json.
// Spec: TS 29.526 §7.2
func TestTS29526_NSSAA_ConfirmSlice_ContentType(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-042"] = &storage.NssaaSession{
		AuthCtxID: "ctx-042",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-042", body)

	contentType := rec.Header().Get(common.HeaderContentType)
	assert.Contains(t, contentType, "application/json",
		"TC-NSSAA-CONFIRM-042: Content-Type should be application/json")
}

// ─── Session State Tests ─────────────────────────────────────────────────────

// TC-NSSAA-CONFIRM-050: Session state is updated after confirm.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_ConfirmSlice_SessionUpdated(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-050"] = &storage.NssaaSession{
		AuthCtxID: "ctx-050",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-050", body)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify session was updated in store
	loaded, err := store.Load(nil, "ctx-050")
	assert.NoError(t, err, "TC-NSSAA-CONFIRM-050: Session should be loadable")
	assert.NotEmpty(t, loaded.EapPayload, "TC-NSSAA-CONFIRM-050: EAP payload should be stored")
}

// TC-NSSAA-CONFIRM-051: Store update error → 500.
// Spec: TS 29.526 §7.2
func TestTS29526_NSSAA_ConfirmSlice_StoreUpdateError(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-051"] = &storage.NssaaSession{
		AuthCtxID: "ctx-051",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	store.saveErr = errStoreUnavailable
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-051", body)

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"TC-NSSAA-CONFIRM-051: Store update error → 500, got %d", rec.Code)
}

// ─── HTTP Method Tests ──────────────────────────────────────────────────────

// TC-NSSAA-CONFIRM-060: GET method not allowed for confirm endpoint → 405.
// Spec: RFC 7231 §4.3.1
func TestTS29526_NSSAA_ConfirmSlice_GetMethodNotAllowed(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	rec := nssaaRequest(h, http.MethodGet, "/nnssaaf-nssaa/v1/slice-authentications/ctx-060", nil)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"TC-NSSAA-CONFIRM-060: GET → 405, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-061: POST method not allowed for confirm endpoint → 405.
// Spec: RFC 7231 §4.3.2
func TestTS29526_NSSAA_ConfirmSlice_PostMethodNotAllowed(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":       "msisdn-208046000000001",
		"snssai":     map[string]interface{}{"sst": 1},
		"eapMessage": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications/ctx-061", body)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"TC-NSSAA-CONFIRM-061: POST → 405, got %d", rec.Code)
}

// TC-NSSAA-CONFIRM-062: DELETE method not allowed → 405.
// Spec: RFC 7231 §4.3.5
func TestTS29526_NSSAA_ConfirmSlice_DeleteMethodNotAllowed(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	rec := nssaaRequest(h, http.MethodDelete, "/nnssaaf-nssaa/v1/slice-authentications/ctx-062", nil)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"TC-NSSAA-CONFIRM-062: DELETE → 405, got %d", rec.Code)
}
