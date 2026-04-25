package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHealthEndpoint verifies that /health returns 200 OK with a JSON body.
func TestHealthEndpoint(t *testing.T) {
	recorder := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	handleHealth(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "ok")
	assert.Contains(t, recorder.Body.String(), "aaa-gateway")
}

// TestSignalReceived_ReturnsChannel verifies that signalReceived returns a channel.
func TestSignalReceived_ReturnsChannel(t *testing.T) {
	ch := signalReceived()
	assert.NotNil(t, ch)
}
