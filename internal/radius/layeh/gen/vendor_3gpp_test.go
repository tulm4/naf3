package gen

import (
	"testing"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func TestNSSAI_Pack(t *testing.T) {
	tests := []struct {
		name  string
		nssai NSSAI
		want  []byte
	}{
		{
			name:  "SST with SD",
			nssai: NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
			want:  []byte{1, 0x00, 0x01, 0x02},
		},
		{
			name:  "SST only (SD zeros)",
			nssai: NSSAI{SST: 128, SD: [3]byte{0, 0, 0}},
			want:  []byte{128, 0, 0, 0},
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
			name:    "valid SST+SD (4-byte data)",
			data:    []byte{1, 0x00, 0x01, 0x02},
			want:    NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
			wantErr: false,
		},
		{
			name:    "empty data",
			data:    []byte{},
			want:    NSSAI{},
			wantErr: true,
		},
		{
			name:    "SST-only (1-byte data)",
			data:    []byte{128},
			want:    NSSAI{SST: 128, SD: [3]byte{0, 0, 0}},
			wantErr: false,
		},
		{
			name:    "SST + partial SD (2-byte data)",
			data:    []byte{128, 0x01},
			want:    NSSAI{SST: 128, SD: [3]byte{0, 0, 0}},
			wantErr: false,
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

func TestNSSAI_Unpack_SSTOnly(t *testing.T) {
	// SST-only: 1 byte data (Length=3 on wire means 1 byte SST per TS 29.561 §16.3.2)
	var n NSSAI
	err := n.Unpack([]byte{128})
	if err != nil {
		t.Fatalf("Unpack SST-only: %v", err)
	}
	if n.SST != 128 {
		t.Errorf("SST = %d, want 128", n.SST)
	}
	if n.SD != [3]byte{0, 0, 0} {
		t.Errorf("SD = %x, want 000000", n.SD)
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

func TestAddNSSAIAttribute_WireFormat(t *testing.T) {
	pkt := radius.New(radius.CodeAccessRequest, []byte("testing123"))
	nssai := NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}}

	if err := AddNSSAIAttribute(pkt, nssai); err != nil {
		t.Fatalf("AddNSSAIAttribute: %v", err)
	}

	// Walk all AVPs and find the Vendor-Specific attribute (Type=26) with
	// 3GPP vendor ID 10415 (0x000028AF).
	for _, avp := range pkt.Attributes {
		if avp.Type != rfc2865.VendorSpecific_Type {
			continue
		}
		raw := []byte(avp.Attribute)
		// raw format: Vendor-ID(4) + sub-TLVs
		if len(raw) < 6 {
			continue
		}
		// Check 3GPP Vendor ID = 10415 (0x000028AF), big-endian
		if raw[0] != 0x00 || raw[1] != 0x00 || raw[2] != 0x28 || raw[3] != 0xAF {
			continue
		}
		// Sub-TLV: Type(1) + Length(1) + Value(n)
		subType := raw[4]
		subLen := raw[5]
		subValue := raw[6:]

		if subType != 200 {
			t.Errorf("sub-TLV type = %d, want 200", subType)
		}
		if subLen != 6 {
			t.Errorf("sub-TLV length = %d, want 6", subLen)
		}
		// Length field includes Type+Length (2 bytes) + Value.
		// sub-TLV Length=6 means 4 bytes of value (SST + SD).
		if len(subValue) != 4 {
			t.Fatalf("sub-TLV value length = %d, want 4", len(subValue))
		}
		// Value: SST(1) + SD(3)
		if subValue[0] != 1 {
			t.Errorf("SST = %d, want 1", subValue[0])
		}
		if subValue[1] != 0x00 || subValue[2] != 0x01 || subValue[3] != 0x02 {
			t.Errorf("SD = %x, want 000102", subValue[1:4])
		}
		return // found and verified
	}
	t.Fatal("3GPP VSA not found in packet")
}

func TestGetNSSAIAttributes_NoNSSAI(t *testing.T) {
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
