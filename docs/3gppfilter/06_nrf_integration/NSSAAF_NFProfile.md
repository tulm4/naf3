# NSSAAF NFProfile Data Structure

**Source:** TS 29.510 V18.11.0 (2026-03) — Network Function Repository Services
**Sections:** §6.1.6.2.2 (NFProfile), §6.1.6.2.104 (NssaafInfo)

## NFProfile Type (§6.1.6.2.2)

NFProfile represents the complete profile of an NSSAAF instance registered in NRF.

### Complete NFProfile for NSSAAF

```json
{
  "nfInstanceId": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "nfInstanceName": "nssAAF-001",
  "heartBeatTimer": 30,
  "servedPrtIds": ["prt-001", "prt-002"],
  "prtUri": "http://nssAAF:8080/prt",
  "plmnList": [
    { "mcc": "001", "mnc": "01" },
    { "mcc": "001", "mnc": "02" }
  ],
  "nssaafInfo": {
    "supiRanges": [
      {
        "start": "imsi-001010000000001",
        "end": "imsi-001019999999999",
        "pattern": "^imsi-00101.*$"
      }
    ],
    "internalGroupIdentifiersRanges": [
      {
        "start": "group-001",
        "end": "group-999"
      }
    ]
  },
  "nfServices": [
    {
      "serviceInstanceId": "nnssaaf-nssaa-001",
      "serviceName": "nnssaaf-nssaa",
      "versions": [
        {
          "apiVersion": "v1",
          "expiry": "2027-01-01T00:00:00Z"
        }
      ],
      "scheme": "http",
      "nfServiceStatus": "REGISTERED",
      "fqdn": "nssAAF-001.operator.com",
      "interApranZoneName": "zone-001",
      "capacity": 1000,
      "priority": 1,
      "notificationUri": "http://nssAAF:8080/nnssaaf-nssaa/notifications",
      "allowedNfTypes": ["AMF"],
      "allowedNfDomains": ["operator.com"]
    },
    {
      "serviceInstanceId": "nnssaaf-aiw-001",
      "serviceName": "nnssaaf-aiw",
      "versions": [
        {
          "apiVersion": "v1",
          "expiry": "2027-01-01T00:00:00Z"
        }
      ],
      "scheme": "http",
      "nfServiceStatus": "REGISTERED",
      "fqdn": "nssAAF-001.operator.com",
      "interApranZoneName": "zone-001",
      "capacity": 1000,
      "priority": 1,
      "notificationUri": "http://nssAAF:8080/nnssaaf-aiw/notifications",
      "allowedNfTypes": ["AUSF"],
      "allowedNfDomains": ["operator.com"]
    }
  ],
  "nfSetId": "nssAAF-set-001",
  "servingScope": ["scope-001"],
  "nrfId": "http://nrf:8080/nnrf-nfm/v1/nf-instances/...",
  "locality": "dc-1",
  "customInfo": {}
}
```

---

## NFProfile Attribute Definitions

### Mandatory Attributes (M)

| Attribute | Type | Description |
|-----------|------|-------------|
| nfInstanceId | NfInstanceId (string) | Globally unique UUID v4 |
| nfType | NFType (enum) | Must be "NSSAAF" |
| nfStatus | NFStatus (enum) | REGISTERED, SUSPENDED, UNDISCOVERABLE |

### Optional Attributes (O)

| Attribute | Type | Cardinality | Description |
|-----------|------|-------------|-------------|
| nfInstanceName | string | 0..1 | Human-readable name |
| heartBeatTimer | integer | 0..1 | Heartbeat interval in seconds |
| nfServices | array(NFService) | 0..N | Services exposed by NSSAAF |
| plmnList | array(Plmn) | 0..N | Supported PLMNs |
| sNssais | array(Snssai) | 0..N | Supported S-NSSAIs |
| servedPrtIds | array(PrtId) | 0..N | PRT identifiers |
| prtUri | Uri | 0..1 | PRT callback URI |
| nssaafInfo | NssaafInfo | 0..1 | NSSAAF-specific data |
| nfSetId | string | 0..1 | NF Set identifier |
| nfSetName | string | 0..1 | NF Set name |
| servingScope | array(string) | 0..N | Scope identifiers |
| nrfId | Uri | 0..1 | NRF that registered this NF |
| locality | string | 0..1 | Data center/location info |
| udrInfo | array(UdrInfo) | 0..N | UDR-specific info |
| udmInfo | array(UdmInfo) | 0..N | UDM-specific info |
| ausfInfo | array(AusfInfo) | 0..N | AUSF-specific info |
| amfInfo | array(AmfInfo) | 0..N | AMF-specific info |
| smfInfo | array(SmfInfo) | 0..N | SMF-specific info |
| upfInfo | array(UpfInfo) | 0..N | UPF-specific info |
| pcfInfo | array(PcfInfo) | 0..N | PCF-specific info |
| bsfInfo | array(BsfInfo) | 0..N | BSF-specific info |
| nwdafInfo | array(NwdafInfo) | 0..N | NWDAF-specific info |
| customInfo | object | 0..1 | Vendor-specific data |
| recoverData | array(RecoverData) | 0..N | Recovery information |
| priority | integer | 0..1 | Priority (deprecated) |
| supportedServices | array(string) | 0..N | Legacy service names |
| allowedNfTypes | array(NFType) | 0..N | Allowed consumer NF types |
| allowedNfDomains | array(string) | 0..N | Allowed consumer domains |
| allowedNssais | array(PlmnSnssai) | 0..N | Allowed S-NSSAIs per PLMN |
| allowedPlmns | array(Plmn) | 0..N | Allowed PLMNs |
| smfServingArea | array(string) | 0..N | SMF serving areas |
| taiList | array(Tai) | 0..N | Tracking Area Identifiers |
| taiRangeList | array(TaiRange) | 0..N | TAI ranges |
| w-agfInfo | array(WAgfInfo) | 0..N | W-AGF info |
| tngfInfo | array(TngfInfo) | 0..N | TNGF info |
| w-enterpriseList | array(string) | 0..N | Enterprise list |
| defaultNotificationSubscription | DefaultNotificationSubscription | 0..1 | Default notification config |

---

## NssaafInfo Type (§6.1.6.2.104)

Specific data for the NSSAAF network function.

### NssaafInfo Structure

```json
{
  "supiRanges": [
    {
      "start": "imsi-001010000000001",
      "end": "imsi-001019999999999",
      "pattern": "^imsi-00101.*$",
      "size": "LARGE"
    }
  ],
  "internalGroupIdentifiersRanges": [
    {
      "start": "group-001",
      "end": "group-999"
    }
  ]
}
```

### NssaafInfo Attributes

| Attribute | Type | P | Cardinality | Description |
|-----------|------|---|-------------|-------------|
| supiRanges | array(SupiRange) | O | 1..N | Ranges of SUPIs served by this NSSAAF |
| internalGroupIdentifiersRanges | array(InternalGroupIdRange) | O | 1..N | Ranges of internal group IDs served |

---

## SupiRange Type

### Definition

| Attribute | Type | P | Cardinality | Description |
|-----------|------|---|-------------|-------------|
| start | string | M | 1 | Start of SUPI range |
| end | string | O | 0..1 | End of SUPI range |
| pattern | string | O | 0..1 | Regex pattern (ECMA-262) |
| size | string | O | 0..1 | Range size hint (SMALL/MEDIUM/LARGE) |

### Example

```json
{
  "start": "imsi-001010000000001",
  "end": "imsi-001019999999999",
  "pattern": "^imsi-00101.*$",
  "size": "LARGE"
}
```

---

## InternalGroupIdRange Type

### Definition

| Attribute | Type | P | Cardinality | Description |
|-----------|------|---|-------------|-------------|
| start | string | M | 1 | Start of group ID range |
| end | string | O | 0..1 | End of group ID range |
| pattern | string | O | 0..1 | Regex pattern |

### Example

```json
{
  "start": "group-001",
  "end": "group-999"
}
```

---

## NFService Type (§6.1.6.2.3)

Represents a service exposed by NSSAAF.

### NFService Structure

```json
{
  "serviceInstanceId": "nnssaaf-nssaa-001",
  "serviceName": "nnssaaf-nssaa",
  "versions": [
    {
      "apiVersion": "v1",
      "expiry": "2027-01-01T00:00:00Z"
    }
  ],
  "scheme": "http",
  "nfServiceStatus": "REGISTERED",
  "fqdn": "nssAAF-001.operator.com",
  "interApranZoneName": "zone-001",
  "capacity": 1000,
  "priority": 1,
  "notificationUri": "http://nssAAF:8080/nnssaaf-nssaa/notifications",
  "allowedNfTypes": ["AMF", "AUSF"],
  "allowedNfDomains": ["operator.com"],
  "allowedNssais": [
    { "sst": 1, "sd": "FFFFFF" }
  ]
}
```

### NFService Attributes

| Attribute | Type | P | Cardinality | Description |
|-----------|------|---|-------------|-------------|
| serviceInstanceId | string | M | 1 | Unique service instance ID |
| serviceName | ServiceName | M | 1 | Service name (e.g., "nnssaaf-nssaa") |
| versions | array(NFServiceVersion) | M | 1..N | Supported API versions |
| scheme | string | O | 0..1 | http, https |
| nfServiceStatus | NFServiceStatus | O | 0..1 | Service status |
| fqdn | Fqdn | O | 0..1 | Fully Qualified Domain Name |
| interApranZoneName | string | O | 0..1 | InterPLMN Area name |
| apiPrefix | Uri | O | 0..1 | API prefix |
| capacity | integer | O | 0..1 | Capacity indicator |
| priority | integer | O | 0..1 | Priority for load balancing |
| recoveryTime | DateTime | O | 0..1 | Recovery time |
| supportedFeatures | string | O | 0..1 | Supported feature flags |
| notificationUri | Uri | O | 0..1 | Notification callback URI |
| allowedNfTypes | array(NFType) | O | 0..N | Allowed consumer NF types |
| allowedNfDomains | array(string) | O | 0..N | Allowed consumer domains |
| allowedNssais | array(PlmnSnssai) | O | 0..N | Allowed S-NSSAIs |
| allowedPlmns | array(Plmn) | O | 0..N | Allowed PLMNs |
| corsPrefix | Uri | O | 0..1 | CORS prefix |

---

## NFType Enum Values

| Value | Description |
|-------|-------------|
| NSSAAF | NSSAAF (Network Slice-Specific Auth & Auth) |
| NRF | Network Function Repository |
| NEF | Network Exposure Function |
| UDM | Unified Data Management |
| UDR | Unified Data Repository |
| UDSF | Unstructured Data Storage Function |
| AUSF | Authentication Server Function |
| AMF | Access and Mobility Management Function |
| SMF | Session Management Function |
| UPF | User Plane Function |
| PCF | Policy Control Function |
| BSF | Binding Support Function |
| CHF | Charging Function |
| SEPP | Security Edge Protection Proxy |
| SCP | Service Communication Proxy |
| ... | Other 3GPP NF types |

---

## NFStatus Enum Values

| Value | Description |
|-------|-------------|
| REGISTERED | NF is active and discoverable |
| SUSPENDED | NF temporarily unavailable (no heartbeat) |
| UNDISCOVERABLE | NF registered but not discoverable via Nnrf_NFDiscovery |

---

## NFServiceStatus Enum Values

| Value | Description |
|-------|-------------|
| REGISTERED | Service is active |
| SUSPENDED | Service temporarily unavailable |
| UNDISCOVERABLE | Service not discoverable |

---

## Service Names for NSSAAF

| Service Name | Description | Exposed On |
|--------------|-------------|------------|
| nnssaaf-nssaa | NSSAA Service (N58) | NSSAAF |
| nnssaaf-aiw | AUSF Interworking Service (N60) | NSSAAF |

---

## Event Types for NFStatusSubscribe

| Event Type | Description |
|------------|-------------|
| NF_REGISTERED | New NF registered |
| NF_DEREGISTERED | NF deregistered |
| NF_PROFILE_CHANGED | NF profile changed |
| NF_STATUS_CHANGED | NF status changed (REGISTERED/SUSPENDED) |

---

## NotificationData Structure

```json
{
  "event": "NF_PROFILE_CHANGED",
  "nfProfile": { ... },
  "nfInstanceId": "...",
  "timestamp": "2026-07-15T10:00:00Z",
  "oldProfile": { ... }
}
```

---

## Implementation Notes

1. **UUID Format:** Use lowercase UUID v4 for nfInstanceId
2. **Heartbeat Timer:** Set based on NRF configuration (typically 30-60 seconds)
3. **NFProfile Updates:** Include `If-Match` header with ETag for conditional updates
4. **Service Discovery:** Cache discovery results with validityPeriod
5. **Token Scope:** Request tokens with specific service names (nnssaaf-nssaa, nnssaaf-aiw)
