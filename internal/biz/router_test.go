package biz

import (
	"testing"

	"github.com/operator/nssAAF/internal/proto"
)

func TestBuildForwardRequest_RADIUS(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "aaa-server.example.com",
			Port:     1812,
		},
	}, nil)

	req, err := r.BuildForwardRequest("auth123", []byte{1, 2, 3, 4}, 1, "010203")
	if err != nil {
		t.Fatalf("BuildForwardRequest error: %v", err)
	}

	if req.TransportType != proto.TransportRADIUS {
		t.Errorf("TransportType: got %v, want %v", req.TransportType, proto.TransportRADIUS)
	}
	if req.AuthCtxID != "auth123" {
		t.Errorf("AuthCtxID: got %q, want %q", req.AuthCtxID, "auth123")
	}
	if req.Sst != 1 {
		t.Errorf("Sst: got %d, want %d", req.Sst, 1)
	}
	if req.Sd != "010203" {
		t.Errorf("Sd: got %q, want %q", req.Sd, "010203")
	}
	if req.Direction != proto.DirectionClientInitiated {
		t.Errorf("Direction: got %v, want %v", req.Direction, proto.DirectionClientInitiated)
	}
	if len(req.SessionID) == 0 {
		t.Error("SessionID should not be empty")
	}
}

func TestBuildForwardRequest_DIAMETER(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolDIAMETER,
			Host:     "diameter-server.example.com",
			Port:     3868,
		},
	}, nil)

	req, err := r.BuildForwardRequest("auth456", []byte{5, 6, 7}, 2, "AABBCC")
	if err != nil {
		t.Fatalf("BuildForwardRequest error: %v", err)
	}

	if req.TransportType != proto.TransportDIAMETER {
		t.Errorf("TransportType: got %v, want %v", req.TransportType, proto.TransportDIAMETER)
	}
	if req.AuthCtxID != "auth456" {
		t.Errorf("AuthCtxID: got %q, want %q", req.AuthCtxID, "auth456")
	}
}

func TestBuildForwardRequest_NoRoute(t *testing.T) {
	r := NewRouter(SnssaiConfig{}, nil)

	_, err := r.BuildForwardRequest("auth789", []byte{1}, 99, "FFFFFF")
	if err == nil {
		t.Error("expected error when no route configured")
	}
}

func TestResolveRoute_ThreeLevelLookup(t *testing.T) {
	// Exact config takes priority when non-nil.
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{Protocol: ProtocolRADIUS, Host: "default"},
		SST:     &ServerConfig{Protocol: ProtocolRADIUS, Host: "sst-only"},
		Exact:   &ServerConfig{Protocol: ProtocolDIAMETER, Host: "exact"},
	}, nil)
	dec := r.ResolveRoute(1, "010203")
	if dec == nil || dec.Host != "exact" {
		t.Errorf("exact config: got host=%v, want host=exact", dec)
	}

	// With Exact set, SST and Default are never reached.
	dec = r.ResolveRoute(1, "XXXXXX")
	if dec == nil || dec.Host != "exact" {
		t.Errorf("exact config (non-matching sd): got host=%v, want host=exact", dec)
	}

	// With no Exact but SST set, SST is returned (no SD matching logic).
	r2 := NewRouter(SnssaiConfig{
		Default: &ServerConfig{Protocol: ProtocolRADIUS, Host: "default"},
		SST:     &ServerConfig{Protocol: ProtocolRADIUS, Host: "sst-only"},
	}, nil)
	dec = r2.ResolveRoute(1, "XXXXXX")
	if dec == nil || dec.Host != "sst-only" {
		t.Errorf("sst-only lookup: got host=%v, want host=sst-only", dec)
	}

	// With no Exact and no SST, Default is returned.
	r3 := NewRouter(SnssaiConfig{
		Default: &ServerConfig{Protocol: ProtocolRADIUS, Host: "default"},
	}, nil)
	dec = r3.ResolveRoute(99, "FFFFFF")
	if dec == nil || dec.Host != "default" {
		t.Errorf("default lookup: got host=%v, want host=default", dec)
	}
}

func TestRouterStats(t *testing.T) {
	r := NewRouter(SnssaiConfig{}, nil)
	stats := r.Stats()
	if stats.HasRadius || stats.HasDiameter {
		t.Error("Biz Pod router should not have direct radius/diameter clients")
	}
}

func TestProxyMode_String(t *testing.T) {
	if ProxyModeDirect.String() != "DIRECT" {
		t.Errorf("ProxyModeDirect: got %q, want %q", ProxyModeDirect.String(), "DIRECT")
	}
	if ProxyModeProxy.String() != "PROXY" {
		t.Errorf("ProxyModeProxy: got %q, want %q", ProxyModeProxy.String(), "PROXY")
	}
}

func TestProtocol_String(t *testing.T) {
	if ProtocolRADIUS.String() != "RADIUS" {
		t.Errorf("ProtocolRADIUS: got %q, want %q", ProtocolRADIUS.String(), "RADIUS")
	}
	if ProtocolDIAMETER.String() != "DIAMETER" {
		t.Errorf("ProtocolDIAMETER: got %q, want %q", ProtocolDIAMETER.String(), "DIAMETER")
	}
}
