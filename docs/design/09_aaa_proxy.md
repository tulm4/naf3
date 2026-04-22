---
spec: TS 29.561 Ch.16-17
section: Ch.16-17
interface: N/A (NSSAAF ↔ AAA-S)
service: AAA Proxy
operation: N/A (internal routing)
---

# NSSAAF AAA Proxy Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, the "AAA Proxy" logic (routing, S-NSSAI → ENSI mapping, protocol passthrough) lives in the **Biz Pod** (`internal/eap/`). The **AAA Gateway** (`cmd/aaa-gateway/`) handles only raw socket I/O. See `docs/design/01_service_model.md` §5.4 for the architecture overview.

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

> **Note (Phase R):** In the 3-component model, this protocol passthrough logic runs in the **Biz Pod** (not in the AAA Gateway). The Biz Pod encodes/decodes EAP and communicates with the AAA Gateway via HTTP. See `01_service_model.md` §5.4.3 for component responsibilities.

```go
// Proxy mode: Biz Pod relays EAP messages via HTTP to AAA Gateway
// The AAA Gateway forwards raw RADIUS/Diameter bytes to/from AAA-S
func (p *AAARelay) RelayToAAAGateway(ctx context.Context, rawPacket []byte, configID string) ([]byte, error) {
    // Build forward request to AAA Gateway
    req := &proto.AaaForwardRequest{
        ConfigId: configID,
        RawPacket: rawPacket,
        Protocol:  "RADIUS", // or "DIAMETER"
    }

    // Send via HTTP to AAA Gateway
    resp, err := p.httpAAAClient.ForwardAAA(ctx, req)
    if err != nil {
        return nil, err
    }

    return resp.RawPacket, nil
}

// For proxy-mode (AAA-P), Biz Pod may add proxy-specific attributes
func (p *AAARelay) AddProxyAttributes(req *radius.Packet, config *AAAConfig) error {
    if config.AddProxyAttr {
        req.AddAttribute(VSA{
            VendorId:  10415,
            VendorType: 201,  // NSSAAF-Proxy-Info
            Data:       []byte(config.ProxyIdentifier),
        })
    }
    return nil
}
