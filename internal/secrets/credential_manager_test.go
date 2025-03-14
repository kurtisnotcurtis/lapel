package secrets_test

import (
	"context"
	"testing"

	"lapel/internal/secrets"
)

func TestCredentialManager_GetNotFound(t *testing.T) {
	cm := secrets.NewCredentialManager()
	ctx := context.Background()

	_, err := cm.Get(ctx, "test-key")
	if err == nil {
		t.Fatal("expected error for unimplemented CredentialManager.Get")
	}
}

func TestCredentialManager_Set(t *testing.T) {
	cm := secrets.NewCredentialManager()
	ctx := context.Background()

	err := cm.Set(ctx, "test-key", "test-value")
	if err == nil {
		t.Fatal("expected error for unimplemented CredentialManager.Set")
	}
}

func TestCredentialManager_Delete(t *testing.T) {
	cm := secrets.NewCredentialManager()
	ctx := context.Background()

	err := cm.Delete(ctx, "test-key")
	if err != nil {
		t.Fatalf("Delete should not error (idempotent): %v", err)
	}
}

func TestCredentialManager_List(t *testing.T) {
	cm := secrets.NewCredentialManager()
	ctx := context.Background()

	keys, err := cm.List(ctx)
	if err != nil {
		t.Fatalf("List should not error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %v", keys)
	}
}
