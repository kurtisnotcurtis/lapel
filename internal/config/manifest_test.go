package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validManifest = `
apiVersion: lapel.dev/v1
name: demo-project
provider:
  name: local
  type: openai-compatible
  baseURL: http://localhost:1234
  connectionTimeout: 8s
agent:
  name: pi
  harness: opencode
  provider: local
workspace:
  path: ~/projects/demo
jail:
  profile: developer
`

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultManifestName)
	if err := os.WriteFile(path, []byte(validManifest), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.APIVersion != apiVersion {
		t.Errorf("apiVersion = %q, want %q", m.APIVersion, apiVersion)
	}
	if m.Name == "" {
		t.Error("expected non-empty name after defaults")
	}
	if m.Environment.Base != "ubuntu" {
		t.Errorf("environment.base default = %q, want ubuntu", m.Environment.Base)
	}
	if m.Jail.Profile != "developer" {
		t.Errorf("jail.profile default = %q, want developer", m.Jail.Profile)
	}
	if m.Jail.User != "agent" {
		t.Errorf("jail.user default = %q, want agent", m.Jail.User)
	}
	if m.Workspace.ImportMode != "bind" {
		t.Errorf("workspace.importMode default = %q, want bind", m.Workspace.ImportMode)
	}
	if got := m.ProviderSpec().Name; got != "local" {
		t.Errorf("provider.name = %q, want local", got)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultManifestName)
	bad := validManifest + "\nthisDoesNotExis: true\n"
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load should reject unknown keys (KnownOnly)")
	}
}

func TestValidateCatchesProblems(t *testing.T) {
	cases := []struct {
		name    string
		mutator func(*Manifest)
		wantErr bool
	}{
		{"valid", func(_ *Manifest) {}, false},
		{"bad apiVersion", func(m *Manifest) { m.APIVersion = "v0" }, true},
		{"missing name", func(m *Manifest) { m.Name = "" }, true},
		{"bad name charset", func(m *Manifest) { m.Name = "-bad" }, true},
		{"bad base", func(m *Manifest) { m.Environment.Base = "fedora" }, true},
		{"import missing version", func(m *Manifest) { m.Environment.Distribution = "import"; m.Environment.Base = "ubuntu" }, true},
		{"import ok", func(m *Manifest) { m.Environment.Distribution = "import"; m.Environment.Base = "ubuntu"; m.Environment.Version = "24.04" }, false},
		{"bad provider type", func(m *Manifest) { m.Provider.Type = "anthropic" }, true},
		{"bad url scheme", func(m *Manifest) { m.Provider.BaseURL = "ftp://nope" }, true},
		{"provider mismatch", func(m *Manifest) { m.Agent.Provider = "other"; m.Provider.Name = "local" }, true},
		{"bad importMode", func(m *Manifest) { m.Workspace.ImportMode = "symlink" }, true},
		{"bad network", func(m *Manifest) { m.Jail.Network = "airgap" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), DefaultManifestName)
			if err := os.WriteFile(path, []byte(validManifest), 0o600); err != nil {
				t.Fatal(err)
			}
			m, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			tc.mutator(m)
			_, verr := m.ValidateAll()
			if tc.wantErr && verr == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantErr && verr != nil {
				t.Errorf("unexpected validation error: %v", verr)
			}
		})
	}
}

func TestDiscoverManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultManifestName)
	if err := os.WriteFile(path, []byte(validManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	// Found from a nested subdirectory (walks up).
	got, err := DiscoverManifest(filepath.Join(dir, "sub", "deeper"))
	if err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	if got != path {
		t.Errorf("DiscoverManifest = %q, want %q", got, path)
	}
	// Not found in an unrelated dir.
	if _, err := DiscoverManifest(t.TempDir()); err == nil {
		t.Error("expected error when no manifest present")
	}
}
