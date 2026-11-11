// Package wsl is the first ExecutionBackend implementation. It owns all
// wsl.exe mechanics: distro discovery, user provisioning, bootstrap, command
// execution, process tracking and lifecycle (spec 4.2).
package wsl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"lapel/internal/domain"
)

// Dist is the WSL distribution name to use. Overridable for testing.
var Dist = "Ubuntu-Lean"

func wslArgs(args ...string) []string {
	out := append([]string{"-d", Dist}, args...)
	return out
}

// runWsl executes `wsl.exe -d <dist> <args...>` and returns trimmed stdout.
func runWsl(ctx context.Context, args ...string) (string, error) {
	return runWslWith(ctx, wslArgs(args...))
}

// runWslGlobal executes `wsl.exe <args...>` WITHOUT the `-d <dist>` prefix.
// Global management flags (--import, --unregister, ...) cannot be combined
// with -d: WSL rejects them ("Invalid command line argument") or mis-parses
// the arguments. Verified on WSL 3.0.3.0. Note: --set-default-user is NOT a
// top-level flag — it is a --manage option (`wsl --manage <name>
// --set-default-user <user>`).
func runWslGlobal(ctx context.Context, args ...string) (string, error) {
	return runWslWith(ctx, args)
}

// runWslWith runs wsl.exe with an explicit argument list and returns trimmed,
// sanitized stdout.
func runWslWith(ctx context.Context, cmdArgs []string) (string, error) {
	return runWslWithInput(ctx, cmdArgs, "")
}

// runWslWithInput is like runWslWith but pipes stdinText into wsl.exe's stdin.
// Use it for bash scripts instead of passing them as -c arguments: WSL 3.0.3.0
// mangles unquoted $(...) in argv — command substitutions arrive empty and
// silently break scripts (verified 2026-11) — while stdin passes through
// intact.
func runWslWithInput(ctx context.Context, cmdArgs []string, stdinText string) (string, error) {
	// NOTE: go1.27 has a bug where spreading a variadic parameter directly as an
	// argument inside another multi-argument call (`exec.CommandContext(..., wslArgs(args)...)`)
	// fails to type-check ("cannot use args as string value"). Assigning the helper
	// result to a local first avoids the nested-spread expression.
	cmd := exec.CommandContext(ctx, "wsl.exe", cmdArgs...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}
	err := cmd.Run()
	if err != nil {
		// Include stderr: wsl.exe and in-distro commands report failures there
		// (e.g. `useradd: Permission denied`), and a stdout-only message can be
		// empty, leaving no clue what went wrong.
		detail := strings.TrimSpace(sanitize(out.String() + "\n" + errb.String()))
		return sanitize(strings.TrimSpace(out.String())), fmt.Errorf("%s: %w", detail, err)
	}
	return sanitize(out.String()), nil
}

// sanitize removes NUL bytes that wsl.exe emits with UTF-16LE encoding when its
// stdout is a pipe rather than a console (e.g. `wsl.exe --list --verbose`).
// Stripping NULs reconstructs the intended ASCII/UTF-8 text for these commands.
func sanitize(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// ListDistros enumerates installed distributions (name + version).
func ListDistros(ctx context.Context) ([]string, error) {
	out, err := runWslGlobal(ctx, "--list", "--verbose")
	if err != nil {
		return nil, err
	}
	return parseDistroNames(out), nil
}

// parseDistroNames extracts distro names from `wsl --list --verbose` output.
// Columns are NAME STATE VERSION; the name may contain spaces, so it is
// everything except the last two fields. The active distro is prefixed with '*'.
// (No dash heuristic: plain names like "Ubuntu" must not be dropped, or import
// idempotency breaks.)
func parseDistroNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "NAME" {
			continue // skip blank lines and header
		}
		name := strings.Join(fields[:len(fields)-2], " ")
		name = strings.TrimSpace(strings.TrimPrefix(name, "*"))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// OSRelease parses /etc/os-release for the current distro.
func OSRelease(ctx context.Context) (map[string]string, error) {
	out, err := runWsl(ctx, "cat", "/etc/os-release")
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = strings.Trim(kv[1], `"`)
		}
	}
	return m, nil
}

// Detect reports whether WSL2 and the target distro are available.
func (d *DistBackend) Detect(ctx context.Context) (domain.BackendStatus, error) {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return domain.BackendStatus{Available: false}, fmt.Errorf("wsl.exe not found on PATH: %w", err)
	}
	names, err := ListDistros(ctx)
	if err != nil {
		return domain.BackendStatus{}, fmt.Errorf("listing distros: %w", err)
	}
	status := domain.BackendStatus{Name: "wsl"}
	for _, n := range names {
		if strings.EqualFold(n, Dist) {
			status.Available = true
			break
		}
	}
	// Populate WSL version if available.
	if status.Available {
		status.Version = detectWSLVersion(ctx)
	}
	return status, nil
}

// detectWSLVersion parses `wsl.exe --version` output to extract the WSL version string.
// Returns empty string if detection fails or output is unexpected.
func detectWSLVersion(ctx context.Context) string {
	out, err := runWslWith(ctx, []string{"--version"})
	if err != nil {
		return ""
	}
	// Parse the first line: "WSL version: 3.0.5.0"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "wsl version:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			return ""
		}
		// parts[2] is the WSL version (e.g., "3.0.5.0")
		return parts[2]
	}
	return ""
}

// Inspect returns the current distro's runtime state.
func (d *DistBackend) Inspect(ctx context.Context) (domain.EnvironmentStatus, error) {
	names, err := ListDistros(ctx)
	if err != nil {
		return domain.EnvironmentStatus{}, err
	}
	meta, _ := OSRelease(ctx)
	st := domain.EnvironmentStatus{Metadata: meta}
	for _, n := range names {
		if strings.EqualFold(n, Dist) {
			st.Exists = true
			st.Distribution = n
			if v, ok := meta["VERSION_ID"]; ok {
				st.Version = v
			}
			break
		}
	}
	return st, nil
}
