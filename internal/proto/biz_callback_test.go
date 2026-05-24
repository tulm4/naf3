package proto

import (
	"encoding/json"
	"testing"
)

func TestSessionCorrEntry_JSONRoundtrip(t *testing.T) {
	entry := &SessionCorrEntry{
		AuthCtxID: "auth456",
		PodID:     "biz-pod-1",
		Sst:       1,
		Sd:        "010203",
		CreatedAt: 1710000000,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got SessionCorrEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.AuthCtxID != entry.AuthCtxID {
		t.Errorf("AuthCtxID: got %q, want %q", got.AuthCtxID, entry.AuthCtxID)
	}
	if got.PodID != entry.PodID {
		t.Errorf("PodID: got %q, want %q", got.PodID, entry.PodID)
	}
	if got.Sst != entry.Sst {
		t.Errorf("Sst: got %d, want %d", got.Sst, entry.Sst)
	}
	if got.Sd != entry.Sd {
		t.Errorf("Sd: got %q, want %q", got.Sd, entry.Sd)
	}
	if got.CreatedAt != entry.CreatedAt {
		t.Errorf("CreatedAt: got %d, want %d", got.CreatedAt, entry.CreatedAt)
	}
}

func TestSessionCorrKey(t *testing.T) {
	tests := []struct {
		sessionID string
		want      string
	}{
		{"abc123", "nssaa:session:abc123"},
		{"nssAAF;123;auth", "nssaa:session:nssAAF;123;auth"},
		{"", "nssaa:session:"},
	}

	for _, tt := range tests {
		got := SessionCorrKey(tt.sessionID)
		if got != tt.want {
			t.Errorf("SessionCorrKey(%q): got %q, want %q", tt.sessionID, got, tt.want)
		}
	}
}

func TestRedisConstants(t *testing.T) {
	if SessionCorrKeyPrefix != "nssaa:session:" {
		t.Errorf("SessionCorrKeyPrefix: got %q, want %q", SessionCorrKeyPrefix, "nssaa:session:")
	}
	if PodsKey != "nssaa:pods" {
		t.Errorf("PodsKey: got %q, want %q", PodsKey, "nssaa:pods")
	}
}
