// Package domain holds the core, backend-independent objects of lapel.
package domain

import (
	"time"
)

// Provider is an inference endpoint and its configuration.
type Provider struct {
	Name              string        `json:"name"`
	Type              string        `json:"type"` // e.g. "lm-studio", "ollama", "openai-compatible"
	BaseURL           string        `json:"baseURL"`
	Model             string        `json:"model"`
	ConnectionTimeout time.Duration `json:"connectionTimeout"`
}

// EnvironmentStyle configures the shell prompt, welcome banner, and Windows
// Terminal appearance for a managed environment.
type EnvironmentStyle struct {
	Preset          string `json:"preset,omitempty"`          // "lapel-dark" | "violet" | "minimal" | "none"
	Badge           string `json:"badge,omitempty"`           // custom prompt badge text (defaults to distro name)
	Banner          *bool  `json:"banner,omitempty"`          // show one-line login banner
	TabColor        string `json:"tabColor,omitempty"`        // Windows Terminal tab hex color (#RRGGBB)
	Background      string `json:"background,omitempty"`      // Windows Terminal scheme background hex (#RRGGBB)
	WindowsTerminal *bool  `json:"windowsTerminal,omitempty"` // write Windows Terminal JSON fragment
}

// Environment is a persistent Linux runtime (e.g. WSL2 Ubuntu).
type Environment struct {
	Name         string           `json:"name"`
	Backend      string           `json:"backend"` // e.g. "wsl"
	Distribution string           `json:"distribution"`
	Base         string           `json:"base,omitempty"`
	Version      string           `json:"version,omitempty"`
	Packages     []string         `json:"packages"`          // apt packages to install
	Systemd      *bool            `json:"systemd,omitempty"` // boot WSL with systemd (nil defaults to true)
	Style        EnvironmentStyle `json:"style,omitempty"`   // shell prompt & terminal styling
}

// EnvironmentStatus is backend-reported status for an environment.
type EnvironmentStatus struct {
	Exists       bool              `json:"exists"`
	Distribution string            `json:"distribution"`
	Version      string            `json:"version"`
	Running      bool              `json:"running"`
	Metadata     map[string]string `json:"metadata"`
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
}

// ExecResult is the structured outcome of an execution.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}
