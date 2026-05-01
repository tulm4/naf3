---
quick_id: 260501-hke
status: in-progress
---

# Quick Task Plan: Audit and Fix Non-3GPP-Compliant Constants

**Created:** 2026-05-01
**Status:** In Progress

---

## Task Description

Audit the entire codebase for non-3GPP-compliant constant values (e.g., `NotificationTypeReAuth = "reauth"` → `NotificationTypeSliceReAuth = "SLICE_RE_AUTH"`).

---

## Verification Baseline (3GPP Spec)

| Type | Correct Value | Spec Reference |
|------|--------------|----------------|
| NotificationType (Re-Auth) | `"SLICE_RE_AUTH"` | TS 29.526 §7.2.4 |
| NotificationType (Revocation) | `"SLICE_REVOCATION"` | TS 29.526 §7.2.4 |
| NssaaStatus | `"NOT_EXECUTED"`, `"PENDING"`, `"EAP_SUCCESS"`, `"EAP_FAILURE"` | TS 29.571 §5.4.4.60 |
| AuthStatus | Same as NssaaStatus | TS 29.571 §5.4.4.60 |

---

## Task 1: Fix Planning Artifacts

### Files to Update

| File | Issue | Fix |
|------|-------|-----|
| `.planning/phases/04-NFIntegration_Observability/04-PLAN.md` | `"reauth"` / `"revocation"` | → `"SLICE_RE_AUTH"` / `"SLICE_REVOCATION"` |
| `.planning/phases/04-NFIntegration_Observability/04-PATTERNS.md` | `"reauth" \| "revocation"` | → `"SLICE_RE_AUTH" \| "SLICE_REVOCATION"` |

### Changes

1. **04-PLAN.md** (lines ~1644-1645):
   - `NotificationTypeReAuth = "reauth"` → `NotificationTypeSliceReAuth = "SLICE_RE_AUTH"`
   - `NotificationTypeRevocation = "revocation"` → `NotificationTypeSliceRevoc = "SLICE_REVOCATION"`

2. **04-PATTERNS.md** (line ~280):
   - `"reauth" | "revocation"` → `"SLICE_RE_AUTH" | "SLICE_REVOCATION"`

---

## Verification

1. Run: `grep -r '"reauth"\|"revocation"' .planning/`
2. Run: `grep -r '"pending"\|"not_executed"\|"eap_success"' --include="*.go" . | grep -v "_NOT_EXECUTED\|_PENDING\|_EAP_SUCCESS"`

Expected: No matches for non-compliant values in code.

---

## Success Criteria

- [ ] All non-compliant NotificationType values in planning artifacts fixed
- [ ] Documentation comments updated to reference correct 3GPP spec values
- [ ] Code compiles without errors
- [ ] Existing tests pass

---

## Files Changed

- `.planning/phases/04-NFIntegration_Observability/04-PLAN.md`
- `.planning/phases/04-NFIntegration_Observability/04-PATTERNS.md`
