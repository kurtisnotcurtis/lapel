package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ClaudeCodeAgent implements Agent for Anthropic's Claude Code CLI.
type ClaudeCodeAgent struct{}

func NewClaudeCodeAgent() *ClaudeCodeAgent { return &ClaudeCodeAgent{} }

func (a *ClaudeCodeAgent) Name() string { return "claude-code" }

func (a *ClaudeCodeAgent) Detect(ctx context.Context) (bool, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return false, nil
	}
	_, err = exec.CommandContext(ctx, path, "--version").Output()
	return err == nil, nil
}

func (a *ClaudeCodeAgent) Configure(env map[string]string, providerURL string, model string) {
	env["ANTHROPIC_BASE_URL"] = providerURL + "/v1"
	env["ANTHROPIC_MODEL_NAME"] = model
	env["ANTHROPIC_API_KEY"] = "not-needed-for-local"
}

func (a *ClaudeCodeAgent) Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error {
	fmt.Printf("\nLaunching claude-code with %s at %s/v1\n", model, providerURL)

	cmd := exec.CommandContext(ctx, "claude")
	cmd.Dir = workingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude exited: %w", err)
	}
	return nil
}
