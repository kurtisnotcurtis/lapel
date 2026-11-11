package provider

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Ollama implements Manager for the Ollama inference provider.
type Ollama struct {
	dist string // WSL distribution name
}

// NewOllama creates a new Ollama provider manager.
// The distro parameter specifies which WSL distribution to use for commands
// (defaults to "Ubuntu-Lean" if empty).
func NewOllama(distro ...string) *Ollama {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &Ollama{dist: d}
}

func (o *Ollama) Name() string { return "ollama" }

// Detect checks if Ollama is available by verifying both the `ollama` binary exists AND the API is reachable.
func (o *Ollama) Detect(ctx context.Context) (bool, error) {
	// First check if ollama CLI is available
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", o.dist, "bash", "-c", "command -v ollama")
	if out, err := cmd.Output(); err != nil {
		return false, &ErrNotInstalled{Name: "Ollama"}
	} else if len(strings.TrimSpace(string(out))) == 0 {
		return false, &ErrNotInstalled{Name: "Ollama"}
	}

	// Then check if API is reachable (background service)
	statusCmd := exec.CommandContext(ctx, "wsl.exe", "-d", o.dist,
		"bash", "-c", "curl -s http://localhost:11434/api/tags 2>/dev/null | head -c 100")
	out, err := statusCmd.Output()
	if err != nil {
		return false, &ErrServerDown{Name: "Ollama", Detail: fmt.Sprintf("API not reachable: %v", err)}
	}

	// If we got a response, Ollama is running
	if len(out) > 0 {
		return true, nil
	}

	return false, nil
}

// Status returns runtime status for Ollama. Since Ollama runs as a background service on Windows,
// we check if the API is reachable on port 11434.
func (o *Ollama) Status(ctx context.Context) (*ProviderStatus, error) {
	status := &ProviderStatus{
		Name: "Ollama",
		Type: "ollama",
		Port: 11434,
	}

	// Check if ollama CLI is available
	installed, err := o.Detect(ctx)
	if err != nil {
		return status, fmt.Errorf("detect failed: %w", err)
	}
	status.Installed = installed
	if !installed {
		return status, nil
	}

	// Check if Ollama API is reachable (background service)
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", o.dist,
		"bash", "-c", "curl -s http://localhost:11434/api/tags 2>/dev/null | head -c 100")
	out, err := cmd.Output()
	if err != nil {
		status.Error = fmt.Sprintf("API not reachable: %v", err)
		return status, nil
	}

	// If we got here without error and got some response, Ollama is running
	if len(out) > 0 {
		status.Running = true
		status.BindAddr = "127.0.0.1" // Ollama defaults to localhost
	} else {
		status.Error = "API returned empty response"
	}

	return status, nil
}

// Start is a no-op for Ollama since it runs as a background service on Windows.
func (o *Ollama) Start(ctx context.Context) error {
	// Ollama auto-starts with the app; nothing to do here
	return nil
}

// Stop stops the Ollama server by running `ollama stop` on the Windows host.
func (o *Ollama) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", o.dist, "bash", "-c", "ollama stop 2>&1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop failed: %v\n%s", err, string(out))
	}
	return nil
}

// ListModels returns available model identifiers by querying the Ollama API.
func (o *Ollama) ListModels(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", o.dist,
		"bash", "-c", "curl -s http://localhost:11434/api/tags 2>/dev/null | grep -o '\"name\":\"[^\"]*\"' | cut -d'\"' -f4")
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
