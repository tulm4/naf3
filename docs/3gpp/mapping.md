# 3GPP Specification Mapping for NSSAAF Project

**Project Context:** 5G NSSAAF (Network Slice-Specific Authentication and Authorization Function)
**Target Release:** Release 17/18 (Standard Compliance)
**Subscriber Scale:** 10M+ (Telecom Grade)

---

## 1. Core Service Operations (Nnssaaf Interface)
*Use these for API Definition, JSON Schemas, and HTTP/2 Binding.*

| Spec Number | Title | Key Relevance for NSSAAF |
| :--- | :--- | :--- |
| **TS 29.526** | NSSAAF Services; Stage 3 | **Primary Spec.** Defines Nnssaaf_NSSAA_Authenticate and Revocation services. |
| **TS 29.500** | Technical Realization of SBA | Principles for HTTP/2, Serialization (JSON), and **Overload Control (OCI)**. |
| **TS 29.501** | Principles and Guidelines for SBI Design | Design patterns for Error Handling (ProblemDetails) and API versioning. |

## 2. Authentication Procedures & Security (EAP/TLS)
*Use these for State Machine logic and Key Management.*

| Spec Number | Title | Key Relevance for NSSAAF |
| :--- | :--- | :--- |
| **TS 33.501** | Security Architecture & Procedures | **Critical.** Details EAP-TLS, EAP-AKA' flows, and MSK (Master Session Key) handling. |
| **TS 23.502** | Procedures for the 5G System | Section 4.2.9: Detailed sequence diagrams for NSSAA during Registration. |

## 3. Interworking & External Interfaces (N58/N60)
*Use these for Radius/Diameter mapping and AAA Server connectivity.*

| Spec Number | Title | Key Relevance for NSSAAF |
| :--- | :--- | :--- |
| **TS 29.561** | Interworking with External DN | Details on N58 (Diameter) and N60 (Radius) attribute mapping. |
| **TS 23.003** | Numbering, Addressing & Identification | Formats for SUPI, GPSI, and S-NSSAI (SST/SD) used in AAA signaling. |
| **RFC 3748** | Extensible Authentication Protocol | Base EAP protocol logic and packet formats. |

## 4. NF Management & Discovery (NRF)
*Use these for Service Registration and Health Monitoring.*

| Spec Number | Title | Key Relevance for NSSAAF |
| :--- | :--- | :--- |
| **TS 29.510** | Network Function Repository Services | Nnrf_NFManagement for NSSAAF Registration, Heartbeat, and Discovery. |
| **TS 23.501** | System Architecture for 5GS | Section 6.2.x: Role of NRF and SCP in Service-Based Communication. |

## 5. Performance & Reliability (Carrier-Grade)
*Use these for scaling, metrics, and high-availability design.*

| Spec Number | Title | Key Relevance for NSSAAF |
| :--- | :--- | :--- |
| **TS 28.532** | Generic Management Services | Standardized Performance Counters (TPS, Success Rate, Latency). |
| **TS 28.541** | 5G Network Resource Model (NRM) | Definition of IOCs (Information Object Classes) for NSSAAF managed objects. |

---

## Instructions for AI Agent (Cursor/Copilot)
1. **API Implementation:** Refer to `TS 29.526` for URI paths and `TS 29.500` for Header requirements.
2. **Logic Flow:** Always validate against `TS 23.502` (Procedure) and `TS 33.501` (Security).
3. **Data Mapping:** Use `TS 29.561` when converting SBI JSON to Diameter/Radius AVPs.
