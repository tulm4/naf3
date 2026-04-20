# NSSAAF Domain Ecosystem Research

**Project:** 5G NSSAAF (Network Slice-Specific Authentication and Authorization Function)
**Researched:** April 2026
**Confidence:** HIGH (grounded in 3GPP specs + implementation docs + web research)

---

## Executive Summary

NSSAAF is a 3GPP-defined Network Function introduced in Release 16 that enables network slice-specific authentication and authorization in 5G. It bridges the gap between the operator's primary authentication (5G-AKA/EAP-AKA') and enterprise or vertical-specific authentication requirements managed by third-party AAA servers. NSSAAF acts as an EAP relay — translating between AMF's SBI (HTTP/2) and AAA servers' RADIUS/Diameter protocols — while maintaining zero knowledge of subscriber credentials.

The domain sits at the intersection of telecom infrastructure (3GPP), identity management (EAP/RADIUS/Diameter), and enterprise networking. Its ecosystem includes commercial vendors (Ericsson, Nokia, Cisco), open-source projects (free5GC, Open5GS), and a rich ecosystem of EAP method specifications (EAP-TLS, EAP-AKA', EAP-TTLS) from IETF. Real-world deployment is nascent — even the leading open-source 5GC (free5GC) notes "free5GC currently does not support this functionality" as of early 2026 — making this implementation timely and pioneering.

---

## 1. What is NSSAAF?

### 1.1 The Role in 5G Architecture

NSSAAF (Network Slice-Specific Authentication and Authorization Function) is defined in **3GPP TS 23.501 §5.15.10** and **TS 33.501 §16.3**. It is triggered when a UE registers with a network slice (S-NSSAI) that requires additional authentication beyond the standard 5G primary authentication.

**Standard 5G authentication** (5G-AKA or EAP-AKA') authenticates the subscriber identity (SUPI) against the operator's home network. It is a mandatory, PLMN-wide procedure.

**NSSAA** is optional and slice-specific. It allows:
- Third-party enterprises or verticals (factories, hospitals, utilities) to authenticate their own devices using their own credentials
- Operators to delegate slice admission control to slice tenants
- S-NSSAI-scoped re-authentication and revocation at any time by the AAA server

TS 23.501 §5.15.10 explicitly states:

> "A serving PLMN or SNPN shall perform Network Slice-Specific Authentication and Authorization for the S-NSSAIs of the HPLMN or SNPN which are subject to it based on subscription information."

### 1.2 How NSSAAF Differs from Standard 5G AKA

| Aspect | 5G Primary Authentication (5G-AKA/EAP-AKA') | NSSAA |
|--------|---------------------------------------------|-------|
| **Authenticator** | AUSF + UDM | AAA-S (external) |
| **Identity** | SUPI/SUCI (3GPP identity) | GPSI + enterprise credential |
| **Credentials** | Operator-issued SIM/USIM keys | Vertical-defined (certificates, passwords, tokens) |
| **Scope** | PLMN-wide | Per S-NSSAI |
| **Protocol** | 5G AKA (internal) | EAP (relayed through AMF → NSSAAF → AAA-S) |
| **Revocation** | Not supported mid-session | AAA-S can revoke at any time |
| **Re-auth** | Not supported mid-session | AAA-S can trigger re-auth at any time |
| **Specification** | TS 33.501 §6 | TS 33.501 §16.3 |

### 1.3 The Problem NSSAAF Solves

5G network slicing enables a single physical network to host multiple logical networks (slices), each tailored for different use cases: eMBB (enhanced Mobile Broadband), URLLC (Ultra-Reliable Low-Latency), and mMTC (massive Machine-Type Communications). Each slice may belong to a different enterprise tenant.

**The authorization gap:** Standard 5G authentication proves "this device belongs to this operator." But it does not answer "should this device be allowed into *this particular enterprise slice*?" Enterprise slices may have their own access policies — device type, security clearance, subscription tier, time-of-day restrictions — that the operator cannot or should not manage.

**NSSAAF fills this gap** by providing a standards-based bridge to external AAA infrastructure that enterprises already operate. The AAA server (AAA-S) makes the authorization decision; NSSAAF just relays the EAP conversation between AMF and AAA-S.

### 1.4 Specification Foundation

| Spec | Version | Relevance |
|------|---------|-----------|
| TS 23.501 | v18.10.0 | Network slice architecture, NSSAA role |
| TS 23.502 | v18.4.0 | §4.2.9.2 AMF-triggered flow, §4.2.9.3 reauth, §4.2.9.4 revocation |
| TS 29.526 | v18.7.0 | Nnssaaf_NSSAA and Nnssaaf_AIW service APIs |
| TS 33.501 | v18.10.0 | §5.13 responsibilities, §16.3-16.5 procedures, Annex B.2 EAP-TLS |
| TS 29.561 | v18.5.0 | Ch.16 RADIUS interworking, Ch.17 Diameter interworking |
| TS 29.571 | v18.2.0 | §5.4.4.60 NssaaStatus data type |
| TS 28.541 | v18.3.0 | §5.3.145 NSSAAF NRM (managed object class) |

**3GPP Release History:**
- Release 15 (2018): Network slicing introduced, but no NSSAA
- Release 16 (2020): NSSAA added — the core NSSAAF specification
- Release 17 (2022): Enhanced NSAC (Network Slice Admission Control), AF authorization
- Release 18 (2024-2026): Further slicing enhancements, study items for home network provisioning of slices

---

## 2. Ecosystem Context

### 2.1 NSSAAF in the 5G Service-Based Architecture

NSSAAF participates in the 5G Service-Based Architecture (SBA) as a producer of two NF services, consumed by AMF and AUSF:

```
5G SBA Service-Based Architecture (simplified)

NRF ←── Nnrf ────────────────────────────────┐
                                             │
AMF ←── Namf ──────────────────────────────┐ │
    ↑                                     │ │
    │ N58: Nnssaaf_NSSAA_Authenticate    │ │
    │ (SBI HTTP/2, OAuth2 Bearer token)    │ │
    │                                     │ │
NSSAAF                                    │ │
    ↑                                     │ │
    │ N60: Nnssaaf_AIW_Authenticate      │ │
    │ (AUSF consumer)                     │ │
    │                                     │ │
AUSF ─────────────────────────────────────┘ │
    │                                         │
    │ N59: Nudm_UECM_Get                    │
    │ (AMF ID lookup by GPSI)               │
    ▼                                         │
UDM ◄─────────────────────────────────────────┘

AAA-S ←── RADIUS/Diameter ────────────── NSSAAF
(enterprise AAA server)                  (AAA protocol relay)
```

**Key interfaces:**
- **N58** (AMF → NSSAAF): `Nnssaaf_NSSAA_Authenticate` — creates and updates slice authentication context
- **N60** (AUSF → NSSAAF): `Nnssaaf_AIW_Authenticate` — SNPN credential holder authentication (uses SUPI, not GPSI)
- **N59** (NSSAAF → UDM): `Nudm_UECM_Get` — discovers serving AMF for reauth/revocation notifications
- **Nnrf** (NSSAAF → NRF): NF registration, heartbeat, service discovery

### 2.2 Which Network Functions Interact with NSSAAF

| NF | Direction | Interface | Role |
|----|----------|----------|------|
| **AMF** | ↔ NSSAAF | N58 (SBI) | EAP Authenticator. Initiates NSSAA, relays EAP between UE and NSSAAF |
| **AUSF** | ↔ NSSAAF | N60 (SBI) | Consumer of Nnssaaf_AIW for SNPN authentication |
| **UDM** | ← NSSAAF | N59 (SBI) | GPSI-to-AMF lookup for AAA-S-triggered notifications |
| **NRF** | ← NSSAAF | Nnrf | Registration, heartbeat, NF discovery |
| **AAA-S** | ↔ NSSAAF | RADIUS/Diameter | Makes authorization decisions |
| **AAA-P** | ↔ NSSAAF | RADIUS/Diameter | Optional proxy for third-party AAA-S routing |
| **UE** | ↔ AMF | NAS (EAP) | Endpoints of the EAP exchange (NSSAAF never talks to UE directly) |

### 2.3 The 3-Component Production Architecture

Production Kubernetes deployments use a **3-component model** (documented in the naf3 implementation):

```
Operator Network
  │
  ├── AMF/AUSF ── HTTPS/443 ──► HTTP Gateway (N replicas)
  │                                     │
  │                                     │ HTTP/ClusterIP
  │                                     ▼
  │                          Business Logic Pods (N replicas)
  │                          (EAP engine, session state)
  │                                     │
  │                                     │ HTTP/9090
  │                                     ▼
  │                          AAA Gateway (2 replicas: active-standby)
  │                          (keepalived VIP via Multus CNI)
  │                                     │
  │                                     │ RADIUS UDP :1812 / Diameter TCP :3868
  │                                     ▼
  └──                                  AAA-S Server
```

**The source-IP problem** motivates this architecture: RADIUS and Diameter require a single stable source IP for shared-secret validation and connection authorization. In a multi-pod Kubernetes deployment, each pod has an ephemeral IP, so RADIUS/Diameter clients cannot live inside NSSAAF app pods. The AAA Gateway provides the stable VIP.

### 2.4 GPSI: The Required Subscriber Identifier

GPSI (Generic Public Subscriber Identifier, pattern `^5[0-9]{8,14}$`) is **mandatory** for NSSAA. TS 23.502 §4.2.9.1 states:

> "The NSSAA procedure requires a GPSI."

If a UE has multiple GPSIs in subscription, the AMF may use any one for NSSAA. The AAA-S stores the GPSI ↔ EAP identity mapping to enable future reauth and revocation.

---

## 3. EAP Methods in NSSAAF

### 3.1 EAP Framework Basics

All NSSAA authentication uses **EAP (Extensible Authentication Protocol)** as defined in **IETF RFC 3748**. NSSAAF does not implement EAP methods — it only relays EAP packets between AMF and AAA-S.

**EAP packet format (RFC 3748):**
```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Code      |       ID        |            Length             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Data                                |
+-+-+-+-+-+-+-+-+                                               |
```

- **Code:** 1=Request, 2=Response, 3=Success, 4=Failure
- **ID:** Sequence number for matching Request/Response pairs
- **Length:** Total packet length
- **Type:** Present in Request/Response (1 byte, e.g., 13=EAP-TLS, 23=EAP-AKA', 21=EAP-TTLS)

### 3.2 EAP-TLS (Type 13, RFC 5216)

**Primary method for enterprise slices.**

EAP-TLS uses TLS (RFC 5246) as the EAP method. Both client (UE) and server (AAA-S) hold X.509 certificates. The TLS handshake performs mutual authentication and derives the Master Session Key (MSK).

**MSK derivation (RFC 5216 §2.1.4):**
```
MSK = TLS-Exporter("EAP-TLS MSK", 64)
```

The MSK is used by AMF for NAS security between AMF and UE for the authenticated slice.

**EAP-TLS Flags (within Type-Data):**
| Flag | Value | Meaning |
|------|-------|---------|
| START | 0x80 | Initiate TLS handshake |
| MORE_FRAGS | 0x40 | More fragments follow |
| LENGTH | 0x20 | Length field present |
| RESERVED | 0x1F | Reserved bits |

**PKI requirements for EAP-TLS:**
- Root CA certificates distributed to all UEs (via MDM, SIM, or remote provisioning per TS 23.501 §5.39)
- AAA-S server certificate: CN/SAN must match AAA-S FQDN
- Client certificates: issued by the enterprise's PKI, EKU = Client Authentication (1.3.6.1.5.5.7.3.2)
- Short-lived certificates recommended (days to weeks, not months)
- OCSP/CRL endpoints required for revocation checking
- TLS 1.3 preferred; TLS 1.2 acceptable with TLS session resumption for roaming performance

**Certificate lifecycle management** is the operational challenge. As noted by WBA (Wireless Broadband Alliance, April 2026):

> "EAP-TLS is the most secure method in the Passpoint set, with one significant limitation: under TLS 1.2, client certificate details can be observed by access network providers and hub operators brokering the authentication exchange. Using TLS 1.3 eliminates that exposure."

### 3.3 EAP-AKA' (Type 50, RFC 9048)

**3GPP-native method, evolved from 4G AKA.**

EAP-AKA' (RFC 9048, updated from RFC 5448) uses the AKA' algorithm already present in USIM cards. It is the natural choice when the slice tenant wants to leverage existing SIM-based credentials.

**Key difference from EAP-AKA (legacy):**
- Uses SHA-256 / HMAC-SHA-256 instead of SHA-1 / HMAC-SHA-1
- Type code: 0x32 (not 0x17)
- Key derivation binds to both the access network and home network identity, reducing key portability across contexts

As analyzed by P1Sec:

> "EAP AKA was widely deployed for authentication in 3GPP adjacent access cases, especially WiFi interworking. It worked, but it had a structural weakness: it did not bind keys and session context as tightly as modern threat models expect. AKA' was introduced to harden binding and make derived keys less portable across contexts."

**AKN (Authentication Key Vector) provisioning:** The home network's UDM/ARPF generates AKA' authentication vectors. These are provisioned to the AAA-S via a proprietary interface (out of 3GPP scope). The AAA-S uses the AKA' algorithm to challenge the UE's USIM.

### 3.4 EAP-TTLS (Type 21, RFC 5281)

Tunneled TLS method. The client first establishes a TLS tunnel to the AAA-S, then authenticates inside the tunnel using a legacy method (PAP, CHAP, MSCHAPv2). Useful for:
- Enterprise scenarios with legacy username/password credentials
- Transition from existing RADIUS infrastructure
- Reducing client certificate management burden

The outer TLS tunnel provides server authentication; the inner method provides client authentication.

### 3.5 Comparison of EAP Methods for NSSAA

| Method | Security | Client Certs | Key Material | Best For | Operational Complexity |
|--------|----------|-------------|--------------|----------|----------------------|
| EAP-TLS | Highest (mutual cert auth) | Required | MSK from TLS | Enterprise slices, zero-trust | High (PKI required) |
| EAP-AKA' | High (SIM-based) | Not required | CK'/IK' from AKA' | Operator-familiar, SIM-based verticals | Medium (AV provisioning) |
| EAP-TTLS | Medium (tunnel + inner method) | Server cert only | MSK from TLS tunnel | Legacy credentials,过渡 | Medium |
| EAP-SIM | Low-Medium | Not required | SRES-based | Legacy SIM, not recommended for 5G | Low |

---

## 4. AAA Protocol Landscape

### 4.1 Why Two AAA Protocols?

NSSAAF supports both RADIUS and Diameter because different operators and verticals have existing AAA infrastructure:

- **RADIUS** (RFC 2865 + RFC 3579): Ubiquitous in enterprise Wi-Fi, ISP authentication, and legacy 3GPP AAA. UDP-based, simple, widely understood.
- **Diameter** (RFC 6733 + RFC 4072): Designed for carrier-grade AAA. Stateful, reliable (SCTP multi-streaming), extensible via AVPs. Required for many 3GPP interfaces (Gx, Gy, S6a, etc.).

### 4.2 RADIUS Profile (TS 29.561 Ch.16)

**Protocol stack:**
```
EAP (from AMF) → NSSAAF RADIUS Client → RADIUS Access-Request (UDP/1812) → AAA-S
```

**Key attributes for NSSAA:**

| RADIUS Attribute | Code | Usage |
|-----------------|------|-------|
| User-Name | 1 | GPSI |
| Calling-Station-Id | 31 | GPSI |
| NAS-IP-Address | 4 | NSSAAF IP |
| NAS-Identifier | 32 | NSSAAF identifier |
| Service-Type | 6 | 10 (Framed) |
| NAS-Port-Type | 61 | 19 (Virtual) |
| EAP-Message | 79 | EAP payload (RFC 3579, may be fragmented) |
| Message-Authenticator | 80 | HMAC-MD5 integrity (RFC 3579 §3.2) |
| 3GPP-S-NSSAI | 26/200 | VSA: SST (1 byte) + SD (3 bytes optional) |

**3GPP-S-NSSAI VSA format (TS 29.561 §16.3.2):**
```
Octets:  1         2        3        4-6
         Type=26  Length  Vendor=10415  VendorType=200  SST   SD(optional)
```

**Message flow:**
```
AMF → NSSAAF: Nnssaaf_NSSAA_Authenticate (POST)
NSSAAF → AAA-S: RADIUS Access-Request (GPSI, S-NSSAI VSA, EAP-Message)
AAA-S → NSSAAF: RADIUS Access-Challenge (EAP challenge, State)
NSSAAF → AMF: Nnssaaf_NSSAA_Authenticate Response (EAP message)
... multi-round ...
AAA-S → NSSAAF: RADIUS Access-Accept (EAP-Success) or Access-Reject (EAP-Failure)
NSSAAF → AMF: Final response with authResult
```

**Revocation via RADIUS:** AAA-S sends **Disconnect-Request** (RFC 5176) to NSSAAF. NSSAAF validates, looks up AMF via Nudm_UECM_Get, sends RevocationNotification to AMF, and responds with Disconnect-ACK.

**Transport security:**
- RFC 2865 RADIUS: insecure (MD5, plaintext in places)
- RADIUS/TLS (RFC 6613): TLS-wrapped RADIUS over TCP
- RADIUS/DTLS (RFC 4818): DTLS-wrapped RADIUS over UDP
- For untrusted networks: VPN or DTLS required

### 4.3 Diameter Profile (TS 29.561 Ch.17)

**Protocol stack:**
```
EAP (from AMF) → NSSAAF Diameter Client → Diameter-EAP-Request (TCP/SCTP/3868) → AAA-S
```

**Key AVPs:**

| Diameter AVP | Code | Type | M/O | Usage |
|-------------|------|------|-----|-------|
| User-Name | 1 | OctetString | M | GPSI |
| Calling-Station-Id | 31 | OctetString | M | GPSI |
| EAP-Payload | 209 | OctetString | M | EAP message |
| 3GPP-S-NSSAI | 310 | Grouped | M | SST + SD (vendor 10415) |
| Auth-Application-Id | 258 | Unsigned32 | M | 5 (Diameter EAP) |
| Auth-Request-Type | 274 | Enumerated | M | 1 (AUTHORIZE_AUTHENTICATE) |
| Auth-Session-State | 277 | Enumerated | M | 1 (NO_STATE_MAINTAINED) |
| Session-Id | 263 | UTF8String | M | Unique per authentication |

**3GPP-S-NSSAI AVP (code 310, vendor 10415):**
```
3GPP-S-NSSAI ::= <AVP Header: 310, Vendor: 10415>
                 { Slice/Service Type }  (AVP 259, 1 byte)
                 [ Slice Differentiator ]  (AVP 260, 3 bytes)
                 [ Mapped HPLMN SNSSAI ]
```

**CER/CEA handshake:** On connection establishment, NSSAAF and AAA-S exchange Capabilities-Exchange-Request/Answer advertising:
- Auth-Application-Id: 1 (NASREQ) and 5 (Diameter EAP)
- Vendor-Id: 10415 (3GPP)
- Supported-Vendor-Id: 10415

**Transport:**
- SCTP (preferred): multi-streaming, multi-homing, no head-of-line blocking
- TCP (fallback): simpler, widely supported

**Diameter watchdog:** DWR/DWA messages every 30s to detect connection failures.

### 4.4 RADIUS vs Diameter Trade-offs

| Aspect | RADIUS | Diameter |
|--------|--------|---------|
| Transport | UDP, TCP, DTLS | SCTP, TCP |
| Reliability | Best-effort with retransmit | Stateful, CER/CEA, DWR/DWA |
| Scalability | Moderate | High (hierarchical, proxy chains) |
| 3GPP adoption | Enterprise Wi-Fi, legacy | All 3GPP interfaces |
| AAA-S availability | Ubiquitous in enterprise | Carriers, modern AAA platforms |
| NSSAAF complexity | Custom implementation | go-diameter library + custom AVPs |
| AAA-S configuration | Per-S-NSSAI shared secret | Per-S-NSSAI host/realm |

---

## 5. Industry Players & Implementations

### 5.1 Open-Source Projects

#### free5GC (Linux Foundation)

The most mature open-source 5GC, written in Go. Version 23 (March 2026) targets 3GPP Release 17.

**Status:** As of the free5GC blog (December 2024):

> "Note that free5GC currently does not support this functionality [NSSAA]. For more detailed information about the NSSAA procedure and NSSAAF service, you can refer to TS 23.502."

This confirms that free5GC implements the 5G core NFs (AMF, SMF, AUSF, UDM, NRF, UDR, PCF, NSSF, etc.) but **NSSAAF is not yet implemented** as of early 2026. The naf3 project is therefore pioneering open-source NSSAAF implementation for the Go ecosystem.

#### Open5GS

C-based 5GC implementation. Also does not include NSSAAF as of early 2026.

#### OAI (OpenAirInterface)

Primarily RAN-oriented, some core components.

#### Serverless5GC (Academic, 2026)

Recent academic work (arXiv:2603.27618, March 2026) demonstrates serverless 5GC function decomposition achieving **median registration latency of 406–522 ms**, comparable to C-based Open5GS (403–606 ms). This validates the viability of microservice/NF decomposition approaches for 5G NFs, including NSSAAF.

### 5.2 Commercial Implementations

#### Ericsson

Ericsson provides cloud-native 5G Core with full network slicing support including NSSAA. Their 5G Core product line (as evidenced by Three Sweden's commercial 5G SA launch, December 2025) implements the full 3GPP stack. As a 3GPP primary contributor, Ericsson's NSSAAF implementation would be the reference implementation against which others are measured.

#### Nokia

Nokia's 5G Core includes NSSAA support, documented in their network slicing white paper:

> "Rel-16 enhanced the 4G interworking for network slices... It also introduced the network slice-specific authentication and authorization (NSSAA) feature to enable user ID authentication and authorization using authentication, authorization and accounting (AAA) servers, which could even be deployed in the network slice tenant's domain."

Nokia's Core as a Service (SaaS) offering (announced April 2026, deployed with Citymesh) demonstrates the cloud-native deployment model for 5G NFs including NSSAAF.

#### Cisco (Ultra Cloud Core SMF)

Cisco's SMF includes RADIUS client and Diameter endpoint capabilities for secondary authentication. Their configuration guides show:

- RADIUS Client: GPSI-based user identification, Framed-Service-Type, NAS-IP-Address from VIP
- Diameter Endpoint: Gx/Gy clients for policy and charging, peer management with DWR/DWA watchdog
- Max outstanding RADIUS requests: 2 million per cluster
- Dead-time configuration for failed servers

#### F5 Networks

F5 provides Diameter Firewall products for mobile operators, compliant with GSMA-FS-19 Diameter Security requirements. These sit between NSSAAF and AAA-S in roaming scenarios, providing:
- Protocol conformance checking (AVP validation per 3GPP specs)
- DDoS protection for Diameter interfaces
- Rate limiting and traffic filtering

### 5.3 AAA Server Vendors

Real-world NSSAAF deployments connect to enterprise AAA infrastructure:

- **Cisco ISE (Identity Services Engine):** Supports EAP-TLS, EAP-TTLS, RADIUS. Cisco specifically mentions ISE integration for "Cisco private 5G" secondary authentication.
- **FreeRADIUS:** Open-source, widely used in enterprise Wi-Fi. Custom VSA development needed for 3GPP-S-NSSAI.
- **Keyfactor, Smallstep:** PKI/certificate management for EAP-TLS client certificates.
- **Diameter-based AAA:** Operators typically use carrier-grade Diameter AAA (e.g., Nokia's AAA, or custom developments) for Diameter-based NSSAA.

### 5.4 Standards Bodies

| Body | Role |
|------|------|
| **3GPP SA3** | Security architecture (TS 33.501), NSSAA procedures (TS 23.502 §4.2.9) |
| **3GPP SA5** | OAM, NRM definitions (TS 28.541 §5.3.145) |
| **IETF EAP WG** | RFC 3748 (EAP), RFC 5216 (EAP-TLS), RFC 5281 (EAP-TTLS), RFC 9048 (EAP-AKA') |
| **IETF RADIUSEXT WG** | RFC 2865 (RADIUS), RFC 3579 (EAP), RFC 4818 (DTLS), RFC 5176 (Disconnect) |
| **IETF DIME WG** | RFC 6733 (Diameter base), RFC 4072 (Diameter EAP) |
| **GSMA** | FS.19 Diameter Security, network slicing security guidelines |
| **WBA (Wireless Broadband Alliance)** | Passpoint, EAP method certification, Wi-Fi roaming security |
| **ETSI** | Hosts 3GPP specifications (e.g., TS 133.501 v18.6.0) |

---

## 6. Regulatory & Compliance Context

### 6.1 Security Standards

**TS 33.501 (5G Security Architecture):**
- §5.13: NSSAAF responsibilities — relay EAP, translate protocols, support reauth/revocation
- §16.3: NSSAA EAP procedure requirements
- §16.4: AAA-S triggered reauth security considerations
- §16.5: Revocation security considerations
- Annex B.2: EAP-TLS requirements for NSSAA

**Key security requirements:**
1. **EAP framework:** RFC 3748 mandatory. AMF acts as EAP Authenticator.
2. **Privacy:** Recommend privacy-protecting EAP method if EAP identity must be protected.
3. **GPSI:** Mandatory for NSSAA. Stored by AAA-S for reauth/revocation.
4. **GPSI ↔ EAP ID mapping:** AAA-S stores to enable future revocation/reauth.
5. **S-NSSAI routing:** NSSAAF routes to AAA-S based on S-NSSAI (local config).
6. **Third-party AAA-S:** Via AAA-P proxy, NSSAAF and AAA-P may be co-located.

### 6.2 PKI Requirements for EAP-TLS

EAP-TLS deployments require:

1. **Root CA:** Enterprise or slice-tenant CA. Distributed to UEs via:
   - MDM (Intune, Jamf, etc.)
   - SIM card provisioning (for some deployments)
   - Remote provisioning (TS 23.501 §5.39)

2. **Server certificates:** AAA-S FQDN in CN/SAN. Short validity (days to weeks). OCSP must be available.

3. **Client certificates:** EKU = Client Authentication (1.3.6.1.5.5.7.3.2). Hardware-backed keys (TPM, Secure Enclave) when supported. ACME or SCEP for enrollment.

4. **Certificate lifecycle automation:** Non-negotiable at scale. As noted by enterprise PKI vendors:
   - Manual certificate management "leaves gaps when device posture, identity, or risk changes"
   - Short-lived certificates (days) require automated renewal
   - OCSP/CRL must be reachable from authentication context

5. **Post-quantum considerations:** WBA (April 2026) notes:
   > "EAP-TLS version 1.3 and EAP-AKA can be enhanced through IETF work to support quantum-resistant Key Encapsulation Mechanisms."

### 6.3 GSMA Requirements

**FS.19 (Diameter Security):** Mobile operators should implement Diameter firewalls compliant with GSMA FS.19, which defines:
- Diameter message validation rules
- AVP conformance checking per 3GPP specs
- Rate limiting and connection management
- Attack detection (flooding, spoofing, reflection)

**Network Slicing Security (ongoing work):**
- Rel-15: Management security, UE authorization, slice NF authorization
- Rel-16: NSSAA
- Rel-17: AF authorization with confidentiality protection
- Rel-18 (ongoing): Home network provisioning of slices, NSAC security procedures (TR 33.886)

### 6.4 Data Protection Considerations

GPSI must be treated as personal data under GDPR and equivalent regulations. NSSAAF implementations must:
- Hash GPSI in audit logs (not store plaintext)
- Encrypt session state in PostgreSQL and Redis
- Implement GPSI access controls consistent with data minimization principles
- Consider that RADIUS/Diameter attribute logging can leak subscriber identifiers

As noted by P1Sec regarding AKA':
> "You can have robust AKA' authentication and still leak subscriber identifiers, session attributes, or correlation material through logs, RADIUS attributes, or analytics exports."

---

## 7. Failure Modes & Operational Concerns

### 7.1 Timeout Scenarios

EAP authentication is inherently multi-round. Timeouts can occur at multiple points:

| Scenario | Cause | Impact | Mitigation |
|----------|-------|--------|------------|
| EAP round timeout (30s default) | UE response delayed | NSSAA status → NOT_EXECUTED, retry next registration | AMF retries with same EAP message, NSSAAF returns cached response |
| AAA-S response timeout (10s) | AAA-S overloaded or unreachable | 504 Gateway Timeout to AMF | Circuit breaker, retry with backoff (1s, 2s, 4s), max 3 retries |
| Nudm_UECM_Get timeout | UDM unavailable during reauth/revocation | Cannot notify AMF | Procedure stops; AAA-S notified of failure |
| AMF notification timeout | AMF unreachable | Reauth/revocation fails silently | Log, alert, retry via NRF re-discovery |

**NSSAAF timeout behavior:** Per TS 33.501 §16.3:
> "If NSSAA cannot be completed (server error, UE unreachable): AMF sets status of corresponding S-NSSAI as requiring NSSAA in UE Context. NSSAA re-executes next time UE requests registration with that S-NSSAI."

### 7.2 AAA-S Unavailability

**Circuit breaker design (per S-NSSAI):**
- 5 consecutive failures → OPEN (block requests)
- 30s recovery timeout → HALF_OPEN (allow probe requests)
- 3 probe successes → CLOSED (resume normal operation)

**Fallback strategy:**
1. Primary AAA-S → Secondary AAA-S (per S-NSSAI config)
2. RADIUS → Diameter (if both protocols configured)
3. Degraded mode: reject new requests, serve cached results for in-flight sessions
4. Full outage: NSSAA status = NOT_EXECUTED, AMF retries on next registration

**Multi-AAA-S topology:**
- Per-S-NSSAI AAA-S address configuration (3-level fallback: exact S-NSSAI match → SST-only → default)
- Multiple AAA-S for same slice → load balancing across shared secret pool

### 7.3 GPSI/SUPI Lookup Failures

**GPSI not found (HTTP 404):**
- AMF sent GPSI not in UDM subscription
- NSSAAF cannot route to correct AAA-S
- Response: ProblemDetails with cause = "USER_NOT_FOUND"

**AMF ID lookup failure (Nudm_UECM_Get):**
- GPSI not registered to any AMF
- UE may be in CM-IDLE state
- For reauth: AAA-S notified via immediate ACK; NSSAA retried when UE connects
- For revocation: AMF not notified; slice access continues until next registration

### 7.4 Operational Monitoring

**Key metrics to track:**

| Category | Metrics |
|----------|---------|
| **Volume** | Requests/sec, active EAP sessions, completed auths/sec |
| **Latency** | P99 end-to-end (<500ms target), AAA-S round-trip (<20ms target) |
| **Reliability** | Success rate, failure rate by cause (timeout, reject, protocol error) |
| **AAA-S health** | Circuit breaker state per AAA-S, response time per AAA-S |
| **Session** | Active session count, session setup rate, max concurrent |
| **Infrastructure** | DB write latency (PostgreSQL P99 <5ms), cache hit ratio (>95%) |

**Health check endpoints:**
- `/healthz/live`: Container alive
- `/healthz/ready`: DB + Redis + NRF reachable
- `/healthz/startup`: Initialization complete

**Prometheus metrics recommended:**
- `nssaa_eap_rounds_total{result="success|failure"}`
- `nssaa_aaa_response_time_seconds{aaa_server, protocol}`
- `nssaa_active_sessions{snssai}`
- `nssaa_circuit_breaker_state{aaa_server}`
- `nssaa_reauth_notifications_total`
- `nssaa_revocation_notifications_total`

### 7.5 Common Production Issues

1. **Shared secret mismatch:** RADIUS shared secret misconfiguration → Message-Authenticator validation fails, all requests rejected. Detection: look for "invalid authenticator" logs.

2. **GPSI format errors:** AMF sends GPSI not matching `^5[0-9]{8,14}$` → 400 Bad Request. Often caused by test UEs or misconfigured AMF.

3. **AAA-S certificate expiry:** EAP-TLS with expired server cert → handshake fails, all slice auths for that AAA-S fail. Mitigation: automated cert renewal monitoring.

4. **Redis session cache miss:** Session state evicted before completion → NSSAA restarts from beginning. Caused by TTL misconfiguration or Redis memory pressure.

5. **AAA Gateway VIP flapping:** keepalived misconfiguration → VIP moves between replicas, RADIUS source IP changes, AAA-S rejects (shared secret by IP). Mitigation: strict keepalived configuration, L2 adjacency requirement.

6. **Database replication lag:** PostgreSQL synchronous replica lag → write latency increases. Mitigation: async replication to warm standby, RPO=0 not achievable with async.

---

## 8. What "Good" Looks Like

### 8.1 Performance Benchmarks

Based on 3GPP requirements and industry data:

| Metric | Target | Rationale |
|--------|--------|-----------|
| Concurrent EAP sessions | 50,000 / instance | Per naf3 design; accommodates burst registrations |
| Requests per second | 10,000 / instance (SBI), 50,000 / sec (RADIUS) | Carrier-grade signaling throughput |
| P99 end-to-end latency | <500ms | AMF → NSSAAF → AAA-S → NSSAAF → AMF |
| P99 NSSAAF processing | <20ms | Excluding AAA-S round-trip |
| RADIUS transaction rate | >50,000 / sec | UDP-based, low overhead |
| Session setup rate | >5,000 / sec | New NSSAA session/s per instance |
| Database write latency | <5ms P99 | PostgreSQL with connection pooling |
| Cache hit ratio | >95% | Redis for session state |
| Availability | 99.999% (5 nines) | Carrier-grade per AZ |
| MTTR | <30s | Automatic failover |
| RPO | 0 seconds | Synchronous replication |

**Industry reference:** Serverless5GC research (2026) demonstrates median registration latency of 406–522ms for full 5G registration procedures. NSSAA adds additional round-trips (EAP exchange + AAA protocol), making the <500ms target ambitious for multi-round EAP methods.

### 8.2 Reliability Requirements

**5-nines availability architecture:**
- Multi-AZ deployment (minimum 3 AZs)
- Stateless application pods (session state in PostgreSQL + Redis)
- Patroni PostgreSQL HA (leader + sync replica + async replica)
- Redis Cluster (3 shards × 2 replicas)
- HPA scaling: 3-50 replicas, scale on CPU + active sessions
- Pod Disruption Budget: max unavailable = 1

**Chaos engineering tests:**
- Pod kill → session survives in Redis/PostgreSQL, any pod handles
- AZ failure → 1/3 capacity lost, HPA scales up
- DB leader failure → Patroni promotes sync replica, <30s
- Redis master failure → Cluster promotes replica, <10s
- AAA-S failure → circuit breaker opens, fallback or degraded mode

### 8.3 Operational Best Practices

1. **Per-S-NSSAI AAA isolation:** Configure separate AAA-S endpoints per slice. This prevents a misbehaving AAA-S for one slice from affecting others.

2. **Rate limiting:** Per-GPSI (10/min), per-AMF (1000/sec), global (100K/sec). Prevents runaway authentication storms.

3. **EAP session limits:** Max 20 rounds per session. Prevents infinite loops and resource exhaustion.

4. **Idempotent retry handling:** Same EAP message + nonce → return cached response. AMF may retry with identical messages on timeout.

5. **NRF heartbeat:** Every 5 minutes, updating load = active_sessions / capacity. Enables NRF-based load balancing.

6. **Graceful shutdown:** Stop accepting requests → drain in-flight (<30s) → deregister from NRF → exit. Prevents orphaned sessions.

7. **Audit logging:** GPSI hashed, timestamps UTC, session outcome logged. For compliance and debugging.

8. **AAA protocol selection by capability:** NSSAAF selects RADIUS or Diameter based on AAA-S capability (local config). Both may be supported simultaneously.

---

## 9. Architecture Patterns

### 9.1 The EAP Relay Pattern

NSSAAF's core function is protocol translation:

```
UE ←── NAS EAP ──→ AMF ←── SBI HTTP/2 ──→ NSSAAF ←── RADIUS/Diameter ──→ AAA-S
              (NAS Security)           (OAuth2)          (shared secret/TLS)
```

Key insight: **NSSAAF never sees subscriber credentials.** It only forwards opaque EAP bytes between AMF and AAA-S. The EAP method (certificate, SIM challenge, password) is entirely between UE and AAA-S.

### 9.2 Session State Management

```
┌─────────────────────────────────────────┐
│            EAP Session State              │
│                                          │
│  authCtxId: "nssaa-auth-01fr5xg..."   │
│  state: PENDING → EAP_EXCHANGE → DONE   │
│  method: EAP-TLS                       │
│  rounds: 3 / 20                        │
│  expectedId: 7                          │
│  methodState: EapTlsState {...}        │
│                                          │
│  CreatedAt: 2026-04-21T10:00:00Z     │
│  LastActivity: 2026-04-21T10:00:15Z   │
└─────────────────────────────────────────┘
         │                    │
         ▼                    ▼
  ┌──────────────┐    ┌──────────────┐
  │  PostgreSQL  │    │    Redis     │
  │  (primary)  │    │   (cache)    │
  │  Permanent  │    │   Hot path   │
  └──────────────┘    └──────────────┘
```

- **Redis:** Hot path reads/writes. TTL = session timeout (5 minutes).
- **PostgreSQL:** Permanent record. Monthly partitions for audit compliance.
- **Write-through:** Every state update writes to both stores.
- **Read-through:** Check Redis first; on miss, load from PostgreSQL.

### 9.3 State Machine (NssaaStatus)

```
NOT_EXECUTED ───(AMF triggers POST /slice-auth)───► PENDING
                                                       │
                         ┌─────────────────────────────┤
                         │                             │
                         ▼                             ▼
                  ┌──────────────┐             ┌──────────────┐
                  │  EAP_SUCCESS  │             │  EAP_FAILURE  │
                  └──────────────┘             └──────────────┘
                         │
          ┌──────────────┴──────────────┐
          │                               │
          ▼                               ▼
   ┌──────────────────┐         ┌──────────────────┐
   │ AAA-S Reauth     │         │ AAA-S Revoke     │
   │ (SLICE_RE_AUTH)  │         │ (SLICE_REVOCATION)│
   └──────────────────┘         └──────────────────┘
          │                               │
          ▼                               ▼
      PENDING                      removed from Allowed NSSAI
```

### 9.4 Anti-Patterns

1. **Embedding EAP logic in NSSAAF:** NSSAAF should relay EAP, not interpret it. AAA-S owns the method semantics.

2. **Shared RADIUS source IP across pods:** Every pod must use the AAA Gateway VIP. NSSAAF app pods must NOT send RADIUS directly.

3. **Skipping Nudm_UECM_Get:** Always look up AMF before reauth/revocation notifications. Otherwise, notifications go to the wrong AMF or are lost.

4. **Hardcoding GPSI format:** Validate against `^5[0-9]{8,14}$`. Non-conforming GPSIs indicate AMF bugs or test data.

5. **Ignoring circuit breaker state:** If a AAA-S is in OPEN state, don't attempt requests until HALF_OPEN probe.

6. **Storing plaintext GPSI in logs:** Hash before logging. GPSI is personally identifiable.

7. **Scaling AAA Gateway beyond 2 replicas:** Diameter requires single-active connection. Only 2 replicas (active-standby) are valid.

---

## 10. Research Gaps & Open Questions

1. **Commercial NSSAAF implementations:** Ericsson and Nokia likely have NSSAAF, but no public implementation details. Deeper vendor research would inform feature parity targets.

2. **AAA-S AV provisioning for AKA':** How operators provision AKA' authentication vectors from UDM to AAA-S is proprietary and undocumented in 3GPP specs.

3. **EAP-TLS in 5G UE:** Which 5G UEs support EAP-TLS for NSSAA? TS 23.501 §5.39 mentions remote credential provisioning, but UE certification for EAP-TLS NSSAA is not publicly documented.

4. **Post-quantum EAP:** IETF work on quantum-resistant EAP methods is nascent. Timeline for PQ-safe NSSAA is unclear.

5. **Multi-operator NSSAA roaming:** How NSSAA works when a UE roams to a visited PLMN but the slice belongs to the home PLMN requires further study.

---

## Sources

- 3GPP TS 23.501 v18.10.0 — System architecture for the 5G System
- 3GPP TS 23.502 v18.4.0 — Procedures for the 5G System
- 3GPP TS 29.526 v18.7.0 — NSSAAF API specifications
- 3GPP TS 33.501 v18.10.0 — Security architecture and procedures
- 3GPP TS 29.561 v18.5.0 — RADIUS/Diameter interworking
- 3GPP TS 29.571 v18.2.0 — Common data types
- 3GPP TS 28.541 v18.3.0 — NSSAAF NRM
- IETF RFC 3748 — EAP (Extensible Authentication Protocol)
- IETF RFC 5216 — EAP-TLS
- IETF RFC 9048 — EAP-AKA'
- IETF RFC 5281 — EAP-TTLS
- IETF RFC 2865 — RADIUS
- IETF RFC 3579 — RADIUS EAP extension
- IETF RFC 6733 — Diameter Base Protocol
- IETF RFC 4072 — Diameter EAP Application
- free5GC blog (Dec 2024) — 5G Network Slicing documentation
- free5GC GitHub — Open source 5GC implementation
- arXiv:2603.27618 (Mar 2026) — Serverless5GC performance analysis
- 3GPP.org — Network Slicing Security technology overview
- Nokia white paper — 5G-Advanced Release 18 slicing
- Cisco UCC SMF Configuration Guide (2025.02) — RADIUS and Diameter configuration
- WBA (Apr 2026) — Wi-Fi roaming security practices
- P1Sec — EAP AKA Prime analysis
- Smallstep — Certificate-based Wi-Fi authentication with EAP-TLS
- F5 Networks — Diameter Firewall for mobile operators
