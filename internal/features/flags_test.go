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

func TestGetEndpoint_OfficialVsSelfHosted(t *testing.T) {
	t.Setenv("LAPEL_COMMUNITY_ENDPOINT", "")
	t.Setenv("LAPEL_USE_OFFICIAL_BACKEND", "")
	t.Setenv("LAPEL_USE_PROPRIETARY_BACKEND", "")
	t.Setenv("LAPEL_SELF_HOSTED_ENDPOINT", "")

	// Default: official backend enabled -> api.lapel.dev
	if got := GetEndpoint(); got != "https://api.lapel.dev/v1" {
		t.Errorf("default GetEndpoint() = %q, want %q", got, "https://api.lapel.dev/v1")
	}

	// Disabling official backend without override must NOT return api.lapel.dev
	t.Setenv("LAPEL_USE_OFFICIAL_BACKEND", "false")
	if got := GetEndpoint(); got != SelfHostedDefaultEndpoint {
		t.Errorf("self-hosted GetEndpoint() = %q, want %q", got, SelfHostedDefaultEndpoint)
	}

	// Explicit LAPEL_SELF_HOSTED_ENDPOINT override
	t.Setenv("LAPEL_SELF_HOSTED_ENDPOINT", "http://10.0.0.5:8080/v1")
	if got := GetEndpoint(); got != "http://10.0.0.5:8080/v1" {
		t.Errorf("custom self-hosted GetEndpoint() = %q, want %q", got, "http://10.0.0.5:8080/v1")
	}

	// Explicit LAPEL_COMMUNITY_ENDPOINT always wins
	t.Setenv("LAPEL_COMMUNITY_ENDPOINT", "http://localhost:9090/v1")
	if got := GetEndpoint(); got != "http://localhost:9090/v1" {
		t.Errorf("explicit community endpoint GetEndpoint() = %q, want %q", got, "http://localhost:9090/v1")
	}
}
