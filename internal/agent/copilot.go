package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// CopilotAgent implements Agent for GitHub Copilot CLI.
type CopilotAgent struct{}

func NewCopilotAgent() *CopilotAgent { return &CopilotAgent{} }

func (a *CopilotAgent) Name() string { return "copilot" }

func (a *CopilotAgent) Detect(ctx context.Context) (bool, error) {
	path, err := exec.LookPath("copilot")
	if err != nil {
		return false, nil
	}
	_, err = exec.CommandContext(ctx, path, "--version").Output()
	return err == nil, nil
}

func (a *CopilotAgent) Configure(env map[string]string, providerURL string, model string) {
	env["GITHUB_COPILOT_BASE_URL"] = providerURL + "/v1"
	env["GITHUB_COPILOT_MODEL_NAME"] = model
}

func (a *CopilotAgent) Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error {
	fmt.Printf("\nLaunching copilot with %s at %s/v1\n", model, providerURL)

	cmd := exec.CommandContext(ctx, "copilot")
	cmd.Dir = workingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copilot exited: %w", err)
	}
	return nil
}
