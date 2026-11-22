package wsl

import (
	"context"
	"fmt"
	"strings"

	"lapel/internal/domain"
)

// BootHookPath is the lightweight boot script executed by WSL's [boot] command
// when systemd is disabled. It ensures /run/sshd exists and starts sshd without
// booting a full systemd init tree.
const BootHookPath = "/usr/local/sbin/lapel-boot.sh"

// ResolveSystemd returns the effective systemd setting for an environment spec.
// When unset (nil), systemd defaults to true for backward compatibility.
func ResolveSystemd(spec domain.Environment) bool {
	if spec.Systemd == nil {
		return true
	}
	return *spec.Systemd
}

// BuildBootHookScript returns the POSIX shell script installed at BootHookPath
// when systemd=false. It prepares /run/sshd on every fresh tmpfs boot, ensures
// SSH host keys exist, and launches sshd if not already running.
func BuildBootHookScript() string {
	return `#!/bin/sh
# Managed by lapel — lightweight WSL [boot] hook when systemd=false.
mkdir -p -m 0755 /run/sshd
if [ -x /usr/sbin/sshd ]; then
  if ! ls /etc/ssh/ssh_host_*_key >/dev/null 2>&1; then
    ssh-keygen -A >/dev/null 2>&1 || true
  fi
  if [ ! -f /run/sshd.pid ] || ! kill -0 "$(cat /run/sshd.pid 2>/dev/null)" 2>/dev/null; then
    /usr/sbin/sshd 2>/dev/null || true
  fi
fi
`
}

// bootSection formats the [boot] section for /etc/wsl.conf.
func bootSection(systemd bool) string {
	if systemd {
		return "[boot]\nsystemd=true"
	}
	return fmt.Sprintf("[boot]\nsystemd=false\ncommand=%s", BootHookPath)
}

// updateWSLConfBoot replaces (or prepends) the [boot] section of an /etc/wsl.conf
// file while preserving all other sections and comments verbatim.
func updateWSLConfBoot(existing string, systemd bool) string {
	normalized := strings.ReplaceAll(existing, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var out []string
	inBoot := false
	replaced := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if section == "boot" {
				if !replaced {
					out = append(out, strings.Split(bootSection(systemd), "\n")...)
					replaced = true
				}
				inBoot = true
				continue
			}
			if inBoot {
				// Preserve a blank separator before the next section.
				if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
					out = append(out, "")
				}
				inBoot = false
			}
		}
		if inBoot {
			continue
		}
		out = append(out, line)
	}

	if !replaced {
		body := strings.TrimLeft(strings.Join(out, "\n"), "\n")
		if strings.TrimSpace(body) == "" {
			return bootSection(systemd) + "\n"
		}
		return bootSection(systemd) + "\n\n" + strings.TrimRight(body, "\n") + "\n"
	}

	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

// parseWSLConfBoot extracts the [boot] systemd boolean and command value from
// /etc/wsl.conf content. Configured is nil when no systemd key is set.
func parseWSLConfBoot(content string) (configured *bool, command string) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	inBoot := false
	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			inBoot = section == "boot"
			continue
		}
		if !inBoot {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		switch key {
		case "systemd":
			switch strings.ToLower(val) {
			case "true", "1", "yes":
				b := true
				configured = &b
			case "false", "0", "no":
				b := false
				configured = &b
			}
		case "command":
			command = val
		}
	}
	return configured, command
}

// SystemdStatus reports both the /etc/wsl.conf boot configuration and the live
// runtime PID 1 / sshd state inside a WSL distribution.
type SystemdStatus struct {
	Distro      string
	Configured  *bool
	BootCommand string
	Active      bool
	SSHDRunning bool
}

// InspectSystemd queries /etc/wsl.conf and live PID 1 / sshd state in distro.
func InspectSystemd(ctx context.Context, distro string) (SystemdStatus, error) {
	if strings.TrimSpace(distro) == "" {
		distro = Dist
	}
	script := `if [ -d /run/systemd/system ]; then echo "LAPEL_ACTIVE=true"; else echo "LAPEL_ACTIVE=false"; fi
if ([ -f /run/sshd.pid ] && kill -0 "$(cat /run/sshd.pid 2>/dev/null)" 2>/dev/null) || pgrep -x sshd >/dev/null 2>&1; then
  echo "LAPEL_SSHD=true"
else
  echo "LAPEL_SSHD=false"
fi
echo "---WSLCONF---"
cat /etc/wsl.conf 2>/dev/null || true
`
	out, err := runWslWithInput(ctx, []string{"-d", distro, "sh"}, script)
	if err != nil {
		return SystemdStatus{}, fmt.Errorf("inspecting systemd status in %s: %w", distro, err)
	}

	st := SystemdStatus{Distro: distro}
	parts := strings.SplitN(out, "---WSLCONF---", 2)
	for _, line := range strings.Split(parts[0], "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "LAPEL_ACTIVE=true":
			st.Active = true
		case "LAPEL_SSHD=true":
			st.SSHDRunning = true
		}
	}
	if len(parts) == 2 {
		st.Configured, st.BootCommand = parseWSLConfBoot(parts[1])
	}
	return st, nil
}

// ApplySystemdConfig updates /etc/wsl.conf in distro to enable or disable
// systemd. When disabled, it installs BootHookPath so sshd starts via WSL's
// lightweight [boot] command hook. Changes to /etc/wsl.conf take effect after
// the distro is restarted (`wsl --terminate <distro>`).
func ApplySystemdConfig(ctx context.Context, distro string, enabled bool) error {
	if strings.TrimSpace(distro) == "" {
		distro = Dist
	}
	existing, _ := runWslWith(ctx, []string{"-d", distro, "sh", "-c", "cat /etc/wsl.conf 2>/dev/null || true"})
	updatedConf := updateWSLConfBoot(existing, enabled)

	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("if [ -f /etc/wsl.conf ] && [ ! -f /etc/wsl.conf.lapel-bak ]; then cp /etc/wsl.conf /etc/wsl.conf.lapel-bak; fi\n")
	if !enabled {
		fmt.Fprintf(&b, "mkdir -p /usr/local/sbin\ncat > %s <<'LAPELBOOT'\n%sLAPELBOOT\nchmod 755 %s\n",
			BootHookPath, BuildBootHookScript(), BootHookPath)
	}
	fmt.Fprintf(&b, "cat > /etc/wsl.conf <<'WSLCONF'\n%sWSLCONF\n", updatedConf)

	if _, err := runRoot(ctx, distro, b.String()); err != nil {
		return fmt.Errorf("updating systemd config in %s: %w", distro, err)
	}
	return nil
}

// TerminateDistro stops a running WSL distribution so /etc/wsl.conf changes
// take effect on next launch.
func TerminateDistro(ctx context.Context, distro string) error {
	if strings.TrimSpace(distro) == "" {
		distro = Dist
	}
	if _, err := runWslGlobal(ctx, "--terminate", distro); err != nil {
		return fmt.Errorf("terminating WSL distro %s: %w", distro, err)
	}
	return nil
}
