package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
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

// Detect checks if Ollama is available via HTTP API on port 11434 or the `ollama` CLI.
func (o *Ollama) Detect(ctx context.Context) (bool, error) {
	if models, err := o.fetchModelsHTTP(ctx, 11434); err == nil && models != nil {
		return true, nil
	}

	if _, err := exec.LookPath("ollama"); err == nil {
		return true, nil
	}
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", o.dist, "bash", "-c", "command -v ollama")
	if out, err := cmd.Output(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return false, &ErrNotInstalled{Name: "Ollama"}
	}
	return false, &ErrServerDown{Name: "Ollama", Detail: "API not reachable on port 11434"}
}

// Status returns runtime status for Ollama, including loaded models.
func (o *Ollama) Status(ctx context.Context) (*ProviderStatus, error) {
	status := &ProviderStatus{
		Name:     "Ollama",
		Type:     "ollama",
		Port:     11434,
		BindAddr: "127.0.0.1",
	}

	if models, err := o.fetchModelsHTTP(ctx, status.Port); err == nil {
		status.Installed = true
		status.Running = true
		status.Models = models
		return status, nil
	}

	installed, err := o.Detect(ctx)
	if err != nil {
		if _, notInst := err.(*ErrNotInstalled); !notInst {
			status.Installed = true
		}
		status.Error = err.Error()
		return status, nil
	}
	status.Installed = installed
	return status, nil
}

// Start is a no-op for Ollama since it runs as a background service on Windows.
func (o *Ollama) Start(ctx context.Context) error {
	return nil
}

// Stop stops the Ollama server.
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
	return o.fetchModelsHTTP(ctx, 11434)
}

func (o *Ollama) fetchModelsHTTP(ctx context.Context, port int) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	var lastErr error
	for _, base := range candidateBaseURLs(ctx, port) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		var payload struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			lastErr = fmt.Errorf("unexpected response from %s/api/tags (status %d)", base, resp.StatusCode)
			continue
		}
		models := make([]string, 0, len(payload.Models))
		for _, m := range payload.Models {
			if name := strings.TrimSpace(m.Name); name != "" {
				models = append(models, name)
			}
		}
		return models, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ollama API unreachable")
	}
	return nil, lastErr
}
