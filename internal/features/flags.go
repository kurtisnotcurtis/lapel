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
//
// Two kinds of flags share this type:
//   - Boolean toggles set Default to true/false and read it via IsEnabled.
//   - String-valued settings (e.g. an endpoint URL) set DefaultString and read
//     it via GetString. The env var still overrides whichever default applies.
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
// and tuning profile queries.
var CommunityEndpoint = Flag{
	Name:          "community.endpoint",
	Description:   "Backend API URL for report submission and recommendation queries",
	DefaultString: "https://api.lapel.dev/v1",
	EnvVar:        "LAPEL_COMMUNITY_ENDPOINT",
}

// UseOfficialBackend toggles between Lapel's hosted backend and self-hosted.
// The name is intentionally neutral so the open-source build never names its
// proprietary/hosted behavior by a closed-source label (Chromium-style gating).
var UseOfficialBackend = Flag{
	Name:          "community.official_backend",
	Description:   "Use Lapel's hosted backend vs a self-hosted instance",
	DefaultString: "https://api.lapel.dev/v1",
	Default:       true,
	EnvVar:        "LAPEL_USE_OFFICIAL_BACKEND",
}

// IsEnabled reports whether a boolean flag is on, honoring env var overrides.
func IsEnabled(flag Flag) bool {
	if env := strings.TrimSpace(os.Getenv(flag.EnvVar)); env != "" {
		return parseBoolEnv(env)
	}
	// Support legacy alias for UseOfficialBackend if set in user environment.
	if flag.EnvVar == UseOfficialBackend.EnvVar {
		if legacy := strings.TrimSpace(os.Getenv("LAPEL_USE_PROPRIETARY_BACKEND")); legacy != "" {
			return parseBoolEnv(legacy)
		}
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

// GetEndpoint returns the configured community backend endpoint URL, applying the
// self-hosted fallback when the official hosted backend is disabled so that
// disabling UseOfficialBackend never silently phones home to api.lapel.dev.
func GetEndpoint() string {
	// An explicit LAPEL_COMMUNITY_ENDPOINT override always wins.
	if explicit := strings.TrimSpace(os.Getenv(CommunityEndpoint.EnvVar)); explicit != "" {
		return explicit
	}

	if IsEnabled(UseOfficialBackend) {
		return CommunityEndpoint.DefaultString
	}

	// Self-hosted mode: check LAPEL_SELF_HOSTED_ENDPOINT, then fall back to localhost.
	if env := strings.TrimSpace(os.Getenv("LAPEL_SELF_HOSTED_ENDPOINT")); env != "" {
		return env
	}

	return SelfHostedDefaultEndpoint
}

// String returns a human-readable representation of the flag state.
func (f Flag) String() string {
	state := "disabled"
	if IsEnabled(f) {
		state = "enabled"
	}
	return fmt.Sprintf("%s=%s", f.Name, state)
}
