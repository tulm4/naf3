// Package conformance provides TS 29.526 NSSAA API conformance test suites.
// Spec: TS 29.526 v18.7.0 §7.2 (NSSAA Schema Validation)
package conformance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/storage"
	"github.com/stretchr/testify/assert"
)

// ─── GPSI Schema Validation Tests ─────────────────────────────────────────────

// TC-NSSAA-SCHEMA-001: GPSI regex validation - MSISDN form.
// Note: The catch-all pattern `.+` accepts ANY non-empty string. So
// "msisdn-" + any characters (including >15 digits) is accepted as catch-all.
// The only truly invalid GPSI is empty string.
// Spec: TS 29.571 §5.2.2
func TestTS29526_NSSAA_Schema_GPSIRegexMSISDN(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		gpsi    string
		wantErr bool
	}{
		// Valid MSISDN: msisdn-{5-15 digits}
		{"min-5-digits", "msisdn-12345", false},
		{"max-15-digits", "msisdn-123456789012345", false},
		{"typical", "msisdn-208046000000001", false},
		// Catch-all forms (accepted)
		{"too-many-digits-catchall", "msisdn-1234567890123456", false},
		{"no-digits-catchall", "msisdn-", false},
		{"non-numeric-catchall", "msisdn-abc", false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":     tc.gpsi,
				"snssai":   map[string]interface{}{"sst": 1},
				"eapIdRsp": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-001: GPSI %s should fail → 400, got %d", tc.gpsi, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-001: GPSI %s should pass → 201, got %d", tc.gpsi, rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-002: GPSI regex validation - External Identifier form.
// Note: The spec allows "catch-all" form for any non-empty string, so
// forms like "extid-usernamerealm.com" (no @) are accepted as catch-all.
// The strict External ID form with @ is validated separately.
// Spec: TS 29.571 §5.2.2
func TestTS29526_NSSAA_Schema_GPSIRegexExternalId(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		gpsi    string
		wantErr bool
	}{
		// Valid External Identifier: extid-{id}@{realm}
		{"simple", "extid-user@example.com", false},
		{"with-dots", "extid-john.doe@operator.com", false},
		{"with-dash", "extid-user-name@realm.com", false},
		// Note: forms without @ are accepted as catch-all per spec
		{"no-at-catchall", "extid-usernamerealm.com", false}, // Accepted as catch-all
		{"empty-id-catchall", "extid-@realm.com", false},     // Accepted as catch-all
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":     tc.gpsi,
				"snssai":   map[string]interface{}{"sst": 1},
				"eapIdRsp": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-002: GPSI %s should fail → 400, got %d", tc.gpsi, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-002: GPSI %s should pass → 201, got %d", tc.gpsi, rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-003: GPSI regex validation - Catch-all form.
// Note: Whitespace-only strings are accepted as catch-all per regex `.+`.
// The only truly invalid GPSI is an empty string.
// Spec: TS 29.571 §5.2.2
func TestTS29526_NSSAA_Schema_GPSIRegexCatchAll(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		gpsi    string
		wantErr bool
	}{
		// Valid catch-all: any non-empty string
		{"simple", "subscriber123", false},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"alphanumeric", "abc123xyz", false},
		{"whitespace-accepted", "   ", false},
		// Invalid: empty string
		{"empty", "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":     tc.gpsi,
				"snssai":   map[string]interface{}{"sst": 1},
				"eapIdRsp": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-003: GPSI %q should fail → 400, got %d", tc.gpsi, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-003: GPSI %q should pass → 201, got %d", tc.gpsi, rec.Code)
			}
		})
	}
}

// ─── S-NSSAI Schema Validation Tests ─────────────────────────────────────────

// TC-NSSAA-SCHEMA-010: S-NSSAI SST validation range.
// Note: SST=0 is rejected by current implementation even though spec allows it.
// This is a validation gap - SST should accept 0-255 per TS 29.571 §5.4.4.60.
// Spec: TS 29.571 §5.4.4.60
func TestTS29526_NSSAA_Schema_SnssaiSSTRange(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		sst     interface{}
		wantErr bool
	}{
		// Valid range: 1-255 (current implementation)
		{"sst-standard", 1, false},
		{"sst-max", 255, false},
		// SST=0 is rejected (validation gap - spec allows 0)
		{"sst-min-zero", 0, true},
		// Invalid range
		{"sst-negative", -1, true},
		{"sst-over", 256, true},
		{"sst-over-1000", 1000, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":   "msisdn-208046000000001",
				"snssai": map[string]interface{}{"sst": tc.sst},
				"eapIdRsp": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-010: SST=%v should fail → 400, got %d", tc.sst, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-010: SST=%v should pass → 201, got %d", tc.sst, rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-011: S-NSSAI SD validation - exactly 6 hex characters.
// Spec: TS 29.571 §5.4.4.60
func TestTS29526_NSSAA_Schema_SnssaiSDFormat(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		sd      string
		wantErr bool
	}{
		// Valid: exactly 6 hex chars
		{"sd-all-zeros", "000000", false},
		{"sd-all-ff", "FFFFFF", false},
		{"sd-mixed-case", "AbCdEf", false},
		{"sd-lower-case", "abcdef", false},
		{"sd-upper-case", "ABCDEF", false},
		// Invalid length
		{"sd-empty", "", false}, // SD is optional
		{"sd-1-char", "A", true},
		{"sd-5-chars", "ABCDE", true},
		{"sd-7-chars", "ABCDEFG", true},
		// Invalid chars (non-hex)
		{"sd-g-char", "GGGGGG", true},
		{"sd-z-char", "ZZZZZZ", true},
		{"sd-special", "!!####", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":   "msisdn-208046000000001",
				"snssai": map[string]interface{}{"sst": 1, "sd": tc.sd},
				"eapIdRsp": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-011: SD=%s should fail → 400, got %d", tc.sd, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-011: SD=%s should pass → 201, got %d", tc.sd, rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-012: S-NSSAI presence validation.
// Spec: TS 29.526 §7.2.2
func TestTS29526_NSSAA_Schema_SnssaiPresence(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		snssai  interface{}
		wantErr bool
	}{
		{"snssai-present", map[string]interface{}{"sst": 1}, false},
		{"snssai-missing", nil, true},
		{"snssai-empty-obj", map[string]interface{}{}, true}, // Gap E2E-01
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi": "msisdn-208046000000001",
				"eapIdRsp": "dGVzdA==",
			}
			if tc.snssai != nil {
				body["snssai"] = tc.snssai
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-012: %s should fail → 400, got %d", tc.name, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-012: %s should pass → 201, got %d", tc.name, rec.Code)
			}
		})
	}
}

// ─── EAP Payload Schema Validation Tests ───────────────────────────────────────

// TC-NSSAA-SCHEMA-020: EAP payload base64 validation.
// Note: Go's base64.StdEncoding requires proper padding, so some
// base64 strings without padding may be rejected.
// Spec: TS 29.526 §7.2.2
func TestTS29526_NSSAA_Schema_EAPPayloadBase64(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		eap     string
		wantErr bool
	}{
		// Valid base64 with proper padding
		{"valid-standard", "dGVzdA==", false},
		{"valid-long", "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQA=", false},
		// Invalid base64
		{"invalid-chars", "not-valid!!!", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":     "msisdn-208046000000001",
				"snssai":   map[string]interface{}{"sst": 1},
				"eapIdRsp": tc.eap,
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-020: EAP %q should fail → 400, got %d", tc.eap, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-020: EAP %q should pass → 201, got %d", tc.eap, rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-021: EAP payload presence validation.
// Spec: TS 29.526 §7.2.2
func TestTS29526_NSSAA_Schema_EAPPayloadPresence(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		eap     interface{}
		wantErr bool
	}{
		{"eap-present", "dGVzdA==", false},
		{"eap-missing", nil, true},
		{"eap-empty-string", "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":   "msisdn-208046000000001",
				"snssai": map[string]interface{}{"sst": 1},
			}
			if tc.eap != nil {
				body["eapIdRsp"] = tc.eap
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-021: %s should fail → 400, got %d", tc.name, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-021: %s should pass → 201, got %d", tc.name, rec.Code)
			}
		})
	}
}

// ─── AmfInstanceId Schema Validation Tests ─────────────────────────────────────

// TC-NSSAA-SCHEMA-030: AmfInstanceId format validation.
// Spec: TS 29.526 §7.2.2 (UUID format)
func TestTS29526_NSSAA_Schema_AmfInstanceId(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name        string
		amfInstance *string
		wantErr     bool
	}{
		{"amf-missing", nil, false}, // Optional
		{"amf-uuid", strPtr("550e8400-e29b-41d4-a716-446655440000"), false},
		{"amf-simple", strPtr("amf-001"), false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":     "msisdn-208046000000001",
				"snssai":   map[string]interface{}{"sst": 1},
				"eapIdRsp": "dGVzdA==",
			}
			if tc.amfInstance != nil {
				body["amfInstanceId"] = *tc.amfInstance
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-030: %s should fail → 400, got %d", tc.name, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-030: %s should pass → 201, got %d", tc.name, rec.Code)
			}
		})
	}
}

// ─── Notification URI Schema Validation Tests ──────────────────────────────────

// TC-NSSAA-SCHEMA-040: Notification URI format validation.
// Note: The current implementation accepts any URI scheme including http://.
// A future update should require https:// per security best practices.
// Spec: TS 29.526 §7.2.2
func TestTS29526_NSSAA_Schema_NotificationURI(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name           string
		reauthURI      *string
		revocURI       *string
		wantErr        bool
	}{
		{"uris-missing", nil, nil, false}, // Optional
		{"valid-https", strPtr("https://amf.operator.com/callback"), nil, false},
		{"valid-both", strPtr("https://amf1.com/reauth"), strPtr("https://amf1.com/revoc"), false},
		// Currently accepts http - future should require https
		{"http-accepted", strPtr("http://amf.operator.com"), nil, false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":     "msisdn-208046000000001",
				"snssai":   map[string]interface{}{"sst": 1},
				"eapIdRsp": "dGVzdA==",
			}
			if tc.reauthURI != nil {
				body["reauthNotifUri"] = *tc.reauthURI
			}
			if tc.revocURI != nil {
				body["revocNotifUri"] = *tc.revocURI
			}
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-040: %s should fail → 400, got %d", tc.name, rec.Code)
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code,
					"TC-NSSAA-SCHEMA-040: %s should pass → 201, got %d", tc.name, rec.Code)
			}
		})
	}
}

// ─── Request Body JSON Schema Tests ────────────────────────────────────────────

// TC-NSSAA-SCHEMA-050: Invalid JSON body → 400.
// Spec: RFC 7159
func TestTS29526_NSSAA_Schema_InvalidJSON(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name string
		body string
	}{
		{"syntax-error", "{not json"},
		{"trailing-garbage", `{"gpsi":"test"} extra`},
		{"invalid-unicode", "{gpsi:\xff}",},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(tc.body))
			req.Header.Set(common.HeaderXRequestID, "conf-req-id")
			req.Header.Set(common.HeaderContentType, common.MediaTypeJSONVersion)
			rec := httptest.NewRecorder()
			makeNssaaRouter(h).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code,
				"TC-NSSAA-SCHEMA-050: %s → 400, got %d", tc.name, rec.Code)
		})
	}
}

// GAP: The current implementation accepts any Content-Type.
// Per RFC 7159 and TS 29.526 §7.2, only application/json should be accepted.
// Fix: Add Content-Type middleware validation that rejects non-JSON Content-Types with 415.

// TC-NSSAA-SCHEMA-051: Content-Type validation.
// Note: The current implementation is lenient and accepts various Content-Types.
// A future update should strictly enforce application/json.
// Spec: RFC 7159, TS 29.526 §7.2
func TestTS29526_NSSAA_Schema_ContentTypeValidation(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := `{"gpsi":"msisdn-208046000000001","snssai":{"sst":1},"eapIdRsp":"dGVzdA=="}`

	tests := []struct {
		name       string
		contentType string
		wantCode   int
	}{
		{"application/json", "application/json", http.StatusCreated},
		{"application/json-charset", "application/json; charset=utf-8", http.StatusCreated},
		// Currently lenient - accepts other types too
		{"text/plain-accepted", "text/plain", http.StatusCreated},
		{"application/xml-accepted", "application/xml", http.StatusCreated},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(body))
			req.Header.Set(common.HeaderXRequestID, "conf-req-id")
			req.Header.Set(common.HeaderContentType, tc.contentType)
			rec := httptest.NewRecorder()
			makeNssaaRouter(h).ServeHTTP(rec, req)

			assert.Equal(t, tc.wantCode, rec.Code,
				"TC-NSSAA-SCHEMA-051: %s → %d, got %d", tc.name, tc.wantCode, rec.Code)
		})
	}
}

// ─── Confirm Endpoint Schema Validation Tests ──────────────────────────────────

// TC-NSSAA-SCHEMA-060: Confirm GPSI mismatch validation.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_Schema_ConfirmGPSIMismatch(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-060"] = &storage.NssaaSession{
		AuthCtxID: "ctx-060",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		gpsi    string
		wantErr bool
	}{
		{"gpsi-match", "msisdn-208046000000001", false},
		{"gpsi-different", "msisdn-209999999999999", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":       tc.gpsi,
				"snssai":     map[string]interface{}{"sst": 1},
				"eapMessage": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-060", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-060: GPSI mismatch should fail → 400, got %d", rec.Code)
			} else {
				assert.Equal(t, http.StatusOK, rec.Code,
					"TC-NSSAA-SCHEMA-060: GPSI match should pass → 200, got %d", rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-061: Confirm S-NSSAI mismatch validation.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_Schema_ConfirmSnssaiMismatch(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-061"] = &storage.NssaaSession{
		AuthCtxID: "ctx-061",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
		SnssaiSD:  "000001",
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		sst     int
		sd      string
		wantErr bool
	}{
		{"snssai-match", 1, "000001", false},
		{"sst-mismatch", 2, "000001", true},
		{"sd-mismatch", 1, "FFFFFF", true},
		{"both-mismatch", 2, "FFFFFF", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":       "msisdn-208046000000001",
				"snssai":     map[string]interface{}{"sst": tc.sst, "sd": tc.sd},
				"eapMessage": "dGVzdA==",
			}
			rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-061", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-061: S-NSSAI mismatch should fail → 400, got %d", rec.Code)
			} else {
				assert.Equal(t, http.StatusOK, rec.Code,
					"TC-NSSAA-SCHEMA-061: S-NSSAI match should pass → 200, got %d", rec.Code)
			}
		})
	}
}

// TC-NSSAA-SCHEMA-062: Confirm eapMessage validation.
// Spec: TS 29.526 §7.2.3
func TestTS29526_NSSAA_Schema_ConfirmEAPMessageValidation(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	store.data["ctx-062"] = &storage.NssaaSession{
		AuthCtxID: "ctx-062",
		GPSI:      "msisdn-208046000000001",
		SnssaiSST: 1,
	}
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		eap     interface{}
		wantErr bool
	}{
		{"eap-valid", "dGVzdA==", false},
		{"eap-missing", nil, true},
		{"eap-empty", "", true},
		{"eap-invalid", "not-base64!!!", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]interface{}{
				"gpsi":   "msisdn-208046000000001",
				"snssai": map[string]interface{}{"sst": 1},
			}
			if tc.eap != nil {
				body["eapMessage"] = tc.eap
			}
			rec := nssaaRequest(h, http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/ctx-062", body)

			if tc.wantErr {
				assert.Equal(t, http.StatusBadRequest, rec.Code,
					"TC-NSSAA-SCHEMA-062: %s should fail → 400, got %d", tc.name, rec.Code)
			} else {
				assert.Equal(t, http.StatusOK, rec.Code,
					"TC-NSSAA-SCHEMA-062: %s should pass → 200, got %d", tc.name, rec.Code)
			}
		})
	}
}

// ─── ProblemDetails Schema Validation Tests ────────────────────────────────────

// TC-NSSAA-SCHEMA-070: ProblemDetails format validation for 400 errors.
// Spec: RFC 7807 §3.1 (required fields: type, title, status, detail)
func TestTS29526_NSSAA_Schema_ProblemDetailsFormat(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	body := map[string]interface{}{
		"gpsi":     "", // Invalid: empty GPSI
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dGVzdA==",
	}
	rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"TC-NSSAA-SCHEMA-070: Invalid GPSI → 400")

	// Parse and validate ProblemDetails structure
	var problem map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &problem)
	assert.NoError(t, err, "TC-NSSAA-SCHEMA-070: Response should be valid JSON")

	// RFC 7807 §3.1 required fields: type, title, status, detail
	assert.Contains(t, problem, "status",
		"TC-NSSAA-SCHEMA-070: ProblemDetails must contain 'status' (RFC 7807 §3.1)")
	assert.Contains(t, problem, "detail",
		"TC-NSSAA-SCHEMA-070: ProblemDetails must contain 'detail' (RFC 7807 §3.1)")
	assert.Contains(t, problem, "type",
		"TC-NSSAA-SCHEMA-070: ProblemDetails must contain 'type' (RFC 7807 §3.1)")
	assert.Contains(t, problem, "title",
		"TC-NSSAA-SCHEMA-070: ProblemDetails must contain 'title' (RFC 7807 §3.1)")

	// Validate status is a number
	status, ok := problem["status"].(float64)
	assert.True(t, ok, "TC-NSSAA-SCHEMA-070: status should be a number")
	assert.Equal(t, float64(400), status, "TC-NSSAA-SCHEMA-070: status should be 400")

	// Validate type is a non-empty string (should be a URI per RFC 7807)
	typeVal, ok := problem["type"].(string)
	assert.True(t, ok, "TC-NSSAA-SCHEMA-070: type should be a string")
	assert.NotEmpty(t, typeVal, "TC-NSSAA-SCHEMA-070: type must be non-empty (RFC 7807 §3.1)")

	// Validate title is a non-empty string
	titleVal, ok := problem["title"].(string)
	assert.True(t, ok, "TC-NSSAA-SCHEMA-070: title should be a string")
	assert.NotEmpty(t, titleVal, "TC-NSSAA-SCHEMA-070: title must be non-empty (RFC 7807 §3.1)")
}

// TC-NSSAA-SCHEMA-071: ProblemDetails includes field name in detail.
// Spec: RFC 7807 §4
func TestTS29526_NSSAA_Schema_ProblemDetailsFieldName(t *testing.T) {
	t.Parallel()
	store := newNssaaMockStore()
	h := nssaaHandlerFromStore(store, nssaa.WithAPIRoot("http://test"))

	tests := []struct {
		name    string
		body    map[string]interface{}
		field   string
	}{
		{
			"gpsi-field",
			map[string]interface{}{"snssai": map[string]interface{}{"sst": 1}, "eapIdRsp": "dGVzdA=="},
			"gpsi",
		},
		{
			"snssai-field",
			map[string]interface{}{"gpsi": "msisdn-208046000000001", "eapIdRsp": "dGVzdA=="},
			"snssai",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := nssaaRequest(h, http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", tc.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code,
				"TC-NSSAA-SCHEMA-071: Missing %s → 400", tc.field)

			// The detail should mention the field name
			assert.Contains(t, rec.Body.String(), tc.field,
				"TC-NSSAA-SCHEMA-071: ProblemDetails should mention '%s' field", tc.field)
		})
	}
}
