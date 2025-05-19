package agent

import (
	"context"
	"fmt"
)

// PiAgent implements Agent for the pi coding harness.
type PiAgent struct{}

func NewPiAgent() *PiAgent { return &PiAgent{} }

func (a *PiAgent) Name() string { return "pi" }

func (a *PiAgent) Detect(ctx context.Context) (bool, error) {
	// TODO: implement actual detection using exec.Command
	return true, nil
}

func (a *PiAgent) Configure(env map[string]string, providerURL string, model string) {
	env["OPENAI_API_BASE_URL"] = providerURL + "/v1"
	env["OPENAI_MODEL_NAME"] = model
	env["OPENAI_API_KEY"] = "not-needed-for-local"
}

func (a *PiAgent) Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error {
	fmt.Printf("\nLaunching pi agent with %s at %s/v1\n", model, providerURL)
	// In a real implementation, this would:
	// 1. Build the command with proper env vars
	// 2. Execute it in WSL with jail applied
	// 3. Return when done or handle interactive mode
	return fmt.Errorf("not yet implemented")
}
