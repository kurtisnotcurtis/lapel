// Package domain holds the core, backend-independent objects of lapel.
//
// These types must stay independent so the model remains stable when new
// execution backends (VM, container) are introduced. See spec section 2.
package domain

import (
	"time"
)

// Provider is an inference endpoint and its configuration.
type Provider struct {
	Name          string `json:"name"`
	Type          string `json:"type"` // e.g. "lm-studio", "ollama"
	BaseURL       string `json:"baseURL"`
	Model         string `json:"model"`
	APIKeySecret  string `json:"apiKeySecret"` // stored outside the manifest, in a secret store
	ConnectionTimeout time.Duration `json:"connectionTimeout"`
}

// ProviderStatus is runtime status for an inference provider.
type ProviderStatus struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Installed bool              `json:"installed"`
	Running   bool              `json:"running"`
	Port      int               `json:"port"`
	BindAddr  string            `json:"bindAddr"`
	Models    []string          `json:"models,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// Agent is a supported coding-agent harness.
type Agent struct {
	Name     string `json:"name"`     // e.g. "pi"
	Harness  string `json:"harness"`  // canonical harness id
	Provider string `json:"provider"` // logical provider name reference
}

// Environment is a persistent Linux runtime (e.g. WSL2 Ubuntu).
type Environment struct {
	Name         string `json:"name"`
	Backend      string `json:"backend"` // e.g. "wsl"
	Distribution string `json:"distribution"`
	Runtimes     map[string]string `json:"runtimes"`  // toolchain -> pinned major version
	Packages     []string            `json:"packages"`  // apt packages to install
	RootFSImage  string `json:"rootFSImage"` // pinned rootfs + checksum
}

// Workspace is a project directory available to a session.
type Workspace struct {
	Name       string `json:"name"`
	Path       string `json:"path"`   // WSL-native path when backend storage is used
	Source     string `json:"source"`
	ImportMode string `json:"importMode"` // "copy" | "bind"
}

// Jail is an execution policy applied per session.
type Jail struct {
	Name       string         `json:"name"`
	Profile    string         `json:"profile"` // e.g. "developer"
	User       string         `json:"user"`    // dedicated non-root user
	Filesystem map[string]string `json:"filesystem"` // scope -> "read-write"|"deny"
	Processes  struct {
		Privileged bool    `json:"privileged"`
		TimeoutSeconds int `json:"timeoutSeconds"`
	} `json:"processes"`
	Network   string `json:"network"` // "unrestricted" | ...
	Resources struct {
		Memory string `json:"memory"`
		CPU    int    `json:"cpu"`
	} `json:"resources"`
}

// Session is a running (or historical) agent process.
type Session struct {
	ID            string        `json:"id"`
	Agent         string        `json:"agent"`
	Provider      string        `json:"provider"`
	Environment   string        `json:"environment"`
	Workspace     string        `json:"workspace"`
	Jail          string        `json:"jail"`
	State         string        `json:"state"` // Created, Running, Completed, Failed...
	StartedAt     time.Time     `json:"startedAt"`
	FinishedAt    *time.Time    `json:"finishedAt,omitempty"`
	ExitCode      int           `json:"exitCode"`
	ProcessIDs    []int         `json:"processIds"`
	EffectiveJail string        `json:"effectiveJail"` // reported enforcement summary
}

// EnvironmentStatus is backend-reported status for an environment.
type EnvironmentStatus struct {
	Exists      bool   `json:"exists"`
	Distribution string `json:"distribution"`
	Version     string `json:"version"`
	Running     bool   `json:"running"`
	Metadata    map[string]string `json:"metadata"`
}

// BackendStatus is backend-reported overall status.
type BackendStatus struct {
	Name      string `json:"name"` // "wsl"
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Note      string `json:"note"`
}

// ExecRequest is a structured request to run inside the backend.
type ExecRequest struct {
	Dist       string
	User       string
	WorkingDir string
	Cmd        []string // fully split command; never shell-concatenated
	Env        map[string]string
	Stdin      bool
}

// ExecResult is the structured outcome of an execution.
type ExecResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	Duration   time.Duration
	Killed     bool
}
