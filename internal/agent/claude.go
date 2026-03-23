package agent

import (
	"context"
	"strings"
)

// ClaudeCodeAgent implements Agent for Anthropic's Claude Code CLI.
type ClaudeCodeAgent struct {
	dist string
}

func NewClaudeCodeAgent(distro ...string) *ClaudeCodeAgent {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &ClaudeCodeAgent{dist: d}
}

func (a *ClaudeCodeAgent) Name() string   { return "claude-code" }
func (a *ClaudeCodeAgent) Binary() string { return "claude" }

func (a *ClaudeCodeAgent) Detect(ctx context.Context) (bool, error) {
	return detectBinaryInWSL(ctx, a.dist, "claude")
}

func (a *ClaudeCodeAgent) Configure(env map[string]string, providerURL string, model string) {
	base := strings.TrimRight(providerURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	env["ANTHROPIC_BASE_URL"] = base
	env["ANTHROPIC_MODEL_NAME"] = model
	env["ANTHROPIC_API_KEY"] = "not-needed-for-local"
}
