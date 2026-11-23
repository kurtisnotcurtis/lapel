// Package features provides a lightweight feature flag system for gating
// hosted/community vs local-only behavior in the lapel CLI.
package features

import (
	"fmt"
	"os"
	"strings"
)

// SelfHostedDefaultEndpoint is the fallback endpoint when UseOfficialBackend is disabled
// and no explicit endpoint override is provided.
const SelfHostedDefaultEndpoint = "http://localhost:8080/v1"

// Flag represents a configurable feature with environment variable override.
type Flag struct {
	Name          string // Human-readable name
	Description   string // What this flag controls
	Default       bool   // Boolean default when env var is not set
	DefaultString string // String default (used for non-boolean settings)
	EnvVar        string // Environment variable to override (e.g., "LAPEL_COMMUNITY_ENABLED")
}

// CommunityEnabled toggles community data sharing and live tuning profile lookups.
// Strictly opt-in (false) by default to respect local-first privacy.
var CommunityEnabled = Flag{
	Name:        "community.enabled",
	Description: "Enable opt-in community data sharing and personalized recommendations",
	Default:     false,
	EnvVar:      "LAPEL_COMMUNITY_ENABLED",
}

// CommunityEndpoint is the API endpoint for opt-in community benchmark sharing
// and tuning profile queries. There is intentionally no built-in default URL;
// callers must set LAPEL_COMMUNITY_ENDPOINT explicitly (or run self-hosted).
var CommunityEndpoint = Flag{
	Name:          "community.endpoint",
	Description:   "Backend API URL for report submission and recommendation queries",
	DefaultString: "",
	EnvVar:        "LAPEL_COMMUNITY_ENDPOINT",
}

// UseOfficialBackend toggles between a configured hosted backend and the
// local self-hosted fallback.
var UseOfficialBackend = Flag{
	Name:        "community.official_backend",
	Description: "Use an explicit hosted backend vs the local self-hosted fallback",
	Default:     true,
	EnvVar:      "LAPEL_USE_OFFICIAL_BACKEND",
}

// IsEnabled reports whether a boolean flag is on, honoring env var overrides.
func IsEnabled(flag Flag) bool {
	if env := strings.TrimSpace(os.Getenv(flag.EnvVar)); env != "" {
		return parseBoolEnv(env)
	}
	return flag.Default
}

func parseBoolEnv(val string) bool {
	v := strings.ToLower(val)
	return v == "true" || v == "1" || v == "yes"
}

// GetString returns the configured string value for a setting, honoring env var overrides.
func GetString(flag Flag) string {
	if env := strings.TrimSpace(os.Getenv(flag.EnvVar)); env != "" {
		return env
	}
	return flag.DefaultString
}

// GetEndpoint returns the configured community backend endpoint URL.
// An explicit LAPEL_COMMUNITY_ENDPOINT always wins. When unset and
// UseOfficialBackend is disabled, it falls back to LAPEL_SELF_HOSTED_ENDPOINT
// or SelfHostedDefaultEndpoint. Otherwise it returns "" so the CLI never
// transmits data to an unconfigured or unhosted endpoint.
func GetEndpoint() string {
	if explicit := strings.TrimSpace(os.Getenv(CommunityEndpoint.EnvVar)); explicit != "" {
		return explicit
	}
	if !IsEnabled(UseOfficialBackend) {
		if env := strings.TrimSpace(os.Getenv("LAPEL_SELF_HOSTED_ENDPOINT")); env != "" {
			return env
		}
		return SelfHostedDefaultEndpoint
	}
	return CommunityEndpoint.DefaultString
}

// String returns a human-readable representation of the flag state.
func (f Flag) String() string {
	state := "disabled"
	if IsEnabled(f) {
		state = "enabled"
	}
	return fmt.Sprintf("%s=%s", f.Name, state)
}
