package jail

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var validPOSIXEnvKeyRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

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

	path := env["PATH"]
	if path == "" {
		t.Fatal("PATH not set")
	}
	if strings.Contains(path, "/mnt/c") || strings.Contains(path, "/mnt/d") {
		t.Errorf("PATH still contains host mounts: %s", path)
	}
	if env["JAIL_TIMEOUT"] != "300" {
		t.Errorf("timeout not set: got %q", env["JAIL_TIMEOUT"])
	}
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

	noTimeout := NewEnforcer(Profile{Name: "developer"})
	if got := noTimeout.WrapCommand([]string{"bash", "-lc", "pi"}); !reflect.DeepEqual(got, []string{"bash", "-lc", "pi"}) {
		t.Errorf("WrapCommand(0) should be no-op, got %v", got)
	}
}

func TestEnforcer_FilesystemScopingProducesValidPOSIXEnvNames(t *testing.T) {
	profile := Profile{
		Name: "developer",
		User: "agent",
		Filesystem: map[string]string{
			"/mnt/c/Users/user/Projects": "read-write",
			`C:\Windows\System32`:        "deny",
		},
	}

	e := NewEnforcer(profile)
	env := e.Apply(map[string]string{"PATH": "/usr/bin"})

	for k := range env {
		if !validPOSIXEnvKeyRe.MatchString(k) {
			t.Errorf("generated invalid POSIX env var name %q", k)
		}
	}

	if env["JAIL_FS_MNT_C_USERS_USER_PROJECTS"] != "/mnt/c/Users/user/Projects" {
		t.Errorf("missing sanitized Linux scope key in %v", env)
	}
	if env["JAIL_FS_C_WINDOWS_SYSTEM32"] != `C:\Windows\System32` {
		t.Errorf("missing sanitized Windows scope key in %v", env)
	}

	// Verify honest reporting marks filesystem-scope as Requested and non-root-user as Enforced.
	foundUser := false
	for _, c := range e.Controls() {
		if c.Name == "filesystem-scope" && c.Level != Requested {
			t.Errorf("expected filesystem-scope level %q, got %q", Requested, c.Level)
		}
		if c.Name == "non-root-user" {
			foundUser = true
			if c.Level != Enforced {
				t.Errorf("expected non-root-user level %q, got %q", Enforced, c.Level)
			}
		}
	}
	if !foundUser {
		t.Error("expected non-root-user control")
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
