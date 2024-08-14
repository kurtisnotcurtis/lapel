package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// LMStudio implements Manager for the LM Studio inference provider.
type LMStudio struct {
	dist string // WSL distribution name
}

// NewLMStudio creates a new LM Studio provider manager.
// The distro parameter specifies which WSL distribution to use for commands
// (defaults to "Ubuntu-Lean" if empty).
func NewLMStudio(distro ...string) *LMStudio {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &LMStudio{dist: d}
}

func (l *LMStudio) Name() string { return "lm-studio" }

// Detect checks if LM Studio is available by verifying both the `lms` CLI exists AND the server is running.
func (l *LMStudio) Detect(ctx context.Context) (bool, error) {
	// First check if lms CLI is available
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c", "command -v lms")
	if out, err := cmd.Output(); err != nil {
		return false, &ErrNotInstalled{Name: "LM Studio"}
	} else if len(strings.TrimSpace(string(out))) == 0 {
		return false, &ErrNotInstalled{Name: "LM Studio"}
	}

	// Then check if server is actually running
	statusCmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c", "lms server status --json 2>/dev/null")
	out, err := statusCmd.Output()
	if err != nil {
		return false, &ErrServerDown{Name: "LM Studio", Detail: fmt.Sprintf("status check failed: %v", err)}
	}

	var result struct {
		Running bool `json:"running"`
	}
	if json.Unmarshal(out, &result) != nil {
		return false, &ErrDetectFailed{Name: "LM Studio", Err: fmt.Errorf("failed to parse status output")}
	}

	return result.Running, nil
}

// Status returns runtime status for LM Studio. It queries the Windows host via wsl.exe.
func (l *LMStudio) Status(ctx context.Context) (*ProviderStatus, error) {
	status := &ProviderStatus{
		Name: "LM Studio",
		Type: "lm-studio",
	}

	// Check if lms CLI is available
	installed, err := l.Detect(ctx)
	if err != nil {
		return status, fmt.Errorf("detect failed: %w", err)
	}
	status.Installed = installed
	if !installed {
		return status, nil
	}

	// Run `lms server status --json` to get server state
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c", "lms server status --json 2>/dev/null")
	out, err := cmd.Output()
	if err != nil {
		status.Error = fmt.Sprintf("server status failed: %v", err)
		return status, nil
	}

	var result struct {
		Running bool `json:"running"`
		Port    int  `json:"port"`
	}
	if json.Unmarshal(out, &result) != nil {
		status.Error = "failed to parse lms server status output"
		return status, nil
	}

	status.Running = result.Running
	status.Port = result.Port
	if status.Port == 0 {
		status.Port = 1234 // default
	}
	status.BindAddr = "127.0.0.1" // LM Studio defaults to localhost-only

	return status, nil
}

// Start starts the LM Studio server if not already running.
func (l *LMStudio) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c", "lms server start 2>&1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("start failed: %v\n%s", err, string(out))
	}
	return nil
}

// Stop stops the LM Studio server.
func (l *LMStudio) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c", "lms server stop 2>&1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop failed: %v\n%s", err, string(out))
	}
	return nil
}

// ListModels returns available model identifiers by querying the LM Studio API.
func (l *LMStudio) ListModels(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c",
		"curl -s http://localhost:1234/v1/models 2>/dev/null | grep -o '\"id\":\"[^\"]*\"' | cut -d'\"' -f4")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list models failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var models []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			models = append(models, line)
		}
	}
	return models, nil
}
