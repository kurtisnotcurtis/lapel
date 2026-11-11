// Package provider resolves and tests inference endpoints from inside WSL,
// plus manages local inference providers (LM Studio, Ollama).
package provider

import "context"

// Manager is the interface for inference provider lifecycle management.
type Manager interface {
	// Name returns the provider type identifier (e.g., "lm-studio", "ollama").
	Name() string

	// Detect checks if the provider is installed on the Windows host.
	Detect(ctx context.Context) (bool, error)

	// Status returns runtime status including port, bind address, and running state.
	Status(ctx context.Context) (*ProviderStatus, error)

	// Start starts the inference server if not already running.
	Start(ctx context.Context) error

	// Stop stops the inference server.
	Stop(ctx context.Context) error

	// ListModels returns available model identifiers.
	ListModels(ctx context.Context) ([]string, error)
}

// ProviderStatus is runtime status for an inference provider.
type ProviderStatus struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Installed bool     `json:"installed"`
	Running   bool     `json:"running"`
	Port      int      `json:"port"`
	BindAddr  string   `json:"bindAddr"`
	Models    []string `json:"models,omitempty"`
	Error     string   `json:"error,omitempty"`
}
