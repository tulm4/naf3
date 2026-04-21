package proto

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// mockBizAAAClient is a test double implementing BizAAAClient.
type mockBizAAAClient struct {
	forwardCalled bool
	forwardReq    *AaaForwardRequest
	forwardResp   *AaaForwardResponse
	forwardErr    error
}

func (m *mockBizAAAClient) ForwardEAP(ctx context.Context, req *AaaForwardRequest) (*AaaForwardResponse, error) {
	m.forwardCalled = true
	m.forwardReq = req
	return m.forwardResp, m.forwardErr
}

func TestAaaForwardRequest_JSONRoundtrip(t *testing.T) {
	req := &AaaForwardRequest{
		Version:       "1.0",
		SessionID:     "nssAAF;123;auth123",
		AuthCtxID:     "auth123",
		TransportType: TransportRADIUS,
		Sst:           1,
		Sd:            "010203",
		Direction:     DirectionClientInitiated,
		Payload:       []byte{1, 2, 3, 4},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got AaaForwardRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.Version != req.Version {
		t.Errorf("Version: got %q, want %q", got.Version, req.Version)
	}
	if got.SessionID != req.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, req.SessionID)
	}
	if got.AuthCtxID != req.AuthCtxID {
		t.Errorf("AuthCtxID: got %q, want %q", got.AuthCtxID, req.AuthCtxID)
	}
	if got.TransportType != req.TransportType {
		t.Errorf("TransportType: got %v, want %v", got.TransportType, req.TransportType)
	}
	if got.Sst != req.Sst {
		t.Errorf("Sst: got %d, want %d", got.Sst, req.Sst)
	}
	if got.Sd != req.Sd {
		t.Errorf("Sd: got %q, want %q", got.Sd, req.Sd)
	}
	if got.Direction != req.Direction {
		t.Errorf("Direction: got %v, want %v", got.Direction, req.Direction)
	}
	if string(got.Payload) != string(req.Payload) {
		t.Errorf("Payload: got %v, want %v", got.Payload, req.Payload)
	}
}

func TestAaaForwardResponse_JSONRoundtrip(t *testing.T) {
	resp := &AaaForwardResponse{
		Version:   "1.0",
		SessionID: "nssAAF;123;auth123",
		AuthCtxID: "auth123",
		Payload:   []byte{5, 6, 7, 8},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got AaaForwardResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.Version != resp.Version {
		t.Errorf("Version: got %q, want %q", got.Version, resp.Version)
	}
	if got.SessionID != resp.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, resp.SessionID)
	}
	if got.AuthCtxID != resp.AuthCtxID {
		t.Errorf("AuthCtxID: got %q, want %q", got.AuthCtxID, resp.AuthCtxID)
	}
	if string(got.Payload) != string(resp.Payload) {
		t.Errorf("Payload: got %v, want %v", got.Payload, resp.Payload)
	}
}

func TestAaaServerInitiatedRequest_JSONRoundtrip(t *testing.T) {
	req := &AaaServerInitiatedRequest{
		Version:       "1.0",
		SessionID:     "nssAAF;456;auth456",
		AuthCtxID:     "auth456",
		TransportType: TransportDIAMETER,
		MessageType:   MessageTypeASR,
		Payload:       []byte{9, 10, 11},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got AaaServerInitiatedRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.Version != req.Version {
		t.Errorf("Version: got %q, want %q", got.Version, req.Version)
	}
	if got.SessionID != req.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, req.SessionID)
	}
	if got.AuthCtxID != req.AuthCtxID {
		t.Errorf("AuthCtxID: got %q, want %q", got.AuthCtxID, req.AuthCtxID)
	}
	if got.TransportType != req.TransportType {
		t.Errorf("TransportType: got %v, want %v", got.TransportType, req.TransportType)
	}
	if got.MessageType != req.MessageType {
		t.Errorf("MessageType: got %v, want %v", got.MessageType, req.MessageType)
	}
	if string(got.Payload) != string(req.Payload) {
		t.Errorf("Payload: got %v, want %v", got.Payload, req.Payload)
	}
}

func TestBizAAAClient_Interface(t *testing.T) {
	// Verify that mockBizAAAClient satisfies BizAAAClient.
	var _ BizAAAClient = &mockBizAAAClient{}

	mock := &mockBizAAAClient{
		forwardResp: &AaaForwardResponse{
			Version:   "1.0",
			SessionID: "test-session",
			AuthCtxID: "test-auth",
			Payload:   []byte{1, 2, 3},
		},
	}

	ctx := context.Background()
	req := &AaaForwardRequest{
		Version:       "1.0",
		SessionID:     "test-session",
		AuthCtxID:     "test-auth",
		TransportType: TransportRADIUS,
		Direction:     DirectionClientInitiated,
		Payload:       []byte{4, 5, 6},
	}

	resp, err := mock.ForwardEAP(ctx, req)
	if err != nil {
		t.Errorf("ForwardEAP error: %v", err)
	}
	if !mock.forwardCalled {
		t.Error("ForwardEAP was not called")
	}
	if mock.forwardReq != req {
		t.Error("forwardReq does not match input request")
	}
	if resp.Version != "1.0" {
		t.Errorf("Version: got %q, want %q", resp.Version, "1.0")
	}
}

func TestTransportTypeConstants(t *testing.T) {
	if TransportRADIUS != "RADIUS" {
		t.Errorf("TransportRADIUS: got %q, want %q", TransportRADIUS, "RADIUS")
	}
	if TransportDIAMETER != "DIAMETER" {
		t.Errorf("TransportDIAMETER: got %q, want %q", TransportDIAMETER, "DIAMETER")
	}
}

func TestDirectionConstants(t *testing.T) {
	if DirectionClientInitiated != "CLIENT_INITIATED" {
		t.Errorf("DirectionClientInitiated: got %q, want %q", DirectionClientInitiated, "CLIENT_INITIATED")
	}
	if DirectionServerInitiated != "SERVER_INITIATED" {
		t.Errorf("DirectionServerInitiated: got %q, want %q", DirectionServerInitiated, "SERVER_INITIATED")
	}
}

func TestMessageTypeConstants(t *testing.T) {
	if MessageTypeRAR != "RAR" {
		t.Errorf("MessageTypeRAR: got %q, want %q", MessageTypeRAR, "RAR")
	}
	if MessageTypeASR != "ASR" {
		t.Errorf("MessageTypeASR: got %q, want %q", MessageTypeASR, "ASR")
	}
	if MessageTypeCoA != "COA" {
		t.Errorf("MessageTypeCoA: got %q, want %q", MessageTypeCoA, "COA")
	}
}

func TestDefaultPayloadTTL(t *testing.T) {
	if DefaultPayloadTTL != 10*time.Minute {
		t.Errorf("DefaultPayloadTTL: got %v, want %v", DefaultPayloadTTL, 10*time.Minute)
	}
}
