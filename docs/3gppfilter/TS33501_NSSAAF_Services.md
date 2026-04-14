# 3GPP TS 33.501 - NSSAAF Security Requirements & Services
## Extracted from: 3GPP TS 33.501 V18.10.0 (2025-07)

**Source:** 3GPP TS 33.501 - Security architecture and procedures for 5G system (Release 18)

---

## 5.13 Requirements on NSSAAF

The Network slice specific and SNPN authentication and authorization function (NSSAAF) shall handle the Network Slice Specific Authentication requests from the serving AMF as specified in clause 16. The NSSAAF shall also support functionality for access to SNPN using credentials from Credentials Holder using AAA Server as specified in clause I.2.2.2.

The NSSAAF is responsible to send the NSSAA requests to the appropriate AAA-S.

The NSSAAF shall support AAA-S triggered Network Slice-Specific Re-authentication and Re-authorization and Slice-Specific Authorization Revocation and translate any AAA protocol into a Service Based format.

NSSAAF shall translate the Service based messages from the serving AMF or AUSF to AAA protocols towards AAA-P/AAA-S.

---

## 14.4 Services provided by NSSAAF

### 14.4.1 Nnssaaf_NSSAA services

#### 14.4.1.1 General

The following table illustrates the security related services for Network Slice Specific Authentication and Authorisation that NSSAAF provides.

**Table 14.4.1.1-1: NF services for the NSSAA service provided by NSSAAF**

| Service Name | Service Operations | Operation Semantics | Example Consumer(s) |
|--------------|-------------------|-------------------|---------------------|
| Nnssaaf_NSSAA | Authenticate | Request/Response | AMF |
|              | Re-AuthenticationNotification | Notify | AMF |
|              | RevocationNotification | Notify | AMF |

#### 14.4.1.2 Nnssaaf_NSSAA_Authenticate service operation

**Service operation name:** Nnssaaf_NSSAA_Authenticate

**Description:** NF consumer requires the NSSAAF to relay Network Slice specific authentication messages towards the corresponding AAA-S handling the Network Slice specific authentication for the requested S-NSSAI (see clause 16).

**Input, Required:**

1. In the initial NSSAA requests: EAP ID Response, GPSI, S-NSSAI
2. In subsequent NSSAA requests: EAP message, GPSI, S-NSSAI

**Input, Optional:** None

**Output, Required:** EAP message, GPSI, S-NSSAI

**Output, Optional:** None

#### 14.4.1.3 Nnssaaf_NSSAA_Re-AuthenticationNotification service operation

**Service operation name:** Nnssaaf_NSSAA_Re-AuthenticationNotification

**Description:** NSSAAF notifies the NF consumer to trigger a Network Slice specific reauthentication procedure for a given UE and S-NSSAI.

**NOTE:** The AMF is implicitly subscribed to receive Nnssaaf_NSSAA_Re-authenticationNotification service operation.

**Input, Required:** GPSI, S-NSSAI

**Input, Optional:** None

**Output, Required:** None

**Output, Optional:** None

#### 14.4.1.4 Nnssaaf_NSSAA_RevocationNotification service operation

**Service operation name:** Nnssaaf_NSSAA_RevocationNotification

**Description:** NSSAAF notifies the NF consumer to trigger a Network Slice specific revocation procedure for a given UE and S-NSSAI.

**NOTE:** The AMF is implicitly subscribed to receive Nnssaaf_NSSAA_RevocationNotification service operation.

**Input, Required:** GPSI, S-NSSAI

**Input, Optional:** None

**Output, Required:** None

**Output, Optional:** None

### 14.4.2 Nnssaaf_AIW services

#### 14.4.2.1 General

The following table illustrates the security related services provided by the NSSAAF for primary authentication in SNPN with Credentials holder using AAA server (see clause I.2.2.2).

**Table 14.4.2.1-1: NF services for CH using AAA for primary authentication provided by NSSAAF**

| Service Name | Service Operations | Operation Semantics | Example Consumer(s) |
|--------------|-------------------|-------------------|---------------------|
| Nnssaaf_AIW | Authenticate | Request/Response | AUSF |

---

## 16.3 Network slice specific authentication and authorization

This clause specifies the optional-to-use NSSAA between a UE and an AAA server (AAA-S) which may be owned by an external 3rd party enterprise. NSSAA uses a User ID and credentials, different from the 3GPP subscription credentials (e.g. SUPI and credentials used for PLMN access) and takes place after the primary authentication.

The EAP framework specified in RFC 3748 [27] shall be used for NSSAA between the UE and the AAA server. The SEAF/AMF shall perform the role of the EAP Authenticator and communicates with the AAA-S via the NSSAAF. The NSSAAF undertakes any AAA protocol interworking with the AAA-S. Multiple EAP methods are possible for NSSAA. If the AAA-S belongs to a third party the NSSAAF contacts the AAA-S via a AAA-P. The NSSAAF and the AAA-P may be co-located.

To protect privacy of the EAP ID used for the EAP based NSSAA, a privacy-protection capable EAP method is recommended, if privacy protection is required.

### NSSAA Procedure (Figure 16.3-1)

The steps involved in NSSAA are described below:

1. For S-NSSAIs that are requiring NSSAA, based on change of subscription information, or triggered by the AAA-S, the AMF may trigger the start of the NSSAA procedure.

   If NSSAA is triggered as a result of Registration procedure, the AMF may determine, based on UE Context in the AMF, that for some or all S-NSSAI(s) subject to NSSAA, the UE has already been authenticated following a Registration procedure on a first access. Depending on NSSAA result (e.g. success/failure) from the previous Registration, the AMF may decide, based on Network policies, to skip NSSAA for these S-NSSAIs during the Registration on a second access.

   If the NSSAA procedure corresponds to a re-authentication and re-authorization procedure triggered as a result of AAA Server-triggered UE re-authentication and re-authorization for one or more S-NSSAIs, as described in clause 16.4, or triggered by the AMF based on operator policy or a subscription change and if S-NSSAIs that are requiring Network Slice-Specific Authentication and Authorization are included in the Allowed NSSAI for each Access Type, the AMF selects an Access Type to be used to perform the NSSAA procedure based on network policies.

2. The AMF may request the UE User ID for EAP authentication (EAP ID) for the S-NSSAI in a NAS MM Transport message including the S-NSSAI.

3. The UE provides the EAP ID for the S-NSSAI alongside the S-NSSAI in an NAS MM Transport message towards the AMF.

4. The AMF sends the EAP ID to the NSSAAF which provides interface with the AAA, in an Nnssaaf_NSSAA_Authenticate Request (EAP ID Response, GPSI, S-NSSAI).

5. If the AAA-P is present (e.g. because the AAA-S belongs to a third party and the operator deploys a proxy towards third parties), the NSSAAF forwards the EAP ID Response message to the AAA-P, otherwise the NSSAAF forwards the message directly to the AAA-S. NSSAAF routes to the AAA-S based on the S-NSSAI. The NSSAAF/AAA-P forwards the EAP Identity message to the AAA-S together with S-NSSAI and GPSI. The AAA-S stores the GPSI to create an association with the EAP ID in the EAP ID response message so the AAA-S can later use it to revoke authorisation or to trigger reauthentication. The AAA-S uses the EAP-ID and S-NSSAI to identify for which UE and slice authorisation is requested.

   **NOTE:** If the AAA-S belongs to the 3rd party, the NSSAAF optionally maps the S-NSSAI to External Network Slice Information (ENSI), and forwards the EAP Identity message to the AAA-S together with ENSI and GPSI. In this case, the AAA-S uses the EAP-ID and ENSI to identify the UE for which slice authorisation is requested.

6-11. EAP-messages are exchanged with the UE. One or more than one iterations of these steps may occur.

12. EAP authentication completes. An EAP-Success/Failure message is delivered to the NSSAAF/AAA-P along with GPSI and S-NSSAI/ENSI.

13. The NSSAAF sends the Nnssaaf_NSSAA_Authenticate Response (EAP-Success/Failure, S-NSSAI, GPSI) to the AMF.

14. The AMF transmits a NAS MM Transport message (EAP-Success/Failure) to the UE.

15. Based on the result of Slice specific authentication (EAP-Success/Failure), if a new Allowed NSSAI or new Rejected NSSAIs needs to be delivered to the UE, or if the AMF re-allocation is required, the AMF initiates the UE Configuration Update procedure, for each Access Type, as described in clause 4.2.4.2 of TS 23.502 [8].

    If the NSSAA procedure cannot be completed (e.g. due to server error or UE becoming unreachable), the AMF sets the status of the corresponding S-NSSAI subject to Network Slice-Specific Authentication and Authorization in the UE context as defined in TS 29.526 [96], so that an NSSAA is executed next time the UE requests to register with the S-NSSAI.

---

## 16.4 AAA Server triggered Network Slice-Specific Re-authentication and Re-authorization procedure

### Procedure (Figure 16.4-1)

0. The UE is registered in 5GC via an AMF. The AMF ID is stored in the UDM.

1. The AAA-S requests the re-authentication and re-authorization for the Network Slice specified by the S-NSSAI/ENSI in the Re-Auth Request message, for the UE identified by the GPSI in this message. This message is sent to an AAA-P, if the AAA-P is used (e.g. the AAA Server belongs to a third party), otherwise it may be sent directly to the NSSAAF. If an AAA-P is present, the AAA-P relays the Reauthentication Request to the NSSAAF.

2. The NSSAAF checks whether the AAA-S is authorized to request the re-authentication and re-authorization by checking the local configuration of AAA-S address per S-NSSAI. If success, the NSSAAF requests UDM for the AMF serving the UE using the Nudm_UECM_Get (GPSI, AMF Registration) service operation. The UDM provides the NSSAAF with the AMF ID of the AMF serving the UE.

3. The NSSAAF provides an acknowledgement to the AAA protocol Re-Auth Request message. If the AMF is not registered in UDM the procedure is stopped here.

4. If the AMF is registered in UDM, the NSSAAF requests the relevant AMF to re-authenticate/re-authorize the S-NSSAI for the UE using the Nnssaaf_NSSAA_Re-authenticationNotification service operation. The AMF is implicitly subscribed to receive Nnssaaf_NSSAA_Re-authenticationNotification service operations. The NSSAAF may discover the Callback URI for the Nnssaaf_NSSAA_Re-authenticationNotification service operation exposed by the AMF via the NRF.

   The AMF acknowledges the notification of Re-authentication request.

5. If the UE is registered with the S-NSSAI in the Mapping Of Allowed NSSAI, the AMF triggers the NSSAA procedure defined in clause 16.3 for the UE identified by the GPSI and the Network Slice identified by the S-NSSAI received from the NSSAAF.

   If the UE is registered but the S-NSSAI is not in the Mapping Of Allowed NSSAI, the AMF removes any status of the corresponding S-NSSAI subject to Network Slice-Specific Authentication and Authorization in the UE context it may have kept, so that an NSSAA is executed next time the UE requests to register with the S-NSSAI.

---

## 16.5 AAA Server triggered Slice-Specific Authorization Revocation

### Procedure (Figure 16.5-1)

0. The UE is registered in 5GC via an AMF. The AMF ID is stored in the UDM.

1. The AAA-S requests the revocation of Slice-Specific Authorization for the Network Slice specified by the S-NSSAI/ENSI in the Revocation Request message, for the UE identified by the GPSI in this message. This message is sent to an AAA-P, if the AAA-P is used, otherwise it may be sent directly to the NSSAAF. If an AAA-P is present, the AAA-P relays the Revocation Request to the NSSAAF.

2. The NSSAAF checks whether the AAA-S is authorized to request the revocation by checking the local configuration of AAA-S address per S-NSSAI. If success, the NSSAAF requests UDM for the AMF serving the UE using the Nudm_UECM_Get (GPSI, AMF Registration) service operation. The UDM provides the NSSAAF with the AMF ID of the AMF serving the UE.

3. The NSSAAF provides an acknowledgement to the AAA protocol Revocation Request message. If the AMF is not registered in UDM the procedure is stopped here.

4. If the AMF is registered in UDM, the NSSAAF notifies the relevant AMF to revoke the Slice-Specific Authorization for the S-NSSAI for the UE using the Nnssaaf_NSSAA_RevocationNotification service operation. The AMF is implicitly subscribed to receive Nnssaaf_NSSAA_RevocationNotification service operations.

   The AMF acknowledges the notification.

5. Based on the revocation notification, the AMF initiates the UE Configuration Update procedure as defined in clause 4.2.4.2 of TS 23.502 [8] to indicate to the UE the new Allowed NSSAI. If the Allowed NSSAI is empty, the AMF initiates the UE Deregistration procedure as defined in clause 4.2.2.3 of TS 23.502 [8].

---

## Annex B.2 EAP TLS (for NSSAA)

### B.2.1 Security procedures

For NSSAA using EAP-TLS, the procedures defined in Annex B.2 apply with the following modifications:

- The SEAF/AMF acts as the EAP authenticator and the NSSAAF acts as the EAP authenticator backend.
- The NSSAAF forwards EAP messages between the AMF and the AAA-S.
- Key derivation follows the MSK derivation from TLS as specified in RFC 5216.

---

**End of NSSAAF Related Sections from TS 33.501**
