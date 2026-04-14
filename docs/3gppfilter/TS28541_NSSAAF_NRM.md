# 3GPP TS 28.541 - NSSAAF Network Resource Model
## Extracted from: 3GPP TS 28.541 V18.3.0 (2025-03)

**Source:** 3GPP TS 28.541 - 5G Network Resource Model (NRM); Stage 2 and Stage 3

---

## 5.3.145 NSSAAFFunction

### 5.3.145.1 Definition

This IOC represents the NSSAAF function in 5GC. For more information about the NSSAAF, see TS 23.501 [2].

### 5.3.145.2 Attributes

The NSSAAFFunction IOC includes attributes inherited from ManagedFunction IOC (defined in TS 28.622[30]) and the following attributes:

| Attribute name | S | isReadable | isWritable | isInvariant | isNotifyable |
|---------------|---|-----------|-----------|------------|-------------|
| pLMNInfoList | M | T | T | F | T |
| sBIFQDN | M | T | T | F | T |
| cNSIIdList | O | T | T | F | T |
| managedNFProfile | M | T | T | F | T |
| commModelList | M | T | T | F | T |
| nssaafInfo | O | T | T | F | T |

**Attribute Descriptions:**

- **pLMNInfoList:** List of PLMN information served by this NSSAAF instance
- **sBIFQDN:** Service-Based Interface Fully Qualified Domain Name
- **cNSIIdList:** List of Network Slice Instance IDs associated with this NSSAAF
- **managedNFProfile:** NF profile as defined in TS 29.510
- **commModelList:** List of communication models supported
- **nssaafInfo:** NSSAAF-specific information (optional)

### 5.3.145.3 Attribute constraints

None.

### 5.3.145.4 Notifications

The common notifications defined in subclause 5.5 are valid for this IOC, without exceptions or additions.

---

## 5.3.146 NssaafInfo <<dataType>>

### 5.3.146.1 Definition

This data type represents the information of a NSSAAF NF Instance. (See clause 6.1.6.2.104 TS 29.510 [23]).

### 5.3.146.2 Attributes

| Attribute name | S | isReadable | isWritable | isInvariant | isNotifyable |
|---------------|---|-----------|-----------|------------|-------------|
| supiRanges | O | T | T | F | T |
| internalGroupIdentifiersRanges | O | T | T | F | T |

**Attribute Descriptions:**

- **supiRanges:** List of ranges of SUPIs that can be served by the NSSAAF instance
- **internalGroupIdentifiersRanges:** List of ranges of Internal Group Identifiers that can be served by the NSSAAF instance. If not provided, it does not imply that the NSSAAF supports all internal groups.

### 5.3.146.3 Attribute constraints

None.

### 5.3.146.4 Notifications

The subclause 5.5 of the <<IOC>> using this <<dataType>> as one of its attributes, shall be applicable.

---

## 5.3.147 EP_N58

### 5.3.147.1 Definition

This IOC represents an end point of N58 interface between NSSAAF and AMF, which is defined in TS 23.501 [2] and 33.501 [52].

### 5.3.147.2 Attributes

The EP_N58 IOC includes attributes inherited from EP_RP IOC (defined in TS 28.622[30]) and the following attributes:

| Attribute name | S | isReadable | isWritable | isInvariant | isNotifyable |
|---------------|---|-----------|-----------|------------|-------------|
| localAddress | O | T | T | F | T |
| remoteAddress | O | T | T | F | T |

**Attribute Descriptions:**

- **localAddress:** Local endpoint address for N58 interface
- **remoteAddress:** Remote endpoint address for N58 interface

### 5.3.147.3 Attribute constraints

None.

### 5.3.147.4 Notifications

The common notifications defined in subclause 5.5 are valid for this IOC, without exceptions or additions.

---

## 5.3.148 EP_N59

### 5.3.148.1 Definition

This IOC represents an end point of N59 interface between NSSAAF and UDM, which is defined in TS 23.501 [2] and 33.501 [52].

### 5.3.148.2 Attributes

The EP_N59 IOC includes attributes inherited from EP_RP IOC (defined in TS 28.622[30]) and the following attributes:

| Attribute name | S | isReadable | isWritable | isInvariant | isNotifyable |
|---------------|---|-----------|-----------|------------|-------------|
| localAddress | O | T | T | F | T |
| remoteAddress | O | T | T | F | T |

**Attribute Descriptions:**

- **localAddress:** Local endpoint address for N59 interface
- **remoteAddress:** Remote endpoint address for N59 interface

### 5.3.148.3 Attribute constraints

None.

### 5.3.148.4 Notifications

The common notifications defined in subclause 5.5 are valid for this IOC, without exceptions or additions.

---

## NSSAAFSubscribedEventNotification

Based on the generic notification structure defined in clause 5.5, the NSSAAFSubscribedEventNotification includes:

- **Event Types:** NSSAA_REAUTH_NOTIFICATION, NSSAA_REVOC_NOTIFICATION
- **Notification URI:** Callback endpoint for receiving notifications
- **Event Data:** Contains GPSI, S-NSSAI, and notification-specific data

---

## Figure 5.2.1.1-27: NSSAAFFunction NRM

The NRM diagram shows the hierarchical relationship:
- NSSAAFFunction at the top
- Contains NssaafInfo data type
- Has EP_N58 endpoint (NSSAAF-AMF interface)
- Has EP_N59 endpoint (NSSAAF-UDM interface)
- Inherits from ManagedFunction IOC

---

**End of NSSAAF NRM Sections from TS 28.541**
