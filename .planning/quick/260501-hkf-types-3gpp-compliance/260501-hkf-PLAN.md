---
quick_id: 260501-hkf
status: in-progress
---

# Quick Task Plan: Audit and Fix Identifier Types for 3GPP Compliance

**Created:** 2026-05-01
**Status:** In Progress

---

## Task Description

Audit and fix identifier types in `internal/types/` to match 3GPP specifications (TS 29.571 v18.11.0).

---

## Issues Found

### 1. GPSI Regex — CRITICAL (Currently WRONG)

**Spec (TS 29.571 §5.2.2):**
```
Pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
```

GPSI has 3 forms:
- `msisdn-<5-15 digits>` — MSISDN-based GPSI
- `extid-<external-id>@<realm>` — External Identifier-based GPSI
- `.*` — any string (catch-all)

**Current Code (INCORRECT):**
```go
var gpsiRegex = regexp.MustCompile(`^5[0-9]{8,14}$`)
```

**Problem:** This regex is from an older spec version. It only matches `5XXXXXXXX...` format, which is NOT the correct GPSI pattern per TS 29.571 v18.11.0.

### 2. SUPI — PARTIALLY CORRECT

**Spec (TS 29.571 §5.2.2):**
```
Pattern: '^(imsi-[0-9]{5,15}|nai-.+|gci-.+|gli-.+|.+)$'
```

**Current Code:**
```go
var supiIMSIRegex = regexp.MustCompile(`^imsi-[0-9]{5,15}$`)
```

**Issue:** Code only accepts IMSI-based SUPI. The spec allows multiple formats, but for NSSAAF use case, IMSI-based SUPI is acceptable. However, the regex allows 5-15 digits while spec says 5-15. The current `15` is correct for this implementation.

### 3. S-NSSAI — CORRECT ✅

Current implementation is compliant with TS 29.571 §5.4.4.60.

---

## Fix Required

### GPSI Fix

Update `internal/types/gpsi.go`:

```go
// GPSI patterns from TS 29.571 §5.2.2:
// Pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
// GPSI has 3 forms:
// 1. MSISDN-based: "msisdn-" + 5-15 digits
// 2. External Identifier-based: "extid-" + <ext-id> + "@" + <realm>
// 3. Any other string (catch-all for backwards compatibility)
var gpsiRegex = regexp.MustCompile(`^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$`)

// Note: Unlike previous implementations, this regex does NOT require
// a specific prefix for non-MSISDN/non-ExtId GPSIs. The "." pattern
// at the end accepts any string that doesn't match the first two forms.
```

Also update:
- `Normalize()` comment to reflect new pattern
- Error message in `Validate()`

---

## Files to Change

| File | Change |
|------|--------|
| `internal/types/gpsi.go` | Fix GPSI regex and comments |
| `internal/types/types_test.go` | Update GPSI test cases |
| `docs/design/04_data_model.md` | Update GPSI pattern documentation |

---

## Verification

1. GPSI with MSISDN format: `msisdn-1234567890` → Valid
2. GPSI with External Identifier: `extid-user@domain.com` → Valid
3. GPSI with other formats: `any-format-here` → Valid (catch-all)
4. GPSI with old incorrect format: `5123456789` → Invalid (no prefix)
5. Build: `go build ./...`
6. Tests: `go test ./internal/types/...`

---

## Success Criteria

- [ ] GPSI regex matches TS 29.571 §5.2.2 pattern
- [ ] GPSI accepts MSISDN-based format: `msisdn-<5-15 digits>`
- [ ] GPSI accepts External Identifier format: `extid-<id>@<realm>`
- [ ] GPSI accepts any other string (catch-all)
- [ ] All tests pass
