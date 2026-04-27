package proto

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockBizServiceClient is a test double implementing BizServiceClient.
type mockBizServiceClient struct {
	forwardCalled     bool
	forwardPath       string
	forwardMethod     string
	forwardBody       []byte
	forwardRespBody   []byte
	forwardRespStatus int
	forwardRespErr    error
}

func (m *mockBizServiceClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	m.forwardCalled = true
	m.forwardPath = path
	m.forwardMethod = method
	m.forwardBody = body
	return m.forwardRespBody, m.forwardRespStatus, m.forwardRespErr
}

func TestBizServiceClient_Interface(t *testing.T) {
	// Verify that mockBizServiceClient satisfies BizServiceClient.
	var _ BizServiceClient = &mockBizServiceClient{}

	mock := &mockBizServiceClient{
		forwardRespBody:   []byte(`{"authCtxId":"test","status":"success"}`),
		forwardRespStatus: 201,
	}

	ctx := context.Background()
	body := []byte(`{"gpsi":"5123456789","snssai":{"sst":1,"sd":"010203"}}`)

	respBody, status, err := mock.ForwardRequest(ctx, "/nnssaaf-nssaa/v1/slice-authentications", "POST", body)
	if err != nil {
		t.Errorf("ForwardRequest error: %v", err)
	}
	if !mock.forwardCalled {
		t.Error("ForwardRequest was not called")
	}
	if mock.forwardPath != "/nnssaaf-nssaa/v1/slice-authentications" {
		t.Errorf("forwardPath: got %q, want %q", mock.forwardPath, "/nnssaaf-nssaa/v1/slice-authentications")
	}
	if mock.forwardMethod != "POST" {
		t.Errorf("forwardMethod: got %q, want %q", mock.forwardMethod, "POST")
	}
	if string(mock.forwardBody) != string(body) {
		t.Errorf("forwardBody: got %v, want %v", mock.forwardBody, body)
	}
	if status != 201 {
		t.Errorf("status: got %d, want %d", status, 201)
	}
	if string(respBody) != `{"authCtxId":"test","status":"success"}` {
		t.Errorf("respBody: got %q, want %q", string(respBody), `{"authCtxId":"test","status":"success"}`)
	}
}

func TestBizServiceClient_Error(t *testing.T) {
	expectedErr := errors.New("all biz pods failed")
	mock := &mockBizServiceClient{
		forwardRespErr: expectedErr,
	}

	ctx := context.Background()
	_, _, err := mock.ForwardRequest(ctx, "/test", "GET", nil)
	if err == nil {
		t.Error("expected error, got nil")
	}
	if err != expectedErr {
		t.Errorf("error: got %v, want %v", err, expectedErr)
	}
}

func TestAaaServerInitiatedResponse_JSONRoundtrip(t *testing.T) {
	resp := &AaaServerInitiatedResponse{
		Version:   "1.0",
		SessionID: "nssAAF;123;auth123",
		AuthCtxID: "auth123",
		Payload:   []byte{0x03, 0x00, 0x00, 0x0c}, // RAR-Nak
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got AaaServerInitiatedResponse
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
