package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Validator holds optional semantic validation functions.
type Validator struct {
	// CheckPathExists verifies a path is accessible. If nil, path checks are skipped.
	CheckPathExists func(path string) bool
	// CheckUserExists verifies a user exists in the distro. If nil, user checks are skipped.
	CheckUserExists func(user string) bool
	// AvailableProviders lists provider names that can be referenced. If nil, no check is performed.
	AvailableProviders []string
}

// ValidateWith runs schema validation plus semantic checks using the provided validator.
func (m *Manifest) ValidateWith(v Validator) error {
	problems, err := m.ValidateAll()
	if err != nil {
		return err
	}

	agentProvider := strings.ToLower(strings.TrimSpace(m.Agent.Provider))

	// Semantic: agent.provider must reference an available provider.
	if v.AvailableProviders != nil && agentProvider != "" {
		found := false
		for _, p := range v.AvailableProviders {
			if strings.EqualFold(p, agentProvider) {
				found = true
				break
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("agent.provider %q not in available providers: %v", agentProvider, v.AvailableProviders))
		}
	}

	// Semantic: jail.user must exist in distro (if specified).
	if v.CheckUserExists != nil && strings.TrimSpace(m.Jail.User) != "" {
		if !v.CheckUserExists(m.Jail.User) {
			problems = append(problems, fmt.Sprintf("jail.user %q does not exist", m.Jail.User))
		}
	}

	// Semantic: workspace path must be accessible (if importMode=copy).
	if v.CheckPathExists != nil && strings.EqualFold(m.Workspace.ImportMode, "copy") {
		if !v.CheckPathExists(m.Workspace.Path) {
			problems = append(problems, fmt.Sprintf("workspace.path %q does not exist", m.Workspace.Path))
		}
	}

	// Semantic: jail.user must exist in distro (if specified).
	if v.CheckUserExists != nil && strings.TrimSpace(m.Jail.User) != "" {
		if !v.CheckUserExists(m.Jail.User) {
			problems = append(problems, fmt.Sprintf("jail.user %q does not exist", m.Jail.User))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(problems, "\n  - "))
}

// Validate checks the manifest's schema and cross-references after defaults have
// been applied. It returns the first problem encountered; callers may call
// ValidateAll to surface every issue at once.
func (m *Manifest) Validate() error {
	_, err := m.ValidateAll()
	return err
}

// ValidateAll runs every rule and reports all problems together so a contributor
// fixes them in one pass instead of one-at-a-time.
func (m *Manifest) ValidateAll() ([]string, error) {
	var problems []string
	check := func(cond bool, format string, args ...any) {
		if !cond {
			problems = append(problems, fmt.Sprintf(format, args...))
		}
	}

	check(m.APIVersion == apiVersion, "apiVersion must be %q (got %q)", apiVersion, m.APIVersion)

	name := strings.TrimSpace(m.Name)
	check(name != "", "name is required")
	check(nameRe.MatchString(name), "name must match ^[a-z0-9][a-z0-9._-]*$ (got %q)", name)

	// Environment.
	base := strings.ToLower(strings.TrimSpace(m.Environment.Base))
	if base != "" && base != "ubuntu" && base != "debian" {
		problems = append(problems, fmt.Sprintf("environment.base must be ubuntu|debian (got %q)", m.Environment.Base))
	}
	importing := strings.EqualFold(strings.TrimSpace(m.Environment.Distribution), "import")
	if importing {
		check(base != "", "environment.distribution=import requires environment.base")
		check(strings.TrimSpace(m.Environment.Version) != "", "environment.distribution=import requires environment.version (e.g. 24.04)")
	}

	// Provider.
	switch strings.ToLower(strings.TrimSpace(m.Provider.Type)) {
	case "", "openai-compatible":
	default:
		problems = append(problems, fmt.Sprintf("provider.type must be openai-compatible (got %q)", m.Provider.Type))
	}
	if strings.TrimSpace(m.Provider.BaseURL) != "" {
		u, err := url.Parse(m.Provider.BaseURL)
		check(err == nil && (u.Scheme == "http" || u.Scheme == "https"),
			"provider.baseURL must be an http/https URL (got %q)", m.Provider.BaseURL)
	}
	if m.Provider.ConnectionTimeout < 0 {
		problems = append(problems, "provider.connectionTimeout must not be negative")
	}

	// Agent.
	if strings.TrimSpace(m.Agent.Name) == "" {
		problems = append(problems, "agent.name is required")
	}
	if m.Agent.Provider != "" && m.Provider.Name != "" && !strings.EqualFold(m.Agent.Provider, m.Provider.Name) {
		problems = append(problems, fmt.Sprintf("agent.provider %q must reference provider.name %q", m.Agent.Provider, m.Provider.Name))
	}

	// Workspace.
	switch strings.ToLower(strings.TrimSpace(m.Workspace.ImportMode)) {
	case "", "copy", "bind":
	default:
		problems = append(problems, fmt.Sprintf("workspace.importMode must be copy|bind (got %q)", m.Workspace.ImportMode))
	}

	// Jail.
	switch strings.ToLower(strings.TrimSpace(m.Jail.Network)) {
	case "", "unrestricted", "restricted":
	default:
		problems = append(problems, fmt.Sprintf("jail.network must be unrestricted|restricted (got %q)", m.Jail.Network))
	}
	if m.Jail.Processes.TimeoutSeconds < 0 {
		problems = append(problems, "jail.processes.timeoutSeconds must not be negative")
	}

	if len(problems) == 0 {
		return nil, nil
	}
	return problems, fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(problems, "\n  - "))
}

