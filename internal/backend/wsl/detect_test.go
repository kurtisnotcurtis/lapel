package wsl

import (
	"reflect"
	"testing"
)

func TestParseDistroNames(t *testing.T) {
	// Real `wsl --list --verbose` output shape: header, '*' prefix with a
	// separating space for the active distro, multi-word names, and plain
	// names without dashes (the old dash heuristic silently dropped those).
	input := "  NAME            STATE           VERSION\n" +
		"* Ubuntu-Lean     Running         2\n" +
		"  docker-desktop    Stopped         2\n" +
		"  Ubuntu          Stopped         2\n" +
		"  My Distro       Stopped         2\n"

	got := parseDistroNames(input)
	want := []string{"Ubuntu-Lean", "docker-desktop", "Ubuntu", "My Distro"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseDistroNames = %q, want %q", got, want)
	}
}

func TestParseDistroNamesEmpty(t *testing.T) {
	if got := parseDistroNames(""); len(got) != 0 {
		t.Errorf("expected no names for empty input, got %q", got)
	}
	if got := parseDistroNames("NAME STATE VERSION"); len(got) != 0 {
		t.Errorf("header-only input should yield no names, got %q", got)
	}
}
