// Package agent manages coding-agent harness detection, configuration, and launching.
package agent

import "context"

// Agent is a supported coding-agent harness (e.g., pi, claude-code, copilot).
type Agent interface {
	// Name returns the agent identifier (e.g., "pi", "claude-code").
	Name() string

	// Detect checks if the agent is installed in the target environment.
	Detect(ctx context.Context) (bool, error)

	// Configure injects provider connection details into the env map.
	// The env map will be passed to the agent process.
	Configure(env map[string]string, providerURL string, model string)

	// Launch starts the agent in interactive mode with jail applied.
	Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error
}
