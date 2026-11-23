// Package reconcile implements the "plan" side of the desired-state model:
// given an effective manifest and current system state, compute whether each
// managed resource matches what the manifest declares.
package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lapel/internal/config"
	"lapel/internal/domain"
)

// EnvProber reports the runtime status of an environment.
type EnvProber func(ctx context.Context, env config.Environment) (domain.EnvironmentStatus, error)

// ProviderProbe reports whether a provider endpoint is reachable and returns a
// human-readable note describing the outcome.
type ProviderProbe func(cfg config.Provider) (reachable bool, note string)

// WorkspaceExists reports whether a workspace path exists on disk.
type WorkspaceExists func(cfg config.Workspace) (exists bool, err error)

// Options configures a Diff run. Zero values fall back to sensible real
// defaults so callers rarely need to set them.
type Options struct {
	EnvProber       EnvProber
	ProviderProbe   ProviderProbe
	WorkspaceExists WorkspaceExists
}

// Entry is a single reconciled resource and its desired-state verdict.
type Entry struct {
	Kind    string // "environment" | "provider" | "workspace"
	Name    string
	Desired bool   // matches the manifest?
	Actual  string // human-readable current state
	Note    string // optional context
}

// DiffResult is the outcome of a reconciliation pass.
type DiffResult struct {
	Manifest config.Manifest `json:"manifest"`
	Entries  []Entry         `json:"entries"`
}

// AsDesired returns true when every entry matches its manifest spec.
func (d *DiffResult) AsDesired() bool {
	for _, e := range d.Entries {
		if !e.Desired {
			return false
		}
	}
	return true
}

// NonDesired returns the entries that do not match the manifest, in order.
func (d *DiffResult) NonDesired() []Entry {
	var out []Entry
	for _, e := range d.Entries {
		if !e.Desired {
			out = append(out, e)
		}
	}
	return out
}

// NewOptions returns probes backed by the real system for the given target distro.
func NewOptions(ctx context.Context, distro ...string) Options {
	dist := config.DefaultDistribution
	if len(distro) > 0 && strings.TrimSpace(distro[0]) != "" {
		dist = strings.TrimSpace(distro[0])
	}
	return Options{
		EnvProber:       DefaultEnvProber(ctx),
		ProviderProbe:   DefaultProviderProbe(dist),
		WorkspaceExists: DefaultWorkspaceExists,
	}
}

// Diff computes the desired-state diff for an effective manifest. It is
// read-only with respect to system state and does not modify anything.
func Diff(ctx context.Context, m *config.Manifest, opts Options) (*DiffResult, error) {
	targetDist := envName(m)
	if opts.EnvProber == nil || opts.ProviderProbe == nil || opts.WorkspaceExists == nil {
		opts = NewOptions(ctx, targetDist)
	}

	res := &DiffResult{Manifest: *m}

	status, err := opts.EnvProber(ctx, m.Environment)
	if err != nil {
		return nil, fmt.Errorf("probing environment %q: %w", targetDist, err)
	}
	desiredEnv := status.Exists && strings.EqualFold(status.Distribution, targetDist)
	res.Entries = append(res.Entries, Entry{
		Kind:    "environment",
		Name:    targetDist,
		Desired: desiredEnv,
		Actual:  describeEnv(status),
	})

	reachable, note := opts.ProviderProbe(m.Provider)
	res.Entries = append(res.Entries, Entry{
		Kind:    "provider",
		Name:    m.Provider.Name,
		Desired: reachable,
		Actual:  note,
	})

	exists, err := opts.WorkspaceExists(m.Workspace)
	if err != nil {
		return nil, fmt.Errorf("checking workspace %q: %w", m.Workspace.Name, err)
	}
	res.Entries = append(res.Entries, Entry{
		Kind:    "workspace",
		Name:    m.Workspace.Name,
		Desired: exists,
		Actual:  describeWorkspace(m.Workspace, exists),
	})

	return res, nil
}

// EnvName returns the resolved WSL distribution name for the manifest.
func EnvName(m *config.Manifest) string { return envName(m) }

func envName(m *config.Manifest) string {
	if m != nil && strings.TrimSpace(m.Environment.Distribution) != "" {
		return strings.TrimSpace(m.Environment.Distribution)
	}
	return config.DefaultDistribution
}

// DefaultEnvProber probes WSL distro discovery for the declared distribution.
func DefaultEnvProber(ctx context.Context) EnvProber {
	return func(_ context.Context, env config.Environment) (domain.EnvironmentStatus, error) {
		target := strings.TrimSpace(env.Distribution)
		if target == "" {
			target = config.DefaultDistribution
		}
		names, err := listDistros(ctx)
		if err != nil {
			return domain.EnvironmentStatus{}, err
		}
		st := domain.EnvironmentStatus{}
		for _, n := range names {
			if strings.EqualFold(n, target) {
				st.Exists = true
				st.Distribution = n
				break
			}
		}
		return st, nil
	}
}

// DefaultProviderProbe checks HTTP(S) reachability of the provider endpoint,
// with type-specific logic for lm-studio and ollama.
func DefaultProviderProbe(distro string) ProviderProbe {
	return func(cfg config.Provider) (bool, string) {
		switch strings.ToLower(cfg.Type) {
		case "lm-studio":
			return probeLMStudio(distro, cfg)
		case "ollama":
			return probeOllama(distro, cfg)
		default:
			reachable, note, err := checkReachable(distro, cfg.BaseURL)
			if err != nil {
				return false, "unreachable: " + note
			}
			return reachable, note
		}
	}
}

func probeLMStudio(distro string, cfg config.Provider) (bool, string) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:1234"
	}
	modelsURL := baseURL
	if !strings.HasSuffix(modelsURL, "/v1/models") {
		if strings.HasSuffix(modelsURL, "/v1") {
			modelsURL += "/models"
		} else {
			modelsURL += "/v1/models"
		}
	}
	reachable, note, err := checkReachable(distro, modelsURL)
	if err != nil {
		return false, "lm-studio unreachable: " + note
	}
	if reachable {
		return true, "lm-studio running at " + baseURL
	}
	return false, "lm-studio not reachable at " + modelsURL
}

func probeOllama(distro string, cfg config.Provider) (bool, string) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	tagsURL := baseURL + "/api/tags"
	reachable, note, err := checkReachable(distro, tagsURL)
	if err != nil {
		return false, "ollama unreachable: " + note
	}
	if reachable {
		return true, "ollama running at " + baseURL
	}
	return false, "ollama not reachable at " + tagsURL
}

// DefaultWorkspaceExists checks whether the workspace path exists on disk.
func DefaultWorkspaceExists(cfg config.Workspace) (bool, error) {
	info, err := os.Stat(cfg.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// DescribeEnv renders a human-readable environment status string.
func DescribeEnv(st domain.EnvironmentStatus) string { return describeEnv(st) }

func describeEnv(st domain.EnvironmentStatus) string {
	if !st.Exists {
		return "distro not installed"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "installed=%s", st.Distribution)
	if st.Version != "" {
		fmt.Fprintf(&b, ", version=%s", st.Version)
	}
	if st.Running {
		b.WriteString(", running")
	}
	return b.String()
}

// DescribeWorkspace renders a human-readable workspace status string.
func DescribeWorkspace(cfg config.Workspace, exists bool) string {
	return describeWorkspace(cfg, exists)
}

func describeWorkspace(cfg config.Workspace, exists bool) string {
	if !exists {
		return "path missing: " + cfg.Path
	}
	switch cfg.ImportMode {
	case "copy":
		if cfg.Source != "" {
			src := filepath.Join(cfg.Source, cfg.Name)
			if _, err := os.Stat(src); err != nil {
				return fmt.Sprintf("present (%s), source %q not found", cfg.Path, src)
			}
		}
	}
	return "present: " + cfg.Path
}
