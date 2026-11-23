package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	distroRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	userRe     = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)
	hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Validate checks the manifest's schema and cross-references after defaults have
// been applied.
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
	if name != "" {
		check(nameRe.MatchString(name), "name must match ^[a-z0-9][a-z0-9._-]*$ (got %q)", name)
	}

	// Environment.
	dist := strings.TrimSpace(m.Environment.Distribution)
	if dist != "" {
		check(distroRe.MatchString(dist), "environment.distribution must match ^[A-Za-z0-9][A-Za-z0-9._-]*$ (got %q)", dist)
	}
	base := strings.ToLower(strings.TrimSpace(m.Environment.Base))
	if base != "" && base != "ubuntu" {
		problems = append(problems, fmt.Sprintf("environment.base must be ubuntu (got %q)", m.Environment.Base))
	}
	switch strings.ToLower(strings.TrimSpace(m.Environment.Style.Preset)) {
	case "", "lapel-dark", "violet", "minimal", "none":
	default:
		problems = append(problems, fmt.Sprintf("environment.style.preset must be lapel-dark|violet|minimal|none (got %q)", m.Environment.Style.Preset))
	}
	if tc := strings.TrimSpace(m.Environment.Style.TabColor); tc != "" {
		check(hexColorRe.MatchString(tc), "environment.style.tabColor must be a #RRGGBB hex color (got %q)", m.Environment.Style.TabColor)
	}
	if bg := strings.TrimSpace(m.Environment.Style.Background); bg != "" {
		check(hexColorRe.MatchString(bg), "environment.style.background must be a #RRGGBB hex color (got %q)", m.Environment.Style.Background)
	}
	if strings.ContainsAny(m.Environment.Style.Badge, "\r\n\x00") {
		problems = append(problems, "environment.style.badge must not contain newlines or NUL bytes")
	}

	// Provider.
	switch strings.ToLower(strings.TrimSpace(m.Provider.Type)) {
	case "", "openai-compatible", "lm-studio", "ollama":
	default:
		problems = append(problems, fmt.Sprintf("provider.type must be openai-compatible|lm-studio|ollama (got %q)", m.Provider.Type))
	}
	if rawURL := strings.TrimSpace(m.Provider.BaseURL); rawURL != "" {
		u, err := url.Parse(rawURL)
		check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "",
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
	if wsName := strings.TrimSpace(m.Workspace.Name); wsName != "" {
		check(nameRe.MatchString(wsName) && !strings.Contains(wsName, ".."),
			"workspace.name must match ^[a-z0-9][a-z0-9._-]*$ without '..' (got %q)", m.Workspace.Name)
	}
	switch strings.ToLower(strings.TrimSpace(m.Workspace.ImportMode)) {
	case "", "copy", "bind":
	default:
		problems = append(problems, fmt.Sprintf("workspace.importMode must be copy|bind (got %q)", m.Workspace.ImportMode))
	}

	// Jail.
	if u := strings.TrimSpace(m.Jail.User); u != "" {
		check(userRe.MatchString(u), "jail.user must be a valid POSIX username (got %q)", m.Jail.User)
	}
	switch strings.ToLower(strings.TrimSpace(m.Jail.Network)) {
	case "", "unrestricted", "restricted", "outbound-only", "none":
	default:
		problems = append(problems, fmt.Sprintf("jail.network must be unrestricted|outbound-only|none|restricted (got %q)", m.Jail.Network))
	}
	if m.Jail.Processes.TimeoutSeconds < 0 {
		problems = append(problems, "jail.processes.timeoutSeconds must not be negative")
	}

	if len(problems) == 0 {
		return nil, nil
	}
	return problems, fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(problems, "\n  - "))
}
