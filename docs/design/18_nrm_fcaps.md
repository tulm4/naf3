---
spec: ETSI NFV-IFA 027 / 3GPP TS 28.541 §5.3.145-148
section: §5.3.145-148
interface: N/A (OAM/NRD)
service: NRM / FCAPS
operation: N/A
---

# NSSAAF NRM & FCAPS Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, NSSAAF is split into HTTP Gateway, Biz Pod, and AAA Gateway. The NRM reflects the SBI-facing view (HTTP Gateway address as `sBIFQDN` and `ep-n58`/`ep-n59`). The AAA Gateway configuration (VIP, ports) is represented in `customInfo`. See `docs/design/01_service_model.md` §5.4 for the architecture overview.

Thiết kế Network Resource Model (NRM) cho NSSAAF theo 3GPP TS 28.541 và ETSI NFV standards. FCAPS framework quản lý Fault, Configuration, Accounting, Performance, và Security.

---

## 2. NRM Model (TS 28.541)

### 2.1 NSSAAFFunction IOC

Từ TS 28.541 §5.3.145:

```yaml
NSSAAFFunction:
  inherits: ManagedFunction

  attributes:
    - pLMNInfoList        # M: PLMN IDs served
    - sBIFQDN            # M: SBI FQDN
    - cNSIIdList         # O: Network Slice Instance IDs
    - managedNFProfile   # M: NF profile (TS 29.510)
    - commModelList      # M: Communication models
    - nssaafInfo         # O: NSSAAF-specific info

  children:
    - EP_N58             # Endpoint: NSSAAF-AMF
    - EP_N59             # Endpoint: NSSAAF-UDM
```

### 2.2 YANG Model

```yang
module 3gpp-nssaaf-nrm {
  namespace "urn:3gpp:ts:ts_28_541";
  prefix "nssAAF-nrm";

  import 3gpp-common-yang-types { prefix "cmn"; }
  import 3gpp-managed-function { prefix "mf"; }

  revision 2025-01-01 { description "Release 18"; }

  grouping nssaa-info {
    description "NSSAAF-specific information";

    leaf-list supi-ranges {
      type string;
      description "SUPI ranges served by this NSSAAF instance";
    }

    leaf-list internal-group-id-ranges {
      type string;
      description "Internal Group ID ranges served";
    }

    leaf-list supported-security-algo {
      type string;
      description "Supported security algorithms: EAP-TLS, EAP-TTLS, EAP-AKA_PRIME";
    }
  }

  grouping ep-n58 {
    description "N58 interface endpoint (NSSAAF-AMF)";
    uses mf:EP_RP-grouping;

    leaf local-address {
      type inet:ip-address;
      description "Local endpoint address";
    }
    leaf remote-address {
      type inet:ip-address;
      description "Remote endpoint address (AMF side)";
    }
  }

  grouping ep-n59 {
    description "N59 interface endpoint (NSSAAF-UDM)";
    uses mf:EP_RP-grouping;

    leaf local-address {
      type inet:ip-address;
    }
    leaf remote-address {
      type inet:ip-address;
    }
  }

  container nssaa-function {
    list nssaa-function {
      key "managed-element-id";

      uses mf:managed-function-attributes;

      leaf pLMNInfoList {
        type leafref {
          path "/cmn:PLMNInfoList";
        }
        mandatory true;
      }

      leaf sBIFQDN {
        type inet:domain-name;
        mandatory true;
      }

      leaf-list cNSIIdList {
        type string;
        description "Network Slice Instance IDs";
      }

      uses mf:nf-profile;

      leaf commModelList {
        type string;
        mandatory true;
        description "Communication models supported";
      }

      uses nssaa-info;

      // Endpoints
      list ep-n58 {
        key "endpoint-id";
        uses ep-n58;
      }
      list ep-n59 {
        key "endpoint-id";
        uses ep-n59;
      }
    }
  }
}
```

---

## 3. FCAPS Implementation

### 3.1 Fault Management

```go
// Alarm types for NSSAAF
const (
    AlarmNssaaAaaServerUnreachable  = "NSSAA_AAA_SERVER_UNREACHABLE"
    AlarmNssaaSessionTableFull      = "NSSAA_SESSION_TABLE_FULL"
    AlarmNssaaDatabaseUnreachable   = "NSSAA_DB_UNREACHABLE"
    AlarmNssaaRedisUnreachable      = "NSSAA_REDIS_UNREACHABLE"
    AlarmNssaaNrfUnreachable        = "NSSAA_NRF_UNREACHABLE"
    AlarmNssaaHighAuthFailureRate   = "NSSAA_HIGH_AUTH_FAILURE_RATE"
    AlarmNssaaCircuitBreakerOpen    = "NSSAA_CIRCUIT_BREAKER_OPEN"
)

type Alarm struct {
    AlarmId          string    `json:"alarm_id"`
    AlarmType        string    `json:"alarm_type"`
    ProbableCause    string    `json:"probable_cause"`
    SpecificProblem  string    `json:"specific_problem"`
    Severity         string    `json:"severity"`  // CRITICAL, MAJOR, MINOR, WARNING, INDETERMINATE
    PerceivedSeverity string   `json:"perceived_severity"`
    BackupObject     string    `json:"backup_object"`
    CorrelatedAlarms []string `json:"correlated_alarms,omitempty"`
    ProposedRepairActions string `json:"proposed_repair_actions"`
    EventTime        time.Time `json:"event_time"`
}

// Alarm rules:
func (f *FaultManager) EvaluateAlarms() {
    // High failure rate: >10% failure rate over 5 min
    rate := f.calculateFailureRate(5 * time.Minute)
    if rate > 0.10 {
        f.raiseAlarm(AlarmNssaaHighAuthFailureRate, SeverityMAJOR, map[string]string{
            "failure_rate": fmt.Sprintf("%.2f%%", rate*100),
        })
    }

    // Circuit breaker open
    for _, cb := range f.circuitBreakers {
        if cb.State() == CB_OPEN {
            f.raiseAlarm(AlarmNssaaCircuitBreakerOpen, SeverityMAJOR, map[string]string{
                "aaa_server": cb.Server(),
            })
        }
    }
}
```

### 3.2 Configuration Management

```go
// YANG-based configuration (via RESTCONF/NETCONF)
type NssaaConfig struct {
    // AAA Server Configuration
    AaaServers []AaaServerConfig `json:"aaa-servers"`

    // EAP Configuration
    EapConfig EapConfig `json:"eap-config"`

    // Rate Limiting
    RateLimits RateLimitConfig `json:"rate-limits"`

    // Session Timeouts
    SessionTimeouts SessionTimeoutConfig `json:"session-timeouts"`
}

type AaaServerConfig struct {
    SnssaiSst int     `json:"snssai-sst"`
    SnssaiSd  string `json:"snssai-sd,omitempty"`
    Protocol  string `json:"protocol"`  // RADIUS or DIAMETER
    Host      string `json:"host"`
    Port      int    `json:"port"`
    Secret    string `json:"secret"`  // encrypted
    Priority  int    `json:"priority"`
    Enabled   bool   `json:"enabled"`
}
```

```yaml
# NETCONF/YANG configuration example
<rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target>
      <running/>
    </target>
    <config>
      <nssaa-function xmlns="urn:3gpp:ts:ts_28_541">
        <nssaa-function>
          <aaa-servers>
            <server>
              <snssai-sst>1</snssai-sst>
              <snssai-sd>000001</snssai-sd>
              <protocol>RADIUS</protocol>
              <host>aaa-server-1.operator.com</host>
              <port>1812</port>
              <priority>100</priority>
              <enabled>true</enabled>
            </server>
          </aaa-servers>
          <eap-config>
            <max-rounds>20</max-rounds>
            <round-timeout-seconds>30</round-timeout-seconds>
            <supported-methods>
              <method>EAP-TLS</method>
              <method>EAP-TTLS</method>
            </supported-methods>
          </eap-config>
        </nssaa-function>
      </nssaa-function>
    </config>
  </edit-config>
</rpc>
```

---

## 4. RESTCONF API

### 4.1 NSSAA Function Management

```go
// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function
GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function

// Response:
{
  "3gpp-nssaaf-nrm:nssaa-function": {
    "nssaa-function": [
      {
        "managed-element-id": "nssaa-1",
        "pLMNInfoList": ["208001"],
        "sBIFQDN": "nssAAF.operator.com",
        "commModelList": ["HTTP2_SBI"],
        "nssaaInfo": {
          "supiRanges": ["208001*"],
          "supportedSecurityAlgo": ["EAP-TLS", "EAP-TTLS"]
        },
        "epN58": [{ "endpoint-id": "n58-1", "localAddress": "10.0.1.50" }],
        "epN59": [{ "endpoint-id": "n59-1", "localAddress": "10.0.1.50" }]
      }
    ]
  }
}
```

### 4.2 Statistics

```go
// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function=.../performance-data
GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function/nssaa-function=1/performance-data

{
  "performance-data": {
    "authTotal": 1000000,
    "authSuccess": 990000,
    "authFailure": 10000,
    "authPending": 150,
    "avgLatencyMs": 45,
    "p99LatencyMs": 120,
    "activeSessions": 5000,
    "aaaServerStats": [
      { "serverId": "aaa-1", "requests": 500000, "failures": 1000, "avgLatencyMs": 30 }
    ]
  }
}
```

---

## 5. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | NSSAAFFunction IOC với tất cả attributes | TS 28.541 §5.3.145 |
| AC2 | EP_N58 và EP_N59 endpoint definitions | TS 28.541 §5.3.147-148 |
| AC3 | YANG model cho configuration | NETCONF/RESTCONF |
| AC4 | Alarm raised khi failure rate > 10% | FaultManager |
| AC5 | Alarm raised khi circuit breaker open | FaultManager |
| AC6 | Performance data exposed via RESTCONF | Performance data endpoint |
