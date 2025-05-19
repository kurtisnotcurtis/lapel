package agent

import (
	"context"
	"fmt"
)

// CopilotAgent implements Agent for GitHub Copilot CLI.
type CopilotAgent struct{}

func NewCopilotAgent() *CopilotAgent { return &CopilotAgent{} }

func (a *CopilotAgent) Name() string { return "copilot" }

func (a *CopilotAgent) Detect(ctx context.Context) (bool, error) {
	// TODO: implement actual detection using exec.Command
	return true, nil
}

func (a *CopilotAgent) Configure(env map[string]string, providerURL string, model string) {
	env["GITHUB_COPILOT_BASE_URL"] = providerURL + "/v1"
	env["GITHUB_COPILOT_MODEL_NAME"] = model
}

func (a *CopilotAgent) Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error {
	fmt.Printf("\nLaunching copilot with %s at %s/v1\n", model, providerURL)
	return fmt.Errorf("not yet implemented")
}
