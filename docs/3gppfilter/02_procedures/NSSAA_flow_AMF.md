# NSSAA Procedure Flows

Extracted from: 3GPP TS 23.502 v18.4.0

Source: `TS23502_NSSAA_Procedures.md` §4.2.9

---

## NSSAA Flow (AMF-triggered) — §4.2.9.2

**Trigger conditions:**
- Registration with S-NSSAI requiring NSSAA
- Subscription change
- AAA-S triggered reauth/revocation

**Precondition:** UE has GPSI in subscription. AMF holds GPSI from UDM.

### Step-by-Step Flow

```
Step 1:  AMF decides to trigger NSSAA for S-NSSAI(s) requiring auth.
         If re-running NSSAA from prior Registration, AMF may skip based on
         prior result and network policy.

Step 2:  AMF sends EAP Identity Request for S-NSSAI in NAS MM Transport message.
         S-NSSAI = H-PLMN S-NSSAI (not mapped value).

Step 3:  UE responds with EAP Identity Response + S-NSSAI in NAS MM Transport.

Step 4:  AMF → NSSAAF: Nnssaaf_NSSAA_Authenticate_Request
         (EAP Identity Response, GPSI, S-NSSAI)

Step 5:  If AAA-P present (third-party AAA-S): NSSAAF → AAA-P → AAA-S
         If no AAA-P: NSSAAF → AAA-S (direct routing by S-NSSAI config)
         NSSAAF includes S-NSSAI + GPSI in AAA protocol message.

Step 6:  AAA-P → AAA-S: EAP Identity + S-NSSAI + GPSI
         AAA-S stores GPSI ↔ EAP Identity mapping for later reauth/revocation.

Steps 7-14: EAP message exchange rounds (1 or many iterations).
         NSSAAF forwards EAP messages between AMF and AAA-S.
         AMF ↔ NSSAAF: Nnssaaf_NSSAA_Authenticate (PUT)
         NSSAAF ↔ AAA-S: AAA protocol (RADIUS Access-Challenge / Diameter DEA)

Step 15: EAP auth completes.
         AAA-S stores S-NSSAI for granted authorization.
         EAP-Success or EAP-Failure delivered to NSSAAF (via AAA-P if present).

Step 16: If AAA-P: AAA-P → NSSAAF: AAA Protocol message (EAP-Success/Failure, S-NSSAI, GPSI)

Step 17: NSSAAF → AMF: Nnssaaf_NSSAA_Authenticate_Response
         (EAP-Success/Failure, S-NSSAI, GPSI)

Step 18: AMF → UE: NAS MM Transport (EAP-Success/Failure)
         AMF stores NSSAA result (per S-NSSAI) in UE Context.

Step 19a: If Allowed NSSAI changes → AMF triggers UE Configuration Update.
         If PDU sessions exist for failed S-NSSAI → AMF triggers PDU Session Release.

Step 19b: If ALL S-NSSAIs in Allowed NSSAI fail AND no default S-NSSAI available
         → AMF triggers Network-initiated Deregistration with Rejected S-NSSAIs list.
```

### Key Points

- AMF is EAP Authenticator; NSSAAF is EAP authenticator backend
- GPSI is mandatory (TS23502 §4.2.9.1)
- AMF sends H-PLMN S-NSSAI (not mapped)
- NSSAAF routes to AAA-S based on local config per S-NSSAI
- AAA-P optional (used when AAA-S is third-party)
- S-NSSAI in `Mapping Of Allowed NSSAI` also subject to NSSAA

---

## Re-Authentication Flow (AAA-S triggered) — §4.2.9.3

**Trigger:** AAA-S sends Re-Auth Request for a registered UE's S-NSSAI.

### Step-by-Step Flow

```
Step 1:  AAA-S → NSSAAF (or AAA-P → NSSAAF): AAA Re-Auth Request
         (GPSI, S-NSSAI/ENSI)

Step 2:  If AAA-P present: AAA-P → NSSAAF: relays Re-Auth Request

Step 3a: NSSAAF → UDM: Nudm_UECM_Get (GPSI, AMF Registration)
         UDM returns AMF ID(s) serving this UE.
         NOTE: If two different AMF addresses returned, NSSAAF may notify
               both or notify one first then retry the other on failure.

Step 3b: If AMF not registered → procedure stops here.

Step 3c: NSSAAF → AAA-S: ACK to Re-Auth Request

Step 4:  NSSAAF → AMF: Nnssaaf_NSSAA_Re-AuthenticationNotification
         (GPSI, S-NSSAI)
         AMF is implicitly subscribed to this notification.
         Callback URI discovered via NRF (§29.501).

Step 5:  AMF → UE: triggers NSSAA procedure (§4.2.9.2) for this S-NSSAI.
         If S-NSSAI in Allowed NSSAI (3GPP access): AMF selects access type by policy.
         If S-NSSAI only in Allowed NSSAI (non-3GPP) and UE CM-IDLE:
           AMF marks S-NSSAI as pending → executes NSSAA when UE becomes CM-CONNECTED.
         If S-NSSAI NOT in Mapping Of Allowed NSSAI:
           AMF clears pending status → NSSAA executes next time UE registers.
```

### Key Points

- NSSAAF checks authorization: local config of AAA-S address per S-NSSAI
- AMF ID discovery via UDM is required before notifying AMF
- If two AMFs serve same UE (multi-registration), NSSAAF may notify both
- AMF may delay NSSAA if UE is idle on non-3GPP access only

---

## Revocation Flow (AAA-S triggered) — §4.2.9.4

**Trigger:** AAA-S revokes slice authorization for a registered UE.

### Step-by-Step Flow

```
Step 1:  AAA-S → NSSAAF (or AAA-P → NSSAAF): AAA Revoke Auth Request
         (GPSI, S-NSSAI/ENSI)

Step 2:  If AAA-P present: AAA-P → NSSAAF: relays Revoke Request

Step 3a: NSSAAF → UDM: Nudm_UECM_Get (GPSI, AMF Registration)
         UDM returns AMF ID(s).

Step 3b: If AMF not registered → procedure stops here.

Step 3c: NSSAAF → AAA-S: ACK to Revoke Auth Request
         (NSSAAF need not wait for AMF/Nudm_UECM_GET response before ACK)

Step 4:  NSSAAF → AMF: Nnssaaf_NSSAA_RevocationNotification
         (GPSI, S-NSSAI)

Step 5:  AMF processes revocation:
         - Remove S-NSSAI from Allowed NSSAI for relevant Access Types
         - If Allowed NSSAI becomes empty but Default NSSAI available
           (no-NSSAA or previously successful NSSAA): provide Default NSSAI
         - If no NSSAI available or Default NSSAA failed:
           trigger Network-initiated Deregistration
         - If PDU sessions exist for revoked S-NSSAI: release them
         - If UE registered but S-NSSAI not in Mapping Of Allowed NSSAI:
           clear any pending NSSAA status
         - Trigger UE Configuration Update with new Allowed NSSAI / Rejected S-NSSAIs
```

### Key Points

- Revocation ACK to AAA-S can be sent immediately (before AMF response)
- If S-NSSAI is in `Mapping Of Allowed NSSAI` only: AMF clears pending status
- Revocation is per Access Type (only affects access types where NSSAA succeeded)
- Rejected S-NSSAIs list includes revoked S-NSSAI with rejection cause
