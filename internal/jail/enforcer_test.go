package jail

import (
	"reflect"
	"strings"
	"testing"
)

func TestEnforcer_Apply(t *testing.T) {
	profile := Profile{
		Name:      "developer",
		User:      "agent",
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
	if strings.Contains(path, "/mnt/c") || strings.Contains(path, "/mnt/d") {
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

func TestEnforcer_Apply_SetsDefaultCleanPathWhenMissing(t *testing.T) {
	profile := Profile{Name: "developer", User: "agent"}
	e := NewEnforcer(profile)

	env := e.Apply(map[string]string{"OPENAI_MODEL_NAME": "ornith-35b"})
	if env["PATH"] != DefaultCleanPath {
		t.Errorf("expected DefaultCleanPath %q when PATH missing, got %q", DefaultCleanPath, env["PATH"])
	}
}

func TestEnforcer_WrapCommand(t *testing.T) {
	e := NewEnforcer(Profile{
		Name:      "developer",
		Processes: ProcessConfig{TimeoutSeconds: 120},
	})
	got := e.WrapCommand([]string{"bash", "-c", "pi"})
	want := []string{"timeout", "120s", "bash", "-c", "pi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapCommand = %v, want %v", got, want)
	}
}

func TestEnforcer_FilesystemScoping(t *testing.T) {
	profile := Profile{
		Name: "developer",
		Filesystem: map[string]string{
			"C:\\Users\\user\\Projects": "read-write",
			"C:\\Windows\\System32":     "deny",
		},
	}

	e := NewEnforcer(profile)
	env := e.Apply(map[string]string{"PATH": "/usr/bin"})

	// Check filesystem scopes were injected.
	expectedKey := "JAIL_C:_USERS_USER_PROJECTS"
	if env[expectedKey] != "C:\\Users\\user\\Projects" {
		t.Errorf("filesystem scope not set correctly: got %v, want key=%q", env, expectedKey)
	}

	// Verify honest reporting marks filesystem-scope as Requested (not overclaimed as Enforced).
	for _, c := range e.Controls() {
		if c.Name == "filesystem-scope" && c.Level != Requested {
			t.Errorf("expected filesystem-scope level %q, got %q", Requested, c.Level)
		}
	}
}

func TestPathFilter(t *testing.T) {
	f := NewPathFilter()
	env := map[string]string{"PATH": "/usr/bin:/mnt/c/Windows:/usr/local/bin:/mnt/d/tools"}

	f.Apply(env)

	path := env["PATH"]
	if strings.Contains(path, "/mnt/c") || strings.Contains(path, "/mnt/d") {
		t.Errorf("PATH not filtered: %s", path)
	}
}

func TestPathFilter_Disabled(t *testing.T) {
	f := NewPathFilter()
	f.Enable(false)
	env := map[string]string{"PATH": "/usr/bin:/mnt/c/Windows"}

	f.Apply(env)

	path := env["PATH"]
	if !strings.Contains(path, "/mnt/c") {
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
			if c.Level != NeedsConfig {
				t.Errorf("expected network control level %q (honest reporting), got %q", NeedsConfig, c.Level)
			}
			if !strings.Contains(c.Details, "mode: outbound-only") {
				t.Errorf("unexpected details: %s", c.Details)
			}
		}
	}

	if !found {
		t.Error("network-restrict control not found")
	}
}
