// Package config loads and validates the Lapel manifest (Lapel.yaml)
// and maps it onto the backend-independent domain model. It owns no WSL or
// provider mechanics — that belongs to the execution-backend boundary (spec 4).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lapel/internal/domain"
	"gopkg.in/yaml.v3"
)

// apiVersion is the only supported manifest schema version.
const apiVersion = "lapel.dev/v1"

// DefaultManifestName is the conventional manifest file name, searched for in
// the current working directory (and ancestors when WalkUp is set).
const DefaultManifestName = "Lapel.yaml"

// Manifest is the declarative description of a desired Lapel setup. It is
// intentionally shallow and maps directly onto domain objects; it must not be
// coupled to any particular execution backend.
type Manifest struct {
	APIVersion  string        `yaml:"apiVersion"`
	Name        string        `yaml:"name"`
	Environment Environment   `yaml:"environment"`
	Provider    Provider      `yaml:"provider"`
	Agent       Agent         `yaml:"agent"`
	Workspace   Workspace     `yaml:"workspace"`
	Jail        Jail          `yaml:"jail"`
}

// Environment mirrors domain.Environment for the manifest's environment section.
type Environment struct {
	// Distribution is the target distro name. Use "import" (or leave blank and
	// supply Base+Version) to have lapel create a fresh pinned rootfs.
	Distribution string            `yaml:"distribution"`
	Base         string            `yaml:"base"`         // "ubuntu" | "debian"
	Version      string            `yaml:"version"`      // e.g. "24.04" / "12"
	RootFSImage  string            `yaml:"rootfsImage"`  // pinned image (+checksum) override
	Runtimes     map[string]string `yaml:"runtimes"`     // toolchain -> pinned major version
	Packages     []string          `yaml:"packages"`     // extra apt packages (agent-toolkit baseline always applies)
}

// Provider mirrors domain.Provider minus the secret-backed API key.
type Provider struct {
	Name              string        `yaml:"name"`
	Type              string        `yaml:"type"` // e.g. "lm-studio", "ollama", "openai-compatible"
	BaseURL           string        `yaml:"baseURL"`
	Model             string        `yaml:"model"`
	APIKeySecret      string        `yaml:"apiKeySecret"` // optional; else resolved from secret store by name
	Timeout           time.Duration `yaml:"timeout"`       // seconds when a bare number is given
	ConnectionTimeout time.Duration `yaml:"connectionTimeout"`
}

// Agent mirrors domain.Agent.
type Agent struct {
	Name     string `yaml:"name"`     // e.g. "pi"
	Harness  string `yaml:"harness"`  // canonical harness id, e.g. "opencode"
	Provider string `yaml:"provider"` // logical provider name reference
}

// Workspace mirrors domain.Workspace.
type Workspace struct {
	Name       string `yaml:"name"`
	Path       string `yaml:"path"`   // host path (Win or Lin)
	Source     string `yaml:"source"`
	ImportMode string `yaml:"importMode"` // "copy" | "bind"
}

// Jail mirrors domain.Jail.
type Jail struct {
	Name          string            `yaml:"name"`
	Profile       string            `yaml:"profile"` // e.g. "developer"
	User          string            `yaml:"user"`    // dedicated non-root user
	Filesystem    map[string]string `yaml:"filesystem"`
	Processes     struct {
		Privileged     bool   `yaml:"privileged"`
		TimeoutSeconds int    `yaml:"timeoutSeconds"`
	} `yaml:"processes"`
	Resources struct {
		Memory string `yaml:"memory"`
		CPU    int    `yaml:"cpu"`
	} `yaml:"resources"`
	Network string `yaml:"network"` // "unrestricted" | ...
}

// DiscoverManifest locates a manifest by searching the given directory and its
// ancestors for DefaultManifestName. It returns an error when none is found.
func DiscoverManifest(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, DefaultManifestName)
		if fileExists(candidate) {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached filesystem root
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no %s found in %s or any ancestor directory", DefaultManifestName, startDir)
}

// LoadEffective loads a manifest at an explicit path, or discovers one in the
// current directory and its ancestors when path is empty. It returns the
// default-applied manifest ready to reconcile.
func LoadEffective(path string) (*Manifest, error) {
	if strings.TrimSpace(path) == "" {
		found, err := DiscoverManifest(".")
		if err != nil {
			return nil, err
		}
		path = found
	}
	return Load(path)
}

// Load reads and parses the manifest at path. It applies defaults; use Validate
// to check schema/cross-reference correctness afterwards.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var m Manifest
	// Decode into a node map first to reject unknown top-level keys (typos),
	// since yaml.v3 does not provide strict decoding.
	var head map[string]yaml.Node
	if err := yaml.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	for k := range head {
		if !allowedTopLevelKey(k) {
			return nil, fmt.Errorf("manifest %s: unknown top-level key %q (allowed: apiVersion, name, environment, provider, agent, workspace, jail)", path, k)
		}
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	m.applyDefaults(path)
	return &m, nil
}

// applyDefaults fills sensible values for empty fields so the manifest is ready
// to reconcile without requiring every section to be present.
func (m *Manifest) applyDefaults(path string) {
	if m.APIVersion == "" {
		m.APIVersion = apiVersion
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = defaultProjectName(path)
	}
	if m.Environment.Base == "" && !strings.Contains(strings.ToLower(m.Environment.RootFSImage), "debian") {
		m.Environment.Base = "ubuntu"
	}
	if m.Agent.Provider == "" {
		m.Agent.Provider = m.Provider.Name
	}
	if strings.EqualFold(m.Workspace.ImportMode, "") {
		m.Workspace.ImportMode = "bind"
	}
	if strings.EqualFold(m.Jail.Profile, "") {
		m.Jail.Profile = "developer"
	}
	if strings.EqualFold(m.Jail.User, "") {
		m.Jail.User = "agent"
	}
	if m.Provider.Timeout > 0 && m.Provider.ConnectionTimeout == 0 {
		m.Provider.ConnectionTimeout = m.Provider.Timeout
	}
}

// defaultProjectName derives a project name from the manifest file's directory,
// falling back to a stable default when no path is available (e.g. unit tests).
func defaultProjectName(path string) string {
	if strings.TrimSpace(path) == "" {
		return "lapel-project"
	}
	base := filepath.Base(path)
	if base == "" || base == "." || base == "/" {
		return "lapel-project"
	}
	return base
}

// EnvSpec maps the environment section onto domain.Environment. Runtimes and
// packages are carried through verbatim; RootFSImage is passed to the backend
// so it can pin/verify the rootfs image when provisioning a fresh distro.
func (m *Manifest) EnvSpec() domain.Environment {
	return domain.Environment{
		Name:         firstNonEmpty(m.Environment.Distribution, m.Provider.Name),
		Backend:      "wsl",
		Distribution: m.Environment.Distribution,
		Runtimes:     m.Environment.Runtimes,
		Packages:     m.Environment.Packages,
		RootFSImage:  m.Environment.RootFSImage,
	}
}

// ProviderSpec maps the provider section onto domain.Provider. The API key is
// resolved from the secret store by name when apiKeySecret is empty; here we
// only surface the reference so callers can resolve it before use.
func (m *Manifest) ProviderSpec() domain.Provider {
	return domain.Provider{
		Name:              firstNonEmpty(m.Provider.Name, "local"),
		Type:              m.Provider.Type,
		BaseURL:           m.Provider.BaseURL,
		Model:             m.Provider.Model,
		ConnectionTimeout: m.Provider.ConnectionTimeout,
	}
}

// firstNonEmpty returns the first non-empty field.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
