// Package wsl implements the backend that talks to WSL2 distros: detection,
// execution inside a running distro, resource-config inspection, and optional
// minimal-distro provisioning. It is intentionally generic — it discovers the
// host mount and .wslconfig at runtime rather than hard-coding any single
// machine's layout (e.g. C:\Users\user\.wslconfig).
package wsl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lapel/internal/domain"
)

// AgentToolkitPackages is the minimum set of packages lapel installs into
// a freshly provisioned minimal Ubuntu/Debian distro for agentic use. It is a
// gap-fill for a clean cloud image, curated so an agent can:
//   - compile and run Go code (golang-go)
//   - use Python tooling explicitly via uv (uv + python3; see installUv)
//   - process JSON/YAML from APIs and configs (jq, libyaml-tools)
//   - SSH out to remote systems (openssh-client) in addition to accepting
//     inbound connections from Windows (openssh-server)
//   - compile C/Rust code when pulling repos that need it (build-essential + make)
//
// Node is managed by fnm (see installFnmAndNode); Python is managed by uv
// (see installUv), which also provides pip-compatible functionality.
var AgentToolkitPackages = []string{
	// core tooling — git, network, archives
	"git", "curl", "wget", "ca-certificates", "gnupg", "unzip", "zip",
	// git history rewriting (provides the git-filter-repo command)
	"git-filter-repo",
	// search / filesystem helpers
	"ripgrep", "fd-find", "tree",
	// editors / session + byte utilities
	"tmux", "xxd",
	// minimal system utilities (keep the distro lean)
	"procps", "iproute2",
	// SSH server: agents reach the distro over `ssh user@localhost` from
	// Windows. On WSL 3.x with NAT networking this is reliable; mirrored mode
	// breaks host->WSL port forwarding (see InspectConfig notes).
	"openssh-server",
	// SSH client: agent needs outbound SSH to reach GitHub, remote hosts,
	// and other agents in multi-agent setups.
	"openssh-client",
	// Go runtime — lapel is written in Go and agents frequently compile/run
	// Go code from repos (e.g. go install, go build, vet).
	"golang-go",
	// Python3 + pip — explicit install so agents can use Python tooling
	// without relying on transitive apt dependencies.
	"python3", "python3-pip",
	// JSON/YAML processing — critical for API responses, config files,
	// and agent tool outputs. yq lives in Ubuntu's universe component.
	"jq", "yq",
	// Build tools — agents compile C/Rust code from repos that need it
	// (e.g. go build with CGO, rustc from source).
	"make", "build-essential",
}

// ProvisionOptions configures optional minimal-distro creation during
// Provision. All fields are generic and machine-independent.
type ProvisionOptions struct {
	// Name is the WSL distribution name to create when it does not exist
	// (e.g. "Ubuntu-Lean"). Defaults to Dist when empty.
	Name string
	// Base selects the base image family: "ubuntu" or "debian".
	Base string
	// Version pins the release, e.g. "24.04" for Ubuntu LTS / "12" for Debian
	// stable. When empty a sensible default per base is used.
	Version string
	// Packages overrides the agent-toolkit baseline (nil => use defaults).
	Packages []string
	// EnsurePackages, when true, installs the agent-toolkit baseline even for an
	// already-installed distro. Default true via DefaultProvisionOptions.
	EnsurePackages bool
	// DefaultUser sets the WSL default user for the imported distro so it does
	// not start as root. Defaults to $USER when empty.
	DefaultUser string
	// SSHPublicKeyPath is the Windows-side path of a public key (e.g.
	// C:\Users\me\.ssh\id_ed25519.pub) appended to the default user's
	// authorized_keys so the host can reach the distro via `ssh user@localhost`.
	// When empty, DefaultProvisionOptions fills in ~/.ssh/id_ed25519.pub. If the
	// file does not exist, SSH key setup is skipped (not an error).
	SSHPublicKeyPath string
	// Systemd controls whether /etc/wsl.conf enables systemd (true) or boots
	// with a lightweight [boot] command for sshd only (false). When nil,
	// fresh imports default to true and existing distros keep their current
	// /etc/wsl.conf [boot] setting.
	Systemd *bool
	// Style configures the visual shell prompt, login banner, and Windows
	// Terminal fragment for the distribution.
	Style domain.EnvironmentStyle
}

// DefaultProvisionOptions derives generic options from an environment spec.
// It always installs the agent-toolkit baseline plus any extra packages.
func DefaultProvisionOptions(spec domain.Environment) ProvisionOptions {
	base := strings.ToLower(strings.TrimSpace(spec.Base))
	if base == "" {
		base = "ubuntu"
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = strings.TrimSpace(spec.Distribution)
	}
	if name == "" {
		name = DefaultDist
	}
	var keyPath string
	if home, err := os.UserHomeDir(); err == nil {
		keyPath = filepath.Join(home, ".ssh", "id_ed25519.pub")
	}
	// Prefer $USER (Unix), fall back to %USERNAME% (Windows) so the imported
	// distro starts with a non-root default user on either side. Normalize
	// Windows usernames (which may contain uppercase letters, dots, or spaces)
	// into a valid POSIX login name.
	rawUser := os.Getenv("USER")
	if rawUser == "" {
		rawUser = os.Getenv("USERNAME")
	}
	user := sanitizeDefaultUsername(rawUser)
	pkgs := append([]string{}, AgentToolkitPackages...)
	seen := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		seen[p] = true
	}
	for _, p := range spec.Packages {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			pkgs = append(pkgs, p)
			seen[p] = true
		}
	}
	return ProvisionOptions{
		Name:             name,
		Base:             base,
		Version:          strings.TrimSpace(spec.Version),
		Packages:         pkgs,
		EnsurePackages:   true,
		DefaultUser:      user,
		SSHPublicKeyPath: keyPath,
		Systemd:          spec.Systemd,
		Style:            spec.Style,
	}
}

// sanitizeDefaultUsername turns an arbitrary host username (e.g. "John.Doe" or
// "Alice Smith") into a valid lowercase POSIX username. Returns "" when raw is
// empty so callers can decide whether to skip default-user setup.
func sanitizeDefaultUsername(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if b.Len() == 0 {
				b.WriteByte('u')
			}
			b.WriteRune(r)
		case r == '_' || r == '-':
			if b.Len() == 0 && r == '-' {
				b.WriteByte('u')
			}
			b.WriteRune(r)
		case r == '.' || r == ' ':
			if b.Len() > 0 {
				b.WriteByte('_')
			}
		}
	}
	out := strings.TrimRight(b.String(), "_-")
	if out == "" {
		return "lapel"
	}
	return out
}

// WSLBuildThreshold is the Windows build number from which [interop]
// appendWindowsPath=false in /etc/wsl.conf correctly suppresses automatic
// Windows PATH appending. Builds before 17093 (RS4) do not support this key.
const WSLBuildThreshold = 17093

// detectWSLBuild parses `wsl.exe --version` output to extract the Windows build
// number. It returns 0 if detection fails, which falls back to the older PATH-
// filtering behavior.
func detectWSLBuild(ctx context.Context) int {
	out, err := runWslWith(ctx, []string{"--version"})
	if err != nil {
		return 0
	}
	// Parse the "Windows version: 10.0.26100.9711" line.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "windows version:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			return 0
		}
		// parts[2] is like "10.0.26100.9711"
		versionParts := strings.Split(parts[2], ".")
		if len(versionParts) < 3 {
			return 0
		}
		// The build number is the third component (e.g., 26100 in Windows 10/11).
		var build int
		fmt.Sscanf(versionParts[2], "%d", &build)
		return build
	}
	return 0
}

// RootfsURL returns the canonical URL of an official ext4 rootfs tarball for
// the given base image family and version. Note: WSL's --import does not accept
// URLs — the tarball must be downloaded to disk first (see downloadRootfs).
func RootfsURL(base, version string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "", "ubuntu":
		if version == "" {
			version = DefaultVersion("ubuntu")
		}
		codename, ok := ubuntuCodenames[version]
		if !ok {
			return "", fmt.Errorf(
				"unsupported Ubuntu version %q (supported: 20.04, 22.04, 24.04, 25.10, 26.04)",
				version)
		}
		return fmt.Sprintf(
			"https://cloud-images.ubuntu.com/%s/current/%s-server-cloudimg-amd64-root.tar.xz",
			codename, codename), nil
	case "debian":
		return "", fmt.Errorf(
			"Debian does not publish WSL-importable cloud rootfs tarballs; use base: ubuntu")
	default:
		return "", fmt.Errorf("unsupported base %q (want ubuntu)", base)
	}
}

// DefaultVersion returns a sensible default release for the given base family.
func DefaultVersion(base string) string {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "", "ubuntu":
		return "24.04" // latest LTS — stable on WSL
	default:
		return "12"
	}
}

// ubuntuCodenames maps Ubuntu version numbers to their codenames for
// building cloud-images.ubuntu.com rootfs URLs.
var ubuntuCodenames = map[string]string{
	"20.04": "focal",
	"22.04": "jammy",
	"24.04": "noble",
	"25.10": "questing",
	"26.04": "resolute",
}

// ImportMinimalDistro downloads the official rootfs tarball and imports it as a
// new WSL distribution via `wsl --import`, then sets its default user so it does
// not start as root (cloud images ship without password logins). It is
// idempotent: if a distro with opts.Name already exists, it returns nil without
// re-downloading or re-importing.
func ImportMinimalDistro(ctx context.Context, opts ProvisionOptions) error {
	if opts.Name == "" {
		opts.Name = DefaultDist
	}
	base := strings.ToLower(strings.TrimSpace(opts.Base))
	switch base {
	case "", "ubuntu":
		opts.Base = "ubuntu"
	case "debian":
		opts.Base = "debian"
	default:
		return fmt.Errorf("unsupported base %q (want ubuntu)", opts.Base)
	}

	// Validate the default user BEFORE downloading or importing so an invalid
	// username never leaves behind a half-initialized distro.
	defaultUser := strings.TrimSpace(opts.DefaultUser)
	if defaultUser == "" {
		defaultUser = sanitizeDefaultUsername(os.Getenv("USER"))
	}
	if defaultUser != "" {
		if err := validateUsername(defaultUser); err != nil {
			return fmt.Errorf("invalid default user %q: %w", defaultUser, err)
		}
	}

	url, err := RootfsURL(opts.Base, opts.Version)
	if err != nil {
		return err
	}

	names, err := ListDistros(ctx)
	if err != nil {
		return fmt.Errorf("listing distros: %w", err)
	}
	for _, n := range names {
		if strings.EqualFold(n, opts.Name) {
			return nil // idempotent: already present, nothing to import
		}
	}

	installLoc, err := DistroStorageDir(opts.Name)
	if err != nil {
		return fmt.Errorf("creating distro storage dir %s: %w", installLoc, err)
	}

	fmt.Printf("  • Downloading %s rootfs from %s...\n", opts.Base, url)
	archive, err := downloadRootfs(ctx, url, installLoc)
	if err != nil {
		os.RemoveAll(installLoc)
		return fmt.Errorf("downloading rootfs %s: %w", url, err)
	}
	defer os.Remove(archive)

	fmt.Printf("  • Importing %q into %s (this may take 15-30s)...\n", opts.Name, installLoc)
	if _, err := runWslGlobal(ctx, "--import", opts.Name, installLoc, archive); err != nil {
		os.RemoveAll(installLoc)
		return fmt.Errorf("importing %s from %s: %w", opts.Name, archive, err)
	}

	if defaultUser != "" {
		systemdEnabled := opts.Systemd == nil || *opts.Systemd
		fmt.Printf("  • Configuring user %q, /etc/wsl.conf (systemd=%v), and DNS in %q...\n", defaultUser, systemdEnabled, opts.Name)
		if err := setupFreshDistro(ctx, opts.Name, defaultUser, systemdEnabled); err != nil {
			return fmt.Errorf("setting up fresh distro %s: %w", opts.Name, err)
		}
		if _, err := runWslGlobal(ctx, "--manage", opts.Name, "--set-default-user", defaultUser); err != nil {
			return fmt.Errorf("setting default user for %s: %w", opts.Name, err)
		}
	}
	return nil
}

// downloadRootfs fetches the rootfs tarball from url into destDir and returns
// the local path. WSL's --import does not accept URLs (see ImportMinimalDistro),
// so the archive must land on disk first.
// downloadRootfs downloads the Ubuntu cloud rootfs from url to destDir.
// It retries up to 3 times with exponential backoff (1s, 2s) because WSL's
// DNS resolver (NAT gateway 10.255.255.254) can be flaky for large downloads.
func downloadRootfs(ctx context.Context, url string, destDir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	// Retry up to 3 times with exponential backoff for transient DNS failures.
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * time.Second
			fmt.Printf("[retry] rootfs download attempt %d/%d, waiting %s\n", attempt+1, 3, delay)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, lastErr = http.DefaultClient.Do(req)
		if lastErr == nil {
			break
		}
	}
	if resp == nil {
		return "", fmt.Errorf("rootfs download failed after 3 attempts: %w", lastErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	// Derive the archive filename from the URL (last path segment, no query).
	name := ""
	if i := strings.LastIndex(url, "/"); i >= 0 {
		name = url[i+1:]
	}
	name = strings.SplitN(name, "?", 2)[0]
	if name == "" || name == "." || name == "/" {
		name = "rootfs.tar.gz"
	}
	dest := filepath.Join(destDir, name)
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	pr := newDownloadProgressReader(resp.Body, resp.ContentLength, os.Stdout)
	if _, err := io.Copy(f, pr); err != nil {
		os.Remove(dest)
		return "", err
	}
	pr.Finish()
	return dest, nil
}

type downloadProgressReader struct {
	r         io.Reader
	total     int64
	read      int64
	nextPct   int64
	nextBytes int64
	start     time.Time
	out       io.Writer
}

func newDownloadProgressReader(r io.Reader, total int64, out io.Writer) *downloadProgressReader {
	return &downloadProgressReader{
		r:         r,
		total:     total,
		nextPct:   20,
		nextBytes: 25 * 1024 * 1024,
		start:     time.Now(),
		out:       out,
	}
}

func (p *downloadProgressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		p.maybeReport()
	}
	return n, err
}

func (p *downloadProgressReader) maybeReport() {
	if p.out == nil {
		return
	}
	const mb = 1024 * 1024
	if p.total > 0 {
		pct := (p.read * 100) / p.total
		for p.nextPct <= 80 && pct >= p.nextPct {
			fmt.Fprintf(p.out, "    downloaded %d%% (%.1f / %.1f MB)\n", p.nextPct, float64(p.read)/mb, float64(p.total)/mb)
			p.nextPct += 20
		}
		return
	}
	if p.read >= p.nextBytes {
		fmt.Fprintf(p.out, "    downloaded %.1f MB...\n", float64(p.read)/mb)
		p.nextBytes += 25 * mb
	}
}

func (p *downloadProgressReader) Finish() {
	if p.out == nil {
		return
	}
	const mb = 1024 * 1024
	fmt.Fprintf(p.out, "    download complete (%.1f MB in %s)\n", float64(p.read)/mb, time.Since(p.start).Round(time.Millisecond))
}

// setupFreshDistro prepares a freshly imported cloud rootfs for agent use. It
// must run while the distro's default user is still root (i.e. immediately
// after --import, before --set-default-user):
//   - creates the non-root default user with a home dir, sudo group access and
//     a passwordless-sudo drop-in so later provisioning steps can escalate;
//   - writes /etc/wsl.conf enabling systemd and pinning the default user. The
//     wsl.conf key is `default` (NOT `defaultUser` — WSL logs a config warning
//     for unknown keys). This file makes the user choice durable across WSL
//     metadata resets, which have been observed to revert the default to root.
func setupFreshDistro(ctx context.Context, distro, user string, systemd bool) error {
	if err := validateUsername(user); err != nil {
		return err
	}
	script := freshDistroSetupScript(user, detectWSLBuild(ctx), systemd)
	// Target the freshly imported distro explicitly: it has no /etc/wsl.conf and
	// no non-root user yet, so WSL runs the script as root. Using the package-
	// level Dist here would run the script in whatever distro that variable
	// names (verified 2026-11: it ran in Ubuntu-Lean as a non-root default user
	// and failed with `useradd: Permission denied`).
	if _, err := runScript(ctx, distro, script, false); err != nil {
		return fmt.Errorf("fresh-distro setup in %s: %w", distro, err)
	}
	return nil
}

// freshDistroSetupScript builds the first-boot provisioning script that creates
// users, configures passwordless sudo, writes /etc/wsl.conf and /etc/resolv.conf,
// and installs the fallback PATH filter. When systemd is false, /etc/wsl.conf
// disables systemd and installs a lightweight BootHookPath script for sshd.
func freshDistroSetupScript(user string, build int, systemd bool) string {
	var interopSection string
	if build >= WSLBuildThreshold || build == 0 { // 0 = detection failed, assume modern
		interopSection = "\n\n[interop]\nappendWindowsPath=false"
	} else {
		// Pre-17093 builds don't support [interop] appendWindowsPath; rely on
		// PATH filtering (perl one-liner) or accept the long PATH.
		interopSection = ""
	}
	var bootHookSetup string
	if !systemd {
		bootHookSetup = fmt.Sprintf(`mkdir -p /usr/local/sbin
cat > %s <<'LAPELBOOT'
%sLAPELBOOT
chmod 755 %s
`, BootHookPath, BuildBootHookScript(), BootHookPath)
	}
	return fmt.Sprintf(`set -e
if ! id -u %s >/dev/null 2>&1; then
  getent group sudo >/dev/null || groupadd sudo
  useradd -m -s /bin/bash -G sudo %s
fi
# Ensure the dedicated unprivileged 'agent' user exists for jailed sessions (no sudo access)
if ! id -u agent >/dev/null 2>&1; then
  useradd -m -s /bin/bash agent
fi
# Passwordless sudo for the default admin user so lapel provisioning can run root scripts
# (the separate 'agent' user is NOT in sudo and has no sudoers entry).
echo %s " ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/90-lapel-%s
chmod 440 /etc/sudoers.d/90-lapel-%s
if [ -f /etc/wsl.conf ]; then cp /etc/wsl.conf /etc/wsl.conf.lapel-bak; fi
%scat > /etc/wsl.conf <<'WSLCONF'
%s

[automount]
enabled = true

[network]
generateResolvConf = false

[user]
default = %s%s
WSLCONF
# Use Google's public DNS for reliable resolution (WSL's NAT gateway 10.255.255.254
# can be flaky). Remove any /etc/resolv.conf symlink first: Ubuntu 24.04 cloud
# images symlink it into /run/systemd/resolve/, which systemd-resolved wipes on
# reboot when generateResolvConf = false.
rm -f /etc/resolv.conf
cat > /etc/resolv.conf <<'RESOLV'
nameserver 8.8.8.8
nameserver 8.8.4.4
RESOLV
# Runtime PATH filter: strip /mnt/[a-zA-Z]+ Windows mount paths from PATH.
# This works even when [interop] appendWindowsPath=false doesn't fully suppress them.
# Handles single-letter mounts (/mnt/c, /mnt/d) and multi-character mounts (/mnt/cdrom).
cat > /etc/profile.d/90-lapel-path-filter.sh <<'PROFILEEOF'
#!/bin/bash
if command -v perl >/dev/null 2>&1; then
  export PATH=$(/usr/bin/printenv PATH | /usr/bin/perl -ne 'print join(":", grep { !/^\/mnt\/[a-zA-Z]+$/ } split(/:/));')
elif command -v python3 >/dev/null 2>&1; then
  export PATH=$(python3 -c "import os,sys,re; print(':'.join(p for p in os.environ.get('PATH','').split(':') if not re.match(r'^/mnt/[a-zA-Z]+$', p)))")
fi
PROFILEEOF
chmod +x /etc/profile.d/90-lapel-path-filter.sh
`, user, user, shellQuote(user), user, user, bootHookSetup, bootSection(systemd), user, interopSection)
}

// validateUsername restricts a username to the POSIX login-name character set
// so it can be interpolated into shell scripts and file paths safely.
func validateUsername(user string) error {
	if user == "" {
		return fmt.Errorf("empty username")
	}
	for i, r := range user {
		ok := (r == '_') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if i > 0 {
			ok = ok || (r >= '0' && r <= '9') || r == '-'
		}
		if !ok {
			return fmt.Errorf("invalid username %q (allowed: letters, digits, '_', '-', no leading digit/special chars)", user)
		}
	}
	return nil
}

// ensureDNSResolvScript returns a shell snippet that replaces a dangling or
// systemd-resolved stub /etc/resolv.conf (127.0.0.53 with no upstream under
// generateResolvConf=false) with a persistent static /etc/resolv.conf.
func ensureDNSResolvScript() string {
	return `if [ -L /etc/resolv.conf ] || ! grep -q '^nameserver ' /etc/resolv.conf 2>/dev/null || grep -q '^nameserver 127\.0\.0\.53' /etc/resolv.conf 2>/dev/null; then
  rm -f /etc/resolv.conf
  cat > /etc/resolv.conf <<'RESOLV'
nameserver 8.8.8.8
nameserver 8.8.4.4
RESOLV
fi`
}

// ensureUbuntuUniverseScript returns a shell snippet that enables the Ubuntu
// `universe` apt component if running on Ubuntu and `universe` is missing from
// DEB822 (/etc/apt/sources.list.d/ubuntu.sources) or legacy (/etc/apt/sources.list).
// Official Ubuntu cloud rootfs tarballs often ship with only `main` enabled.
func ensureUbuntuUniverseScript() string {
	return `if grep -qi '^ID=ubuntu' /etc/os-release 2>/dev/null; then
  if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then
    awk '/^Components:/ && $0 !~ /(^|[ \t])universe([ \t]|$)/ { $0 = $0 " universe" } { print }' /etc/apt/sources.list.d/ubuntu.sources > /etc/apt/sources.list.d/ubuntu.sources.tmp && mv /etc/apt/sources.list.d/ubuntu.sources.tmp /etc/apt/sources.list.d/ubuntu.sources
  fi
  if [ -f /etc/apt/sources.list ]; then
    awk '/^deb[ \t]+/ && $0 !~ /^deb-src/ && $0 ~ /[ \t]main([ \t]|$)/ && $0 !~ /[ \t]universe([ \t]|$)/ { $0 = $0 " universe" } { print }' /etc/apt/sources.list > /etc/apt/sources.list.tmp && mv /etc/apt/sources.list.tmp /etc/apt/sources.list
  fi
fi`
}

// InstallAgentToolkit installs the agent-toolkit baseline packages into an
// existing distro idempotently. It checks installed packages first via
// dpkg-query; only when packages are missing does it repair resolv.conf if
// needed, enable Ubuntu's universe component, run `apt-get update` with strict
// error checking, and install the missing subset.
func InstallAgentToolkit(ctx context.Context, dist string, packages []string) error {
	if len(packages) == 0 {
		packages = AgentToolkitPackages
	}
	fmt.Printf("  • Checking agent-toolkit packages in %q...\n", dist)
	missing, pkgErr := missingPackages(ctx, dist, packages)
	// If dpkg-query failed, we conservatively reinstall everything to ensure
	// the toolkit is present. The error is logged but doesn't block installation.
	if pkgErr != nil {
		fmt.Printf("  [WARN] package query failed (%v), checking/installing all %d packages\n", pkgErr, len(packages))
	}
	if len(missing) == 0 {
		fmt.Printf("  ✓ All %d requested package(s) already installed\n", len(packages))
		return nil
	}

	fmt.Printf("  • Updating apt package lists in %q...\n", dist)
	updateScript := "set -e\n" + ensureDNSResolvScript() + "\n" + ensureUbuntuUniverseScript() + "\nDEBIAN_FRONTEND=noninteractive apt-get -y -o APT::Update::Error-Mode=any update"
	if _, err := runRootStream(ctx, dist, updateScript, os.Stdout); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}

	// Quote each package name so apt-get never interprets shell metacharacters
	// in an untrusted manifest entry (spec 8).
	var quoted []string
	for _, p := range missing {
		quoted = append(quoted, shellQuote(p))
	}
	fmt.Printf("  • Installing %d missing package(s) in %q: %s\n", len(missing), dist, strings.Join(missing, ", "))
	script := "set -e\nDEBIAN_FRONTEND=noninteractive apt-get -y install " + strings.Join(quoted, " ")
	if _, err := runRootStream(ctx, dist, script, os.Stdout); err != nil {
		return fmt.Errorf("apt-get install %v: %w", missing, err)
	}
	return nil
}

func uvInstallScript() string {
	return `set -e
if ! command -v curl >/dev/null 2>&1; then apt-get -y install curl; fi
mkdir -p /usr/local/uv/bin /usr/local/uv/python
curl -fsSL https://astral.sh/uv/install.sh | env UV_UNMANAGED_INSTALL=/usr/local/uv/bin sh
chmod -R a+rX /usr/local/uv
cat > /etc/profile.d/uv.sh <<'UVSH'
export UV_CACHE_DIR=/tmp/uv-cache
export UV_PYTHON_INSTALL_DIR=/usr/local/uv/python
export PATH="/usr/local/uv/bin:$PATH"
UVSH`
}

// installUv ensures uv (Rust-based Python package manager + runner) is installed
// and usable inside the distro. It is idempotent: if /usr/local/uv exists it skips
// the download; otherwise it runs the official installer as root, then pins uv to
// a stable version so `uv` works for agents out of the box.
func installUv(ctx context.Context, dist string) error {
	fmt.Printf("  • Ensuring uv and Python 3.12 in %q...\n", dist)
	// Check whether uv is already installed (root-owned binary in /usr/local/uv).
	check := `test -x /usr/local/uv/bin/uv && echo ok`
	out, err := runRoot(ctx, dist, check)
	if err != nil || !strings.Contains(out, "ok") {
		fmt.Printf("    installing uv...\n")
		if _, err := runRootStream(ctx, dist, uvInstallScript(), os.Stdout); err != nil {
			return fmt.Errorf("installing uv: %w", err)
		}
	}
	// Pin a stable Python version for agents. `uv python install 3.12` installs
	// CPython 3.12 as a standalone binary (no system Python dependency), so it
	// works even on minimal distros without /usr/bin/python3.
	fmt.Printf("    ensuring Python 3.12 via uv...\n")
	useScript := `set -e
export UV_CACHE_DIR=/tmp/uv-cache
export UV_PYTHON_INSTALL_DIR=/usr/local/uv/python
export PATH="/usr/local/uv/bin:$PATH"
uv python install 3.12
chmod -R a+rX /usr/local/uv`
	if _, err := runRootStream(ctx, dist, useScript, os.Stdout); err != nil {
		return fmt.Errorf("pinning Python 3.12 via uv: %w", err)
	}
	return nil
}

// installFnmAndNode ensures fnm + a Node LTS runtime are installed and usable
// inside the distro. It is idempotent: if /usr/local/fnm exists it skips the
// download; otherwise it runs the official installer as root, then pins Node LTS
// as the active version so `node` works for agents out of the box.
func installFnmAndNode(ctx context.Context, dist string) error {
	fmt.Printf("  • Ensuring fnm and Node.js LTS in %q...\n", dist)
	// Check whether fnm is already installed (root-owned binary in /usr/local/fnm).
	check := `test -x /usr/local/fnm/fnm && echo ok`
	out, err := runRoot(ctx, dist, check)
	if err != nil || !strings.Contains(out, "ok") {
		fmt.Printf("    installing fnm...\n")
		// Official fnm installer: downloads the latest release tarball and
		// extracts to /usr/local/fnm. We use the root path so any user can
		// pick it up from PATH.
		script := `set -e
if ! command -v curl >/dev/null 2>&1; then apt-get -y install curl; fi
curl -fsSL https://fnm.vercel.app/install | bash -s -- --install-dir /usr/local/fnm --skip-shell
# fnm's installer writes to ~/.bashrc of the calling user, but we want it
# system-wide so every agent session picks up ` + "`export PATH=\"/usr/local/fnm:$PATH\"`" + `. The
# per-user init is harmless; we just make sure /usr/local/fnm is on PATH.
# fnm requires eval of $(fnm env) to set up multishells — a plain PATH export
# alone leaves node unresolved after a fresh shell login.
cat > /etc/profile.d/fnm.sh <<'FNMSH'
export FNM_DIR=/usr/local/fnm
export PATH="/usr/local/fnm:$PATH"
eval "$(/usr/local/fnm/fnm env)" 2>/dev/null || true
FNMSH`
		if _, err := runRootStream(ctx, dist, script, os.Stdout); err != nil {
			return fmt.Errorf("installing fnm: %w", err)
		}
	}
	// Pin Node LTS. `fnm install --lts` auto-marks the installed version as
	// default + lts-latest, so a single install call is enough — no separate
	// `use` step needed.
	fmt.Printf("    ensuring Node.js LTS via fnm...\n")
	useScript := `set -e
export FNM_DIR=/usr/local/fnm
export PATH="/usr/local/fnm:$PATH"
eval "$(/usr/local/fnm/fnm env)" 2>/dev/null || true
fnm install --lts`
	if _, err := runRootStream(ctx, dist, useScript, os.Stdout); err != nil {
		return fmt.Errorf("pinning Node LTS via fnm: %w", err)
	}
	return nil
}

// provisionDistro orchestrates import-if-missing then baseline-install for the
// given options. When the distro already exists it installs packages only when
// opts.EnsurePackages is set (default true), keeping existing-distro calls cheap.
func provisionDistro(ctx context.Context, opts ProvisionOptions) error {
	fmt.Printf("  • Checking WSL distro %q...\n", opts.Name)
	names, err := ListDistros(ctx)
	if err != nil {
		return fmt.Errorf("listing distros: %w", err)
	}
	exists := false
	for _, n := range names {
		if strings.EqualFold(n, opts.Name) {
			exists = true
			break
		}
	}
	if !exists {
		fmt.Printf("  • Distro %q not found — creating minimal environment...\n", opts.Name)
		if err := ImportMinimalDistro(ctx, opts); err != nil {
			return err
		}
	} else if !opts.EnsurePackages {
		fmt.Printf("  • Distro %q already exists (skipping package check)\n", opts.Name)
		return nil // existing distro; opt out of package maintenance
	} else {
		fmt.Printf("  • Distro %q already exists\n", opts.Name)
		if opts.Systemd != nil {
			fmt.Printf("  • Updating /etc/wsl.conf systemd=%v in %q...\n", *opts.Systemd, opts.Name)
			if err := ApplySystemdConfig(ctx, opts.Name, *opts.Systemd); err != nil {
				return err
			}
			fmt.Printf("    ℹ Run `wsl --terminate %s` for the updated systemd setting to take effect.\n", opts.Name)
		}
	}
	if err := InstallAgentToolkit(ctx, opts.Name, opts.Packages); err != nil {
		return err
	}
	if err := installFnmAndNode(ctx, opts.Name); err != nil {
		return fmt.Errorf("fnm + Node LTS: %w", err)
	}
	if err := installUv(ctx, opts.Name); err != nil {
		return fmt.Errorf("uv + Python 3.12: %w", err)
	}
	// Give the Windows host a direct `ssh <user>@localhost` path into the
	// distro. Skipped (not an error) when no public key file exists.
	user := opts.DefaultUser
	if user == "" {
		user = sanitizeDefaultUsername(os.Getenv("USER"))
	}
	fmt.Printf("  • Configuring SSH access for %q...\n", user)
	keyInstalled, err := EnsureSSHAccess(ctx, opts.Name, user, opts.SSHPublicKeyPath)
	if err != nil {
		return fmt.Errorf("ensuring ssh access: %w", err)
	}
	if keyInstalled {
		// WSL's localhost forwarding can take time or require passphrase-unlocked
		// keys; treat verification failure as a warning so styling still applies.
		fmt.Printf("  • Verifying SSH connectivity for %q...\n", user)
		if err := VerifySSH(ctx, opts.Name, user, 30*time.Second); err != nil {
			fmt.Printf("  ⚠ SSH verification warning: %v (continuing; sshd is configured)\n", err)
		}
	}
	fmt.Printf("  • Applying shell prompt and Windows Terminal style...\n")
	if err := ApplyDistroStyle(ctx, opts.Name, user, opts.Style); err != nil {
		return fmt.Errorf("applying distro style: %w", err)
	}
	if _, err := ApplyWindowsTerminalFragment(opts.Name, opts.Style); err != nil {
		return fmt.Errorf("updating Windows Terminal fragment: %w", err)
	}
	return nil
}

// missingPackages returns the subset of packages that are not installed in the
// current distro (queried once via dpkg-query). The dpkg-query command is piped
// via stdin so WSL never mangles ${Package} or ${db:Status-Status} in argv.
func missingPackages(ctx context.Context, dist string, packages []string) ([]string, error) {
	out, err := runWslWithInput(ctx, []string{"-d", dist, "bash"}, `dpkg-query -W -f='${Package}\t${db:Status-Status}\n'`)
	if err != nil {
		return append([]string{}, packages...), fmt.Errorf("dpkg-query failed: %w", err)
	}
	installed := parseInstalledPackages(out)
	var missing []string
	for _, p := range packages {
		if !installed[p] {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// parseInstalledPackages parses `dpkg-query -W -f=${Package}\t${db:Status-Status}\n`
// output into a set of installed package names.
func parseInstalledPackages(out string) map[string]bool {
	installed := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "installed" {
			installed[fields[0]] = true
		}
	}
	return installed
}

// DistroStorageDir creates and returns the persistent Windows host directory
// that holds the imported distro's ext4.vhdx (%LOCALAPPDATA%\lapel\distros\<name>).
// Placing VHDX files under LocalAppData instead of %TEMP% prevents Windows
// Storage Sense / temp cleanup from deleting the registered distro's disk.
func DistroStorageDir(distroName string) (string, error) {
	safeName := sanitizeDistroDirName(distroName)
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if profile := os.Getenv("USERPROFILE"); profile != "" {
			candidate := filepath.Join(profile, "AppData", "Local")
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				base = candidate
			}
		}
	}
	if base == "" {
		base = os.TempDir()
	}
	target := filepath.Join(base, "lapel", "distros", safeName)
	return target, os.MkdirAll(target, 0o755)
}

func sanitizeDistroDirName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultDist
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return DefaultDist
	}
	return out
}
