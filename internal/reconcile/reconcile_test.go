package reconcile

import (
	"context"
	"errors"
	"testing"

	"lapel/internal/config"
	"lapel/internal/domain"
)

func baseManifest() config.Manifest {
	return config.Manifest{
		Name: "demo",
		Environment: config.Environment{Distribution: "Ubuntu-Lean"},
		Provider:    config.Provider{Name: "local", BaseURL: "http://localhost:1234/v1"},
		Workspace:   config.Workspace{Name: "work", Path: "/tmp/x", ImportMode: "bind"},
	}
}

func TestDiffReportsMissingResources(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		EnvProber: EnvProber(func(_ context.Context, _ config.Environment) (domain.EnvironmentStatus, error) {
			return domain.EnvironmentStatus{}, nil // not installed
		}),
		ProviderProbe: ProviderProbe(func(config.Provider) (bool, string) { return false, "closed port" }),
		WorkspaceExists: WorkspaceExists(func(config.Workspace) (bool, error) {
			return false, nil
		}),
	}
	m := baseManifest()
	res, err := Diff(ctx, &m, opts)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if res.AsDesired() {
		t.Fatal("expected not-as-desired")
	}
	got := res.NonDesired()
	if len(got) != 3 {
		t.Fatalf("want 3 non-desired entries, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "environment" || got[0].Desired {
		t.Fatalf("env entry wrong: %+v", got[0])
	}
	if got[1].Kind != "provider" || got[1].Actual == "" {
		t.Fatalf("provider entry wrong: %+v", got[1])
	}
	if got[2].Kind != "workspace" {
		t.Fatalf("workspace entry wrong: %+v", got[2])
	}
}

func TestDiffAllDesired(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		EnvProber: EnvProber(func(_ context.Context, cfg config.Environment) (domain.EnvironmentStatus, error) {
			return domain.EnvironmentStatus{Exists: true, Distribution: cfg.Distribution, Version: "24.04"}, nil
		}),
		ProviderProbe: ProviderProbe(func(config.Provider) (bool, string) { return true, "reachable" }),
		WorkspaceExists: WorkspaceExists(func(config.Workspace) (bool, error) {
			return true, nil
		}),
	}
	m := baseManifest()
	res, err := Diff(ctx, &m, opts)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !res.AsDesired() {
		t.Fatalf("expected all-desired; entries=%+v", res.Entries)
	}
	if len(res.NonDesired()) != 0 {
		t.Fatalf("expected no non-desired entries, got %+v", res.NonDesired())
	}
	if res.Manifest.Name != "demo" {
		t.Fatalf("manifest not carried through: %+v", res.Manifest)
	}
}

func TestEnvNameFallback(t *testing.T) {
	m := baseManifest()
	m.Environment.Distribution = ""
	if got := envName(&m); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
	m.Environment.Distribution = "Ubuntu-Lean"
	if got := envName(&m); got != "Ubuntu-Lean" {
		t.Fatalf("expected distribution name, got %q", got)
	}
}

func TestEnvProbeMismatch(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		// Installed under a different distro name than declared.
		EnvProber: EnvProber(func(_ context.Context, _ config.Environment) (domain.EnvironmentStatus, error) {
			return domain.EnvironmentStatus{Exists: true, Distribution: "Ubuntu"}, nil
		}),
		ProviderProbe:   ProviderProbe(func(config.Provider) (bool, string) { return true, "ok" }),
		WorkspaceExists: WorkspaceExists(func(config.Workspace) (bool, error) { return true, nil }),
	}
	m := baseManifest()
	res, err := Diff(ctx, &m, opts)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if res.Entries[0].Desired {
		t.Fatalf("expected env mismatch to be non-desired: %+v", res.Entries[0])
	}
}

func TestProbeErrorPropagates(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		EnvProber:       EnvProber(func(_ context.Context, _ config.Environment) (domain.EnvironmentStatus, error) { return domain.EnvironmentStatus{}, errors.New("boom") }),
		ProviderProbe:   ProviderProbe(func(config.Provider) (bool, string) { return true, "ok" }),
		WorkspaceExists: WorkspaceExists(func(config.Workspace) (bool, error) { return true, nil }),
	}
	m := baseManifest()
	if _, err := Diff(ctx, &m, opts); err == nil {
		t.Fatal("expected probe error to propagate")
	}
}
