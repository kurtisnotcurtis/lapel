package state

import (
	"path/filepath"
	"testing"
)

func TestStoreMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")

	s1, err := New(dbPath)
	if err != nil {
		t.Fatalf("first New() failed: %v", err)
	}
	if err := s1.SetDesired(KindEnvironment, "Ubuntu-Lean", `{"distribution":"Ubuntu-Lean"}`, "h1"); err != nil {
		t.Fatalf("SetDesired failed: %v", err)
	}
	_ = s1.Close()

	// Re-open the same database to ensure migrations are idempotent and preserve data.
	s2, err := New(dbPath)
	if err != nil {
		t.Fatalf("second New() failed: %v", err)
	}
	defer s2.Close()

	resources, err := s2.DesiredResources()
	if err != nil {
		t.Fatalf("DesiredResources failed: %v", err)
	}
	if len(resources) != 1 || resources[0].Name != "Ubuntu-Lean" {
		t.Fatalf("expected 1 preserved resource, got %+v", resources)
	}
}
