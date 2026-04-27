//go:build !softhsm

package crypto

import (
	"context"
	"errors"
)

type SoftHSMKeyManager struct {
	libraryPath string
	tokenLabel  string
	pin         string
}

func NewSoftHSMKeyManager(cfg *SoftHSMConfig) (*SoftHSMKeyManager, error) {
	return nil, errors.New("SoftHSMKeyManager requires -tags=softhsm; use soft or vault key manager instead")
}

func (m *SoftHSMKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	return nil, 0, errors.New("SoftHSM not available (build with -tags=softhsm)")
}

func (m *SoftHSMKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	return nil, errors.New("SoftHSM not available (build with -tags=softhsm)")
}

func (m *SoftHSMKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	return 1, nil
}

func (m *SoftHSMKeyManager) Rotate(ctx context.Context) error {
	return nil
}

func (m *SoftHSMKeyManager) Close() error {
	return nil
}
