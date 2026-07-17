package gen

import (
	"testing"

	"layeh.com/radius"
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
			want:  []byte{200, 6, 1, 0x00, 0x01, 0x02},
		},
		{
			name:  "SST only (SD zeros)",
			nssai: NSSAI{SST: 128, SD: [3]byte{0, 0, 0}},
			want:  []byte{200, 6, 128, 0, 0, 0},
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
