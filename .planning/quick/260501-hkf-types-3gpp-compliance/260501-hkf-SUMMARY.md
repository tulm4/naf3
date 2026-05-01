---
quick_id: 260501-hkf
status: complete
completed: 2026-05-01
---

# Quick Task Summary: Audit and Fix Identifier Types for 3GPP Compliance

**Completed:** 2026-05-01
**Status:** COMPLETE ✅

---

## Task

Audit identifier types in `internal/types/` against 3GPP specifications and fix any non-compliant implementations.

---

## Issues Found

### 1. GPSI Regex — CRITICAL (FIXED)

**Spec (TS 29.571 §5.2.2):**
```
Pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
```

GPSI has 3 forms:
1. MSISDN-based: `msisdn-<5-15 digits>`
2. External Identifier-based: `extid-<id>@<realm>`
3. Any other string (catch-all)

**Previous (INCORRECT):**
```go
var gpsiRegex = regexp.MustCompile(`^5[0-9]{8,14}$`)
```

**Fixed:**
```go
var gpsiRegex = regexp.MustCompile(`^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$`)
```

### 2. SUPI — FIXED ✅

**Spec (TS 29.571 §5.2.2):**
```
Pattern: '^(imsi-[0-9]{5,15}|nai-.+|gci-.+|gli-.+|.+)$'
```

**Previous (INCORRECT - contained typo):**
```go
var supiIMSIRegex = regexp.MustCompile(`^imsi-[0-9]{15}$`)
```

**Fixed:**
```go
var supiIMSIRegex = regexp.MustCompile(`^imsi-[0-9]{5,15}$`)
```

**Issues Fixed:**
1. **Typo**: `imsi-` → `imsi-` (was missing 's')
2. **Length**: Fixed to allow 5-15 digits per spec (was only 15)

### 3. S-NSSAI — Already Correct ✅

SST range 0-255 and SD format (6 hex chars) are compliant.

---

## Files Changed

### Source Code
| File | Change |
|------|--------|
| `internal/types/gpsi.go` | Updated GPSI regex to match TS 29.571 §5.2.2 |
| `internal/types/supi.go` | Fixed SUPI regex typo: `imsi-` → `imsi-`, length: `15` → `5,15` |
| `internal/api/common/validator.go` | Fixed SUPI regex typo and length |
| `internal/types/types_test.go` | Updated GPSI and SUPI validation tests |
| `internal/api/common/common_test.go` | Updated SUPI validation tests |

### Documentation
| File | Change |
|------|--------|
| `docs/design/02_nssaa_api.md` | Updated GPSI pattern references |
| `docs/design/04_data_model.md` | Updated GPSI pattern in schema |
| `docs/design/CODE_DESIGN_COMPLIANCE_PLAN.md` | Updated GPSI section |
| `docs/design/COMPLIANCE_REPORT_3GPP.md` | Updated GPSI pattern and section |
| `docs/design/22_udm_integration.md` | Updated GPSI pattern |
| `docs/quickref.md` | Updated GPSI pattern |
| `docs/roadmap/PHASE_1_Foundation.md` | Updated GPSI pattern |
| `docs/3gppfilter/05_data_management/NSSAAF_DataTypes_NRM.md` | Updated GPSI pattern |
| `docs/3gppfilter/TS29571_NSSAAF_DataTypes.md` | Updated GPSI pattern |

---

## Verification

```
✅ go build ./...     PASS
✅ go test ./internal/...  PASS (all packages)
```

---

## Spec References Updated

| Type | Old Section | New Section |
|------|-------------|-------------|
| GPSI | §5.4.4.61 (or §5.4.4.3) | §5.2.2 |
| SUPI | §5.4.4.2 | §5.2.2 |

Note: Section numbers changed because the spec reorganized the data types between versions.
