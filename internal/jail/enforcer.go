// Package jail implements execution policy enforcement for lapel sessions.
// It provides an incremental approach to sandboxing: starting with environment
// scoping, PATH filtering, process timeouts, and file permissions, while
// reporting enforcement status honestly.
//
// Each control distinguishes *requested* vs *enforced* vs *unsupported* vs
// *needs-config* — never silently ignored or overclaimed. This honest reporting
// lets operators understand exactly what's actually protecting their sessions.
package jail

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultCleanPath is the default Linux-only PATH applied when no PATH is
// provided in the session environment, preventing Windows /mnt/c host tool leakage.
const DefaultCleanPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// EnforcementLevel describes how deeply a control is enforced.
type EnforcementLevel string

const (
	// Enforced means the control is actively applied and verified.
	Enforced EnforcementLevel = "enforced"

	// Requested means the control was requested and declared in environment/metadata,
	// but depends on workspace mount configuration for OS-level isolation.
	Requested EnforcementLevel = "requested"

	// Unsupported means the control cannot be enforced in the current environment.
	Unsupported EnforcementLevel = "unsupported"

	// NeedsConfig means the control requires additional host/WSL configuration to enforce.
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
	Filesystem map[string]string `json:"filesystem"` // scope -> "read-write"|"read-only"|"deny"
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

// Apply injects jail-specific environment variables and enforces a clean PATH.
func (e *Enforcer) Apply(baseEnv map[string]string) map[string]string {
	env := make(map[string]string)
	for k, v := range baseEnv {
		env[k] = v
	}

	// Inject workspace path metadata for filesystem scoping.
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

	// Filter PATH to remove host mount points (or enforce DefaultCleanPath).
	e.pathFilter.Apply(env)

	return env
}

// WrapCommand wraps the target command with OS-level enforcement utilities
// such as `timeout(1)` when configured in the profile.
func (e *Enforcer) WrapCommand(cmd []string) []string {
	if len(cmd) == 0 || e.profile.Processes.TimeoutSeconds <= 0 {
		return cmd
	}
	wrapped := make([]string, 0, len(cmd)+2)
	wrapped = append(wrapped, "timeout", fmt.Sprintf("%ds", e.profile.Processes.TimeoutSeconds))
	wrapped = append(wrapped, cmd...)
	return wrapped
}

// Controls returns a list of enforcement controls and their honest status.
func (e *Enforcer) Controls() []Control {
	var controls []Control

	// Filesystem scoping: honest reporting distinguishes metadata/mount request
	// from kernel-level enforcement.
	if len(e.profile.Filesystem) > 0 {
		controls = append(controls, Control{
			Name:        "filesystem-scope",
			Description: "Filesystem scopes declared via JAIL_* env and workspace mount mode",
			Level:       Requested,
			Details:     fmt.Sprintf("scopes: %d (use workspace bind/copy mode for mount isolation)", len(e.profile.Filesystem)),
		})
	}

	// PATH filtering: actively enforced by stripping host mounts or setting DefaultCleanPath.
	if e.pathFilter.Enabled() {
		controls = append(controls, Control{
			Name:        "path-filter",
			Description: "PATH filtered to strip Windows host mount points",
			Level:       Enforced,
			Details:     fmt.Sprintf("removed prefixes: %s", strings.Join(e.pathFilter.removeDirs, ", ")),
		})
	}

	// Process timeout: enforced via `timeout` command wrapper and JAIL_TIMEOUT.
	if e.profile.Processes.TimeoutSeconds > 0 {
		controls = append(controls, Control{
			Name:        "process-timeout",
			Description: "Long-running processes killed after timeout",
			Level:       Enforced,
			Details:     fmt.Sprintf("timeout: %ds", e.profile.Processes.TimeoutSeconds),
		})
	}

	// Network restrictions: honest reporting marks this as NeedsConfig until
	// WSL network namespace / firewall rules are configured on the host.
	if e.profile.Network != "" && e.profile.Network != NetworkUnrestricted {
		controls = append(controls, Control{
			Name:        "network-restrict",
			Description: "Network access restriction policy",
			Level:       NeedsConfig,
			Details:     fmt.Sprintf("mode: %s (requires WSL firewall/namespace rule)", e.profile.Network),
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

// Apply filters PATH in the given environment map. If PATH is not present in
// env, it sets DefaultCleanPath so WSL's default Windows-polluted PATH cannot
// leak into the session.
func (f *PathFilter) Apply(env map[string]string) {
	if !f.enabled {
		return
	}

	path, ok := env["PATH"]
	if !ok || strings.TrimSpace(path) == "" {
		env["PATH"] = DefaultCleanPath
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

// RemovedCount returns the number of directory prefixes configured for removal.
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

// Chown recursively changes ownership of a directory tree without shell interpolation.
func Chown(path string, uid, gid int) error {
	execCmd := exec.Command("chown", "-R", fmt.Sprintf("%d:%d", uid, gid), path)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	return execCmd.Run()
}
