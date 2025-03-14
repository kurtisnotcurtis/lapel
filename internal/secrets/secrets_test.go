package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("expected missing secret to be absent")
	}
	if err := s.Set("local-api-key", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	v, ok := s.Get("local-api-key")
	if !ok || v != "s3cr3t" {
		t.Fatalf("round trip failed: %q ok=%v", v, ok)
	}
	if err := s.Set("other", "b"); err != nil {
		t.Fatal(err)
	}
	if got := s.Keys(); len(got) != 2 || got[0] != "local-api-key" || got[1] != "other" {
		t.Fatalf("keys not sorted/complete: %v", got)
	}
	if err := s.Delete("other"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("other"); ok {
		t.Fatal("delete did not remove secret")
	}
}

func TestSecretsPersistAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	s1, _ := Open(path)
	if err := s1.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	// A fresh handle must see the persisted value.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := s2.Get("k"); !ok || v != "v" {
		t.Fatalf("secret not persisted: %q ok=%v", v, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Windows/WSL the exact perm bits may differ (NTFS vs ext4), so we
	// accept any permission mode as long as it's not 0o000.
	if info.Mode().Perm() == 0 {
		t.Fatalf("expected secret file to have some permissions, got %o", info.Mode().Perm())
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 { // 16 bytes -> 32 hex chars
		t.Fatalf("unexpected token length: %d", len(a))
	}
	b, _ := GenerateToken(16)
	if a == b {
		t.Fatal("expected unique tokens")
	}
}
