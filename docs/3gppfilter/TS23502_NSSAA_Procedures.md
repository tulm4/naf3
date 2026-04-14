# 3GPP TS 23.502 - NSSAA Procedures
## Extracted from: 3GPP TS 23.502 V18.4.0 (2025-03)

**Source:** 3GPP TS 23.502 - Procedures for the 5G System (Release 18)

---

## 4.2.9 Network Slice-Specific Authentication and Authorization procedure

### 4.2.9.1 General

The Network Slice-Specific Authentication and Authorization procedure is triggered for an S-NSSAI requiring Network Slice-Specific Authentication and Authorization with an AAA Server (AAA-S) which may be hosted by the H-PLMN operator or by a third party which has a business relationship with the H-PLMN, using the EAP framework as described in TS 33.501 [15]. An AAA Proxy (AAA-P) in the HPLMN may be involved e.g. if the AAA Server belongs to a third party.

This procedure is triggered by the AMF during a Registration procedure when some Network Slices require Slice-Specific Authentication and Authorization, when AMF determines that Network Slice-Specific Authentication and Authorization is requires for an S-NSSAI in the current Allowed NSSAI or Partially Allowed NSSAI (e.g. subscription change), or when the AAA Server that authenticated the Network Slice triggers a re-authentication.

**NOTE 1:** The S-NSSAI in the procedure can be part of Allowed NSSAI, Mapping Of Allowed NSSAI, Partially Allowed NSSAI or Mapping Of Partially Allowed NSSAI.

The AMF performs the role of the EAP Authenticator and communicates with the AAA-S via the Network Slice specific and SNPN Authentication and Authorization Function (NSSAAF). The NSSAAF undertakes any AAA protocol interworking with the AAA protocol supported by the AAA-S.

The Network Slice-Specific Authentication and Authorization procedure requires the use of a GPSI. In other words, a subscription that contains S-NSSAIs subject to Network Slice-Specific Authentication and Authorization shall include at least one GPSI.

After a successful or unsuccessful UE Network Slice-Specific Authentication and Authorization, the AMF store the NSSAA result status for the related S-NSSAI in the UE context.

**NOTE 2:** If an S-NSSAI subject to the NSSAA is rejected due to Network Slice Admission Control (e.g. the maximum number of UEs per network slice has been reached), the NSSAA result status stored in the UE context is not impacted.

---

### 4.2.9.2 Network Slice-Specific Authentication and Authorization

The detailed procedure steps are as follows:

**Figure 4.2.9.2-1: Network Slice-Specific Authentication and Authorization procedure**

#### Step-by-Step Procedure:

**Step 1:**
For S-NSSAIs that are requiring Network Slice-Specific Authentication and Authorization, based on change of subscription information, or triggered by the AAA-S, the AMF may trigger the start of the Network Slice Specific Authentication and Authorization procedure.

If Network Slice Specific Authentication and Authorization is triggered as a result of Registration procedure, the AMF may determine, based on UE Context in the AMF, that for some or all S-NSSAI(s) subject to Network Slice Specific Authentication and Authorization, the UE has already been authenticated following a Registration procedure on a first access. Depending on Network Slice Specific Authentication and Authorization result (e.g. success/failure) from the previous Registration, the AMF may decide, based on Network policies, to skip Network Slice Specific Authentication and Authorization for these S-NSSAIs during the Registration on a second access.

If the Network Slice Specific Authentication and Authorization procedure corresponds to a re-authentication and re-authorization procedure triggered as a result of AAA Server-triggered UE re-authentication and re-authorization for one or more S-NSSAIs, as described in 4.2.9.2, or triggered by the AMF based on operator policy or a subscription change and if S-NSSAIs that are requiring Network Slice-Specific Authentication and Authorization are included in the Allowed NSSAI for each Access Type, the AMF selects an Access Type to be used to perform the Network Slice Specific Authentication and Authorization procedure based on network policies.

**Step 2:**
The AMF may send an EAP Identity Request for the S-NSSAI in a NAS MM Transport message including the S-NSSAI. This is the S-NSSAI of the H-PLMN, not the locally mapped S-NSSAI value.

**Step 3:**
The UE provides the EAP Identity Response for the S-NSSAI alongside the S-NSSAI in an NAS MM Transport message towards the AMF.

**Step 4:**
The AMF sends the EAP Identity Response to the NSSAAF in a Nnssaaf_NSSAA_Authenticate Request (EAP Identity Response, GPSI, S-NSSAI).

**NOTE:** If the UE subscription includes multiple GPSIs, the AMF uses any GPSI in the list provided by the UDM for NSSAA procedures.

**Step 5:**
If the AAA-P is present (e.g. because the AAA-S belongs to a third party and the operator deploys a proxy towards third parties), the NSSAAF forwards the EAP ID Response message to the AAA-P, otherwise the NSSAAF forwards the message directly to the AAA-S. The NSSAAF is responsible to send the NSSAA requests to the appropriate AAA-S based on local configuration of AAA-S address per S-NSSAI. The NSSAAF uses towards the AAA-P or the AAA-S an AAA protocol message of the same protocol supported by the AAA-S.

**Step 6:**
The AAA-P forwards the EAP Identity message to the AAA-S addressable by the AAA-S address together with S-NSSAI and GPSI. The AAA-S stores the GPSI to create an association with the EAP Identity in the EAP ID response message, so the AAA-S can later use it to revoke authorization or to trigger reauthentication.

**Steps 7-14:**
EAP-messages are exchanged with the UE. One or more than one iteration of these steps may occur.

**Step 15:**
EAP authentication completes. The AAA-S stores the S-NSSAI for which the authorisation has been granted, so it may decide to trigger reauthentication and reauthorization based on its local policies. An EAP-Success/Failure message is delivered to the AAA-P (or if the AAA-P is not present, directly to the NSSAAF) with GPSI and S-NSSAI.

**Step 16:**
If the AAA-P is used, the AAA-P sends an AAA Protocol message including (EAP-Success/Failure, S-NSSAI, GPSI) to the NSSAAF.

**Step 17:**
The NSSAAF sends the Nnssaaf_NSSAA_Authenticate Response (EAP-Success/Failure, S-NSSAI, GPSI) to the AMF.

**Step 18:**
The AMF transmits a NAS MM Transport message (EAP-Success/Failure) to the UE. The AMF shall store the EAP result for each S-NSSAI for which the NSSAA procedure in steps 1-17 was executed.

**Step 19a (Conditional):**
If a new Allowed NSSAI (i.e. including any new S-NSSAIs in a Requested NSSAI for which the NSSAA procedure succeeded and/or excluding any S-NSSAI(s) in the existing Allowed NSSAI for the UE for which the procedure has failed, or including default S-NSSAI(s) if all S-NSSAIs in a Requested NSSAI or in the existing Allowed NSSAI are subject to NSSAA and due to failure of the NSSAA procedures, they cannot be in the Allowed NSSAI)) and/or new Rejected S-NSSAIs (i.e. including any S-NSSAI(s) in the existing Allowed NSSAI for the UE for which the procedure has failed, or any new requested S-NSSAI(s) for which the NSSAA procedure failed) need to be delivered to the UE, or if the AMF re-allocation is required, the AMF initiates the UE Configuration Update procedure, for each Access Type, as described in clause 4.2.4.2. If the Network Slice-Specific Re-Authentication and Re-Authorization fails and there are PDU session(s) established that are associated with the S-NSSAI for which the NSSAA procedure failed, the AMF shall initiate the PDU Session Release procedure as specified in clause 4.3.4 to release the PDU sessions with the appropriate cause value.

**Step 19b (Conditional):**
If the Network Slice-Specific Authentication and Authorization fails for all S-NSSAIs (if any) in the existing Allowed NSSAI for the UE and (if any) for all S-NSSAIs in the Requested NSSAI and no default S-NSSAI could be added in the Allowed NSSAI, the AMF shall execute the Network-initiated Deregistration procedure described in clause 4.2.2.3.3 and it shall include in the explicit De-Registration Request the list of Rejected S-NSSAIs, each of them with the appropriate rejection cause value.

---

### 4.2.9.3 AAA Server triggered Network Slice-Specific Re-authentication and Re-authorization procedure

**Figure 4.2.9.3-1: AAA Server initiated Network Slice-Specific Re-authentication and Re-authorization procedure**

#### Step-by-Step Procedure:

**Step 1:**
The AAA-S requests the re-authentication and re-authorization for the Network Slice specified by the S-NSSAI in the AAA protocol Re-Auth Request message, for the UE identified by the GPSI in this message. This message is sent to a AAA-P, if the AAA-P is used (e.g. the AAA Server belongs to a third party), otherwise it is sent directly to the NSSAAF.

**Step 2:**
The AAA-P, if present, relays the request to the NSSAAF.

**Steps 3a-3b:**
NSSAAF gets AMF ID from UDM using Nudm_UECM_Get with the GPSI in the received AAA message. If NSSAAF receives two different AMF address then the NSSAAF either decide to notify both AMFs or the NSSAF may decide to notify one AMF first and if NSSAA fails also notify the other AMF.

**Step 3c:**
The NSSAAF provides an acknowledgement to the AAA protocol Re-Auth Request message. If the AMF is not registered in UDM the procedure is stopped here.

**Step 4:**
If the AMF is registered in UDM, the NSSAAF notifies the AMF to re-authenticate/re-authorize the S-NSSAI for the UE using Nnssaaf_NSSAA_Re-AuthNotification with the GPSI and S-NSSAI in the received AAA message. The callback URI of the notification for the AMF is derived via NRF as specified in TS 29.501 [62].

**Step 5:**
If the UE is registered with the S-NSSAI in the Mapping Of Allowed NSSAI, the AMF triggers the Network Slice-Specific Authentication and Authorization procedure defined in clause 4.2.9.1. If the S-NSSAI is included in the Allowed NSSAI for 3GPP access and non-3GPP access, AMF selects an access type to perform NSSAA based on network policies. If the S-NSSAI is only included in the Allowed NSSAI of non-3GPP access and UE is CM-IDLE in non-3GPP access, the AMF marks the S-NSSAI as pending. In this case, when UE becomes CM-CONNECTED in non-3GPP access, the AMF initiates NSSAA if needed.

If the UE is registered but the S-NSSAI is not in the Mapping Of Allowed NSSAI, the AMF removes any status of the corresponding S-NSSAI subject to Network Slice-Specific Authentication and Authorization in the UE context it may have kept, so that an NSSAA is executed next time the UE requests to register with the S-NSSAI.

---

### 4.2.9.4 AAA Server triggered Slice-Specific Authorization Revocation

**Figure 4.2.9.4-1: AAA Server-initiated Network Slice-Specific Authorization Revocation procedure**

#### Step-by-Step Procedure:

**Step 1:**
The AAA-S requests the revocation of authorization for the Network Slice specified by the S-NSSAI in the AAA protocol Revoke Auth Request message, for the UE identified by the GPSI in this message. This message is sent to AAA-P if it is used.

**Step 2:**
The AAA-P, if present, relays the request to the NSSAAF.

**Steps 3a-3b:**
The NSSAAF gets AMF ID from UDM using Nudm_UECM_Get with the GPSI in the received AAA message. If two different AMF addresses are received, the NSSAAF initiates the step 4 towards both AMFs.

**Step 3c:**
The NSSAAF provides an acknowledgement to the AAA protocol Re-Auth Request message. If the AMF is not registered in UDM the procedure is stopped here.

**Step 4:**
If the AMF is registered in UDM, the NSSAAF notifies the AMF to revoke the S-NSSAI authorization for the UE using Nnssaaf_NSSAA_RevocationNotification with the GPSI and S-NSSAI in the received AAA message. The callback URI of the notification for the AMF is derived via NRF as specified in TS 29.501 [62].

**Step 5:**
If the UE is registered with the S-NSSAI in the Mapping Of Allowed NSSAI, the AMF updates the UE configuration to revoke the S-NSSAI from the current Allowed NSSAI, for any Access Type for which Network Slice Specific Authentication and Authorization had been successfully run on this S-NSSAI. The UE Configuration Update may include a request to Register if the AMF needs to be re-allocated. The AMF provides a new Allowed NSSAI to the UE by removing the S-NSSAI for which authorization has been revoked. The AMF provides new rejected NSSAIs to the UE including the S-NSSAI for which authorization has been revoked. If no S-NSSAI is left in Allowed NSSAI for an access after the revocation and a Default NSSAI exists that requires no Network Slice Specific Authentication or for which a Network Slice Specific Authentication did not previously fail over this access, then the AMF may provide a new Allowed NSSAI to the UE containing the Default NSSAI. If no S-NSSAI is left in Allowed NSSAI for an access after the revocation and no Default NSSAI can be provided to the UE in the Allowed NSSAI or a previous Network Slice Specific Authentication failed for the Default NSSAI over this access, then the AMF shall execute the Network-initiated Deregistration procedure for the access as described in clause 4.2.2.3.3 and it shall include in the explicit De-Registration Request message the list of Rejected S-NSSAIs, each of them with the appropriate rejection cause value. If there are PDU session(s) established that are associated with the revoked S-NSSAI, the AMF shall initiate the PDU Session Release procedure as specified in clause 4.3.4 to release the PDU sessions with the appropriate cause value.

If the UE is registered but the S-NSSAI is not in the Mapping Of Allowed NSSAI, the AMF removes any status it may have kept of the corresponding S-NSSAI subject to Network Slice-Specific Authentication and Authorization in the UE context.

---

## 5.2.20 NSSAAF Services

### 5.2.20.1 General

### 5.2.20.2 Nnssaaf_NSSAA service

### 5.2.20.2.1 General

### 5.2.20.2.2 Nnssaaf_NSSAA_Authenticate service operation

### 5.2.20.2.3 Nnssaaf_NSSAA_Re-AuthenticationNotification service operation

### 5.2.20.2.4 Nnssaaf_NSSAA_RevocationNotification service operation

---

**End of NSSAA Procedure Sections from TS 23.502**
