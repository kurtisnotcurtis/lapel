package reconcile_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"lapel/internal/reconcile"
	"lapel/internal/state"
)

func TestRetryApply_SuccessOnFirstTry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	rec := reconcile.New(store)
	rec.MaxRetries = 3
	rec.RetryDelay = time.Millisecond

	attempts := 0
	err = rec.RetryApply(func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestRetryApply_SuccessOnLastRetry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	rec := reconcile.New(store)
	rec.MaxRetries = 3
	rec.RetryDelay = time.Millisecond

	attempts := 0
	err = rec.RetryApply(func() error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("transient error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryApply_FailureAfterAllRetries(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	rec := reconcile.New(store)
	rec.MaxRetries = 2
	rec.RetryDelay = time.Millisecond

	attempts := 0
	err = rec.RetryApply(func() error {
		attempts++
		return fmt.Errorf("permanent error")
	})

	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if attempts != 3 { // initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRollback_RestoresState(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	store, err := state.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	distroName := "test-distro"

	// Set up initial state
	err = store.UpsertPackage(state.PackageRecord{Distro: distroName, Name: "pkg1", Source: "manifest"})
	if err != nil {
		t.Fatalf("setup pkg1: %v", err)
	}
	err = store.UpsertJailRule(state.JailRule{Distro: distroName, Type: "filesystem", Key: "/path1", Value: "readonly"})
	if err != nil {
		t.Fatalf("setup rule1: %v", err)
	}
	err = store.UpsertEnvVar(state.EnvVar{Distro: distroName, Key: "KEY1", Value: "val1"})
	if err != nil {
		t.Fatalf("setup env1: %v", err)
	}

	// Capture baseline
	pkgsBefore, _ := store.GetPackages(distroName)
	rulesBefore, _ := store.GetJailRules(distroName)
	envBefore, _ := store.GetEnvVars(distroName)

	// Modify state (simulate apply)
	err = store.UpsertPackage(state.PackageRecord{Distro: distroName, Name: "pkg2", Source: "manifest"})
	if err != nil {
		t.Fatalf("add pkg2: %v", err)
	}
	err = store.UpsertJailRule(state.JailRule{Distro: distroName, Type: "filesystem", Key: "/path2", Value: "readwrite"})
	if err != nil {
		t.Fatalf("add rule2: %v", err)
	}
	err = store.UpsertEnvVar(state.EnvVar{Distro: distroName, Key: "KEY2", Value: "val2"})
	if err != nil {
		t.Fatalf("add env2: %v", err)
	}

	// Verify modifications
	pkgsAfter, _ := store.GetPackages(distroName)
	if len(pkgsAfter) != 2 {
		t.Fatalf("expected 2 packages after modify, got %d", len(pkgsAfter))
	}

	// Rollback
	rec := reconcile.New(store)
	err = rec.Rollback(distroName, pkgsBefore, rulesBefore, envBefore)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// Verify restoration
	pkgsRollback, _ := store.GetPackages(distroName)
	if len(pkgsRollback) != 1 || pkgsRollback[0].Name != "pkg1" {
		t.Errorf("expected [pkg1] after rollback, got %v", pkgsRollback)
	}
}
