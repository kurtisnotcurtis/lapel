// Package agent manages coding-agent harness detection and configuration.
package agent

import (
	"context"
	"os/exec"
	"strings"
)

// Agent is a supported coding-agent harness (e.g., pi, claude-code, copilot).
type Agent interface {
	// Name returns the agent identifier (e.g., "pi", "claude-code").
	Name() string

	// Binary returns the executable command name invoked inside the WSL environment.
	Binary() string

	// Detect checks if the agent is installed in the target WSL environment.
	Detect(ctx context.Context) (bool, error)

	// Configure injects provider connection details into the env map.
	// The env map will be passed to the agent process.
	Configure(env map[string]string, providerURL string, model string)
}

// detectBinaryInWSL checks whether any of the candidate binary names exist in
// the target WSL distro's login PATH (including ~/.local/bin, /usr/local/fnm,
// /usr/local/uv/bin), falling back to host exec.LookPath on native Linux.
func detectBinaryInWSL(ctx context.Context, dist string, candidates ...string) (bool, error) {
	if dist == "" {
		dist = "Ubuntu-Lean"
	}
	if _, err := exec.LookPath("wsl.exe"); err == nil {
		for _, bin := range candidates {
			script := "export PATH=\"$HOME/.local/bin:/usr/local/fnm:/usr/local/uv/bin:/usr/local/bin:/usr/bin:/bin:$PATH\"; command -v " + bin
			cmd := exec.CommandContext(ctx, "wsl.exe", "-d", dist, "bash", "-lc", script)
			out, err := cmd.Output()
			if err == nil && len(strings.TrimSpace(string(out))) > 0 {
				return true, nil
			}
		}
		return false, nil
	}

	// Native Linux fallback when wsl.exe is not present.
	for _, bin := range candidates {
		if _, err := exec.LookPath(bin); err == nil {
			return true, nil
		}
	}
	return false, nil
}
