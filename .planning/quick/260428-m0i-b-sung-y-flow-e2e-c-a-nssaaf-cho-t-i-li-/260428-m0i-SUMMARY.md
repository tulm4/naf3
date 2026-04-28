---
status: complete
date: "2026-04-28"
---

# Quick Task 260428-m0i: AIW E2E Flow Coverage in Test Strategy

**Task:** bổ sung đầy đủ flow e2e của NSSAAF cho tài liệu @docs/design/24_test_strategy.md (đang thiếu các luồng cho nssaaf_aiw..)
**Status:** complete
**Date:** 2026-04-28

## Summary

Added `§5.3 AIW E2E Test Cases (N60 / AUSF / SUPI)` to `docs/design/24_test_strategy.md` immediately after `§5.2 E2E Test Cases` (before `## 6. 3GPP Conformance Tests`). The new section covers all Nnssaaf_AIW-specific E2E flows using the 3-component model with AUSF Mock replacing AMF Mock.

## What Changed

### `docs/design/24_test_strategy.md`

- **Added §5.3** with 6 Go E2E test functions:
  - `TestE2E_AIW_BasicFlow` — Full AIW happy-path with EAP-TLS, MSK, PVS
  - `TestE2E_AIW_MSKExtraction` — Verifies MSK is 64 octets, MSK != EMSK (RFC 5216 §2.1.4)
  - `TestE2E_AIW_EAPFailure` — HTTP 200 with `authResult=EAP_FAILURE` in body (not HTTP 403)
  - `TestE2E_AIW_InvalidSupi` — HTTP 400 with `cause=INVALID_SUPI`
  - `TestE2E_AIW_AAA_NotConfigured` — HTTP 404 with `cause=AAA_SERVER_NOT_CONFIGURED`
  - `TestE2E_AIW_TTLS` — EAP-TTLS with `ttlsInnerMethodContainer`, multi-round, MSK+PVS
- **Added 13 conformance test stubs** (AIW-01 through AIW-13) mapped to TS 29.526 §7.3, RFC 5216 §2.1.4, TS 33.501 §I.2.2.2
- Re-auth and revocation explicitly excluded (AIW-12, AIW-13 confirm NSSAA-only scope)

## Decisions Made

| Decision | Resolution |
|----------|-----------|
| AIW E2E flow coverage | All 6 flows documented; timeout implicit in BasicFlow multi-round loop |
| Section placement | New §5.3 (separate from §5.2 NSSAA flows) |
| Naming convention | `TestE2E_AIW_*` matching existing `TestE2E_NSSAA_*` pattern |
| AIW-12/AIW-13 | Confirm AIW excludes re-auth/revocation (AC8 in 03_aiw_api.md) |

## Verification

```
grep -c "TestE2E_AIW_" docs/design/24_test_strategy.md  → 6
grep -c "AIW-[0-9]" docs/design/24_test_strategy.md      → 13
```

All grep counts match plan targets.
