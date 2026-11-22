package wsl

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"lapel/internal/domain"
)

// Built-in visual style presets for lapel-managed WSL distributions.
const (
	PresetLapelDark = "lapel-dark"
	PresetAmber     = "amber"
	PresetMinimal   = "minimal"
	PresetNone      = "none"
)

// ResolvedStyle holds the concrete prompt, banner, and Windows Terminal
// settings after applying preset defaults and user overrides.
type ResolvedStyle struct {
	Preset          string
	Enabled         bool
	Distro          string
	Badge           string
	Banner          bool
	WindowsTerminal bool
	SchemeName      string
	TabColor        string
	Background      string
	Foreground      string
	CursorColor     string
	SelectionBg     string
	AccentAnsiCode  string // "36" (cyan) or "33" (amber)
}

// ResolveStyle computes the effective style configuration for a distro from
// a domain.EnvironmentStyle spec, applying preset defaults and any overrides.
func ResolveStyle(distro string, spec domain.EnvironmentStyle) ResolvedStyle {
	if strings.TrimSpace(distro) == "" {
		distro = Dist
	}
	preset := strings.ToLower(strings.TrimSpace(spec.Preset))
	if preset == "" {
		preset = PresetLapelDark
	}

	badge := sanitizeBadge(spec.Badge)
	if badge == "" {
		badge = sanitizeBadge(distro)
	}

	if preset == PresetNone {
		return ResolvedStyle{
			Preset:          PresetNone,
			Enabled:         false,
			Distro:          distro,
			Badge:           badge,
			Banner:          false,
			WindowsTerminal: false,
		}
	}

	// Default to lapel-dark baseline, then specialize per preset.
	res := ResolvedStyle{
		Preset:          preset,
		Enabled:         true,
		Distro:          distro,
		Badge:           badge,
		Banner:          true,
		WindowsTerminal: true,
		SchemeName:      "Lapel Dark",
		TabColor:        "#0EA5E9",
		Background:      "#0D1520",
		Foreground:      "#E2E8F0",
		CursorColor:     "#38BDF8",
		SelectionBg:     "#1E3A5F",
		AccentAnsiCode:  "36",
	}

	switch preset {
	case PresetAmber:
		res.SchemeName = "Lapel Amber"
		res.TabColor = "#F59E0B"
		res.Background = "#18120B"
		res.Foreground = "#FEF3C7"
		res.CursorColor = "#F59E0B"
		res.SelectionBg = "#452709"
		res.AccentAnsiCode = "33"
	case PresetMinimal:
		res.Banner = false
	default:
		res.Preset = PresetLapelDark
	}

	if spec.Banner != nil {
		res.Banner = *spec.Banner
	}
	if spec.WindowsTerminal != nil {
		res.WindowsTerminal = *spec.WindowsTerminal
	}
	if tc := strings.TrimSpace(spec.TabColor); tc != "" {
		res.TabColor = tc
	}
	if bg := strings.TrimSpace(spec.Background); bg != "" {
		res.Background = bg
	}

	return res
}

// sanitizeBadge strips control characters and newlines from a prompt badge.
func sanitizeBadge(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// BuildShellStyleScript generates the /etc/profile.d/95-lapel-style.sh content
// for a resolved style. It sets PS1, the Windows Terminal tab title (OSC 0),
// and an optional one-line welcome banner on first interactive login.
func BuildShellStyleScript(style ResolvedStyle) string {
	if !style.Enabled {
		return ""
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# Managed by lapel — visual prompt and banner for WSL sessions.\n")
	b.WriteString("case $- in\n  *i*) ;;\n  *) return ;;\nesac\n\n")

	fmt.Fprintf(&b, "_LAPEL_BADGE=%s\n", shellQuote(style.Badge))
	fmt.Fprintf(&b, "_LAPEL_DISTRO=%s\n", shellQuote(style.Distro))
	fmt.Fprintf(&b, "_LAPEL_PRESET=%s\n\n", shellQuote(style.Preset))

	b.WriteString(`_lapel_jail_tag=""
if [ -n "${JAIL_TIMEOUT:-}" ]; then
  _lapel_jail_tag=" \[\e[1;33m\][jail]\[\e[0m\]"
fi
_lapel_label="lapel"
_lapel_title="lapel"
if [ -n "${_LAPEL_BADGE}" ] && [ "${_LAPEL_BADGE}" != "lapel" ] && [ "${_LAPEL_BADGE}" != "Lapel" ]; then
  _lapel_label="lapel:${_LAPEL_BADGE}"
  _lapel_title="lapel (${_LAPEL_BADGE})"
fi
`)

	switch style.Preset {
	case PresetAmber:
		b.WriteString(`export PS1="\[\e]0;⚡ ${_lapel_title}: \w\a\]\[\e[1;30;43m\] ⚡ ${_lapel_label} \[\e[0m\]${_lapel_jail_tag} \[\e[1;33m\]\u\[\e[0m\]:\[\e[1;34m\]\w\[\e[0m\] \[\e[1;33m\]▸\[\e[0m\] "` + "\n")
	case PresetMinimal:
		b.WriteString(`export PS1="\[\e]0;${_lapel_title}: \w\a\]\[\e[1;36m\][${_lapel_label}]\[\e[0m\]${_lapel_jail_tag} \u@\h:\w\$ "` + "\n")
	default: // PresetLapelDark
		b.WriteString(`export PS1="\[\e]0;⚡ ${_lapel_title}: \w\a\]\[\e[1;30;46m\] ⚡ ${_lapel_label} \[\e[0m\]${_lapel_jail_tag} \[\e[1;36m\]\u\[\e[0m\]:\[\e[1;34m\]\w\[\e[0m\] \[\e[1;36m\]▸\[\e[0m\] "` + "\n")
	}

	if style.Banner {
		fmt.Fprintf(&b, `
if [ -z "${LAPEL_BANNER_SHOWN:-}" ]; then
  export LAPEL_BANNER_SHOWN=1
  _lapel_mode="WSL (${_LAPEL_DISTRO})"
  if [ -n "${JAIL_TIMEOUT:-}" ]; then
    _lapel_mode="${_lapel_mode} · jailed (${JAIL_TIMEOUT}s timeout)"
  fi
  printf '\033[1;%sm⚡ lapel\033[0m \033[2m· %%s · user: %%s · Linux-only PATH\033[0m\n' "${_lapel_mode}" "${USER:-$(id -un 2>/dev/null)}"
fi
`, style.AccentAnsiCode)
	}

	return b.String()
}

// wtFragment is the Windows Terminal JSON Fragment schema.
type wtFragment struct {
	Profiles []wtProfile `json:"profiles"`
	Schemes  []wtScheme  `json:"schemes,omitempty"`
}

type wtProfile struct {
	Updates           string `json:"updates,omitempty"`
	Name              string `json:"name,omitempty"`
	Commandline       string `json:"commandline,omitempty"`
	TabTitle          string `json:"tabTitle,omitempty"`
	TabColor          string `json:"tabColor,omitempty"`
	ColorScheme       string `json:"colorScheme,omitempty"`
	Icon              string `json:"icon,omitempty"`
	StartingDirectory string `json:"startingDirectory,omitempty"`
}

// wtNamespaceGUID is Windows Terminal's TERMINAL_PROFILE_NAMESPACE_GUID
// ({2bde4a90-d05f-401c-9492-e40884ead1d8}) in RFC 4122 big-endian byte order.
var wtNamespaceGUID = [16]byte{
	0x2b, 0xde, 0x4a, 0x90,
	0xd0, 0x5f,
	0x40, 0x1c,
	0x94, 0x92,
	0xe4, 0x08, 0x84, 0xea, 0xd1, 0xd8,
}

// wslDynamicProfileGUID computes the deterministic GUID that Windows Terminal's
// Windows.Terminal.Wsl generator assigns to a WSL distro (UUIDv5 over
// wtNamespaceGUID + UTF-16LE distro name).
func wslDynamicProfileGUID(distro string) string {
	units := utf16.Encode([]rune(distro))
	h := sha1.New()
	h.Write(wtNamespaceGUID[:])
	var buf [2]byte
	for _, u := range units {
		binary.LittleEndian.PutUint16(buf[:], u)
		h.Write(buf[:])
	}
	sum := h.Sum(nil)
	sum[6] = (sum[6] & 0x0f) | 0x50 // version 5
	sum[8] = (sum[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("{%08x-%04x-%04x-%04x-%012x}",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

type wtScheme struct {
	Name                string `json:"name"`
	Background          string `json:"background"`
	Foreground          string `json:"foreground"`
	CursorColor         string `json:"cursorColor"`
	SelectionBackground string `json:"selectionBackground"`
	Black               string `json:"black"`
	Red                 string `json:"red"`
	Green               string `json:"green"`
	Yellow              string `json:"yellow"`
	Blue                string `json:"blue"`
	Purple              string `json:"purple"`
	Cyan                string `json:"cyan"`
	White               string `json:"white"`
	BrightBlack         string `json:"brightBlack"`
	BrightRed           string `json:"brightRed"`
	BrightGreen         string `json:"brightGreen"`
	BrightYellow        string `json:"brightYellow"`
	BrightBlue          string `json:"brightBlue"`
	BrightPurple        string `json:"brightPurple"`
	BrightCyan          string `json:"brightCyan"`
	BrightWhite         string `json:"brightWhite"`
}

// BuildWindowsTerminalFragment builds the JSON payload for a Windows Terminal
// fragment file that registers the Lapel color scheme and updates the managed
// WSL distribution's dynamic profile without touching the user's settings.json.
func BuildWindowsTerminalFragment(style ResolvedStyle) ([]byte, error) {
	return buildWindowsTerminalFragmentWithIcon(style, "⚡")
}

func buildWindowsTerminalFragmentWithIcon(style ResolvedStyle, icon string) ([]byte, error) {
	if !style.Enabled || !style.WindowsTerminal {
		return nil, nil
	}
	if strings.TrimSpace(icon) == "" {
		icon = "⚡"
	}

	scheme := wtScheme{
		Name:                style.SchemeName,
		Background:          style.Background,
		Foreground:          style.Foreground,
		CursorColor:         style.CursorColor,
		SelectionBackground: style.SelectionBg,
		Black:               "#0F172A",
		Red:                 "#F87171",
		Green:               "#4ADE80",
		Yellow:              "#FACC15",
		Blue:                "#38BDF8",
		Purple:              "#C084FC",
		Cyan:                "#22D3EE",
		White:               "#E2E8F0",
		BrightBlack:         "#475569",
		BrightRed:           "#FCA5A5",
		BrightGreen:         "#86EFAC",
		BrightYellow:        "#FDE047",
		BrightBlue:          "#7DD3FC",
		BrightPurple:        "#D8B4FE",
		BrightCyan:          "#67E8F9",
		BrightWhite:         "#F8FAFC",
	}

	profileTitle := "⚡ lapel"
	if style.Badge != "" && !strings.EqualFold(style.Badge, "lapel") {
		profileTitle = fmt.Sprintf("⚡ lapel (%s)", style.Badge)
	}
	frag := wtFragment{
		Schemes: []wtScheme{scheme},
		Profiles: []wtProfile{
			{
				Updates:           wslDynamicProfileGUID(style.Distro),
				Name:              profileTitle,
				TabTitle:          profileTitle,
				TabColor:          style.TabColor,
				ColorScheme:       style.SchemeName,
				Icon:              icon,
				StartingDirectory: "~",
			},
		},
	}

	return json.MarshalIndent(frag, "", "  ")
}

// windowsTerminalFragmentPath resolves the path to lapel's Windows Terminal
// JSON fragment (%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\lapel\lapel.json).
// Returns "" when no Windows LocalAppData directory can be determined.
func windowsTerminalFragmentPath() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		if profile := os.Getenv("USERPROFILE"); profile != "" {
			candidate := filepath.Join(profile, "AppData", "Local")
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				localAppData = candidate
			}
		}
	}
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "Microsoft", "Windows Terminal", "Fragments", "lapel", "lapel.json")
}

// ApplyWindowsTerminalFragment writes (or removes, when disabled) the Windows
// Terminal JSON fragment for the given distro and style. Returns the fragment
// path written, or "" if skipped/removed.
func ApplyWindowsTerminalFragment(distro string, spec domain.EnvironmentStyle) (string, error) {
	fragPath := windowsTerminalFragmentPath()
	if fragPath == "" {
		return "", nil
	}
	return applyWindowsTerminalFragmentAt(fragPath, distro, spec)
}

func applyWindowsTerminalFragmentAt(fragPath, distro string, spec domain.EnvironmentStyle) (string, error) {
	style := ResolveStyle(distro, spec)
	iconPath := filepath.Join(filepath.Dir(fragPath), "lapel-icon.png")
	if !style.Enabled || !style.WindowsTerminal {
		if err := os.Remove(fragPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("removing Windows Terminal fragment %s: %w", fragPath, err)
		}
		if err := os.Remove(iconPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("removing Windows Terminal icon %s: %w", iconPath, err)
		}
		return "", nil
	}

	if err := os.MkdirAll(filepath.Dir(fragPath), 0o755); err != nil {
		return "", fmt.Errorf("creating Windows Terminal fragment dir: %w", err)
	}

	iconColor := style.CursorColor
	if style.TabColor != "" && style.Preset != PresetLapelDark {
		iconColor = style.TabColor
	}
	iconPNG, err := RenderWindowsTerminalIconPNG(iconColor)
	if err != nil {
		return "", fmt.Errorf("rendering Windows Terminal icon: %w", err)
	}
	if err := os.WriteFile(iconPath, iconPNG, 0o644); err != nil {
		return "", fmt.Errorf("writing Windows Terminal icon %s: %w", iconPath, err)
	}

	data, err := buildWindowsTerminalFragmentWithIcon(style, iconPath)
	if err != nil {
		return "", fmt.Errorf("building Windows Terminal fragment: %w", err)
	}
	if err := os.WriteFile(fragPath, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing Windows Terminal fragment %s: %w", fragPath, err)
	}
	return fragPath, nil
}

// ApplyDistroStyle installs (or removes, when preset is "none") the shell
// prompt, welcome banner, and .hushlogin inside the target WSL distro. It hooks
// /etc/bash.bashrc, /etc/skel/.bashrc, and the default user's ~/.bashrc so both
// login and interactive non-login shells display the lapel prompt instead of
// stock Ubuntu's PS1.
func ApplyDistroStyle(ctx context.Context, distro, user string, spec domain.EnvironmentStyle) error {
	if distro == "" {
		distro = Dist
	}
	style := ResolveStyle(distro, spec)

	if !style.Enabled {
		removeScript := `set -e
rm -f /etc/profile.d/95-lapel-style.sh
for rc in /etc/bash.bashrc /etc/skel/.bashrc; do
  if [ -f "$rc" ]; then
    sed -i '/# >>> lapel style >>>/,/# <<< lapel style <<</d' "$rc"
  fi
done
for rc in /home/*/.bashrc /root/.bashrc; do
  if [ -f "$rc" ]; then
    sed -i '/# >>> lapel style >>>/,/# <<< lapel style <<</d' "$rc"
  fi
done
`
		_, err := runRoot(ctx, distro, removeScript)
		return err
	}

	profileScript := BuildShellStyleScript(style)
	var userSetup string
	if user != "" && validateUsername(user) == nil {
		userSetup = fmt.Sprintf(`
if [ -d /home/%s ]; then
  touch /home/%s/.hushlogin
  chown %s:%s /home/%s/.hushlogin 2>/dev/null || true
  if [ -f /home/%s/.bashrc ] && ! grep -q "# >>> lapel style >>>" /home/%s/.bashrc; then
    cat >> /home/%s/.bashrc <<'LAPELRC'
# >>> lapel style >>>
[ -f /etc/profile.d/95-lapel-style.sh ] && . /etc/profile.d/95-lapel-style.sh
# <<< lapel style <<<
LAPELRC
  fi
fi
`, user, user, user, user, user, user, user, user)
	}

	installScript := fmt.Sprintf(`set -e
cat > /etc/profile.d/95-lapel-style.sh <<'LAPELSTYLE'
%sLAPELSTYLE
chmod 644 /etc/profile.d/95-lapel-style.sh
touch /etc/skel/.hushlogin
for rc in /etc/bash.bashrc /etc/skel/.bashrc; do
  if [ -f "$rc" ] && ! grep -q "# >>> lapel style >>>" "$rc"; then
    cat >> "$rc" <<'LAPELRC'
# >>> lapel style >>>
[ -f /etc/profile.d/95-lapel-style.sh ] && . /etc/profile.d/95-lapel-style.sh
# <<< lapel style <<<
LAPELRC
  fi
done
%s`, profileScript, userSetup)

	if _, err := runRoot(ctx, distro, installScript); err != nil {
		return fmt.Errorf("applying shell style in %s: %w", distro, err)
	}
	return nil
}
