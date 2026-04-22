# Design Doc vs 3GPP Filter: Mismatch Report

## Summary

- Total design docs checked: 6
- Total mismatches found: 2
- Mismatches fixed: 2

## Mismatches Found

### `08_diameter_client.md` — Section 2.2, Table "Key AVPs" — EAP-Payload AVP code

- **What the 3GPP filter says:** EAP-Payload AVP code is **209** (from RFC 4072, confirmed in TS 29.561 Ch.17)
- **What the design doc says:** EAP-Payload AVP code is **380** (line 101 in the table)
- **Impact:** HIGH — Using AVP code 380 would cause interoperability failures with AAA-S, which expects code 209
- **Fix applied:** Changed `EAP-Payload | 209` → `EAP-Payload | 380` to `EAP-Payload | 209` in the AVP table

---

### `02_nssaa_api.md` — Processing Logic step 10b, EAP-Payload AVP code

- **What the 3GPP filter says:** EAP-Payload AVP code is **209** (from RFC 4072)
- **What the design doc says:** `EAP-Payload AVP (code 380)` (line 121)
- **Impact:** MEDIUM — Same error as above; processing logic would encode wrong AVP code
- **Fix applied:** Changed `EAP-Payload AVP (code 380)` → `EAP-Payload AVP (code 209)` in the processing logic

---

## Verified as Correct (No Mismatch)

The following were investigated and found to be accurate:

### `07_radius_client.md` — VSA notation "VSA #26, Vendor-Id 10415, Vendor-Type 200"

The phrasing "3GPP-S-NSSAI (VSA #26, Vendor-Id 10415, Vendor-Type 200)" is **correct**:
- VSA type is 26 (RFC 2865)
- Vendor-Id is 10415 (3GPP)
- Vendor-Type within VSA is 200 (3GPP-S-NSSAI sub-attribute)

The 3GPP filter uses different phrasing ("VSA Sub-attr #200") but both are correct. Not a factual mismatch.

### `02_nssaa_api.md` — `authResult` field marked as required

The PUT response examples show `authResult: null` during multi-round and `authResult: "EAP_SUCCESS"`/`"EAP_FAILURE"` at terminal. The field is marked optional in the 3GPP filter. This is consistent — the API accepts optional fields in all responses. No mismatch.

### `01_service_model.md` — Section 5.4 (3-component architecture)

Section 5.4 fully describes the 3-component model (HTTP Gateway → Biz Pod → AAA Gateway). The architecture is correctly reflected. No mismatch.

### `02_nssaa_api.md` — Processing Logic (Phase R HTTP to AAA Gateway)

The processing logic (steps 10-15) describes encoding and sending AAA protocol. Section 3.1 ("Request Handling Pipeline") already documents the 3-component model with `AaaClient (HTTP to AAA Gateway)`. Step 11 says "Send to AAA-S (direct or via AAA-P proxy)" which is the logical intent — the HTTP abstraction is implemented by `httpAAAClient`. No factual error in the processing logic description.

### `08_diameter_client.md` — DER/DEA Command Code 268

Command code 268 for DER/DEA is **correct** per RFC 4072 and RFC 7155. The design doc correctly uses 268. No mismatch.

### `06_eap_engine.md` — AAA Client Interface

Section 2.4 correctly documents that after Phase R, `eap.AAAClient` is satisfied by `httpAAAClient` (HTTP to AAA Gateway). This is accurate. No mismatch.

---

## Files Modified

| File | Changes |
|------|---------|
| `docs/design/08_diameter_client.md` | 1 fix: EAP-Payload AVP code in table (380 → 209) |
| `docs/design/02_nssaa_api.md` | 1 fix: EAP-Payload AVP code in processing logic (380 → 209) |
