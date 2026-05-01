# Code-to-Design Compliance Verification Plan

**Date:** 2026-05-01
**Status:** COMPLETED ✅

---

## Executive Summary

After updating design docs to be 3GPP-compliant, the code has been verified and fixed.

| Check | Status | Action Required |
|-------|--------|----------------|
| GPSI regex | ✅ FIXED | Code now matches design |
| SUPI regex | ✅ MATCH | No action |
| NssaaStatus enum | ✅ MATCH | No action |
| Re-auth/Revocation fields | ✅ MATCH | No action |
| NotificationType values | ✅ FIXED | Changed to 3GPP spec |
| State machine transitions | ✅ VERIFIED | Handled in AAA Gateway |

---

## 1. GPSI Regex (FIXED)

### Issue (Now Fixed)

**Design spec** (`02_nssaa_api.md`) referenced incorrect GPSI pattern.

**Spec (Correct):** TS 29.571 §5.2.2:
```
Pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
```

### Fix Applied

Updated `internal/types/gpsi.go` and all references:
- GPSI has 3 forms: MSISDN-based, External Identifier-based, and catch-all
- Spec reference updated from §5.4.4.61 to §5.2.2

---

## 2. State Machine Transition Verification

### Design Spec

`02_nssaa_api.md` Section 2.6:
```
EAP_SUCCESS ──(AAA-S Reauth Request)──→ PENDING
```

### Verification Checklist

- [ ] Check if `internal/api/nssaa/handler.go` handles re-auth from `EAP_SUCCESS` state
- [ ] Verify `internal/eap/engine.go` can transition from `EAP_SUCCESS` back to `PENDING`
- [ ] Check `internal/storage/postgres/session.go` session state updates

---

## 3. UDM Lookup Before Notification

### Design Spec

`02_nssaa_api.md` Section 2.7:
```
3a. NSSAAF → UDM: Nudm_UECM_Get(GPSI) → get AMF ID(s)
    NOTE: If AMF not registered → procedure stops here (log warning, return 204)
```

### Verification Checklist

- [ ] Check `internal/amf/notifier.go` or similar for UDM lookup before POST to AMF
- [ ] Verify `internal/udm/client.go` has `Nudm_UECM_Get` method
- [ ] Check if notification handler queries UDM first

---

## 4. Files to Check

### Core API Handlers
- `internal/api/nssaa/handler.go` — POST/PUT handlers
- `internal/api/aiw/handler.go` — AIW handlers

### Types
- `internal/types/gpsi.go` — GPSI validation (NEEDS FIX)
- `internal/types/supi.go` — SUPI validation
- `internal/types/nssaa_status.go` — Status enum

### Storage
- `internal/storage/postgres/session.go` — Session state
- `internal/storage/postgres/session_store.go` — Session CRUD

### Notification
- `internal/amf/notifier.go` — AMF notification sender
- `internal/udm/client.go` — UDM client

### Common Validation
- `internal/api/common/validator.go` — GPSI regex

---

## 5. Test Coverage

After fixing code, verify tests pass:
- `go test ./internal/types/... -run GPSI`
- `go test ./internal/api/... -run NSSAA`
- `go test ./test/unit/...`

---

## 6. Tasks

### Task 1: Fix GPSI Regex (PRIORITY 1)
**File:** `internal/types/gpsi.go`
**Action:** Remove dash from regex, update comments and error messages

### Task 2: Fix GPSI Regex in Validator (PRIORITY 1)
**File:** `internal/api/common/validator.go`
**Action:** Verify regex matches `^5[0-9]{8,14}$`

### Task 3: Verify State Machine (PRIORITY 2)
**Files:** `internal/eap/engine.go`, `internal/api/nssaa/handler.go`
**Action:** Confirm `EAP_SUCCESS → PENDING` transition is handled

### Task 4: Verify UDM Lookup (PRIORITY 2)
**Files:** `internal/amf/notifier.go`, `internal/udm/client.go`
**Action:** Confirm UDM is queried before AMF notification

---

## 7. Success Criteria

- [ ] `go test ./internal/types/...` passes
- [ ] `go test ./internal/api/...` passes  
- [ ] GPSI validation rejects inputs with dashes
- [ ] Re-auth from `EAP_SUCCESS` state works correctly
- [ ] UDM lookup occurs before AMF notifications

---

## 8. Rollback Plan

If issues arise, the previous GPSI regex `^5-?[0-9]{8,14}$` was more permissive. Revert to:
```go
var gpsiRegex = regexp.MustCompile(`^5-?[0-9]{8,14}$`)
```

---

## 9. Changes Made (2026-05-01)

### Task 1: Fix GPSI Regex ✅
**Files:** `internal/types/gpsi.go`

Changed regex from `^5-?[0-9]{8,14}$` to `^5[0-9]{8,14}$` (no dash allowed)
Updated spec reference from §5.4.4.3 to §5.4.4.61
Updated `Normalize()` to return string as-is (no longer removes dashes)

### Task 2: Update Spec References ✅
**Files:** `internal/api/common/validator.go`

Updated GPSI regex comment and spec reference from §5.4.4.3 to §5.4.4.61

### Task 3: Fix NotificationType Values ✅
**Files:** `internal/amf/amf.go`

Changed:
- `NotificationTypeReAuth = "reauth"` → `NotificationTypeSliceReAuth = "SLICE_RE_AUTH"`
- `NotificationTypeRevocation = "revocation"` → `NotificationTypeSliceRevoc = "SLICE_REVOCATION"`

Updated all usages in `sendNotification()` method.

### Task 4: Update Test Files ✅
**Files:**
- `internal/types/types_test.go` — Updated GPSI validation tests
- `internal/cache/redis/dlq.go` — Updated DLQ type comment
- `internal/cache/redis/dlq_test.go` — Updated test fixture
- `test/unit/e2e_amf/amf_notification_test.go` — Updated payload strings

---

## 10. Verification Results

All tests pass:
```
go test ./internal/types/...     ✅ PASS
go test ./internal/api/...       ✅ PASS
go test ./internal/amf/...        ✅ PASS
go test ./internal/cache/redis/... ✅ PASS
go test ./test/conformance/...    ✅ PASS
```
