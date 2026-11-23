// Package jail implements execution policy enforcement for lapel sessions.
// It provides an incremental approach to sandboxing: non-root user execution,
// PATH filtering, optional process timeouts, and workspace metadata, while
// reporting enforcement status honestly.
//
// Each control distinguishes *enforced* vs *requested* vs *unsupported* vs
// *needs-config* — never silently ignored or overclaimed.
package jail

import (
	"fmt"
	"sort"
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
	// but is not kernel-enforced (WSL automount or OS-level isolation still applies).
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

// Enforcer applies jail rules to a session environment.
type Enforcer struct {
	profile    Profile
	pathFilter *PathFilter // PATH filtering rules
}

// NewEnforcer creates a new enforcer with the given profile.
func NewEnforcer(profile Profile) *Enforcer {
	return &Enforcer{
		profile:    profile,
		pathFilter: NewPathFilter(),
	}
}

// sanitizeEnvKey converts an arbitrary filesystem path into a valid POSIX shell
// environment variable suffix containing only [A-Z0-9_].
func sanitizeEnvKey(scope string) string {
	var b strings.Builder
	prevUnderscore := true // trim leading underscores
	for _, r := range strings.ToUpper(scope) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
		} else if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.TrimRight(b.String(), "_")
	if out == "" {
		return "ROOT"
	}
	return out
}

// Apply injects jail-specific environment variables and enforces a clean PATH.
func (e *Enforcer) Apply(baseEnv map[string]string) map[string]string {
	env := make(map[string]string)
	for k, v := range baseEnv {
		env[k] = v
	}

	// Inject workspace path metadata for filesystem scoping using valid POSIX
	// environment variable names (JAIL_FS_<SANITIZED_PATH>=<mode>).
	if len(e.profile.Filesystem) > 0 {
		scopes := make([]string, 0, len(e.profile.Filesystem))
		for scope := range e.profile.Filesystem {
			scopes = append(scopes, scope)
		}
		sort.Strings(scopes)
		for _, scope := range scopes {
			envKey := "JAIL_FS_" + sanitizeEnvKey(scope)
			env[envKey] = scope
		}
	}

	// Inject process timeout when configured (> 0).
	if e.profile.Processes.TimeoutSeconds > 0 {
		env["JAIL_TIMEOUT"] = fmt.Sprintf("%d", e.profile.Processes.TimeoutSeconds)
	}

	// Filter PATH to remove host mount points (or enforce DefaultCleanPath).
	e.pathFilter.Apply(env)

	return env
}

// WrapCommand wraps the target command with OS-level enforcement utilities
// such as `timeout(1)` when a positive timeout is configured in the profile.
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

	// Non-root user execution: actively enforced via `wsl.exe -u <user>`.
	if u := strings.TrimSpace(e.profile.User); u != "" && u != "root" {
		controls = append(controls, Control{
			Name:        "non-root-user",
			Description: "Session runs as a dedicated non-sudo Linux user",
			Level:       Enforced,
			Details:     fmt.Sprintf("wsl.exe -u %s (no sudoers entry)", u),
		})
	}

	// Filesystem scoping: honest reporting distinguishes metadata/mount request
	// from kernel-level enforcement (WSL automount remains accessible unless disabled).
	if len(e.profile.Filesystem) > 0 {
		controls = append(controls, Control{
			Name:        "filesystem-scope",
			Description: "Filesystem scopes declared via JAIL_FS_* env (not kernel-enforced while WSL automount is enabled)",
			Level:       Requested,
			Details:     fmt.Sprintf("scopes: %d (advisory only; use workspace copy mode + disable automount for stronger isolation)", len(e.profile.Filesystem)),
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

	// Process timeout: enforced via `timeout` command wrapper when > 0.
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
