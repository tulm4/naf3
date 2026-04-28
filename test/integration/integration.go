// Package integration provides integration test suite for nssAAF.
package integration

import (
	"testing"

	_ "github.com/operator/nssAAF/test/mocks"
	_ "github.com/operator/nssAAF/test/aaa_sim"
)

func TestMain(m *testing.M) {
	// Integration test main.
	// Future waves will add test setup/teardown (NRF mock, UDM mock, etc.).
	m.Run()
}
