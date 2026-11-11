package state_test

import (
	"path/filepath"
	"testing"
	"time"

	"lapel/internal/state"
)

func TestStoreCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")

	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Test distro CRUD
	distroRec := &state.DistroRecord{
		Name:        "Ubuntu-Lean",
		Base:        "ubuntu",
		Version:     "24.04",
		DefaultUser: "kurt",
		Systemd:     true,
	}

	if err := store.UpsertDistro(distroRec); err != nil {
		t.Fatalf("upsert distro: %v", err)
	}

	got, err := store.GetDistro("Ubuntu-Lean")
	if err != nil {
		t.Fatalf("get distro: %v", err)
	}

	if got.Name != "Ubuntu-Lean" || got.Base != "ubuntu" || got.Version != "24.04" {
		t.Errorf("distro mismatch: %+v", got)
	}

	// Test packages CRUD
	pkg := state.PackageRecord{
		Distro:  "Ubuntu-Lean",
		Name:    "git",
		Version: "2.39.0",
		Source:  "manifest",
	}

	if err := store.UpsertPackage(pkg); err != nil {
		t.Fatalf("upsert package: %v", err)
	}

	pkgs, err := store.GetPackages("Ubuntu-Lean")
	if err != nil {
		t.Fatalf("get packages: %v", err)
	}

	if len(pkgs) != 1 || pkgs[0].Name != "git" {
		t.Errorf("packages mismatch: %+v", pkgs)
	}

	// Test jail rules CRUD
	rule := state.JailRule{
		Distro: "Ubuntu-Lean",
		Type:   "filesystem",
		Key:    "/home/kurt",
		Value:  "read-write",
	}

	if err := store.UpsertJailRule(rule); err != nil {
		t.Fatalf("upsert jail rule: %v", err)
	}

	rules, err := store.GetJailRules("Ubuntu-Lean")
	if err != nil {
		t.Fatalf("get jail rules: %v", err)
	}

	if len(rules) != 1 || rules[0].Value != "read-write" {
		t.Errorf("jail rules mismatch: %+v", rules)
	}

	// Test env vars CRUD
	envVar := state.EnvVar{
		Distro: "Ubuntu-Lean",
		Key:    "TERM",
		Value:  "xterm-256color",
	}

	if err := store.UpsertEnvVar(envVar); err != nil {
		t.Fatalf("upsert env var: %v", err)
	}

	vars, err := store.GetEnvVars("Ubuntu-Lean")
	if err != nil {
		t.Fatalf("get env vars: %v", err)
	}

	if len(vars) != 1 || vars[0].Value != "xterm-256color" {
		t.Errorf("env vars mismatch: %+v", vars)
	}

	// Test apply log
	if err := store.LogApply("Ubuntu-Lean", "apply", "installed git"); err != nil {
		t.Fatalf("log apply: %v", err)
	}

	logs, err := store.GetApplyLog("Ubuntu-Lean", 10)
	if err != nil {
		t.Fatalf("get apply log: %v", err)
	}

	if len(logs) != 1 || logs[0].Action != "apply" {
		t.Errorf("apply log mismatch: %+v", logs)
	}
}

func TestStoreMigrate(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")

	// First open creates schema
	store1, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}
	store1.Close()

	// Second open should not fail (idempotent migration)
	store2, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	defer store2.Close()
}

func TestStoreUpsertIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")

	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	distroRec := &state.DistroRecord{
		Name:        "Ubuntu-Lean",
		Base:        "ubuntu",
		Version:     "24.04",
		DefaultUser: "kurt",
		Systemd:     true,
	}

	if err := store.UpsertDistro(distroRec); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Update version
	distroRec.Version = "24.10"
	if err := store.UpsertDistro(distroRec); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.GetDistro("Ubuntu-Lean")
	if err != nil {
		t.Fatalf("get distro: %v", err)
	}

	if got.Version != "24.10" {
		t.Errorf("version not updated: got %q, want %q", got.Version, "24.10")
	}
}

func TestStoreGetNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")

	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	_, err = store.GetDistro("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent distro")
	}
}

func TestStoreApplyLogLimit(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")

	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Log multiple actions
	for i := 0; i < 5; i++ {
		if err := store.LogApply("Ubuntu-Lean", "apply", "action"); err != nil {
			t.Fatalf("log apply: %v", err)
		}
	}

	logs, err := store.GetApplyLog("Ubuntu-Lean", 3)
	if err != nil {
		t.Fatalf("get apply log: %v", err)
	}

	if len(logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(logs))
	}
}

// Ensure time package is used (suppress unused import warning in production).
var _ = time.Now
