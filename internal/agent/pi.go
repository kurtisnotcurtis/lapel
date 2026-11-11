package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// PiAgent implements Agent for the pi coding harness.
type PiAgent struct{}

func NewPiAgent() *PiAgent { return &PiAgent{} }

func (a *PiAgent) Name() string { return "pi" }

func (a *PiAgent) Detect(ctx context.Context) (bool, error) {
	path, err := exec.LookPath("pi")
	if err != nil {
		return false, nil
	}
	_, err = exec.CommandContext(ctx, path, "--version").Output()
	return err == nil, nil
}

func (a *PiAgent) Configure(env map[string]string, providerURL string, model string) {
	env["OPENAI_API_BASE_URL"] = providerURL + "/v1"
	env["OPENAI_MODEL_NAME"] = model
	env["OPENAI_API_KEY"] = "not-needed-for-local"
}

func (a *PiAgent) Launch(ctx context.Context, providerURL string, model string, workingDir string, user string) error {
	fmt.Printf("\nLaunching pi agent with %s at %s/v1\n", model, providerURL)

	cmd := exec.CommandContext(ctx, "pi")
	cmd.Dir = workingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pi agent exited: %w", err)
	}
	return nil
}
