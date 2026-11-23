package features

import (
	"testing"
)

func TestCommunityEnabled_DefaultsToOptInFalse(t *testing.T) {
	t.Setenv("LAPEL_COMMUNITY_ENABLED", "")
	if IsEnabled(CommunityEnabled) {
		t.Fatal("expected CommunityEnabled to default to false (opt-in)")
	}

	t.Setenv("LAPEL_COMMUNITY_ENABLED", "true")
	if !IsEnabled(CommunityEnabled) {
		t.Fatal("expected CommunityEnabled to be true when LAPEL_COMMUNITY_ENABLED=true")
	}
}

func TestGetEndpoint_RequiresExplicitOrSelfHosted(t *testing.T) {
	t.Setenv("LAPEL_COMMUNITY_ENDPOINT", "")
	t.Setenv("LAPEL_USE_OFFICIAL_BACKEND", "")
	t.Setenv("LAPEL_SELF_HOSTED_ENDPOINT", "")

	// Default: no built-in remote endpoint so the CLI never phones home silently.
	if got := GetEndpoint(); got != "" {
		t.Errorf("default GetEndpoint() = %q, want empty", got)
	}

	// Disabling official backend selects self-hosted fallback.
	t.Setenv("LAPEL_USE_OFFICIAL_BACKEND", "false")
	if got := GetEndpoint(); got != SelfHostedDefaultEndpoint {
		t.Errorf("self-hosted GetEndpoint() = %q, want %q", got, SelfHostedDefaultEndpoint)
	}

	// Explicit LAPEL_SELF_HOSTED_ENDPOINT override.
	t.Setenv("LAPEL_SELF_HOSTED_ENDPOINT", "http://10.0.0.5:8080/v1")
	if got := GetEndpoint(); got != "http://10.0.0.5:8080/v1" {
		t.Errorf("custom self-hosted GetEndpoint() = %q, want %q", got, "http://10.0.0.5:8080/v1")
	}

	// Explicit LAPEL_COMMUNITY_ENDPOINT always wins.
	t.Setenv("LAPEL_COMMUNITY_ENDPOINT", "http://localhost:9090/v1")
	if got := GetEndpoint(); got != "http://localhost:9090/v1" {
		t.Errorf("explicit community endpoint GetEndpoint() = %q, want %q", got, "http://localhost:9090/v1")
	}
}
