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

// candidateBaseURLs returns candidate base URLs (127.0.0.1 and resolved WSL host IP)
// so HTTP probes work whether lapel runs on the Windows host or inside WSL NAT mode.
func candidateBaseURLs(ctx context.Context, port int) []string {
	urls := []string{fmt.Sprintf("http://127.0.0.1:%d", port)}
	if hostIP, err := HostIP(ctx); err == nil && hostIP != "" && hostIP != "127.0.0.1" {
		urls = append(urls, fmt.Sprintf("http://%s:%d", hostIP, port))
	}
	return urls
}

// Detect checks if LM Studio is available via HTTP API on port 1234 or the `lms` CLI.
func (l *LMStudio) Detect(ctx context.Context) (bool, error) {
	if models, err := l.fetchModelsHTTP(ctx, 1234); err == nil && models != nil {
		return true, nil
	}

	// Fallback: check if `lms` CLI is available on host or in WSL.
	if _, err := exec.LookPath("lms"); err == nil {
		return true, nil
	}
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", l.dist, "bash", "-c", "command -v lms")
	if out, err := cmd.Output(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return false, &ErrNotInstalled{Name: "LM Studio"}
	}

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

// Status returns runtime status for LM Studio, including any loaded models.
func (l *LMStudio) Status(ctx context.Context) (*ProviderStatus, error) {
	status := &ProviderStatus{
		Name:     "LM Studio",
		Type:     "lm-studio",
		Port:     1234,
		BindAddr: "127.0.0.1",
	}

	// Fast path: probe the HTTP /v1/models endpoint directly.
	if models, err := l.fetchModelsHTTP(ctx, status.Port); err == nil {
		status.Installed = true
		status.Running = true
		status.Models = models
		return status, nil
	}

	// Fallback to CLI detection.
	installed, err := l.Detect(ctx)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.Installed = installed
	if !installed {
		return status, nil
	}

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
	if result.Port > 0 {
		status.Port = result.Port
	}
	if status.Running {
		if models, err := l.fetchModelsHTTP(ctx, status.Port); err == nil {
			status.Models = models
		}
	}

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

// ListModels returns available model identifiers by querying the LM Studio OpenAI-compatible API.
func (l *LMStudio) ListModels(ctx context.Context) ([]string, error) {
	return l.fetchModelsHTTP(ctx, 1234)
}

func (l *LMStudio) fetchModelsHTTP(ctx context.Context, port int) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	var lastErr error
	for _, base := range candidateBaseURLs(ctx, port) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
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
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			lastErr = fmt.Errorf("unexpected response from %s/v1/models (status %d)", base, resp.StatusCode)
			continue
		}
		models := make([]string, 0, len(payload.Data))
		for _, m := range payload.Data {
			if id := strings.TrimSpace(m.ID); id != "" {
				models = append(models, id)
			}
		}
		return models, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("lm-studio API unreachable")
	}
	return nil, lastErr
}
