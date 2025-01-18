package manifest_test

import (
	"strings"
	"testing"

	"lapel/internal/domain"
	"lapel/internal/manifest"
)

func TestParseMinimal(t *testing.T) {
	yaml := `
schemaVersion: 1
distro:
  name: Ubuntu-Lean
`
	m, err := manifest.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Distro.Name != "Ubuntu-Lean" {
		t.Errorf("Distro.Name = %q, want %q", m.Distro.Name, "Ubuntu-Lean")
	}
	if *m.Distro.Systemd != true {
		t.Errorf("Systemd default = %v, want true", *m.Distro.Systemd)
	}
}

func TestParseFull(t *testing.T) {
	yaml := `
schemaVersion: 1
distro:
  name: Ubuntu-Lean
  base: ubuntu
  version: "24.04"
  defaultUser: kurt
packages:
  baseline: [git, curl]
  extras: [ripgrep]
jail:
  name: developer
  user: kurt
  filesystem:
    "/home/kurt": read-write
    "/tmp": deny
  network: outbound-only
  processes:
    privileged: false
    timeoutSeconds: 300
env:
  TERM: xterm-256color
`
	m, err := manifest.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if m.Distro.Base != "ubuntu" {
		t.Errorf("Distro.Base = %q, want %q", m.Distro.Base, "ubuntu")
	}
	if len(m.Packages.Baseline) != 2 {
		t.Errorf("Packages.Baseline length = %d, want 2", len(m.Packages.Baseline))
	}
	if m.Jail.Filesystem["/home/kurt"] != "read-write" {
		t.Errorf("Jail.Filesystem[/home/kurt] = %q, want %q", m.Jail.Filesystem["/home/kurt"], "read-write")
	}
	if m.Jail.Network != "outbound-only" {
		t.Errorf("Jail.Network = %q, want %q", m.Jail.Network, "outbound-only")
	}
	if m.Env["TERM"] != "xterm-256color" {
		t.Errorf("Env[TERM] = %q, want %q", m.Env["TERM"], "xterm-256color")
	}
}

func TestParseInvalidBase(t *testing.T) {
	yaml := `
schemaVersion: 1
distro:
  name: test
  base: fedora
`
	_, err := manifest.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for invalid base")
	}
	if !strings.Contains(err.Error(), "fedora") {
		t.Errorf("error = %q, want to mention 'fedora'", err.Error())
	}
}

func TestParseInvalidNetwork(t *testing.T) {
	yaml := `
schemaVersion: 1
distro:
  name: test
jail:
  network: wifi
`
	_, err := manifest.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for invalid network")
	}
	if !strings.Contains(err.Error(), "wifi") {
		t.Errorf("error = %q, want to mention 'wifi'", err.Error())
	}
}

func TestParseInvalidFilesystemMode(t *testing.T) {
	yaml := `
schemaVersion: 1
distro:
  name: test
jail:
  filesystem:
    "/path": execute
`
	_, err := manifest.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for invalid filesystem mode")
	}
	if !strings.Contains(err.Error(), "execute") {
		t.Errorf("error = %q, want to mention 'execute'", err.Error())
	}
}

func TestParseMissingName(t *testing.T) {
	yaml := `
schemaVersion: 1
distro: {}
`
	_, err := manifest.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for missing distro name")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want to mention 'required'", err.Error())
	}
}

func TestWrite(t *testing.T) {
	m := &domain.Manifest{
		SchemaVersion: 1,
		Distro:        domain.DistroConfig{Name: "test"},
	}
	data, err := manifest.Write(m)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty YAML output")
	}
}
