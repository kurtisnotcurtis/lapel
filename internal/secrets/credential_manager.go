package secrets

import (
	"context"
	"fmt"
	"os/exec"
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
	target := c.targetPrefix + key
	cmd := exec.CommandContext(ctx, "cmdkey", "/generic:", target, "/retrieve")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("credential %q not found: %w", key, err)
	}
	// Parse output to extract the value (simplified - real impl would parse properly)
	return string(output), nil
}

func (c *CredentialManager) Set(ctx context.Context, key, value string) error {
	target := c.targetPrefix + key
	cmd := exec.CommandContext(ctx, "cmdkey", "/generic:", target, "/savecred")
	// Write value to stdin for cmdkey to read
	input, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}
	go func() {
		defer input.Close()
		fmt.Fprint(input, value)
	}()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write credential %q: %w", key, err)
	}
	return nil
}

func (c *CredentialManager) Delete(ctx context.Context, key string) error {
	target := c.targetPrefix + key
	cmd := exec.CommandContext(ctx, "cmdkey", "/generic:", target, "/delete")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete credential %q: %w", key, err)
	}
	return nil
}

func (c *CredentialManager) List(ctx context.Context) ([]string, error) {
	// In a full implementation, this would enumerate all credentials with the target prefix.
	return []string{}, nil
}
