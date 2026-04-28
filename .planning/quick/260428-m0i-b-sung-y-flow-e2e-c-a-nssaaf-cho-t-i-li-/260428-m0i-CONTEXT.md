# Quick Task 260428-m0i: bổ sung đầy đủ flow e2e của NSSAAF cho tài liệu @docs/design/24_test_strategy.md (đang thiếu các luồng cho nssaaf_aiw..) - Context

**Gathered:** 2026-04-28
**Status:** Ready for planning

<domain>
## Task Boundary

Add missing AIW (Nnssaaf_AIW) E2E test flows to `docs/design/24_test_strategy.md`. The doc currently has only NSSAA (Nnssaaf_NSSAA) E2E flows in §5.2.

</domain>

<decisions>
## Implementation Decisions

### AIW E2E flow coverage (gray_area_1)
- All AIW E2E flows: happy-path, multi-round, EAP success with MSK+PVS, EAP failure, TTLS, timeout, AAA not configured
- Re-auth and revocation are NSSAA-only — AIW (Nnssaaf_AIW) does NOT support these per AC8 in 03_aiw_api.md
- Coverage must reflect the 3-component model (AUSF Mock → HTTP Gateway → Biz Pod → AAA Gateway → AAA Simulator)

### AIW E2E section placement (gray_area_2)
- New §5.x dedicated section for AIW E2E flows (separate from §5.2 NSSAA flows)
- Do NOT mix AIW and NSSAA flows in the same section

### Claude's Discretion
- Test function naming convention: `TestE2E_AIW_*` matching existing `TestE2E_NSSAA_*` pattern
- Code examples in Go following the same style as existing §5.2 code blocks
- 3GPP conformance references: TS 29.526 §7.3, TS 33.501 §I.2.2.2
- AIW test cases should mirror the 3-component architecture diagram in §5.1

</decisions>

<specifics>
## Specific Ideas

From `docs/design/03_aiw_api.md`:
- AIW uses SUPI (not GPSI): pattern `^imu-[0-9]{15}$`
- Consumer is AUSF (not AMF) — N60 interface
- MSK returned on EAP_SUCCESS (64-byte, base64)
- pvsInfo returned when applicable
- No re-auth/revocation support
- ttlsInnerMethodContainer support
- supportedFeatures: "3GPP-R18-AIW"

From `docs/design/24_test_strategy.md` existing §5.2:
- 3-component model: AUSF Mock, HTTP Gateway, Biz Pod, AAA Gateway, AAA Simulator
- Functions: `EstablishSession`, `TriggerReAuth`, `TriggerRevocation`
- Expected patterns: `StartNSSAAFService`, `StartAMFMock`, `StartAAASimulator`

</specifics>

<canonical_refs>
## Canonical References

- `docs/design/03_aiw_api.md` — AIW API spec (AC1-AC8)
- `docs/design/24_test_strategy.md` — Current test strategy doc (this is what gets edited)
- TS 29.526 §7.3 — Nnssaaf_AIW service operation
- TS 33.501 §I.2.2.2 — SNPN authentication with Credentials Holder

</canonical_refs>
