# 3GPP NSSAAF Filtered Documentation

Thư mục này chứa các phần trích xuất từ tài liệu 3GPP liên quan đến NSSAAF (Network Slice-Specific Authentication and Authorization Function).

## Cấu trúc thư mục (MỚI — dùng cho design)

```
docs/3gppfilter/
├── INDEX.md                              # Knowledge index — ĐỌC TRƯỚC TIÊN
├── README.md                              # File hướng dẫn này
├── SOURCE_MAPPING.md                      # Ánh xạ source → filtered files
│
├── 01_api_specs/
│   └── NSSAA_API_operations.md           # TS 29.526 — API operations & schemas
│
├── 02_procedures/
│   └── NSSAA_flow_AMF.md                 # TS 23.502 — Tất cả NSSAA flows
│
├── 03_security/
│   └── NSSAAF_services.md                # TS 33.501 — Security & services
│
├── 04_protocols/
│   └── AAA_interworking.md               # TS 29.561 — RADIUS/Diameter
│
└── 05_data_management/
    └── NSSAAF_DataTypes_NRM.md          # TS 29.571 + TS 28.541

# Reference files (không đọc trực tiếp — dùng chunk ở trên):
├── TS29526_Nnssaaf_NSSAA.yaml            # TS 29.526 v18.7.0 — Full OpenAPI
├── TS29526_Nnssaaf_AIW.yaml              # TS 29.526 v18.7.0 — Full OpenAPI
└── TS29571_CommonData.yaml               # TS 29.571 v18.2.0 — Shared schemas (6740 dòng)
```

## Cách sử dụng (QUAN TRỌNG)

### Nguyên tắc: Không bao giờ đọc tất cả file cùng lúc

**Mỗi chunk nhỏ hơn 200 dòng** → Cursor đọc trong 1 lần gọi, không overflow context.

### Luồng làm việc cho design document

1. **Đọc INDEX.md** — xác định file cần đọc cho câu hỏi cụ thể của bạn
2. **Đọc đúng chunk** — theo decision tree trong INDEX.md
3. **Dùng design template** — `docs/design/nssAAF_design_template.md`
4. **Validate** — dùng checklist trong Cursor rules

### Decision Tree nhanh

```
API endpoint / Schema?          → 01_api_specs/NSSAA_API_operations.md
AMF-triggered NSSAA flow?      → 02_procedures/NSSAA_flow_AMF.md
AAA-S triggered reauth?         → 02_procedures/NSSAA_flow_AMF.md
AAA-S triggered revocation?     → 02_procedures/NSSAA_flow_AMF.md
Security requirements / EAP?    → 03_security/NSSAAF_services.md
RADIUS / Diameter protocol?    → 04_protocols/AAA_interworking.md
Data types (NssaaStatus)?       → 05_data_management/NSSAAF_DataTypes_NRM.md
NRM / Management?              → 05_data_management/NSSAAF_DataTypes_NRM.md
```

## Cursor Rules

Khi làm việc với NSSAAF, Cursor sẽ tự động có 2 rules:

- **nssAAF-design-guide** — Chiến lược đọc tài liệu + decision tree
- **nssAAF-design-doc-standard** — Tiêu chuẩn viết design document

## Spec versions

| Spec | Version |
|------|---------|
| TS 23.502 | v18.4.0 |
| TS 29.526 | v18.7.0 |
| TS 29.561 | v18.5.0 |
| TS 29.571 | v18.2.0 |
| TS 28.541 | v18.3.0 |
| TS 33.501 | v18.10.0 |

Release: 3GPP Release 18

## License

Các file này được trích xuất từ tài liệu 3GPP với mục đích reference và implementation.
