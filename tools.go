// Package tools tracks build and test tool dependencies.
// These are not imported by production code but are required for testing.
// Use: go run -tools tools.go or build with tools tracked.
//go:build tools

package tools

import (
	_ "github.com/DATA-DOG/go-sqlmock"
)
