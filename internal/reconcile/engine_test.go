package reconcile_test

import (
	"os"
	"path/filepath"
	"testing"

	"lapel/internal/reconcile"
	"lapel/internal/state"
)

func newTestStore(t *testing.T) (*state.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := state.New(path)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	return s, func() { s.Close() }
}

func TestPlanEmptyState(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	recon := reconcile.New(store)

	mf := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mf, []byte(`schemaVersion: 1
distro:
  name: test-distro
packages:
  baseline: [curl, git]
  extras: [python3]
jail:
  filesystem:
    /tmp: read-only
env:
  MY_VAR: hello
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := recon.Plan(mf, "test-distro")
	if err != nil {
		t.Fatal(err)
	}

	if plan.Distro != "test-distro" {
		t.Errorf("expected distro test-distro, got %s", plan.Distro)
	}

	if len(plan.Drift) == 0 {
		t.Fatal("expected drift entries for empty state")
	}

	// Should have: 3 packages (curl, git, python3), 1 jail rule, 1 env var = 5 total
	expectedDrift := 5
	if len(plan.Drift) != expectedDrift {
		t.Errorf("expected %d drift entries, got %d", expectedDrift, len(plan.Drift))
	}

	// Verify package drift
	var pkgCount int
	for _, d := range plan.Drift {
		if d.Kind == "package" && d.Action == "add" {
			pkgCount++
		}
	}
	if pkgCount != 3 {
		t.Errorf("expected 3 package adds, got %d", pkgCount)
	}

	// Verify jail rule drift
	var jailCount int
	for _, d := range plan.Drift {
		if d.Kind == "jail_rule" && d.Action == "add" {
			jailCount++
		}
	}
	if jailCount != 1 {
		t.Errorf("expected 1 jail rule add, got %d", jailCount)
	}

	// Verify env var drift
	var envCount int
	for _, d := range plan.Drift {
		if d.Kind == "env_var" && d.Action == "add" {
			envCount++
		}
	}
	if envCount != 1 {
		t.Errorf("expected 1 env var add, got %d", envCount)
	}
}

func TestPlanNoDrift(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	recon := reconcile.New(store)

	mf := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mf, []byte(`schemaVersion: 1
distro:
  name: test-distro
packages:
  baseline: [curl]
env:
  FOO: bar
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate state to match manifest
	if err := store.UpsertPackage(state.PackageRecord{
		Distro: "test-distro",
		Name:   "curl",
		Source: "manifest",
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertEnvVar(state.EnvVar{
		Distro: "test-distro",
		Key:    "FOO",
		Value:  "bar",
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := recon.Plan(mf, "test-distro")
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Drift) != 0 {
		t.Errorf("expected no drift, got %d entries", len(plan.Drift))
		for _, d := range plan.Drift {
			t.Logf("  - [%s] %s: %s → %s", d.Action, d.Key, d.From, d.To)
		}
	}
}

func TestPlanDriftDetection(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	recon := reconcile.New(store)

	mf := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mf, []byte(`schemaVersion: 1
distro:
  name: test-distro
packages:
  baseline: [curl, git]
env:
  MY_VAR: updated_value
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate state with drift
	if err := store.UpsertPackage(state.PackageRecord{
		Distro: "test-distro",
		Name:   "curl",
		Source: "manifest",
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertEnvVar(state.EnvVar{
		Distro: "test-distro",
		Key:    "MY_VAR",
		Value:  "old_value",
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := recon.Plan(mf, "test-distro")
	if err != nil {
		t.Fatal(err)
	}

	// Should detect: git missing (add), MY_VAR value changed (update) = 2 drift entries
	expectedDrift := 2
	if len(plan.Drift) != expectedDrift {
		t.Errorf("expected %d drift entries, got %d", expectedDrift, len(plan.Drift))
		for _, d := range plan.Drift {
			t.Logf("  - [%s] %s: %s → %s", d.Action, d.Key, d.From, d.To)
		}
	}

	// Verify git is marked as add
	var gitAdd bool
	for _, d := range plan.Drift {
		if d.Kind == "package" && d.Key == "git" && d.Action == "add" {
			gitAdd = true
		}
	}
	if !gitAdd {
		t.Error("expected git to be marked for addition")
	}

	// Verify MY_VAR is marked as update with old→new value
	var myVarUpdate bool
	for _, d := range plan.Drift {
		if d.Kind == "env_var" && d.Key == "MY_VAR" && d.Action == "update" && d.From == "old_value" && d.To == "updated_value" {
			myVarUpdate = true
		}
	}
	if !myVarUpdate {
		t.Error("expected MY_VAR to be marked for update with correct from/to values")
	}
}

func TestApply(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	recon := reconcile.New(store)

	mf := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mf, []byte(`schemaVersion: 1
distro:
  name: test-distro
packages:
  baseline: [curl]
env:
  TEST_VAR: test_value
`), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := recon.Apply(mf, "test-distro")
	if err != nil {
		t.Fatal(err)
	}

	// Should have applied 2 changes (curl package + TEST_VAR env var)
	if result.Applied != 2 {
		t.Errorf("expected 2 applied changes, got %d", result.Applied)
		for _, f := range result.Failures {
			t.Logf("  failure: %s", f)
		}
	}

	if len(result.Failures) > 0 {
		t.Errorf("expected no failures, got %v", result.Failures)
	}

	// Verify state was updated
	pkgs, err := store.GetPackages("test-distro")
	if err != nil {
		t.Fatal(err)
	}
	foundCurl := false
	for _, p := range pkgs {
		if p.Name == "curl" && p.Source == "manifest" {
			foundCurl = true
		}
	}
	if !foundCurl {
		t.Error("expected curl package in state after apply")
	}

	envVars, err := store.GetEnvVars("test-distro")
	if err != nil {
		t.Fatal(err)
	}
	foundTestVar := false
	for _, e := range envVars {
		if e.Key == "TEST_VAR" && e.Value == "test_value" {
			foundTestVar = true
		}
	}
	if !foundTestVar {
		t.Error("expected TEST_VAR in state after apply")
	}

	// Verify apply was logged
	log, err := store.GetApplyLog("test-distro", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) == 0 {
		t.Error("expected apply log entry")
	} else {
		lastEntry := log[len(log)-1]
		if lastEntry.Action != "apply" {
			t.Errorf("expected action 'apply', got '%s'", lastEntry.Action)
		}
	}
}

func TestApplyIdempotent(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	recon := reconcile.New(store)

	mf := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mf, []byte(`schemaVersion: 1
distro:
  name: test-distro
packages:
  baseline: [curl]
env:
  MY_VAR: value
`), 0644); err != nil {
		t.Fatal(err)
	}

	// First apply
	result1, err := recon.Apply(mf, "test-distro")
	if err != nil {
		t.Fatal(err)
	}
	if result1.Applied != 2 {
		t.Errorf("first apply: expected 2 applied, got %d", result1.Applied)
	}

	// Second apply should find no drift
	result2, err := recon.Apply(mf, "test-distro")
	if err != nil {
		t.Fatal(err)
	}
	if result2.Applied != 0 {
		t.Errorf("second apply: expected 0 applied (idempotent), got %d", result2.Applied)
		for _, d := range result2.Drift {
			t.Logf("  drift: [%s] %s", d.Action, d.Key)
		}
	}

	if len(result2.Failures) > 0 {
		t.Errorf("second apply: expected no failures, got %v", result2.Failures)
	}
}

func TestFormatPlan(t *testing.T) {
	plan := &reconcile.Plan{
		Distro: "test-distro",
		Drift: []reconcile.DriftEntry{
			{Kind: "package", Key: "git", Action: "add"},
			{Kind: "env_var", Key: "MY_VAR", Action: "update", From: "old", To: "new"},
			{Kind: "jail_rule", Key: "/tmp", Action: "remove"},
		},
	}

	output := reconcile.FormatPlan(plan)

	expectedLines := []string{
		"+ [package] git = ",
		"~ [env_var] MY_VAR: old → new",
		"- [jail_rule] /tmp",
	}

	for _, line := range expectedLines {
		if !contains(output, line) {
			t.Errorf("expected output to contain '%s', got:\n%s", line, output)
		}
	}
}

func TestFormatPlanEmpty(t *testing.T) {
	plan := &reconcile.Plan{
		Distro: "test-distro",
		Drift:  []reconcile.DriftEntry{},
	}

	output := reconcile.FormatPlan(plan)

	if !contains(output, "No drift detected") {
		t.Errorf("expected 'No drift detected' in output, got:\n%s", output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
