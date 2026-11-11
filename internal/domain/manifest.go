// Package domain defines the lapel manifest — a declarative description
// of the Linux environment, jail rules, and runtime configuration that
// lapel provisions into WSL. The manifest is the single source of truth:
// `apply` reconciles the live distro against it; `status` reports drift.
package domain

import "time"

// Manifest is the top-level declarative config file (lapel.yaml).
// All fields are optional — sensible defaults apply where omitted.
type Manifest struct {
	// SchemaVersion pins the manifest format version. Incremented on breaking
	// changes to field semantics or structure. v1 is the current schema.
	SchemaVersion int `yaml:"schemaVersion" json:"schemaVersion"`

	// Distro configures the WSL distribution that lapel manages.
	Distro DistroConfig `yaml:"distro" json:"distro"`

	// Packages controls which apt packages are installed beyond the baseline
	// agent toolkit (git, curl, tmux, openssh-server, etc.).
	Packages PackageConfig `yaml:"packages,omitempty" json:"packages,omitempty"`

	// Jail defines filesystem scoping and execution constraints for agents.
	Jail JailManifest `yaml:"jail,omitempty" json:"jail,omitempty"`

	// Env are environment variables injected into every session inside the
	// distro (e.g., API keys, feature flags). Sensitive values should reference
	// a secret store rather than being stored plaintext in the manifest.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// CreatedAt records when this manifest was first written. Set by apply.
	CreatedAt time.Time `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`

	// UpdatedAt records the last reconcile. Set by apply.
	UpdatedAt time.Time `yaml:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

// DistroConfig describes the WSL distribution to provision and manage.
type DistroConfig struct {
	// Name is the WSL distribution name (e.g., "Ubuntu-Lean"). Required.
	Name string `yaml:"name" json:"name"`

	// Base selects the image family: "ubuntu" or "debian". Defaults to
	// "ubuntu" when empty. Determines the default rootfs URL pattern.
	Base string `yaml:"base,omitempty" json:"base,omitempty"`

	// Version pins the release (e.g., "24.04", "bookworm"). When omitted a
	// sensible default per base is used.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// RootFSURL overrides the auto-derived rootfs URL. When set, Base and
	// Version are ignored for download purposes. Useful for custom images.
	RootFSURL string `yaml:"rootfsUrl,omitempty" json:"rootfsUrl,omitempty"`

	// DefaultUser sets the WSL default user so the distro doesn't start as
	// root. Defaults to $USER from the Windows environment.
	DefaultUser string `yaml:"defaultUser,omitempty" json:"defaultUser,omitempty"`

	// Systemd enables systemd in /etc/wsl.conf. Defaults to true.
	Systemd *bool `yaml:"systemd,omitempty" json:"systemd,omitempty"`

	// AppendWindowsPaths controls [interop] appendWindowsPaths in wsl.conf.
	// nil (default) = auto-detect based on Windows build number. false forces
	// suppression; true forces inclusion.
	AppendWindowsPaths *bool `yaml:"appendWindowsPaths,omitempty" json:"appendWindowsPaths,omitempty"`
}

// PackageConfig controls apt package installation.
type PackageConfig struct {
	// Baseline overrides the default agent-toolkit packages (git, curl, tmux,
	// openssh-server, etc.). Nil means use the built-in defaults.
	Baseline []string `yaml:"baseline,omitempty" json:"baseline,omitempty"`

	// Extras are additional packages installed on top of the baseline.
	Extras []string `yaml:"extras,omitempty" json:"extras,omitempty"`
}

// JailManifest defines execution constraints for agents running in the distro.
type JailManifest struct {
	// Name is a human-readable label (e.g., "developer", "ci-worker").
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// User is the dedicated non-root user the agent runs as. Defaults to
	// Distro.DefaultUser when empty.
	User string `yaml:"user,omitempty" json:"user,omitempty"`

	// Filesystem maps host paths (Windows-style, e.g., "C:\\Users\\me\\proj")
	// to access mode: "read-write", "read-only", or "deny". Paths not listed
	// default to read-only. An empty map means the agent has no filesystem
	// access beyond /tmp and its home directory.
	Filesystem map[string]string `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`

	// Network controls outbound connectivity from the distro. Options:
	// "unrestricted" (default), "outbound-only", "none".
	Network string `yaml:"network,omitempty" json:"network,omitempty"`

	// Processes configures process-level constraints.
	Processes ProcessJail `yaml:"processes,omitempty" json:"processes,omitempty"`

	// Resources sets resource limits for agent processes.
	Resources ResourceLimits `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// ProcessJail controls process execution within the jail.
type ProcessJail struct {
	// Privileged allows the agent to run commands as root inside the distro.
	// Defaults to false.
	Privileged bool `yaml:"privileged,omitempty" json:"privileged,omitempty"`

	// TimeoutSeconds kills processes that exceed this duration. 0 means no limit.
	TimeoutSeconds int `yaml:"timeoutSeconds,omitempty" json:"timeoutSeconds,omitempty"`
}

// ResourceLimits sets resource caps for agent processes.
type ResourceLimits struct {
	// Memory is a memory cap string (e.g., "2g"). Empty means no limit.
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`

	// CPU is the number of CPU cores available to the agent. 0 means all cores.
	CPU int `yaml:"cpu,omitempty" json:"cpu,omitempty"`
}
