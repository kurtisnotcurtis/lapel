// Package wsl implements the backend that talks to WSL2 distros: detection,
// execution inside a running distro, resource-config inspection, and optional
// minimal-distro provisioning. It is intentionally generic — it discovers the
// host mount and .wslconfig at runtime rather than hard-coding any single
// machine's layout (e.g. C:\Users\Kurt\.wslconfig).
package wsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resource-control keys reported by the .wslconfig check. These are the WSL2
// [wsl2] section controls that most directly keep a distro "system-sparing".
const (
	CtrlMemory     = "memory"
	CtrlProcessors = "processors"
	CtrlSwap       = "swap"
	CtrlGuiApps    = "guiApplications"
)

// ConfigReport is a read-only assessment of the host .wslconfig resource caps.
// It never mutates anything (report-only). A zero-value report with an empty
// Path simply means no config was found — not an error.
type ConfigReport struct {
	// Path is the resolved config file on the host mount, if one was found.
	Path string `json:"path,omitempty"`
	// Format is "ini" (legacy .wslconfig), "json" (.wslconfig.json), or "" when none.
	Format string `json:"format,omitempty"`
	// Memory / Processors / Swap hold the raw values from [wsl2] (e.g. "4GB").
	Memory     string `json:"memory,omitempty"`
	Processors string `json:"processors,omitempty"`
	Swap       string `json:"swap,omitempty"`
	// GuiAppsEnforced reports whether guiApplications was explicitly configured
	// by the user in [wsl2] (true for either true or false). The raw value is in
	// GuiApps so callers can distinguish an explicit "false" from an unset key.
	GuiAppsEnforced bool `json:"guiAppsEnforced,omitempty"`
	// GuiApps holds the raw guiApplications value as written (e.g. "true").
	GuiApps string `json:"guiApps,omitempty"`
	// Controls reports which resource controls are explicitly set.
	Controls map[string]bool `json:"controls"`
	// Notes surfaces other notable [wsl2] settings (informational, not caps).
	Notes []string `json:"notes,omitempty"`
	// Warnings flags settings that are legal but likely to break host↔guest
	// connectivity or DNS management (e.g. mirrored networking, disabled
	// resolv.conf generation). Empty when nothing needs attention.
	Warnings []string `json:"warnings,omitempty"`
}

// Enforced returns true when the given resource control is explicitly set in
// the config. Unknown keys return false.
func (r ConfigReport) Enforced(key string) bool { return r.Controls[key] }

// ControlLine renders "<key>: <state> (<value>)" for a resource-control key,
// where state is ENFORCED when the control is set and UNAVAILABLE otherwise.
func (r ConfigReport) ControlLine(key string) string {
	state := "UNAVAILABLE"
	if r.Enforced(key) {
		state = "ENFORCED"
	}
	var val string
	switch key {
	case CtrlMemory:
		val = r.Memory
	case CtrlProcessors:
		val = r.Processors
	case CtrlSwap:
		val = r.Swap
	case CtrlGuiApps:
		if r.GuiApps == "" {
			val = "unset"
		} else {
			val = r.GuiApps
		}
	}
	return fmt.Sprintf("%s: %s (%s)", key, state, val)
}

// InspectConfig performs a read-only assessment of the host .wslconfig resource
// caps. It never modifies anything and returns an empty-path report when no
// config is found (rather than an error), so it degrades gracefully on any
// machine or non-WSL environment.
func InspectConfig(_ context.Context) (ConfigReport, error) {
	mount, err := discoverHostMount()
	if err != nil {
		return ConfigReport{}, nil // not WSL / no drive mount: nothing to report
	}
	path, format, err := findConfigPath(mount)
	if err != nil {
		return ConfigReport{Format: "none"}, nil // found the mount, but no config file
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ConfigReport{}, fmt.Errorf("read %s: %w", path, err)
	}
	m := parseINIConfig(string(data))
	report := buildConfigReport(path, format, m)
	return report, nil
}

// discoverHostMount returns the directory that contains per-user profile
// directories. The lapel binary runs natively on Windows (it invokes
// wsl.exe), so there is no /mnt tree on the host side: the users root is
// simply <SystemDrive>\Users, derived from %USERPROFILE%.
func discoverHostMount() (string, error) {
	profile := os.Getenv("USERPROFILE")
	if profile == "" {
		return "", fmt.Errorf("USERPROFILE is not set; cannot locate user profiles")
	}
	usersRoot := filepath.Dir(profile)
	if fi, err := os.Stat(usersRoot); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("stat %s: %w", usersRoot, err)
	}
	return usersRoot, nil
}

// findConfigPath locates the .wslconfig (legacy INI) or .wslconfig.json file.
// It prefers the current user's home, then the WSL APPDATA location used by
// newer WSL releases, and finally falls back to scanning every user home.
func findConfigPath(mount string) (string, string, error) {
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	var candidates []string
	if user != "" {
		home := filepath.Join(mount, user)
		candidates = append(candidates,
			filepath.Join(home, ".wslconfig"),
			filepath.Join(home, ".wslconfig.json"),
			filepath.Join(home, "AppData", "Local", "Microsoft", "WSL", ".wslconfig.json"),
		)
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		candidates = append(candidates, filepath.Join(appdata, "Microsoft", "WSL", ".wslconfig.json"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, iniOrJSONFormat(c), nil
		}
	}
	matches, err := findConfigInUsers(mount)
	if err != nil {
		return "", "", fmt.Errorf("scanning user homes: %w", err)
	}
	if len(matches) > 0 {
		return matches[0], iniOrJSONFormat(matches[0]), nil
	}
	return "", "", fmt.Errorf("no .wslconfig found")
}

// findConfigInUsers scans the immediate child directories of the users root
// (e.g. C:\Users) for a .wslconfig / .wslconfig.json file. It deliberately
// does not recurse into deep trees (AppData, etc.), keeping the scan cheap
// and predictable.
func findConfigInUsers(mount string) ([]string, error) {
	var out []string
	base := mount
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cands := []string{
			filepath.Join(base, e.Name(), ".wslconfig"),
			filepath.Join(base, e.Name(), ".wslconfig.json"),
		}
		for _, c := range cands {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// parseINIConfig extracts [wsl2] section key/value pairs from a legacy .wslconfig.
// Comments use '#' and lines are "key=value". Non-[wsl2] sections are ignored.
func parseINIConfig(text string) map[string]string {
	section := ""
	out := make(map[string]string)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(strings.TrimPrefix(line, "["), "]")
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 || section != "wsl2" {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		out[key] = val
	}
	return out
}

// buildConfigReport maps parsed [wsl2] controls into a ConfigReport and records
// other notable settings as informational notes.
func buildConfigReport(path, format string, m map[string]string) ConfigReport {
	r := ConfigReport{Path: path, Format: format, Controls: map[string]bool{}}
	if v, ok := m["memory"]; ok {
		r.Memory = v
		r.Controls[CtrlMemory] = true
	}
	if v, ok := m["processors"]; ok {
		r.Processors = v
		r.Controls[CtrlProcessors] = true
	}
	if v, ok := m["swap"]; ok {
		r.Swap = v
		r.Controls[CtrlSwap] = true
	}
	// An explicit guiApplications setting (either true or false) is treated as
	// enforced; the raw value is retained in GuiApps so callers can distinguish
	// an explicit restriction from WSL's unset default.
	if v, ok := m["guiApplications"]; ok {
		r.GuiAppsEnforced = true
		r.GuiApps = strings.TrimSpace(v)
		r.Controls[CtrlGuiApps] = true
	}
	for _, k := range []string{"networkingMode", "localhostforwarding", "nestedVirtualization", "debugConsole", "pageReporting"} {
		if v, ok := m[k]; ok {
			r.Notes = append(r.Notes, fmt.Sprintf("%s=%s", k, v))
		}
	}
	// Warnings for settings that are legal but change host↔guest addressing or
	// DNS management in ways that silently break common assumptions (NAT
	// gateway IP for ssh, WSL-managed /etc/resolv.conf).
	if v := m["networkingMode"]; v == "mirrored" {
		r.Warnings = append(r.Warnings,
			"networkingMode=mirrored: no NAT gateway — host↔guest IP assumptions (e.g. ssh via the 172.x gateway) do not hold; verify port forwarding and reachability paths")
	}
	if v := m["generateResolvConf"]; v == "false" {
		r.Warnings = append(r.Warnings,
			"generateResolvConf=false: WSL will not manage /etc/resolv.conf — DNS must be maintained manually (static file or systemd-resolved)")
	}
	return r
}

// iniOrJSONFormat returns the config format suffix for a given path.
func iniOrJSONFormat(p string) string {
	if strings.HasSuffix(strings.ToLower(p), ".json") {
		return "json"
	}
	return "ini"
}
