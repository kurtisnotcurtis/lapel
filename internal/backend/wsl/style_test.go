package wsl

import (
	"bytes"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lapel/internal/domain"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveStylePresetsAndOverrides(t *testing.T) {
	// Default (empty) resolves to lapel-dark with distro name "lapel".
	def := ResolveStyle("", domain.EnvironmentStyle{})
	if !def.Enabled || def.Preset != PresetLapelDark {
		t.Fatalf("expected enabled lapel-dark, got %+v", def)
	}
	if def.Badge != "lapel" {
		t.Errorf("badge = %q, want lapel", def.Badge)
	}
	if !def.Banner || !def.WindowsTerminal {
		t.Errorf("expected Banner=true and WindowsTerminal=true by default: %+v", def)
	}
	if def.TabColor != "#0EA5E9" || def.Background != "#0D1520" {
		t.Errorf("unexpected default colors: tab=%s bg=%s", def.TabColor, def.Background)
	}

	// Amber preset with custom badge and color overrides.
	amber := ResolveStyle("lapel", domain.EnvironmentStyle{
		Preset:     PresetAmber,
		Badge:      "ai-sandbox",
		TabColor:   "#D97706",
		Background: "#1C1917",
		Banner:     boolPtr(false),
	})
	if amber.Preset != PresetAmber || amber.SchemeName != "Lapel Amber" {
		t.Errorf("unexpected amber preset resolution: %+v", amber)
	}
	if amber.Badge != "ai-sandbox" || amber.TabColor != "#D97706" || amber.Background != "#1C1917" {
		t.Errorf("overrides not applied: %+v", amber)
	}
	if amber.Banner {
		t.Error("expected Banner=false override to be respected")
	}

	// Minimal preset disables banner by default.
	min := ResolveStyle("lapel", domain.EnvironmentStyle{Preset: PresetMinimal})
	if !min.Enabled || min.Banner {
		t.Errorf("minimal should be enabled with Banner=false, got %+v", min)
	}

	// None preset disables everything.
	none := ResolveStyle("lapel", domain.EnvironmentStyle{Preset: PresetNone})
	if none.Enabled || none.Banner || none.WindowsTerminal {
		t.Errorf("none preset should disable all styling, got %+v", none)
	}
}

func TestBuildShellStyleScript(t *testing.T) {
	dark := ResolveStyle("lapel", domain.EnvironmentStyle{
		Preset: PresetLapelDark,
		Badge:  "dev-rig",
	})
	script := BuildShellStyleScript(dark)
	for _, want := range []string{
		"_LAPEL_BADGE='dev-rig'",
		"_LAPEL_DISTRO='lapel'",
		`export PS1=`,
		`ϟ ${_lapel_label}`,
		`LAPEL_BANNER_SHOWN`,
		`Linux-only PATH`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}

	// Minimal omits banner.
	min := ResolveStyle("lapel", domain.EnvironmentStyle{Preset: PresetMinimal})
	minScript := BuildShellStyleScript(min)
	if strings.Contains(minScript, "LAPEL_BANNER_SHOWN") {
		t.Errorf("minimal script should not include login banner:\n%s", minScript)
	}
	if !strings.Contains(minScript, `[${_lapel_label}]`) {
		t.Errorf("minimal script missing compact prompt badge:\n%s", minScript)
	}

	// None returns empty script.
	none := ResolveStyle("lapel", domain.EnvironmentStyle{Preset: PresetNone})
	if got := BuildShellStyleScript(none); got != "" {
		t.Errorf("none script = %q, want empty", got)
	}
}

func TestApplyWindowsTerminalFragmentLifecycle(t *testing.T) {
	tmp := t.TempDir()
	fragPath := filepath.Join(tmp, "Fragments", "lapel", "lapel.json")

	// 1. Write fragment for amber preset.
	written, err := applyWindowsTerminalFragmentAt(fragPath, "lapel", domain.EnvironmentStyle{
		Preset:   PresetAmber,
		Badge:    "lapel",
		TabColor: "#F59E0B",
	})
	if err != nil {
		t.Fatalf("applyWindowsTerminalFragmentAt: %v", err)
	}
	if written != fragPath {
		t.Fatalf("written = %q, want %q", written, fragPath)
	}

	raw, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("reading fragment: %v", err)
	}
	var parsed wtFragment
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshaling fragment JSON: %v", err)
	}
	if len(parsed.Profiles) != 1 || parsed.Profiles[0].TabColor != "#F59E0B" || parsed.Profiles[0].Name != "lapel" {
		t.Errorf("unexpected fragment profiles: %+v", parsed.Profiles)
	}
	if got, want := parsed.Profiles[0].Updates, wslDynamicProfileGUID("lapel"); got == "" || got != want {
		t.Errorf("fragment updates GUID = %q, want %q", got, want)
	}
	wantIcon := filepath.Join(filepath.Dir(fragPath), "lapel-icon.png")
	if parsed.Profiles[0].Icon != wantIcon {
		t.Errorf("fragment icon = %q, want %q", parsed.Profiles[0].Icon, wantIcon)
	}
	iconBytes, err := os.ReadFile(wantIcon)
	if err != nil {
		t.Fatalf("reading generated icon PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(iconBytes))
	if err != nil {
		t.Fatalf("decoding icon PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("icon PNG dimensions = %dx%d, want 64x64", b.Dx(), b.Dy())
	}
	if len(parsed.Schemes) != 1 || parsed.Schemes[0].Name != "Lapel Amber" {
		t.Errorf("unexpected fragment schemes: %+v", parsed.Schemes)
	}

	// 2. Disable via preset=none and verify fragment & icon files are removed.
	written, err = applyWindowsTerminalFragmentAt(fragPath, "lapel", domain.EnvironmentStyle{
		Preset: PresetNone,
	})
	if err != nil {
		t.Fatalf("removing fragment: %v", err)
	}
	if written != "" {
		t.Errorf("expected empty path on removal, got %q", written)
	}
	if _, err := os.Stat(fragPath); !os.IsNotExist(err) {
		t.Errorf("expected fragment file to be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(wantIcon); !os.IsNotExist(err) {
		t.Errorf("expected icon file to be deleted, stat err = %v", err)
	}
}

func TestWSLDynamicProfileGUID(t *testing.T) {
	// Golden vector from Windows Terminal's Windows.Terminal.Wsl generator for "Ubuntu".
	if got, want := wslDynamicProfileGUID("Ubuntu"), "{2c4de342-38b7-51cf-b940-2309a097f518}"; got != want {
		t.Errorf("wslDynamicProfileGUID(Ubuntu) = %q, want %q", got, want)
	}
}
