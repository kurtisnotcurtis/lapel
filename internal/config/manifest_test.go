package config

import (
	"errors"
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
  provider: local
workspace:
  name: demo-workspace
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
	if m.Environment.Distribution != DefaultDistribution {
		t.Errorf("environment.distribution default = %q, want %q", m.Environment.Distribution, DefaultDistribution)
	}
	if m.EnvSpec().Name != DefaultDistribution {
		t.Errorf("EnvSpec().Name = %q, want %q", m.EnvSpec().Name, DefaultDistribution)
	}
	if m.Environment.Base != "ubuntu" {
		t.Errorf("environment.base default = %q, want ubuntu", m.Environment.Base)
	}
	if m.Environment.Style.Preset != "lapel-dark" {
		t.Errorf("environment.style.preset default = %q, want lapel-dark", m.Environment.Style.Preset)
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
	if m.Environment.Systemd != nil || m.EnvSpec().Systemd != nil {
		t.Errorf("expected unset environment.systemd to remain nil (defaults to true in backend)")
	}
}

func TestLoadEnvironmentSystemdAndVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultManifestName)
	yamlWithFlag := validManifest + "\nenvironment:\n  version: \"24.04\"\n  systemd: false\n"
	if err := os.WriteFile(path, []byte(yamlWithFlag), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Environment.Systemd == nil || *m.Environment.Systemd {
		t.Fatalf("expected environment.systemd=false, got %v", m.Environment.Systemd)
	}
	spec := m.EnvSpec()
	if spec.Systemd == nil || *spec.Systemd {
		t.Fatalf("expected EnvSpec().Systemd=false, got %v", spec.Systemd)
	}
	if spec.Version != "24.04" || spec.Base != "ubuntu" {
		t.Fatalf("expected EnvSpec base=ubuntu version=24.04, got base=%q version=%q", spec.Base, spec.Version)
	}
}

func TestLoadRejectsUnknownTopLevelAndNestedKeys(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"top-level typo", validManifest + "\nthisDoesNotExist: true\n"},
		{"nested typo", validManifest + "\nenvironment:\n  distributon: lapel\n"},
		{"removed field", validManifest + "\nagent:\n  name: pi\n  harness: opencode\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), DefaultManifestName)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("Load should reject %s", tc.name)
			}
		})
	}
}

func TestLoadRunsValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"bogus apiVersion", "apiVersion: bogus/v0\nname: demo\nagent:\n  name: pi\n"},
		{"bogus importMode", "apiVersion: lapel.dev/v1\nname: demo\nagent:\n  name: pi\nworkspace:\n  importMode: teleport\n"},
		{"unsafe workspace name", "apiVersion: lapel.dev/v1\nname: demo\nagent:\n  name: pi\nworkspace:\n  name: \"../etc\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), DefaultManifestName)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("Load should fail validation for %s", tc.name)
			}
		})
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
		{"bad distribution", func(m *Manifest) { m.Environment.Distribution = "bad distro; rm -rf /" }, true},
		{"bad provider type", func(m *Manifest) { m.Provider.Type = "anthropic" }, true},
		{"bad url scheme", func(m *Manifest) { m.Provider.BaseURL = "ftp://nope" }, true},
		{"provider mismatch", func(m *Manifest) { m.Agent.Provider = "other"; m.Provider.Name = "local" }, true},
		{"bad workspace name", func(m *Manifest) { m.Workspace.Name = "bad/name" }, true},
		{"bad workspace traversal", func(m *Manifest) { m.Workspace.Name = "a..b" }, true},
		{"bad importMode", func(m *Manifest) { m.Workspace.ImportMode = "symlink" }, true},
		{"bad jail user", func(m *Manifest) { m.Jail.User = "bad user" }, true},
		{"bad network", func(m *Manifest) { m.Jail.Network = "airgap" }, true},
		{"bad style preset", func(m *Manifest) { m.Environment.Style.Preset = "neon" }, true},
		{"valid style preset violet", func(m *Manifest) { m.Environment.Style.Preset = "violet" }, false},
		{"bad tabColor", func(m *Manifest) { m.Environment.Style.TabColor = "blue" }, true},
		{"valid tabColor", func(m *Manifest) { m.Environment.Style.TabColor = "#0EA5E9" }, false},
		{"bad background", func(m *Manifest) { m.Environment.Style.Background = "#12345" }, true},
		{"valid background", func(m *Manifest) { m.Environment.Style.Background = "#0D1520" }, false},
		{"bad badge newline", func(m *Manifest) { m.Environment.Style.Badge = "bad\nbadge" }, true},
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
	// Not found in an unrelated dir returns ErrManifestNotFound.
	if _, err := DiscoverManifest(t.TempDir()); !errors.Is(err, ErrManifestNotFound) {
		t.Errorf("expected ErrManifestNotFound, got %v", err)
	}
}
