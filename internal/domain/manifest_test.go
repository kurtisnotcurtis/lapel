package domain_test

import (
	"encoding/json"
	"testing"

	"lapel/internal/domain"
)

func TestManifestJSONRoundTrip(t *testing.T) {
	m := domain.Manifest{
		SchemaVersion: 1,
		Distro: domain.DistroConfig{
			Name:        "Ubuntu-Lean",
			Base:        "ubuntu",
			Version:     "24.04",
			DefaultUser: "kurt",
		},
		Packages: domain.PackageConfig{
			Baseline: []string{"git", "curl"},
			Extras:   []string{"ripgrep"},
		},
		Jail: domain.JailManifest{
			Name:   "developer",
			User:   "kurt",
			Network: "unrestricted",
			Filesystem: map[string]string{
				"C:\\Users\\Kurt\\Projects": "read-write",
			},
			Processes: domain.ProcessJail{
				Privileged:     false,
				TimeoutSeconds: 300,
			},
		},
		Env: map[string]string{
			"TERM": "xterm-256color",
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got domain.Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SchemaVersion != m.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, m.SchemaVersion)
	}
	if got.Distro.Name != "Ubuntu-Lean" {
		t.Errorf("Distro.Name = %q, want %q", got.Distro.Name, "Ubuntu-Lean")
	}
	if got.Jail.Filesystem["C:\\Users\\Kurt\\Projects"] != "read-write" {
		t.Errorf("Jail.Filesystem[Projects] = %q, want %q", got.Jail.Filesystem["C:\\Users\\Kurt\\Projects"], "read-write")
	}
	if got.Env["TERM"] != "xterm-256color" {
		t.Errorf("Env[TERM] = %q, want %q", got.Env["TERM"], "xterm-256color")
	}
}

func TestManifestDefaults(t *testing.T) {
	m := domain.Manifest{SchemaVersion: 1}

	if m.Distro.Base != "" {
		t.Errorf("Distro.Base should default to empty (caller fills in), got %q", m.Distro.Base)
	}
	if len(m.Packages.Baseline) != 0 {
		t.Errorf("Packages.Baseline should default to nil, got %v", m.Packages.Baseline)
	}
	if m.Jail.Network != "" {
		t.Errorf("Jail.Network should default to empty (caller fills in), got %q", m.Jail.Network)
	}
}
