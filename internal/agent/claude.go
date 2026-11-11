package agent

import (
	"context"
	"fmt"
)

// ClaudeCodeAgent implements Agent for Anthropic's Claude Code CLI.
type ClaudeCodeAgent struct{}

func NewClaudeCodeAgent() *ClaudeCodeAgent { return &ClaudeCodeAgent{} }

func (a *ClaudeCodeAgent) Name() string { return "claude-code" }

func (a *ClaudeCodeAgent) Detect(ctx context.Context) (bool, error) {
	// TODO: implement actual detection using exec.Command
	return true, nil
}

func (a *ClaudeCodeAgent) Configure(env map[string]string, providerURL string, model string) {
	env["ANTHROPIC_BASE_URL"] = providerURL + "/v1"
	env["ANTHROPIC_MODEL_NAME"] = model
	env["ANTHROPIC_API_KEY"] = "not-needed-for-local"
}

func (a *ClaudeCodeAgent) Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error {
	fmt.Printf("\nLaunching claude-code with %s at %s/v1\n", model, providerURL)
	return fmt.Errorf("not yet implemented")
}
