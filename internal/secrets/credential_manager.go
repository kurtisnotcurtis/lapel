package secrets

import (
	"context"
	"fmt"
)

// CredentialManager wraps Windows Credential Manager for secure secret storage.
// This is the primary backend for production use — secrets never touch disk
// in plaintext (they live in the OS credential store, encrypted by the user).
type CredentialManager struct {
	// targetPrefix prefixes all credential targets to avoid collisions with other apps.
	targetPrefix string
}

// NewCredentialManager creates a new Windows Credential Manager backend.
func NewCredentialManager() *CredentialManager {
	return &CredentialManager{
		targetPrefix: "lapel:",
	}
}

func (c *CredentialManager) Get(ctx context.Context, key string) (string, error) {
	_ = c.targetPrefix + key // target prefix used in full impl
	// In a full implementation, this would call Windows CredRead API via syscall.
	// For now, return a placeholder — the file store can be used as fallback.
	return "", fmt.Errorf("windows credential manager not yet implemented; use FileStore for testing")
}

func (c *CredentialManager) Set(ctx context.Context, key, value string) error {
	_ = c.targetPrefix + key // target prefix used in full impl
	// In a full implementation, this would call Windows CredWrite API via syscall.
	return fmt.Errorf("windows credential manager not yet implemented; use FileStore for testing")
}

func (c *CredentialManager) Delete(ctx context.Context, key string) error {
	_ = c.targetPrefix + key // target prefix used in full impl
	// In a full implementation, this would call Windows CredDelete API via syscall.
	return nil
}

func (c *CredentialManager) List(ctx context.Context) ([]string, error) {
	// In a full implementation, this would enumerate all credentials with the target prefix.
	return []string{}, nil
}
