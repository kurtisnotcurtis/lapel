// Package config loads and validates the Lapel manifest (lapel.yaml)
// and maps it onto the backend-independent domain model.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"lapel/internal/domain"
)

// apiVersion is the only supported manifest schema version.
const apiVersion = "lapel.dev/v1"

// DefaultDistribution is the default WSL distribution name used when
// environment.distribution is omitted.
const DefaultDistribution = "lapel"

// DefaultManifestName is the conventional manifest file name, searched for in
// the current working directory and its ancestors.
const DefaultManifestName = "lapel.yaml"

// legacyManifestName is accepted as a fallback on case-sensitive filesystems.
const legacyManifestName = "Lapel.yaml"

// ErrManifestNotFound is returned when no lapel.yaml manifest exists in the
// search path. Callers can check errors.Is(err, config.ErrManifestNotFound) to
// distinguish "no manifest present" from a parse or validation error.
var ErrManifestNotFound = errors.New("manifest not found")

// Manifest is the declarative description of a desired Lapel setup.
type Manifest struct {
	APIVersion  string      `yaml:"apiVersion"`
	Name        string      `yaml:"name"`
	Environment Environment `yaml:"environment"`
	Provider    Provider    `yaml:"provider"`
	Agent       Agent       `yaml:"agent"`
	Workspace   Workspace   `yaml:"workspace"`
	Jail        Jail        `yaml:"jail"`
}

// EnvironmentStyle configures the visual shell prompt, welcome banner, and
// Windows Terminal fragment for the managed WSL distro.
type EnvironmentStyle struct {
	Preset          string `yaml:"preset" json:"preset,omitempty"`                   // "lapel-dark" | "violet" | "minimal" | "none"
	Badge           string `yaml:"badge" json:"badge,omitempty"`                     // custom prompt badge text
	Banner          *bool  `yaml:"banner" json:"banner,omitempty"`                   // show one-line welcome banner
	TabColor        string `yaml:"tabColor" json:"tabColor,omitempty"`               // hex color (#RRGGBB)
	Background      string `yaml:"background" json:"background,omitempty"`           // hex color (#RRGGBB)
	WindowsTerminal *bool  `yaml:"windowsTerminal" json:"windowsTerminal,omitempty"` // manage Windows Terminal fragment
}

// Environment describes the target WSL distribution and provisioning options.
type Environment struct {
	Distribution string           `yaml:"distribution"` // target distro name (defaults to "lapel")
	Base         string           `yaml:"base"`         // "ubuntu"
	Version      string           `yaml:"version"`      // e.g. "24.04"
	Systemd      *bool            `yaml:"systemd"`      // boot WSL with systemd (nil defaults to true)
	Packages     []string         `yaml:"packages"`     // extra apt packages (agent-toolkit baseline always applies)
	Style        EnvironmentStyle `yaml:"style"`        // shell prompt & Windows Terminal styling
}

// Provider describes the inference provider endpoint and model defaults.
type Provider struct {
	Name              string        `yaml:"name"`
	Type              string        `yaml:"type"` // "lm-studio" | "ollama" | "openai-compatible"
	BaseURL           string        `yaml:"baseURL"`
	Model             string        `yaml:"model"`
	Timeout           time.Duration `yaml:"timeout"`
	ConnectionTimeout time.Duration `yaml:"connectionTimeout"`
}

// Agent describes the default coding-agent CLI selection.
type Agent struct {
	Name     string `yaml:"name"`     // "pi" | "claude-code" | "copilot"
	Provider string `yaml:"provider"` // logical provider name reference
}

// Workspace describes the host project directory and how it is mapped into WSL.
type Workspace struct {
	Name       string `yaml:"name"`
	Path       string `yaml:"path"` // host path (Win or Lin)
	Source     string `yaml:"source"`
	ImportMode string `yaml:"importMode"` // "copy" | "bind"
}

// Jail describes execution policy settings applied to agent sessions.
type Jail struct {
	Name       string            `yaml:"name"`
	Profile    string            `yaml:"profile"` // e.g. "developer"
	User       string            `yaml:"user"`    // dedicated non-root user
	Filesystem map[string]string `yaml:"filesystem"`
	Processes  struct {
		Privileged     bool `yaml:"privileged"`
		TimeoutSeconds int  `yaml:"timeoutSeconds"`
	} `yaml:"processes"`
	Network string `yaml:"network"` // "unrestricted" | "outbound-only" | "none"
}

// DiscoverManifest locates a manifest by searching the given directory and its
// ancestors for DefaultManifestName. It returns an error wrapping
// ErrManifestNotFound when none is found.
func DiscoverManifest(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		for _, name := range []string{DefaultManifestName, legacyManifestName} {
			candidate := filepath.Join(dir, name)
			if fileExists(candidate) {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached filesystem root
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("%w: no %s in %s or any ancestor directory", ErrManifestNotFound, DefaultManifestName, startDir)
}

// LoadEffective loads a manifest at an explicit path, or discovers one in the
// current directory and its ancestors when path is empty. It returns the
// default-applied, validated manifest ready to reconcile.
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

// LoadOptional loads a manifest when present. If path is empty and no manifest
// exists in the current directory or its ancestors, it returns (nil, nil).
// Any parse or validation error is returned so malformed manifests are never
// silently ignored.
func LoadOptional(path string) (*Manifest, error) {
	m, err := LoadEffective(path)
	if err != nil {
		if strings.TrimSpace(path) == "" && errors.Is(err, ErrManifestNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// Load reads, strictly decodes (rejecting unknown keys at every nesting level),
// applies defaults, and validates the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var m Manifest
	if len(bytes.TrimSpace(data)) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
		}
	}
	m.applyDefaults(path)
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
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
	if strings.TrimSpace(m.Environment.Distribution) == "" {
		m.Environment.Distribution = DefaultDistribution
	}
	if strings.TrimSpace(m.Environment.Base) == "" {
		m.Environment.Base = "ubuntu"
	}
	if strings.TrimSpace(m.Environment.Style.Preset) == "" {
		m.Environment.Style.Preset = "lapel-dark"
	}
	if m.Agent.Provider == "" {
		m.Agent.Provider = m.Provider.Name
	}
	if strings.TrimSpace(m.Workspace.ImportMode) == "" {
		m.Workspace.ImportMode = "bind"
	}
	if strings.TrimSpace(m.Jail.Profile) == "" {
		m.Jail.Profile = "developer"
	}
	if strings.TrimSpace(m.Jail.User) == "" {
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
	dir := filepath.Dir(path)
	base := strings.ToLower(filepath.Base(dir))
	if base == "" || base == "." || base == "/" || base == `\` || !nameRe.MatchString(base) {
		return "lapel-project"
	}
	return base
}

// EnvSpec maps the environment section onto domain.Environment.
func (m *Manifest) EnvSpec() domain.Environment {
	dist := firstNonEmpty(m.Environment.Distribution, DefaultDistribution)
	return domain.Environment{
		Name:         dist,
		Backend:      "wsl",
		Distribution: dist,
		Base:         m.Environment.Base,
		Version:      m.Environment.Version,
		Packages:     m.Environment.Packages,
		Systemd:      m.Environment.Systemd,
		Style: domain.EnvironmentStyle{
			Preset:          m.Environment.Style.Preset,
			Badge:           m.Environment.Style.Badge,
			Banner:          m.Environment.Style.Banner,
			TabColor:        m.Environment.Style.TabColor,
			Background:      m.Environment.Style.Background,
			WindowsTerminal: m.Environment.Style.WindowsTerminal,
		},
	}
}

// ProviderSpec maps the provider section onto domain.Provider.
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
