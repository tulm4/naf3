---
spec: TS 29.561 Ch.16-17
section: Ch.16-17
interface: N/A (NSSAAF ↔ AAA-S)
service: AAA Proxy
operation: N/A (internal routing)
---

# NSSAAF AAA Proxy Design

## 1. Overview

AAA Proxy (AAA-P) là component trong NSSAAF hỗ trợ routing đến third-party AAA-S. Theo TS 29.561, AAA-P được yêu cầu khi AAA-S belongs to a third party.

**Modes:**
1. **Direct mode:** NSSAAF ↔ AAA-S direct (AAA-P not present)
2. **Proxy mode:** NSSAAF ↔ AAA-P ↔ AAA-S (AAA-P present for third-party)

---

## 2. Routing Logic

```go
type AAAProxyRouter struct {
    config *AAAConfig
    cache  *RedisCache
}

type RouteDecision struct {
    Mode         RoutingMode  // DIRECT or PROXY
    TargetHost   string
    TargetPort   int
    Protocol     string       // RADIUS or DIAMETER
    Timeout      time.Duration
}

func (r *AAAProxyRouter) ResolveRoute(ctx *EapContext) (*RouteDecision, error) {
    // Lookup AAA config by S-NSSAI (3-level fallback)
    config, err := r.getAaaConfig(ctx.Snssai)
    if err != nil {
        return nil, err
    }

    // Determine routing mode
    if config.AaaProxyHost != "" {
        // Proxy mode
        return &RouteDecision{
            Mode:       ROUTING_MODE_PROXY,
            TargetHost: config.AaaProxyHost,
            TargetPort: config.AaaProxyPort,
            Protocol:   config.Protocol,
            Timeout:    config.ProxyTimeout,
        }, nil
    }

    // Direct mode
    return &RouteDecision{
        Mode:       ROUTING_MODE_DIRECT,
        TargetHost: config.AaaServerHost,
        TargetPort: config.AaaServerPort,
        Protocol:   config.Protocol,
        Timeout:    config.ServerTimeout,
    }, nil
}
```

---

## 3. S-NSSAI → ENSI Mapping

Khi AAA-S là third-party, NSSAAF có thể map S-NSSAI sang ENSI (External Network Slice Information) trước khi forward:

```go
// ENSI mapping (operator policy dependent)
type ENSIMapper struct {
    mappings map[string]string  // S-NSSAI → ENSI
}

func (m *ENSIMapper) MapSnssaiToEnsi(snssai Snssai) string {
    key := fmt.Sprintf("%d:%s", snssai.Sst, snssai.Sd)
    if ensi, ok := m.mappings[key]; ok {
        return ensi
    }
    // No mapping: use original S-NSSAI
    return key
}
```

---

## 4. Protocol Passthrough

```go
// Proxy mode: relay without modification (except routing headers)
func (p *AAAProxy) RelayRADIUS(ctx context.Context, in []byte) ([]byte, error) {
    // Parse incoming RADIUS packet
    req, err := p.radiusCodec.Decode(in)
    if err != nil {
        return nil, err
    }

    // Optionally: add proxy-specific attributes
    if p.config.AddProxyAttr {
        req.AddAttribute(VSA{
            VendorId:  10415,
            VendorType: 201,  // NSSAAF-Proxy-Info
            Data:       []byte(p.config.ProxyIdentifier),
        })
    }

    // Forward to AAA-P
    resp, err := p.aaaClient.Request(ctx, req)
    if err != nil {
        return nil, err
    }

    return p.radiusCodec.Encode(resp)
}
```
