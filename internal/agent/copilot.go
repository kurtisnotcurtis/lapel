package agent

import (
	"context"
	"strings"
)

// CopilotAgent implements Agent for GitHub Copilot CLI.
type CopilotAgent struct {
	dist string
}

func NewCopilotAgent(distro ...string) *CopilotAgent {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &CopilotAgent{dist: d}
}

func (a *CopilotAgent) Name() string   { return "copilot" }
func (a *CopilotAgent) Binary() string { return "github-copilot-cli" }

func (a *CopilotAgent) Detect(ctx context.Context) (bool, error) {
	return detectBinaryInWSL(ctx, a.dist, "github-copilot-cli", "copilot")
}

func (a *CopilotAgent) Configure(env map[string]string, providerURL string, model string) {
	base := strings.TrimRight(providerURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	env["GITHUB_COPILOT_BASE_URL"] = base
	env["GITHUB_COPILOT_MODEL_NAME"] = model
}
