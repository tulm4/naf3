# 3GPP TS 29.561 - NSS-AAA Interworking
## Extracted from: 3GPP TS 29.561 V18.5.0 (2025-03)

**Source:** 3GPP TS 29.561 - 5G System; External Network Domain Proxy (NDep) for Untrusted Non-3GPP Access; Stage 3

---

# 16 Interworking with NSS-AAA (RADIUS)

## 16.1 RADIUS procedures

### 16.1.1 General

The Network Slice Specific Authentication and Authorization procedure is triggered for a network slice requiring Network Slice Specific Authentication and Authorization with an NSS-AAA server which may be hosted by the H-PLMN operator or by a third party which has a business relationship with the H-PLMN. An AAA Proxy (AAA-P) in the HPLMN may be involved e.g. if the NSS-AAA Server belongs to a third party.

### 16.1.2 RADIUS Authentication and Authorization

RADIUS Authentication and Authorization shall be used according to IETF RFC 2865 [8], IETF RFC 3162 [9] and IETF RFC 4818 [10]. In 5G, multiple authentication methods using Extensible Authentication Protocol (EAP) may be used such as EAP-TLS (see IETF RFC 5216 [11]), EAP-TTLS (see IETF RFC 5281 [37]). The NSSAAF or AAA-P shall implement the RADIUS extension to support EAP as specified in IETF RFC 3579 [7].

The RADIUS client function may reside in an NSSAAF. When the NSSAAF receives Nnssaaf_NSSAA_Authenticate request from AMF, the RADIUS client function shall send the authentication information with network slice information to a NSS-AAA server directly or via an AAA-P.

The NSS-AAA server performs authentication and authorization for the user and requested network slice information. When the NSSAAF receives an Access-Accept message from the NSS-AAA server or AAA-P, it shall complete the network slice specific authentication procedure. If Access-Reject or no response is received, the NSSAAF shall reject the network slice specific authentication procedure with a suitable cause code.

The NSS-AAA may revoke the authorization for the network slice, see details in clause 16.2.2. In the present release, the NSS-AAA initiated re-authentication is not supported.

## 16.2 Message flows for network slice specific authentication

### 16.2.1 Authentication and Authorization procedures

When the NSSAAF receives Nnssaaf_NSSAA_Authenticate request from AMF, it shall send a RADIUS Access-Request message with EAP extension to an NSS-AAA server directly or via an AAA-P if AAA-P is involved. The Access-Request message shall include GPSI in Calling-Station-Id or External-Identifier attribute and network slice information in 3GPP-S-NSSAI attribute. Upon receipt of the Access-Request message, the NSS-AAA server shall respond with an Access-Challenge message. Multi-round authentication using the Access-Challenge (sent by NSS-AAA) and Access-Request messages may be used. The NSS-AAA server finally authenticates and authorizes the user and the network slice by replying with an Access Accept message.

For re-authentication and re-authorization, the NSSAAF shall send a RADIUS Access-Request message with EAP extension to the NSS-AAA server directly or via the AAA-P if AAA-P is used and the NSS-AAA shall respond with an Access-Challenge message. Multi-round authentication using the Access-Challenge (sent by NSS-AAA) and Access-Request messages may be used. The NSS-AAA server finally authenticates and authorizes the user and the network slice by replying with an Access Accept message.

### NSSAA RADIUS Message Flow (Figure 16.2.1-1)

1. AMF decides to trigger the start of the Network Slice Specific Authentication and Authorization procedure.

2. The AMF may send an EAP Identity Request in a NAS Network Slice-Specific Authentication Command message.

3. The UE provides the EAP Identity Response in a NAS Network Slice-Specific Authentication Complete message towards the AMF.

4. The AMF sends Nnssaaf_NSSAA_Authenticate Request to the NSSAAF including the authentication/authorization information.

5-6. If the AAA-P is present (e.g. because the NSS-AAA belongs to a third party and the operator deploys a proxy towards third parties), the NSSAAF sends the Access-Request message to the NSS-AAA via the AAA-P to forward the authentication/authorization information, otherwise the NSSAAF sends the Access-Request message directly to the NSS-AAA.

7-14. The NSS-AAA responds with the Access-Challenge message to the NSSAAF directly or via the AAA-P. The authentication/authorization information is further transferred to UE via AMF by Nnssaaf_NSSAA_Authenticate service and NAS Network Slice-Specific Authentication Command message. UE responds to the received authentication/authorization data and such information is transferred in NAS Network Slice-Specific Authentication Complete message and Nnssaaf_NSSAA_Authenticate service, then finally sent to the NSS-AAA by the NSSAAF, via the AAA-P if the AAA-P is used, in the Access-Request message.

**NOTE:** Step 7 to step 14 can be repeated depending on the authentication/authorization mechanism used (e.g. EAP-TLS).

15-16. If the AAA-P is used, the NSS-AAA sends a Access-Accept message with the final result of authentication/authorization to the NSSAAF via the AAA-P, otherwise the NSS-AAA sends the Access-Accept message directly to the NSSAAF.

17. The NSSAAF sends a Nnssaaf_NSSAA_Authenticate Response with the final result of authentication/authorization information to the AMF.

18. The AMF transfers the final result of authentication/authorization information in a NAS Network Slice-Specific Authentication Result message to the UE.

### 16.2.2 NSS-AAA initiated revocation of network slice authorization

The NSS-AAA server may send a RADIUS Disconnect-Request to the NSSAAF directly or via AAA-P (if AAA-P is used) asking for revocation of network slice authorization. On receipt of the Disconnect-Request from the NSS-AAA server, the NSSAAF shall check whether the NSS-AAA server is authorized to request the revocation by verifying the local configuration of the address of the NSS-AAA server per S-NSSAI, if successful, the NSSAAF shall release the resources, interact with its succeeding Network Function AMF which is got from the UDM by Nudm_UECM_GET service operation with GPSI and reply with a Disconnect-ACK. If the NSSAAF is unable to release the corresponding resources, it shall reply to the NSS-AAA server with a Disconnect-NAK. For more information on RADIUS Disconnect, see IETF RFC 5176 [27]. It is not necessary for the NSSAAF to wait for the response (i.e. Nudm_UECM_GET or Nnssaaf_NSSAA_Notify response) from the succeeding Network Function before sending the RADIUS Disconnect-ACK to the NSS-AAA server or AAA-P (if AAA-P is used).

### RADIUS Revocation Flow (Figure 16.2.2-1)

If the AAA-P is not used, the Disconnect Request and Response messages are exchanged between the NSS-AAA and the NSSAAF.

## 16.3 List of RADIUS attributes

### 16.3.1 General

Information defined in clause 11.3 are re-used for network slice specific authentication with the following differences:

- NSSAAF replaces SMF.
- IP, Ethernet and PDU session related descriptions and attributes are not applicable.
- RADIUS messages for accounting function (Accounting Request/Response) are not applicable.
- Additional detailed information needed for network slice specific authentication are described below.

### 16.3.2 3GPP-S-NSSAI Sub-attribute

**Sub-attribute Number:** 200

**Description:** Contains the S-NSSAI information for network slice authentication.

**Format:**

```
Octets: 1       | 2        | 3        | 4-6
        3GPP Type=200 | Length   | SST     | SD (optional)
```

**Fields:**
- **3GPP Type:** 200
- **Length:** 3 or 6 (depending on whether SD is present)
- **SST:** Slice/Service Type (0-255)
- **SD:** 3-octet Slice Differentiator (optional)

**Table 16.3-2: 3GPP Vendor-Specific sub-attributes applicability**

| Sub-attr # | Sub-attribute Name | Description | Presence Requirement | Associated attribute | Applicability |
|------------|-------------------|-------------|---------------------|---------------------|---------------|
| 200 | 3GPP-S-NSSAI | It includes the S-NSSAI. | Conditional (NOTE) | Access-Request | |

**NOTE:** This VSA shall be included in the initial Access-Request message.

---

# 17 Interworking with NSS-AAA (Diameter)

## 17.1 Diameter procedures

### 17.1.1 General

The Network Slice Specific Authentication and Authorization procedure is triggered for a network slice requiring Network Slice Specific Authentication and Authorization with an NSS-AAA server which may be hosted by the H-PLMN operator or a third party which has a business relationship with the H-PLMN. An AAA Proxy (AAA-P) in the HPLMN may be involved e.g. if the NSS-AAA Server belongs to a third party.

### 17.1.2 Diameter Authentication and Authorization

Diameter Authentication and Authorization shall be used according to IETF RFC 7155 [23]. In 5G, multiple authentication methods using Extensible Authentication Protocol (EAP) may be used such as EAP-TLS (see IETF RFC 5216 [11]), EAP-TTLS (see IETF RFC 5281 [37]). The NSSAAF or AAA-P shall support Diameter EAP application as specified in IETF RFC 4072 [25].

The NSSAAF or AAA-P and the NSS-AAA shall advertise the support of the Diameter NASREQ and EAP applications by including the value (1 and 5) of the application identifier in the Auth-Application-Id AVP (as specified in IETF RFC 4072 [25]) and the value of the 3GPP (10415) in the Vendor-Id AVP of the Capabilities-Exchange-Request and Capabilities-Exchange-Answer commands as specified in IETF RFC 6733 [24], i.e. as part of the Vendor-Specific-Application-Id AVP.

The Diameter client function may reside in an NSSAAF. When the NSSAAF receives Nnssaaf_NSSAA_Authenticate request from AMF, the Diameter client function shall send the authentication information with network slice information to a NSS-AAA server directly or via an AAA-P (if AAA-P is used).

The NSS-AAA server performs authentication and authorization for the requested network slice information. When the Nnssaaf receives a positive response from the NSS-AAA server or AAA-P (if AAA-P is used), it shall complete the network slice specific authentication procedure. If negative response or no response is received, the NSSAAF shall reject the network slice specific authentication procedure with a suitable cause code.

The NSS-AAA may revoke the authorization for the network slice, see details in clause 17.2.2. NSS-AAA may initiate re-authentication and re-authorization, see details in clause 17.2.3.

## 17.2 Message flows for network slice specific authentication

### 17.2.1 Authentication and Authorization procedures

For network slice specific authentication and authorization, when the NSSAAF receives Nnssaaf_NSSAA_Authenticate request from AMF, it shall send a Diameter DER message with GPSI in Calling-Station-Id or External-Identifier attribute and network slice information in 3GPP-S-NSSAI attribute to a NSS-AAA server directly or via AAA-P if AAA-P is involved. Upon receipt of the DER message, the DN-AAA server shall respond with an DEA message. Multi-round authentication using the DEA and DER messages may be used. The NSS-AAA server finally authenticates and authorizes the user and the network slice by replying with a Diameter DEA message.

For re-authentication and re-authorization, the NSSAAF shall send a DER message to the NSS-AAA server directly or via AAA-P if AAA-P is used and the NSS-AAA server shall respond with a DEA message. Multi-round authentication using the DEA and DER messages may be used. The NSS-AAA server finally authenticates and authorizes the user and the network slice by replying with a Diameter DEA message.

If the network slice specific authentication is not required, the NSSAAF shall send a Diameter STR message to the NSS-AAA server directly or via AAA-P if AAA-P is involved. The NSS-AAA server shall reply with a Diameter STA message.

### NSSAA Diameter Message Flow (Figure 17.2.1-1)

1. AMF decides to trigger the start of the Network Slice Specific Authentication and Authorization procedure.

2. The AMF may send an EAP Identity Request in a NAS Network Slice-Specific Authentication Command message.

3. The UE provides the EAP Identity Response in a NAS Network Slice-Specific Authentication Complete message towards the AMF.

4. The AMF sends Nnssaaf_NSSAA_Authenticate Request to the NSSAAF including the authentication/authorization information.

5-6. If the AAA-P is present, the NSSAAF sends the Diameter DER message to the NSS-AAA via the AAA-P to forward the authentication/authorization information, otherwise the NSSAAF sends the DER message directly to the NSS-AAA.

7-14. Multi-round EAP authentication exchanges via DEA/DER messages between NSSAAF and NSS-AAA, with NAS messages via AMF to UE.

15-16. NSS-AAA sends final Diameter DEA message with authentication result to NSSAAF via AAA-P if used.

17. The NSSAAF sends a Nnssaaf_NSSAA_Authenticate Response with the final result to the AMF.

18. The AMF transfers the final result in a NAS Network Slice-Specific Authentication Result message to the UE.

### 17.2.2 NSS-AAA Initiated Revocation (Diameter)

### 17.2.3 NSS-AAA Initiated Re-authentication (Diameter)

## 17.3 List of Diameter AVPs

### 17.3.1 3GPP-S-NSSAI AVP

**AVP Name:** 3GPP-S-NSSAI

**AVP Code:** TBD (Vendor-Specific)

**AVP Format:** Grouped

**Description:** Contains the S-NSSAI information for network slice authentication.

---

**End of NSS-AAA Interworking Sections from TS 29.561**
