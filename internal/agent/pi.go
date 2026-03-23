package agent

import (
	"context"
	"strings"
)

// PiAgent implements Agent for the pi coding harness.
type PiAgent struct {
	dist string
}

func NewPiAgent(distro ...string) *PiAgent {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &PiAgent{dist: d}
}

func (a *PiAgent) Name() string   { return "pi" }
func (a *PiAgent) Binary() string { return "pi" }

func (a *PiAgent) Detect(ctx context.Context) (bool, error) {
	return detectBinaryInWSL(ctx, a.dist, "pi")
}

func (a *PiAgent) Configure(env map[string]string, providerURL string, model string) {
	base := strings.TrimRight(providerURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	env["OPENAI_API_BASE_URL"] = base
	env["OPENAI_MODEL_NAME"] = model
	env["OPENAI_API_KEY"] = "not-needed-for-local"
}
