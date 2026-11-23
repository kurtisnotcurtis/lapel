package agent

import (
	"context"
	"strings"
)

// CopilotAgent implements Agent for GitHub Copilot CLI.
type CopilotAgent struct {
	dist        string
	detectedBin string
}

func NewCopilotAgent(distro ...string) *CopilotAgent {
	d := "lapel"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &CopilotAgent{dist: d}
}

func (a *CopilotAgent) Name() string { return "copilot" }

func (a *CopilotAgent) Binary() string {
	if a.detectedBin != "" {
		return a.detectedBin
	}
	return "copilot"
}

func (a *CopilotAgent) Detect(ctx context.Context) (bool, error) {
	bin, ok, err := detectBinaryInWSL(ctx, a.dist, "copilot", "github-copilot-cli")
	if ok {
		a.detectedBin = bin
	}
	return ok, err
}

func (a *CopilotAgent) Configure(env map[string]string, providerURL string, model string) {
	base := strings.TrimRight(providerURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	env["GITHUB_COPILOT_BASE_URL"] = base
	env["GITHUB_COPILOT_MODEL_NAME"] = model
}
