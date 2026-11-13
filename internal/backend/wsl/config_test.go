package wsl

import (
	"strings"
	"testing"
)

func TestParseINIConfig(t *testing.T) {
	input := `# WSL config
[wsl2]
memory=4GB
processors=4
swap=1GB
guiApplications=false

[experimental]
proxyDns=true`
	m := parseINIConfig(input)
	if got, want := m["memory"], "4GB"; got != want {
		t.Errorf("memory = %q, want %q", got, want)
	}
	if got, want := m["processors"], "4"; got != want {
		t.Errorf("processors = %q, want %q", got, want)
	}
	if got, want := m["swap"], "1GB"; got != want {
		t.Errorf("swap = %q, want %q", got, want)
	}
	if got, want := m["guiApplications"], "false"; got != want {
		t.Errorf("guiApplications = %q, want %q", got, want)
	}
	// [experimental] section must be ignored — its keys should not leak in.
	if _, ok := m["proxyDns"]; ok {
		t.Errorf("non-[wsl2] key leaked into parsed map")
	}
}

func TestBuildConfigReportControls(t *testing.T) {
	m := parseINIConfig(`[wsl2]
memory=4GB
processors=4
swap=1GB
guiApplications=false
networkingMode=mirrored`)
	r := buildConfigReport("/mnt/c/Users/user/.wslconfig", "ini", m)

	if !r.Enforced(CtrlMemory) || !r.Enforced(CtrlProcessors) || !r.Enforced(CtrlSwap) {
		t.Errorf("expected memory/processors/swap enforced, got %+v", r.Controls)
	}
	if !r.GuiAppsEnforced {
		t.Errorf("guiApplications=false should be reported as enforced (explicit), got %v", r.GuiAppsEnforced)
	}
	if _, ok := r.Controls[CtrlGuiApps]; !ok {
		t.Errorf("expected guiApplications control present")
	}
	found := false
	for _, n := range r.Notes {
		if n == "networkingMode=mirrored" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected networkingMode note, got %+v", r.Notes)
	}
}

func TestBuildConfigReportWarnings(t *testing.T) {
	m := parseINIConfig(`[wsl2]
networkingMode=mirrored
generateResolvConf=false`)
	r := buildConfigReport("/x/.wslconfig", "ini", m)

	if len(r.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %+v", len(r.Warnings), r.Warnings)
	}
	joined := ""
	for _, w := range r.Warnings {
		joined += w + "\n"
	}
	if !contains(joined, "mirrored") || !contains(joined, "port forwarding") {
		t.Errorf("expected mirrored networking warning, got: %s", joined)
	}
	if !contains(joined, "generateResolvConf=false") || !contains(joined, "/etc/resolv.conf") {
		t.Errorf("expected resolv.conf warning, got: %s", joined)
	}

	// No warnings when the risky keys are absent or set to safe values.
	safe := parseINIConfig(`[wsl2]
networkingMode=nat
generateResolvConf=true`)
	if r := buildConfigReport("/x/.wslconfig", "ini", safe); len(r.Warnings) != 0 {
		t.Errorf("safe config should have no warnings, got %+v", r.Warnings)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func TestControlLineState(t *testing.T) {
	m := parseINIConfig(`[wsl2]
memory=4GB`)
	r := buildConfigReport("/x/.wslconfig", "ini", m)

	if line := r.ControlLine(CtrlMemory); line != "memory: ENFORCED (4GB)" {
		t.Errorf("memory line = %q, want %q", line, "memory: ENFORCED (4GB)")
	}
	if line := r.ControlLine(CtrlProcessors); line != "processors: UNAVAILABLE ()" {
		t.Errorf("processors line = %q, want %q", line, "processors: UNAVAILABLE ()")
	}
	if r.Enforced(CtrlProcessors) {
		t.Errorf("unprocessed key should not be enforced")
	}
}

func TestIniOrJSONFormat(t *testing.T) {
	if got := iniOrJSONFormat("/mnt/c/Users/user/.wslconfig"); got != "ini" {
		t.Errorf("format = %q, want ini", got)
	}
	if got := iniOrJSONFormat("/mnt/c/Users/user/.wslconfig.json"); got != "json" {
		t.Errorf("format = %q, want json", got)
	}
}
