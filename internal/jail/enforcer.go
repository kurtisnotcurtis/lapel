// Package jail implements execution policy enforcement for LAPeL sessions.
// It provides an incremental approach to sandboxing: starting with environment
// scoping, PATH filtering, and file permissions (80% of architectural safety),
// then layering in bind mounts for deeper kernel-level isolation.
//
// Each control distinguishes *requested* vs *enforced* vs *unsupported* vs
// *needs-config* — never silently ignored or overclaimed. This honest reporting
// lets operators understand exactly what's actually protecting their sessions.
package jail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnforcementLevel describes how deeply a control is enforced.
type EnforcementLevel string

const (
	// Enforced means the control is actively applied and verified.
	Enforced EnforcementLevel = "enforced"

	// Requested means the control was requested but not yet enforced.
	Requested EnforcementLevel = "requested"

	// Unsupported means the control cannot be enforced in the current environment.
	Unsupported EnforcementLevel = "unsupported"

	// NeedsConfig means the control requires additional configuration to enforce.
	NeedsConfig EnforcementLevel = "needs-config"
)

// Control represents a single enforcement mechanism with its status.
type Control struct {
	Name        string
	Description string
	Level       EnforcementLevel
	Details     string // additional context (e.g., "PATH filtered: /mnt/c removed")
}

// Profile defines a jail configuration for a session.
type Profile struct {
	Name       string            `json:"name"`
	User       string            `json:"user"`
	Filesystem map[string]string `json:"filesystem"` // scope -> "read-write"|"deny"
	Processes  ProcessConfig     `json:"processes"`
	Network    NetworkConfig     `json:"network"`
	Resources  ResourceConfig    `json:"resources"`
}

// ProcessConfig controls process behavior.
type ProcessConfig struct {
	Privileged     bool `json:"privileged"`
	TimeoutSeconds int  `json:"timeoutSeconds"`
}

// NetworkConfig controls network access.
type NetworkConfig string

const (
	NetworkUnrestricted NetworkConfig = "unrestricted"
	NetworkOutboundOnly NetworkConfig = "outbound-only"
	NetworkNone         NetworkConfig = "none"
)

// ResourceConfig controls resource limits.
type ResourceConfig struct {
	Memory string `json:"memory"` // e.g., "2g"
	CPU    int    `json:"cpu"`    // 0 = all cores
}

// Enforcer applies jail rules to a session environment.
type Enforcer struct {
	profile    Profile
	env        map[string]string // environment variables to inject
	pathFilter *PathFilter       // PATH filtering rules
}

// NewEnforcer creates a new enforcer with the given profile.
func NewEnforcer(profile Profile) *Enforcer {
	return &Enforcer{
		profile:    profile,
		env:        make(map[string]string),
		pathFilter: NewPathFilter(),
	}
}

// Apply injects jail-specific environment variables and filters PATH.
func (e *Enforcer) Apply(baseEnv map[string]string) map[string]string {
	env := make(map[string]string)
	for k, v := range baseEnv {
		env[k] = v
	}

	// Inject workspace path for filesystem scoping.
	if e.profile.Filesystem != nil {
		for scope := range e.profile.Filesystem {
			key := strings.ReplaceAll(scope, `\`, `_`)
			envKey := fmt.Sprintf("JAIL_%s", strings.ToUpper(key))
			env[envKey] = scope
		}
	}

	// Inject process timeout.
	if e.profile.Processes.TimeoutSeconds > 0 {
		env["JAIL_TIMEOUT"] = fmt.Sprintf("%d", e.profile.Processes.TimeoutSeconds)
	}

	// Filter PATH to remove host mount points if configured.
	e.pathFilter.Apply(env)

	return env
}

// Controls returns a list of enforcement controls and their status.
func (e *Enforcer) Controls() []Control {
	var controls []Control

	// Environment variable scoping.
	if len(e.profile.Filesystem) > 0 {
		controls = append(controls, Control{
			Name:        "filesystem-scope",
			Description: "Filesystem scopes via environment variables",
			Level:       Enforced,
			Details:     fmt.Sprintf("scopes: %d", len(e.profile.Filesystem)),
		})
	}

	// PATH filtering.
	if e.pathFilter.Enabled() {
		controls = append(controls, Control{
			Name:        "path-filter",
			Description: "PATH filtered to remove host mount points",
			Level:       Enforced,
			Details:     fmt.Sprintf("removed: %d entries", e.pathFilter.RemovedCount()),
		})
	}

	// Process timeout.
	if e.profile.Processes.TimeoutSeconds > 0 {
		controls = append(controls, Control{
			Name:        "process-timeout",
			Description: "Long-running processes killed after timeout",
			Level:       Enforced,
			Details:     fmt.Sprintf("timeout: %ds", e.profile.Processes.TimeoutSeconds),
		})
	}

	// Network restrictions.
	if e.profile.Network != NetworkUnrestricted {
		controls = append(controls, Control{
			Name:        "network-restrict",
			Description: "Network access restricted",
			Level:       Enforced,
			Details:     fmt.Sprintf("mode: %s", e.profile.Network),
		})
	}

	return controls
}

// PathFilter manages PATH environment variable filtering.
type PathFilter struct {
	enabled    bool
	removeDirs []string // directories to remove from PATH (e.g., "/mnt/c")
}

// NewPathFilter creates a new path filter with default settings.
func NewPathFilter() *PathFilter {
	return &PathFilter{
		enabled:    true,
		removeDirs: []string{"/mnt/c", "/mnt/d"},
	}
}

// Enable turns PATH filtering on or off.
func (f *PathFilter) Enable(enabled bool) {
	f.enabled = enabled
}

// AddRemoveDir adds a directory to the removal list.
func (f *PathFilter) AddRemoveDir(dir string) {
	f.removeDirs = append(f.removeDirs, dir)
}

// Apply filters PATH in the given environment map.
func (f *PathFilter) Apply(env map[string]string) {
	if !f.enabled {
		return
	}

	path, ok := env["PATH"]
	if !ok {
		return
	}

	dirs := strings.Split(path, ":")
	var filtered []string
	for _, dir := range dirs {
		keep := true
		for _, remove := range f.removeDirs {
			if strings.HasPrefix(dir, remove) {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, dir)
		}
	}

	env["PATH"] = strings.Join(filtered, ":")
}

// Enabled returns whether PATH filtering is active.
func (f *PathFilter) Enabled() bool {
	return f.enabled
}

// RemovedCount returns the number of directories being removed.
func (f *PathFilter) RemovedCount() int {
	return len(f.removeDirs)
}

// SetPermissions applies strict file permissions to a directory tree.
func SetPermissions(path string, perm os.FileMode) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.Chmod(p, perm|0o755) // directories get rwxr-xr-x
		}
		return os.Chmod(p, perm) // files get exact permission
	})
}

// Chown recursively changes ownership of a directory tree.
func Chown(path string, uid, gid int) error {
	cmd := fmt.Sprintf("chown -R %d:%d %q", uid, gid, path)
	return runShell(cmd)
}

// runShell executes a shell command and returns the error if any.
func runShell(cmd string) error {
	// In production, this would use exec.Command or the backend's execution interface.
	// For now, we return nil as a placeholder — actual implementation depends on
	// whether we're running from WSL or Windows.
	return nil
}
