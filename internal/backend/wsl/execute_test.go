package wsl

import (
	"reflect"
	"strings"
	"testing"

	"lapel/internal/domain"
)

func TestBuildScript(t *testing.T) {
	env := map[string]string{
		"OPENAI_API_BASE_URL": "http://172.29.16.1:1234/v1",
		"PATH":                "/usr/bin:/bin",
	}
	cmd := []string{"bash", "-c", "echo 'hi'"}
	script := buildScript("/home/agent", env, cmd)

	want := "export 'OPENAI_API_BASE_URL'='http://172.29.16.1:1234/v1'; export 'PATH'='/usr/bin:/bin'; cd '/home/agent' && 'bash' '-c' 'echo '\\''hi'\\'''"
	if script != want {
		t.Errorf("buildScript =\n  %q\nwant\n  %q", script, want)
	}
}

func TestBuildInteractiveWSLArgs(t *testing.T) {
	req := domain.ExecRequest{
		Dist:       "my-distro",
		User:       "agent",
		WorkingDir: "/home/agent/workspace/demo",
		Env: map[string]string{
			"OPENAI_API_BASE_URL": "http://172.29.16.1:1234/v1",
			"PATH":                "/usr/bin:/bin",
		},
		Cmd: []string{"bash", "-lc", "pi"},
	}

	got, err := buildInteractiveWSLArgs("lapel", req)
	if err != nil {
		t.Fatalf("buildInteractiveWSLArgs: %v", err)
	}
	want := []string{
		"-d", "my-distro",
		"-u", "agent",
		"--",
		"env",
		"OPENAI_API_BASE_URL=http://172.29.16.1:1234/v1",
		"PATH=/usr/bin:/bin",
		"sh", "-c", `cd "$1" 2>/dev/null || cd "$HOME" 2>/dev/null || true; shift; exec "$@"`,
		"_", "/home/agent/workspace/demo",
		"bash", "-lc", "pi",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildInteractiveWSLArgs =\n  %#v\nwant\n  %#v", got, want)
	}
}

func TestBuildInteractiveWSLArgsRejectsInvalidUser(t *testing.T) {
	_, err := buildInteractiveWSLArgs("lapel", domain.ExecRequest{
		User: "agent; rm -rf /",
		Cmd:  []string{"bash"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid user error, got %v", err)
	}
}

func TestBuildInteractiveWSLArgsDefaultsDistroAndCommand(t *testing.T) {
	got, err := buildInteractiveWSLArgs("", domain.ExecRequest{})
	if err != nil {
		t.Fatalf("buildInteractiveWSLArgs: %v", err)
	}
	want := []string{"-d", DefaultDist, "--", "bash", "-l"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildInteractiveWSLArgs = %#v, want %#v", got, want)
	}
}
