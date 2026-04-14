# 3GPP NSSAAF Source Mapping

Bảng ánh xạ các file đã filter với source documents gốc.

## Tổng hợp các nguồn 3GPP cho NSSAAF

### Source Files → Filtered Files Mapping

| Source File | Spec Number | Filtered File | NSSAAF Sections |
|-----------|-------------|---------------|-----------------|
| TS29526_Nnssaaf_NSSAA.yaml | TS 29.526 | TS29526_Nnssaaf_NSSAA.yaml | Full API definition |
| TS29526_Nnssaaf_AIW.yaml | TS 29.526 | TS29526_Nnssaaf_AIW.yaml | Full API definition |
| 33501-ia0.md | TS 33.501 | TS33501_NSSAAF_Services.md | §5.13, §14.4, §16.3-16.5 |
| 29561-i50.md | TS 29.561 | TS29561_NSSAAA_Interworking.md | Ch.16 (RADIUS), Ch.17 (Diameter) |
| 23502-id0.md | TS 23.502 | TS23502_NSSAA_Procedures.md | §4.2.9.1-4.2.9.4 |
| 28541-ie0.md | TS 28.541 | TS28541_NSSAAF_NRM.md | §5.3.145-5.3.148 |
| 29571-ib0.md | TS 29.571 | TS29571_NSSAAF_DataTypes.md | §5.4.4.60-5.4.4.61 |
| TS29571_CommonData.yaml | TS 29.571 | TS29571_CommonData.yaml | Full YAML schemas |

### NSSAAF Interface Summary

#### SBI Interfaces (Service Based Interface)

| Interface | From | To | Spec | Service Operations |
|-----------|------|-----|------|-------------------|
| N58 | AMF | NSSAAF | TS 29.526 | Nnssaaf_NSSAA_Authenticate, Re-AuthNotification, RevocationNotification |
| N60 | AUSF | NSSAAF | TS 29.526 | Nnssaaf_AIW_Authenticate |

#### AAA Protocol Interfaces

| Interface | From | To | Protocol | Spec |
|-----------|------|-----|---------|------|
| N58 | NSSAAF | NSS-AAA | RADIUS | TS 29.561 Ch.16 |
| N58 | NSSAAF | NSS-AAA | Diameter | TS 29.561 Ch.17 |

#### Management Interfaces

| Interface | From | To | Protocol | Spec |
|-----------|------|-----|---------|------|
| Nnrf | NSSAAF | NRF | HTTP/2 | TS 29.510 |
| Nudm | NSSAAF | UDM | HTTP/2 | TS 29.503 |

### Key Specifications by Area

#### 1. Core API (Nnssaaf)
- **Primary:** TS 29.526 (NSSAAF Services; Stage 3)
- **Reference:** TS 29.500 (SBA Technical Realization)
- **Reference:** TS 29.501 (SBI Design Principles)

#### 2. Procedures
- **Primary:** TS 23.502 §4.2.9 (NSSAA Procedures)
- **Reference:** TS 23.501 §5.15.10, §6.3.17 (Architecture)

#### 3. Security
- **Primary:** TS 33.501 §5.13, §14.4, §16.3-16.5
- **EAP Methods:** RFC 3748 (EAP), RFC 5216 (EAP-TLS), RFC 4187 (EAP-AKA')

#### 4. AAA Interworking
- **RADIUS:** TS 29.561 Ch.16, RFC 2865, RFC 3579
- **Diameter:** TS 29.561 Ch.17, RFC 4072, RFC 7155

#### 5. Management & Monitoring
- **NRM:** TS 28.541 §5.3.145-5.3.148
- **Common Data:** TS 29.571 §5.4.4.60-5.4.4.61

### File Locations

```
docs/3gpp/                          # Source documents
├── TS29526_Nnssaaf_NSSAA.yaml
├── TS29526_Nnssaaf_AIW.yaml
├── 33501-ia0.md                    # TS 33.501
├── 29561-i50.md                   # TS 29.561
├── 23502-id0.md                   # TS 23.502
├── 28541-ie0.md                   # TS 28.541
├── 29571-ib0.md                   # TS 29.571
└── TS29571_CommonData.yaml

docs/3gppfilter/                    # Filtered NSSAAF documents
├── README.md
├── TS29526_Nnssaaf_NSSAA.yaml
├── TS29526_Nnssaaf_AIW.yaml
├── TS33501_NSSAAF_Services.md
├── TS29561_NSSAAA_Interworking.md
├── TS23502_NSSAA_Procedures.md
├── TS28541_NSSAAF_NRM.md
├── TS29571_NSSAAF_DataTypes.md
├── TS29571_CommonData.yaml
└── SOURCE_MAPPING.md              # This file
```

### Implementation Priority

1. **High Priority (Must Have)**
   - TS29526_Nnssaaf_NSSAA.yaml - Core API
   - TS23502_NSSAA_Procedures.md - Flow logic
   - TS33501_NSSAAF_Services.md - Security requirements

2. **Medium Priority (Should Have)**
   - TS29561_NSSAAA_Interworking.md - AAA connectivity
   - TS29571_NSSAAF_DataTypes.md - Data structures

3. **Lower Priority (Nice to Have)**
   - TS28541_NSSAAF_NRM.md - O&M integration
   - TS29571_CommonData.yaml - Full schema reference

### Version History

| Filtered File | Source Version | Last Updated |
|-------------|----------------|--------------|
| TS29526_Nnssaaf_NSSAA.yaml | v18.7.0 | 2025-07 |
| TS29526_Nnssaaf_AIW.yaml | v18.7.0 | 2025-07 |
| TS33501_NSSAAF_Services.md | v18.10.0 | 2025-07 |
| TS29561_NSSAAA_Interworking.md | v18.5.0 | 2025-03 |
| TS23502_NSSAA_Procedures.md | v18.4.0 | 2025-03 |
| TS28541_NSSAAF_NRM.md | v18.3.0 | 2025-03 |
| TS29571_NSSAAF_DataTypes.md | v18.2.0 | 2025-03 |
| TS29571_CommonData.yaml | v18.2.0 | 2025-03 |
