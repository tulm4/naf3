// Package e2e provides end-to-end test suite for nssAAF.
package e2e

import (
	"testing"

	_ "github.com/operator/nssAAF/test/mocks"
	_ "github.com/operator/nssAAF/test/aaa_sim"
)

func TestMain(m *testing.M) {
	// E2E test main.
	// Future waves will add:
	// - docker-compose lifecycle (via test/mocks/compose.go)
	// - 3-component E2E with real components
	m.Run()
}
