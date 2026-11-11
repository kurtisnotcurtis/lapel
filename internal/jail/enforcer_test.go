package jail

import (
	"testing"
)

func TestEnforcer_Apply(t *testing.T) {
	profile := Profile{
		Name:   "developer",
		User:   "agent",
		Processes: ProcessConfig{TimeoutSeconds: 300},
	}

	e := NewEnforcer(profile)
	baseEnv := map[string]string{
		"PATH": "/usr/local/bin:/mnt/c/Windows/System32:/usr/bin:/mnt/d/tools",
		"HOME": "/home/agent",
	}

	env := e.Apply(baseEnv)

	// Check PATH was filtered.
	path := env["PATH"]
	if path == "" {
		t.Fatal("PATH not set")
	}
	if contains(path, "/mnt/c") || contains(path, "/mnt/d") {
		t.Errorf("PATH still contains host mounts: %s", path)
	}

	// Check timeout was injected.
	if env["JAIL_TIMEOUT"] != "300" {
		t.Errorf("timeout not set: got %q", env["JAIL_TIMEOUT"])
	}

	// Check HOME preserved.
	if env["HOME"] != "/home/agent" {
		t.Errorf("HOME changed: got %q", env["HOME"])
	}
}

func TestEnforcer_FilesystemScoping(t *testing.T) {
	profile := Profile{
		Name: "developer",
		Filesystem: map[string]string{
			"C:\\Users\\Kurt\\Projects": "read-write",
			"C:\\Windows\\System32":     "deny",
		},
	}

	e := NewEnforcer(profile)
	env := e.Apply(map[string]string{"PATH": "/usr/bin"})

	// Check filesystem scopes were injected.
	expectedKey := "JAIL_C:_USERS_KURT_PROJECTS"
	if env[expectedKey] != "C:\\Users\\Kurt\\Projects" {
		t.Errorf("filesystem scope not set correctly: got %v, want key=%q", env, expectedKey)
	}
}

func TestPathFilter(t *testing.T) {
	f := NewPathFilter()
	env := map[string]string{"PATH": "/usr/bin:/mnt/c/Windows:/usr/local/bin:/mnt/d/tools"}

	f.Apply(env)

	path := env["PATH"]
	if contains(path, "/mnt/c") || contains(path, "/mnt/d") {
		t.Errorf("PATH not filtered: %s", path)
	}
}

func TestPathFilter_Disabled(t *testing.T) {
	f := NewPathFilter()
	f.Enable(false)
	env := map[string]string{"PATH": "/usr/bin:/mnt/c/Windows"}

	f.Apply(env)

	path := env["PATH"]
	if !contains(path, "/mnt/c") {
		t.Errorf("filter should be disabled: %s", path)
	}
}

func TestEnforcer_NetworkRestrictions(t *testing.T) {
	profile := Profile{
		Name:    "developer",
		Network: NetworkOutboundOnly,
	}

	e := NewEnforcer(profile)
	controls := e.Controls()

	found := false
	for _, c := range controls {
		if c.Name == "network-restrict" {
			found = true
			if c.Level != Enforced {
				t.Errorf("network control not enforced: %v", c.Level)
			}
			if c.Details != "mode: outbound-only" {
				t.Errorf("unexpected details: %s", c.Details)
			}
		}
	}

	if !found {
		t.Error("network-restrict control not found")
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
