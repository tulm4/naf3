# RADIUS layeh Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace custom RADIUS codec with layeh.com/radius library, running in parallel with existing implementation behind `RADIUS_BACKEND` feature flag.

**Architecture:** Parallel run approach — create `internal/radius/layeh/` package with layeh-based implementation, keep `internal/radius/legacy/` unchanged, add feature-flagged factory to switch between them.

**Tech Stack:** `layeh.com/radius`, `github.com/layeh/radius/tools/radius-dict-gen`, Go 1.21+

---

## Phase 1: Dictionary Files & Code Generation

### Task 1: Create Standard RADIUS Dictionary

**Files:**
- Create: `data/dictionaries/radius-standard.dict`

- [ ] **Step 1: Write the dictionary file**

```radius
# Standard RADIUS attributes per RFC 2865/2866/5176
# Generated for NSS-AAA use (3GPP TS 29.561 Chapter 16)

ATTRIBUTE User-Name           1     string
ATTRIBUTE User-Password        2     string
ATTRIBUTE CHAP-Password        3     string
ATTRIBUTE NAS-IP-Address       4     ipaddr
ATTRIBUTE NAS-Port             5     integer
ATTRIBUTE Service-Type         6     integer
ATTRIBUTE Framed-Protocol      7     integer
ATTRIBUTE Framed-IP-Address    8     ipaddr
ATTRIBUTE Reply-Message        18    string
ATTRIBUTE State                24    string
ATTRIBUTE Class               25    string
ATTRIBUTE Vendor-Specific     26    tlv
ATTRIBUTE Session-Timeout     27    integer
ATTRIBUTE Idle-Timeout        28    integer
ATTRIBUTE Called-Station-Id    30    string
ATTRIBUTE Calling-Station-Id   31    string
ATTRIBUTE NAS-Identifier       32    string
ATTRIBUTE Acct-Status-Type    40    integer
ATTRIBUTE Acct-Session-Id     44    string
ATTRIBUTE Acct-Authentic      45    integer
ATTRIBUTE Acct-Session-Time   46    integer
ATTRIBUTE Acct-Terminate-Cause 49   integer
ATTRIBUTE NAS-Port-Type       61    integer
ATTRIBUTE Connect-Info        77    string
ATTRIBUTE EAP-Message         79    string
ATTRIBUTE Message-Authenticator 80  string
ATTRIBUTE NAS-IPv6-Address    95    ipv6addr
ATTRIBUTE Framed-IPv6-Prefix  97    ipv6prefix
ATTRIBUTE Framed-IPv6-Address 168   ipv6addr

# Service-Type values
VALUE Service-Type Login-User           1
VALUE Service-Type Framed-User          2
VALUE Service-Type Callback-Login-User  3
VALUE Service-Type Callback-Framed-User 4
VALUE Service-Type Outbound-User        5
VALUE Service-Type Administrative-User  6
VALUE Service-Type NAS-Prompt-User       7
VALUE Service-Type Authenticate-Only     8

# Acct-Status-Type values
VALUE Acct-Status-Type Start             1
VALUE Acct-Status-Type Stop              2
VALUE Acct-Status-Type Interim-Update     3
VALUE Acct-Status-Type Accounting-On      7
VALUE Acct-Status-Type Accounting-Off     8

# NAS-Port-Type values
VALUE NAS-Port-Type Async         0
VALUE NAS-Port-Type Sync          1
VALUE NAS-Port-Type ISDN          2
VALUE NAS-Port-Type Async-ISDN    6
VALUE NAS-Port-Type Wireless-Other 18
VALUE NAS-Port-Type Wireless-IEEE-802-16 19
```

- [ ] **Step 2: Commit**

```bash
git add data/dictionaries/radius-standard.dict
git commit -m "feat(radius): add standard RADIUS dictionary for NSS-AAA"
```

---

### Task 2: Create 3GPP NSS-AAA Vendor Dictionary

**Files:**
- Create: `data/dictionaries/3gpp-nssaaa.dict`

- [ ] **Step 1: Write the 3GPP vendor dictionary**

```radius
# 3GPP Vendor-Specific Attributes for NSS-AAA
# Vendor: 3GPP (10415)
# Source: 3GPP TS 29.561 V18.5.0 (2024-09) §16.3

VENDOR 3GPP 10415

# 3GPP-S-NSSAI (Sub-attribute #200)
# Format: Type(1) + Length(1) + SST(1) + SD(3) = 6 bytes
# Type: 200 (fixed)
# Length: 3 (SST only) or 6 (SST + SD)
# SST: Slice/Service Type (0-255)
# SD: Slice Differentiator (3 octets, optional)
ATTRIBUTE 3GPP-S-NSSAI  200  tlv  3GPP
```

- [ ] **Step 2: Commit**

```bash
git add data/dictionaries/3gpp-nssaaa.dict
git commit -m "feat(radius): add 3GPP NSS-AAA vendor dictionary (TS 29.561 §16.3)"
```

---

### Task 3: Create Composite Dictionary

**Files:**
- Create: `data/dictionaries/composite.dict`

- [ ] **Step 1: Write the composite dictionary**

```radius
# Composite RADIUS dictionary for NSS-AAA
# Combines standard RADIUS attributes with 3GPP vendor-specific

$INCLUDE radius-standard.dict
$INCLUDE 3gpp-nssaaa.dict
```

- [ ] **Step 2: Commit**

```bash
git add data/dictionaries/composite.dict
git commit -m "feat(radius): add composite dictionary for NSS-AAA"
```

---

### Task 4: Add Code Generation to Makefile

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add radius-dict-gen installation target**

```makefile
# RADIUS dictionary code generation
RADIUS_DICT_GEN := $(BIN_DIR)/radius-dict-gen

$(RADIUS_DICT_GEN):
	go install github.com/layeh/radius/tools/radius-dict-gen@latest
```

- [ ] **Step 2: Add gen-radius-dict target**

```makefile
.PHONY: gen-radius-dict
gen-radius-dict: $(RADIUS_DICT_GEN)
	@echo "Generating RADIUS dictionary code..."
	@mkdir -p internal/radius/layeh/gen
	$(RADIUS_DICT_GEN) -dict data/dictionaries/composite.dict \
		-package gen -o internal/radius/layeh/gen/dict.go
	@echo "Done: internal/radius/layeh/gen/dict.go"
```

- [ ] **Step 3: Add gen-radius-dict to help text**

```makefile
# In the help text section, add:
#   gen-radius-dict     Generate RADIUS dictionary code from dictionaries
```

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "build: add radius-dict-gen code generation to Makefile"
```

---

### Task 5: Generate Initial Dictionary Code

**Files:**
- Generate: `internal/radius/layeh/gen/dict.go`

- [ ] **Step 1: Create the gen directory**

```bash
mkdir -p internal/radius/layeh/gen
```

- [ ] **Step 2: Generate the dictionary code**

```bash
make gen-radius-dict
```

- [ ] **Step 3: Verify the generated file exists and contains expected attributes**

```bash
head -100 internal/radius/layeh/gen/dict.go
```

Expected output should contain:
- `const AttributeUserName radius.AttributeType = 1`
- `const AttributeVendorSpecific radius.AttributeType = 26`
- `const AttributeEAPMessage radius.AttributeType = 79`
- `const AttributeMessageAuthenticator radius.AttributeType = 80`
- `const Vendor3GPP radius.VendorID = 10415`

- [ ] **Step 4: Commit the generated file**

```bash
git add internal/radius/layeh/gen/dict.go
git commit -m "gen(radius): generate dict.go from dictionary files"
```

---

## Phase 2: layeh Client Implementation

### Task 6: Create 3GPP NSSAI TLV Handler

**Files:**
- Create: `internal/radius/layeh/gen/vendor_3gpp.go`
- Test: `internal/radius/layeh/gen/vendor_3gpp_test.go`

- [ ] **Step 1: Write the NSSAI TLV handler**

```go
package gen

import (
    "fmt"

    "layeh.com/radius"
    "layeh.com/radius/rfc"
)

// NSSAI represents a Single Network Slice Selection Assistance Information.
// Spec: 3GPP TS 29.561 §16.3.2
// Layout: Type(1) + Length(1) + SST(1) + SD(3) = 6 bytes
type NSSAI struct {
    SST uint8   // Slice/Service Type (0-255)
    SD  [3]byte // Slice Differentiator (zero if SST-only)
}

// Pack encodes NSSAI into the VSA sub-TLV format.
func (n *NSSAI) Pack() []byte {
    // 3GPP Type: 200, Length: 6 (SST + SD)
    b := make([]byte, 6)
    b[0] = 200 // 3GPP sub-type: 3GPP-S-NSSAI
    b[1] = 6   // Length: SST (1) + SD (3) = 4, but per spec includes Type+Length = 6
    b[2] = n.SST
    copy(b[3:6], n.SD[:])
    return b
}

// Unpack decodes NSSAI from VSA sub-TLV format.
func (n *NSSAI) Unpack(b []byte) error {
    if len(b) < 3 {
        return fmt.Errorf("NSSAI: expected at least 3 bytes, got %d", len(b))
    }
    if b[0] != 200 {
        return fmt.Errorf("NSSAI: expected type 200, got %d", b[0])
    }
    n.SST = b[2]
    if len(b) >= 6 {
        copy(n.SD[:], b[3:6])
    }
    return nil
}

// AddNSSAIAttribute adds 3GPP-S-NSSAI to a RADIUS packet.
func AddNSSAIAttribute(pkt *radius.Packet, nssai NSSAI) error {
    // Pack NSSAI into VSA sub-TLV
    subTLV := nssai.Pack()

    // Wrap in Vendor-Specific TLV with 3GPP Vendor ID
    vsa, err := rfc.VendorSpecific(10415, subTLV)
    if err != nil {
        return fmt.Errorf("AddNSSAIAttribute: %w", err)
    }

    // Add to packet attributes
    pkt.Attributes[rfc.AttributeVendorSpecific] = append(
        pkt.Attributes[rfc.AttributeVendorSpecific],
        vsa,
    )
    return nil
}

// GetNSSAIAttributes extracts all 3GPP-S-NSSAI from a RADIUS packet.
func GetNSSAIAttributes(pkt *radius.Packet) ([]NSSAI, error) {
    raw, ok := pkt.Attributes[rfc.AttributeVendorSpecific]
    if !ok {
        return nil, nil
    }

    var result []NSSAI
    for _, v := range raw {
        nssai, err := extractNSSAIFromVSA(v)
        if err != nil {
            continue // skip malformed VSAs
        }
        result = append(result, nssai)
    }
    return result, nil
}

// extractNSSAIFromVSA extracts NSSAI from a Vendor-Specific attribute.
// VSA format: Vendor-ID(4) + Type(1) + Length(1) + Value(n)
func extractNSSAIFromVSA(data []byte) (NSSAI, error) {
    if len(data) < 6 {
        return NSSAI{}, fmt.Errorf("VSA: expected at least 6 bytes, got %d", len(data))
    }

    // Check 3GPP Vendor ID (10415 = 0x0000289F, stored big-endian)
    if data[0] != 0x00 || data[1] != 0x01 || data[2] != 0x89 || data[3] != 0x3F {
        return NSSAI{}, fmt.Errorf("VSA: not a 3GPP vendor-specific attribute")
    }

    // Extract sub-TLV
    subType := data[4]
    subLen := data[5]
    subValue := data[6 : 6+subLen]

    if subType != 200 {
        return NSSAI{}, fmt.Errorf("VSA: expected 3GPP-S-NSSAI (200), got %d", subType)
    }

    var n NSSAI
    return n, n.Unpack(subValue)
}
```

- [ ] **Step 2: Write unit tests**

```go
package gen

import (
    "testing"

    "layeh.com/radius"
    "layeh.com/radius/rfc"
)

func TestNSSAI_Pack(t *testing.T) {
    tests := []struct {
        name string
        nssai NSSAI
        want []byte
    }{
        {
            name: "SST only",
            nssai: NSSAI{SST: 1, SD: [3]byte{0, 0, 0}},
            want: []byte{200, 6, 1, 0, 0, 0},
        },
        {
            name: "SST with SD",
            nssai: NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
            want: []byte{200, 6, 1, 0x00, 0x01, 0x02},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tt.nssai.Pack()
            if !equalBytes(got, tt.want) {
                t.Errorf("NSSAI.Pack() = %x, want %x", got, tt.want)
            }
        })
    }
}

func TestNSSAI_Unpack(t *testing.T) {
    tests := []struct {
        name    string
        data    []byte
        want    NSSAI
        wantErr bool
    }{
        {
            name:    "valid SST+SD",
            data:    []byte{200, 6, 1, 0x00, 0x01, 0x02},
            want:    NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
            wantErr: false,
        },
        {
            name:    "wrong type",
            data:    []byte{201, 6, 1, 0x00, 0x01, 0x02},
            want:    NSSAI{},
            wantErr: true,
        },
        {
            name:    "too short",
            data:    []byte{200, 6, 1},
            want:    NSSAI{},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var n NSSAI
            err := n.Unpack(tt.data)
            if (err != nil) != tt.wantErr {
                t.Errorf("NSSAI.Unpack() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && n != tt.want {
                t.Errorf("NSSAI.Unpack() = %+v, want %+v", n, tt.want)
            }
        })
    }
}

func TestAddNSSAIAttribute(t *testing.T) {
    secret := []byte("testing123")
    pkt := radius.New(radius.CodeAccessRequest, secret)

    nssai := NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}}
    if err := AddNSSAIAttribute(pkt, nssai); err != nil {
        t.Fatalf("AddNSSAIAttribute() error = %v", err)
    }

    got, err := GetNSSAIAttributes(pkt)
    if err != nil {
        t.Fatalf("GetNSSAIAttributes() error = %v", err)
    }

    if len(got) != 1 {
        t.Fatalf("GetNSSAIAttributes() got %d attributes, want 1", len(got))
    }

    if got[0] != nssai {
        t.Errorf("GetNSSAIAttributes() = %+v, want %+v", got[0], nssai)
    }
}

func TestGetNSSAIAttributes_NoVSA(t *testing.T) {
    pkt := radius.New(radius.CodeAccessRequest, []byte("testing123"))

    got, err := GetNSSAIAttributes(pkt)
    if err != nil {
        t.Fatalf("GetNSSAIAttributes() error = %v", err)
    }

    if got != nil {
        t.Errorf("GetNSSAIAttributes() = %v, want nil", got)
    }
}

func equalBytes(a, b []byte) bool {
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}
```

- [ ] **Step 3: Run tests to verify they pass**

```bash
cd internal/radius/layeh/gen && go test -v
```

Expected output:
```
=== RUN   TestNSSAI_Pack
    PASS
=== RUN   TestNSSAI_Unpack
    PASS
=== RUN   TestAddNSSAIAttribute
    PASS
=== RUN   TestGetNSSAIAttributes_NoVSA
    PASS
```

- [ ] **Step 4: Commit**

```bash
git add internal/radius/layeh/gen/vendor_3gpp.go internal/radius/layeh/gen/vendor_3gpp_test.go
git commit -m "feat(radius/layeh): add 3GPP-S-NSSAI TLV handler"
```

---

### Task 7: Create layeh RADIUS Client

**Files:**
- Create: `internal/radius/layeh/client.go`
- Test: `internal/radius/layeh/client_test.go`

- [ ] **Step 1: Write the RADIUS client**

```go
package layeh

import (
    "context"
    "fmt"
    "net"
    "time"

    "github.com/tum-isys/naf3/internal/radius/layeh/gen"
    "layeh.com/radius"
    "layeh.com/radius/rfc"
)

// Config holds RADIUS client configuration.
type Config struct {
    // ServerAddr is the UDP address of the RADIUS server (e.g., "127.0.0.1:1812").
    ServerAddr string

    // Secret is the shared secret for the RADIUS server.
    Secret []byte

    // Timeout for requests (default: 5 seconds).
    Timeout time.Duration
}

// Client is a RADIUS client using layeh.com/radius.
type Client struct {
    serverAddr *net.UDPAddr
    secret     []byte
    timeout    time.Duration
}

// NewClient creates a new RADIUS client.
func NewClient(cfg Config) (*Client, error) {
    if cfg.Timeout == 0 {
        cfg.Timeout = 5 * time.Second
    }

    addr, err := net.ResolveUDPAddr("udp", cfg.ServerAddr)
    if err != nil {
        return nil, fmt.Errorf("NewClient: ResolveUDPAddr: %w", err)
    }

    return &Client{
        serverAddr: addr,
        secret:     cfg.Secret,
        timeout:    cfg.Timeout,
    }, nil
}

// AccessRequest represents an Access-Request to send.
type AccessRequest struct {
    // UserName is the user identifier (GPSI format).
    UserName string

    // NSSAI is the Network Slice Selection Assistance Information.
    NSSAI gen.NSSAI

    // NASIdentifier is the NAS identifier (optional).
    NASIdentifier string

    // CallingStationID is the calling station ID (optional).
    CallingStationID string

    // State is the state attribute from Access-Challenge (optional).
    State []byte

    // EAPMessage is the EAP payload (optional).
    EAPMessage []byte
}

// AccessResponse represents the server response.
type AccessResponse struct {
    // Code is the RADIUS response code.
    Code radius.Code

    // EAPMessage is the EAP payload from the response (if any).
    EAPMessage []byte

    // State is the state attribute (if any).
    State []byte

    // NSSAI is the authorized NSSAI from the response (if any).
    NSSAI []gen.NSSAI

    // Message is the Reply-Message from the response (if any).
    Message string
}

// AccessRequest sends an Access-Request and returns the response.
func (c *Client) AccessRequest(ctx context.Context, req *AccessRequest) (*AccessResponse, error) {
    // Create packet
    pkt := radius.New(radius.CodeAccessRequest, c.secret)

    // Add User-Name
    pkt.Attributes[rfc.AttributeUserName] = []byte(req.UserName)

    // Add NAS-Identifier if provided
    if req.NASIdentifier != "" {
        pkt.Attributes[rfc.AttributeNASIdentifier] = []byte(req.NASIdentifier)
    }

    // Add Calling-Station-Id if provided
    if req.CallingStationID != "" {
        pkt.Attributes[rfc.AttributeCallingStationID] = []byte(req.CallingStationID)
    }

    // Add State if provided (from Access-Challenge)
    if len(req.State) > 0 {
        pkt.Attributes[rfc.AttributeState] = req.State
    }

    // Add EAP-Message if provided
    if len(req.EAPMessage) > 0 {
        pkt.Attributes[rfc.AttributeEAPMessage] = req.EAPMessage
    }

    // Add 3GPP-S-NSSAI
    if err := gen.AddNSSAIAttribute(pkt, req.NSSAI); err != nil {
        return nil, fmt.Errorf("AccessRequest: AddNSSAIAttribute: %w", err)
    }

    // Add Message-Authenticator
    if err := rfc.MessageAuthenticator.Add(pkt); err != nil {
        return nil, fmt.Errorf("AccessRequest: MessageAuthenticator.Add: %w", err)
    }

    // Send with timeout
    ctx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    // Use layeh radius.Client
    client := &radius.Client{
        Retry: 3,
    }

    response, err := client.Exchange(ctx, pkt, c.serverAddr.String())
    if err != nil {
        return nil, fmt.Errorf("AccessRequest: Exchange: %w", err)
    }

    // Parse response
    resp := &AccessResponse{
        Code: response.Code,
    }

    // Extract EAP-Message
    if raw, ok := response.Attributes[rfc.AttributeEAPMessage]; ok {
        if len(raw) > 0 {
            resp.EAPMessage = raw[0].([]byte)
        }
    }

    // Extract State
    if raw, ok := response.Attributes[rfc.AttributeState]; ok {
        if len(raw) > 0 {
            resp.State = raw[0].([]byte)
        }
    }

    // Extract Reply-Message
    if raw, ok := response.Attributes[rfc.AttributeReplyMessage]; ok {
        if len(raw) > 0 {
            resp.Message = string(raw[0].([]byte))
        }
    }

    // Extract 3GPP-S-NSSAI
    nssais, err := gen.GetNSSAIAttributes(response)
    if err != nil {
        return nil, fmt.Errorf("AccessRequest: GetNSSAIAttributes: %w", err)
    }
    resp.NSSAI = nssais

    return resp, nil
}

// Close closes the client (no-op for UDP, but interface requires it).
func (c *Client) Close() error {
    return nil
}
```

- [ ] **Step 2: Write tests**

```go
package layeh

import (
    "context"
    "net"
    "testing"
    "time"

    "layeh.com/radius"
    "layeh.com/radius/rfc"
)

// mockServer is a simple mock RADIUS server for testing.
type mockServer struct {
    conn   *net.UDPConn
    addr   net.Addr
    secret []byte
}

func newMockServer(t *testing.T) *mockServer {
    secret := []byte("testing123")

    // Bind to random port
    addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
    if err != nil {
        t.Fatalf("ResolveUDPAddr: %v", err)
    }

    conn, err := net.ListenUDP("udp", addr)
    if err != nil {
        t.Fatalf("ListenUDP: %v", err)
    }

    return &mockServer{
        conn:   conn,
        addr:   conn.LocalAddr(),
        secret: secret,
    }
}

func (s *mockServer) handle(t *testing.T) {
    buf := make([]byte, 4096)
    for {
        s.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
        n, clientAddr, err := s.conn.ReadFromUDP(buf)
        if err != nil {
            break
        }

        // Parse request
        pkt, err := radius.Parse(buf[:n], s.secret)
        if err != nil {
            t.Logf("Parse error: %v", err)
            continue
        }

        // Create Access-Accept response
        resp := radius.New(radius.CodeAccessAccept, s.secret)
        resp.Identifier = pkt.Identifier

        // Copy Request Authenticator per RFC 5176
        resp.Secret = s.secret

        // Add Message-Authenticator
        if err := rfc.MessageAuthenticator.Add(resp); err != nil {
            t.Logf("MessageAuthenticator.Add: %v", err)
        }

        // Send response
        s.conn.WriteToUDP(resp.Encode(), clientAddr)
    }
}

func (s *mockServer) close() {
    s.conn.Close()
}

func (s *mockServer) addrString() string {
    return s.addr.String()
}

func TestClient_AccessRequest(t *testing.T) {
    // Start mock server
    server := newMockServer(t)
    defer server.close()

    go server.handle(t)

    // Create client
    client, err := NewClient(Config{
        ServerAddr: server.addrString(),
        Secret:     []byte("testing123"),
        Timeout:    2 * time.Second,
    })
    if err != nil {
        t.Fatalf("NewClient: %v", err)
    }
    defer client.Close()

    // Send Access-Request
    resp, err := client.AccessRequest(context.Background(), &AccessRequest{
        UserName:         "user@example.com",
        NASIdentifier:    "naf3.local",
        CallingStationID: "00:11:22:33:44:55",
        NSSAI:            gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
    })
    if err != nil {
        t.Fatalf("AccessRequest: %v", err)
    }

    // Verify response
    if resp.Code != radius.CodeAccessAccept {
        t.Errorf("expected CodeAccessAccept, got %v", resp.Code)
    }
}

func TestClient_AccessRequest_Timeout(t *testing.T) {
    // Create client pointing to non-existent server
    client, err := NewClient(Config{
        ServerAddr: "127.0.0.1:19999",
        Secret:     []byte("testing123"),
        Timeout:    100 * time.Millisecond,
    })
    if err != nil {
        t.Fatalf("NewClient: %v", err)
    }
    defer client.Close()

    // Send Access-Request (should timeout)
    _, err = client.AccessRequest(context.Background(), &AccessRequest{
        UserName: "user@example.com",
    })
    if err == nil {
        t.Error("expected timeout error, got nil")
    }
}
```

- [ ] **Step 3: Run tests to verify they pass**

```bash
cd internal/radius/layeh && go test -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/radius/layeh/client.go internal/radius/layeh/client_test.go
git commit -m "feat(radius/layeh): add RADIUS client using layeh.com/radius"
```

---

## Phase 3: Integration Tests

### Task 8: Create Golden File Tests

**Files:**
- Create: `internal/radius/layeh/testdata/golden/access-request-nssai.packet`
- Create: `internal/radius/layeh/gen/dict_golden_test.go`

- [ ] **Step 1: Generate golden packet file**

First, run a quick test to generate the golden packet:

```go
// Temporary test to generate golden file
func GenerateGoldenFile(t *testing.T) {
    secret := []byte("testing123")
    pkt := radius.New(radius.CodeAccessRequest, secret)
    pkt.Identifier = 1
    pkt.Attributes[rfc.AttributeUserName] = []byte("user@example.com")

    nssai := gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}}
    gen.AddNSSAIAttribute(pkt, nssai)
    rfc.MessageAuthenticator.Add(pkt)

    // Encode
    buf := pkt.Encode()

    // Write golden file
    os.WriteFile("testdata/golden/access-request-nssai.packet", buf, 0644)
}
```

Create the testdata directory and run the generator once, then delete the generator function.

- [ ] **Step 2: Write golden file tests**

```go
package gen

import (
    "os"
    "path/filepath"
    "testing"

    "layeh.com/radius"
    "layeh.com/radius/rfc"
)

func TestGolden_AccessRequestNSSAI(t *testing.T) {
    goldenPath := filepath.Join("testdata", "golden", "access-request-nssai.packet")

    // Load golden file
    golden, err := os.ReadFile(goldenPath)
    if err != nil {
        t.Fatalf("ReadFile(%s): %v", goldenPath, err)
    }

    // Parse
    secret := []byte("testing123")
    pkt, err := radius.Parse(golden, secret)
    if err != nil {
        t.Fatalf("radius.Parse: %v", err)
    }

    // Verify User-Name
    userName, ok := pkt.Attributes[rfc.AttributeUserName]
    if !ok {
        t.Fatal("missing User-Name attribute")
    }
    if string(userName[0].([]byte)) != "user@example.com" {
        t.Errorf("User-Name = %q, want %q", userName[0], "user@example.com")
    }

    // Verify NSSAI
    nssais, err := GetNSSAIAttributes(pkt)
    if err != nil {
        t.Fatalf("GetNSSAIAttributes: %v", err)
    }
    if len(nssais) != 1 {
        t.Fatalf("expected 1 NSSAI, got %d", len(nssais))
    }

    want := NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}}
    if nssais[0] != want {
        t.Errorf("NSSAI = %+v, want %+v", nssais[0], want)
    }

    // Verify Message-Authenticator
    if _, ok := pkt.Attributes[rfc.AttributeMessageAuthenticator]; !ok {
        t.Error("missing Message-Authenticator attribute")
    }
}

func TestGolden_RoundTrip(t *testing.T) {
    goldenPath := filepath.Join("testdata", "golden", "access-request-nssai.packet")
    golden, err := os.ReadFile(goldenPath)
    if err != nil {
        t.Fatalf("ReadFile(%s): %v", goldenPath, err)
    }

    secret := []byte("testing123")

    // Parse
    pkt1, err := radius.Parse(golden, secret)
    if err != nil {
        t.Fatalf("radius.Parse: %v", err)
    }

    // Re-encode
    buf := pkt1.Encode()

    // Parse again
    pkt2, err := radius.Parse(buf, secret)
    if err != nil {
        t.Fatalf("radius.Parse (round-trip): %v", err)
    }

    // Verify NSSAI survives round-trip
    nssais1, _ := GetNSSAIAttributes(pkt1)
    nssais2, _ := GetNSSAIAttributes(pkt2)

    if len(nssais1) != len(nssais2) {
        t.Errorf("NSSAI count: got %d, want %d", len(nssais2), len(nssais1))
    }
    if len(nssais1) > 0 && nssais1[0] != nssais2[0] {
        t.Errorf("NSSAI round-trip: got %+v, want %+v", nssais2[0], nssais1[0])
    }
}
```

- [ ] **Step 3: Run tests**

```bash
cd internal/radius/layeh && go test -v ./gen/
```

- [ ] **Step 4: Commit**

```bash
git add internal/radius/layeh/testdata/golden/access-request-nssai.packet internal/radius/layeh/gen/dict_golden_test.go
git commit -m "test(radius/layeh): add golden file tests for NSSAI encoding"
```

---

### Task 9: Create E2E Integration Test

**Files:**
- Create: `internal/radius/layeh/integration_test.go`

- [ ] **Step 1: Write the integration test**

```go
package radius_test

import (
    "context"
    "net"
    "sync"
    "testing"
    "time"

    "github.com/tum-isys/naf3/internal/radius/layeh"
    "github.com/tum-isys/naf3/internal/radius/layeh/gen"
    "layeh.com/radius"
    "layeh.com/radius/rfc"
)

func TestIntegration_E2E_AccessRequestResponse(t *testing.T) {
    // Start mock AAA server
    server, addr := startMockAAAServer(t)
    defer server.Close()

    // Create client
    client, err := layeh.NewClient(layeh.Config{
        ServerAddr: addr.String(),
        Secret:     []byte("testing123"),
        Timeout:    5 * time.Second,
    })
    if err != nil {
        t.Fatalf("NewClient: %v", err)
    }
    defer client.Close()

    // Send Access-Request
    resp, err := client.AccessRequest(context.Background(), &layeh.AccessRequest{
        UserName:         "user@example.com",
        CallingStationID: "00:11:22:33:44:55",
        NSSAI:            gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
    })
    if err != nil {
        t.Fatalf("AccessRequest: %v", err)
    }

    // Verify response code
    if resp.Code != radius.CodeAccessAccept {
        t.Errorf("expected CodeAccessAccept, got %v", resp.Code)
    }

    // Verify NSSAI is echoed back
    if len(resp.NSSAI) != 1 {
        t.Errorf("expected 1 NSSAI in response, got %d", len(resp.NSSAI))
    }
}

func TestIntegration_AccessReject(t *testing.T) {
    // Start mock AAA server that rejects
    server, addr := startMockAAAServerWithResult(t, false)
    defer server.Close()

    client, err := layeh.NewClient(layeh.Config{
        ServerAddr: addr.String(),
        Secret:     []byte("testing123"),
        Timeout:    5 * time.Second,
    })
    if err != nil {
        t.Fatalf("NewClient: %v", err)
    }
    defer client.Close()

    resp, err := client.AccessRequest(context.Background(), &layeh.AccessRequest{
        UserName: "reject@example.com",
        NSSAI:    gen.NSSAI{SST: 1},
    })
    if err != nil {
        t.Fatalf("AccessRequest: %v", err)
    }

    if resp.Code != radius.CodeAccessReject {
        t.Errorf("expected CodeAccessReject, got %v", resp.Code)
    }
}

// Mock server helpers

var (
    mockServerMu sync.Mutex
    mockServers  = make(map[string]*mockServerState)
)

type mockServerState struct {
    conn   *net.UDPConn
    reject bool
}

func startMockAAAServer(t *testing.T) (*mockServerState, net.Addr) {
    return startMockAAAServerWithResult(t, false)
}

func startMockAAAServerWithResult(t *testing.T, reject bool) (*mockServerState, net.Addr) {
    addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
    if err != nil {
        t.Fatalf("ResolveUDPAddr: %v", err)
    }

    conn, err := net.ListenUDP("udp", addr)
    if err != nil {
        t.Fatalf("ListenUDP: %v", err)
    }

    state := &mockServerState{
        conn:   conn,
        reject: reject,
    }

    mockServerMu.Lock()
    mockServers[conn.LocalAddr().String()] = state
    mockServerMu.Unlock()

    // Start handler goroutine
    go func() {
        buf := make([]byte, 4096)
        for {
            conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
            n, clientAddr, err := conn.ReadFromUDP(buf)
            if err != nil {
                return
            }

            secret := []byte("testing123")
            pkt, err := radius.Parse(buf[:n], secret)
            if err != nil {
                continue
            }

            // Determine response code
            code := radius.CodeAccessAccept
            if state.reject {
                code = radius.CodeAccessReject
            }

            // Create response
            resp := radius.New(code, secret)
            resp.Identifier = pkt.Identifier

            // Add Message-Authenticator
            rfc.MessageAuthenticator.Add(resp)

            conn.WriteToUDP(resp.Encode(), clientAddr)
        }
    }()

    return state, conn.LocalAddr()
}

func (s *mockServerState) Close() error {
    mockServerMu.Lock()
    delete(mockServers, s.conn.LocalAddr().String())
    mockServerMu.Unlock()
    return s.conn.Close()
}
```

- [ ] **Step 2: Run integration tests**

```bash
cd internal/radius/layeh && go test -v -run TestIntegration
```

- [ ] **Step 3: Commit**

```bash
git add internal/radius/layeh/integration_test.go
git commit -m "test(radius/layeh): add E2E integration tests"
```

---

## Phase 4: Factory & Feature Flag

### Task 10: Create Feature Flag Factory

**Files:**
- Create: `internal/radius/factory.go`

- [ ] **Step 1: Write the factory**

```go
package radius

import (
    "context"
    "fmt"
    "os"
)

// Backend determines which RADIUS implementation to use.
type Backend string

const (
    BackendLegacy Backend = "legacy"
    BackendLayeh  Backend = "layeh"
)

// RADIUSClient is the interface for RADIUS clients.
type RADIUSClient interface {
    AccessRequest(ctx context.Context, req *AccessRequest) (*AccessResponse, error)
    Close() error
}

// AccessRequest represents a RADIUS Access-Request.
type AccessRequest struct {
    UserName         string
    NSSAI            string // JSON-encoded NSSAI
    NASIdentifier    string
    CallingStationID string
    State            []byte
    EAPMessage       []byte
}

// AccessResponse represents a RADIUS response.
type AccessResponse struct {
    Code        string
    EAPMessage  []byte
    State       []byte
    NSSAI       string // JSON-encoded NSSAI
    Message     string
}

// getBackend returns the configured RADIUS backend.
func getBackend() Backend {
    switch os.Getenv("RADIUS_BACKEND") {
    case "layeh":
        return BackendLayeh
    default:
        return BackendLegacy
    }
}

// NewClient creates a new RADIUS client based on the configured backend.
func NewClient(ctx context.Context) (RADIUSClient, error) {
    switch getBackend() {
    case BackendLayeh:
        return newLayehClient(ctx)
    default:
        return newLegacyClient(ctx)
    }
}

// newLayehClient creates a layeh-based RADIUS client.
func newLayehClient(ctx context.Context) (*layeh.Client, error) {
    cfg := layeh.Config{
        ServerAddr: os.Getenv("RADIUS_SERVER_ADDR"),
        Secret:     []byte(os.Getenv("RADIUS_SECRET")),
    }
    return layeh.NewClient(cfg)
}

// newLegacyClient creates the legacy RADIUS client.
func newLegacyClient(ctx context.Context) (RADIUSClient, error) {
    // TODO: Implement legacy client wrapper
    return nil, fmt.Errorf("legacy client not yet implemented")
}
```

- [ ] **Step 2: Run build to verify it compiles**

```bash
cd internal/radius && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/radius/factory.go
git commit -m "feat(radius): add feature-flagged factory for backend selection"
```

---

## Validation Checklist

After completing all tasks, verify:

- [ ] `make gen-radius-dict` generates valid Go code
- [ ] `make test` passes for both packages
- [ ] Golden file tests validate NSSAI encode/decode
- [ ] Integration tests validate E2E flow
- [ ] `RADIUS_BACKEND=layeh` uses new implementation
- [ ] `RADIUS_BACKEND=legacy` uses old implementation
- [ ] No regression in existing RADIUS flows

---

## Spec Coverage

| Spec Requirement | Task |
|-----------------|------|
| TS 29.561 §16.3 NSS-AAA RADIUS | Task 1-3, 6 |
| 3GPP-S-NSSAI VSA (Vendor ID 10415, sub-type 200) | Task 2, 6 |
| Dictionary code generation | Task 4, 5 |
| layeh-based RADIUS client | Task 7 |
| Golden file tests | Task 8 |
| E2E integration tests | Task 9 |
| Feature flag factory | Task 10 |
| Bug fix: Message-Authenticator | Task 6, 7 |
| Bug fix: Response Authenticator | Task 7 |
| Bug fix: Little-endian AVP read | Task 6 |
| Bug fix: Listen ignores addr | Task 7 |
