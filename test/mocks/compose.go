// Package mocks provides httptest.Server implementations of 3GPP NF APIs for integration testing.
package mocks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"time"
)

// MockServer represents a generic mock HTTP server with address tracking.
type MockServer struct {
	Server *httptest.Server
}

// URL returns the mock server's base URL.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close shuts down the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

// HealthStatus is the health check response from a service.
type HealthStatus struct {
	Status string `json:"status"`
}

// MockHTTPServer creates a generic httptest.Server that returns the configured response.
func MockHTTPServer(path string, statusCode int, body interface{}) (*MockServer, string) {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if body != nil {
			if bs, ok := body.([]byte); ok {
				w.WriteHeader(statusCode)
				_, _ = w.Write(bs)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		w.WriteHeader(statusCode)
	}
	if path != "" {
		mux.HandleFunc(path, handler)
	} else {
		mux.HandleFunc("/", handler)
	}
	srv := httptest.NewServer(mux)
	return &MockServer{Server: srv}, srv.URL
}

// ComposeUp starts services defined in the compose file.
// Returns when all services are healthy or timeout is reached.
func ComposeUp(ctx context.Context, composeFile string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	// Start services
	cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "up", "-d")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker-compose up: %w: %s", err, stderr.String())
	}

	// Wait for healthy
	if err := WaitForComposeHealthy(ctx, composeFile, 2*time.Minute); err != nil {
		return fmt.Errorf("services not healthy: %w", err)
	}

	logger.Info("compose_up_complete", "compose_file", composeFile)
	return nil
}

// ComposeDown stops and removes services defined in the compose file.
func ComposeDown(ctx context.Context, composeFile string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "down", "--remove-orphans")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker-compose down: %w: %s", err, stderr.String())
	}

	logger.Info("compose_down_complete", "compose_file", composeFile)
	return nil
}

// WaitForComposeHealthy polls all services until all are healthy or timeout.
func WaitForComposeHealthy(ctx context.Context, composeFile string, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.Done():
			return fmt.Errorf("timeout after %v waiting for services to be healthy", timeout)
		case <-ticker.C:
			if allHealthy, _ := checkComposeHealth(composeFile); allHealthy {
				return nil
			}
		}
	}
}

// checkComposeHealth uses docker-compose ps to check service health.
func checkComposeHealth(composeFile string) (bool, error) {
	cmd := exec.Command("docker-compose", "-f", composeFile, "ps", "--format", "json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, err
	}

	// Parse JSON output (one JSON object per line per service)
	dec := json.NewDecoder(&stdout)
	for dec.More() {
		var svc struct {
			Service string `json:"Service"`
			State   string `json:"State"`
			Health  string `json:"Health"`
		}
		if err := dec.Decode(&svc); err != nil {
			continue
		}
		// A service is healthy if it has no health or health is "healthy"
		if svc.Health != "" && svc.Health != "healthy" && svc.Health != "(healthy)" {
			return false, nil
		}
		if svc.State != "running" {
			return false, nil
		}
	}
	return true, nil
}

// WaitForHealthy polls a single service's health endpoint until healthy or timeout.
func WaitForHealthy(ctx context.Context, host string, port int, timeout time.Duration) error {
	url := fmt.Sprintf("http://%s:%d/health", host, port)
	client := &http.Client{Timeout: 2 * time.Second}

	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.Done():
			return fmt.Errorf("timeout after %v waiting for %s", timeout, url)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

// GetServiceAddr reads the published port for a service from docker-compose ps.
func GetServiceAddr(service string, composeFile string) (host string, port int, _ error) {
	cmd := exec.Command("docker-compose", "-f", composeFile, "port", service, "0")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("docker-compose port: %w: %s", err, stderr.String())
	}

	addr := strings.TrimSpace(stdout.String())
	if addr == "" {
		return "", 0, fmt.Errorf("no published port for service %s", service)
	}

	// addr is in format "0.0.0.0:port" or "[::]:port"
	parts := strings.SplitN(addr, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("unexpected address format: %s", addr)
	}
	host = parts[0]
	fmt.Sscanf(parts[1], "%d", &port)
	return host, port, nil
}
