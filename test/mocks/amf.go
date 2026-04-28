// Package mocks provides httptest.Server implementations of 3GPP NF APIs for integration testing.
package mocks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// NssaaNotification represents the AMF Nssaa-Notification payload.
// Spec: TS 23.502 §4.2.9.3, TS 29.518 §5.2.2.27
type NssaaNotification struct {
	NotificationType string `json:"notificationType"` // "SLICE_RE_AUTH" or "SLICE_REVOCATION"
	AuthCtxID        string `json:"authCtxId"`
	GPSI             string `json:"gpsi"`
	Snssai           struct {
		Sst int    `json:"sst"`
		Sd  string `json:"sd,omitempty"`
	} `json:"snssai"`
	ReAuthURL  string `json:"reAuthNotifUri,omitempty"`
	RevocURL   string `json:"revocNotifUri,omitempty"`
	AuthResult string `json:"authResult,omitempty"` // EAP_SUCCESS | EAP_FAILURE
	Timestamp  string `json:"timestamp,omitempty"`
}

// AMFMock is an httptest.Server implementing the AMF callback receiver.
// Spec: TS 23.502 §4.2.9.3
type AMFMock struct {
	Server *httptest.Server

	mu            sync.Mutex
	notifications []NssaaNotification
	// failNext causes the next notification request to return errorCode
	failNext  bool
	errorCode int
}

// NewAMFMock creates an AMF mock server.
func NewAMFMock() *AMFMock {
	m := &AMFMock{
		notifications: make([]NssaaNotification, 0),
		errorCode:     http.StatusServiceUnavailable,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/namf-callback/v1/", m.handleNotification)
	m.Server = httptest.NewServer(mux)
	return m
}

// Close shuts down the mock server.
func (m *AMFMock) Close() {
	m.Server.Close()
}

// URL returns the mock server's base URL.
func (m *AMFMock) URL() string {
	return m.Server.URL
}

// GetNotifications returns all received notifications.
func (m *AMFMock) GetNotifications() []NssaaNotification {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]NssaaNotification, len(m.notifications))
	copy(out, m.notifications)
	return out
}

// ClearNotifications resets the stored notifications.
func (m *AMFMock) ClearNotifications() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = m.notifications[:0]
}

// SetFailureNext causes the next notification to return errorCode instead of 204.
func (m *AMFMock) SetFailureNext(errorCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = true
	m.errorCode = errorCode
}

func (m *AMFMock) handleNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}

	m.mu.Lock()
	fail := m.failNext
	errCode := m.errorCode
	if fail {
		m.failNext = false
	}
	m.mu.Unlock()

	if fail {
		http.Error(w, `{"cause":"SERVICE_UNAVAILABLE"}`, errCode)
		return
	}

	var notif NssaaNotification
	if err := json.NewDecoder(r.Body).Decode(&notif); err != nil {
		http.Error(w, `{"cause":"INVALID_PAYLOAD"}`, http.StatusBadRequest)
		return
	}

	// Validate notification type
	switch notif.NotificationType {
	case "SLICE_RE_AUTH", "SLICE_REVOCATION":
		// valid
	default:
		http.Error(w, `{"cause":"INVALID_NOTIFICATION_TYPE"}`, http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.notifications = append(m.notifications, notif)
	m.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// NssaaNotificationPath builds the callback path for a given AMF ID.
func NssaaNotificationPath(amfID string) string {
	return "/namf-callback/v1/" + amfID + "/Nssaa-Notification"
}
