//go:build e2e
// +build e2e

package fullchain

import (
	"testing"
)

func TestMain(m *testing.M) {
	// Delegate to e2e.TestMain which manages docker compose lifecycle.
	// fullchain tests run in the same docker compose stack.
	m.Run()
}
