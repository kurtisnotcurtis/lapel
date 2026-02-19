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
	// and agent tool outputs. libyaml-tools provides yq via apt.
	"jq", "libyaml-tools",
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
}

// DefaultProvisionOptions derives generic options from an environment spec. the
// distribution name and a base image family inferred from RootFSImage (falling
// back to Ubuntu). It always installs the agent-toolkit baseline.
func DefaultProvisionOptions(spec domain.Environment) ProvisionOptions {
	base := "ubuntu"
	if strings.Contains(strings.ToLower(spec.RootFSImage), "debian") {
		base = "debian"
	}
	name := spec.Name
	if name == "" {
		name = Dist
	}
	var keyPath string
	if home, err := os.UserHomeDir(); err == nil {
		keyPath = filepath.Join(home, ".ssh", "id_ed25519.pub")
	}
	// Prefer $USER (Unix), fall back to %USERNAME% (Windows) so the imported
	// distro starts with a non-root default user on either side. Linux login
	// names are case-sensitive and conventionally lowercase — normalise the
	// Windows USERNAME value to lower-case so `useradd Alice` does not leave us
	// with a mismatched "Alice" vs "alice" later in the SSH step.
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	user = strings.ToLower(user)
	return ProvisionOptions{
		Name:             name,
		Base:             base,
		Version:          "",
		Packages:         AgentToolkitPackages,
		EnsurePackages:   true,
		DefaultUser:      user,
		SSHPublicKeyPath: keyPath,
	}
}

// WSLBuildThreshold is the Windows build number from which [interop]
// appendWindowsPaths=false in /etc/wsl.conf correctly suppresses automatic
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
// the given base image family and version. Ubuntu uses the cloud-images
// rootfs; Debian uses its cloud release portal. The URLs are
// correct-by-construction and can be overridden per-distro via
// ProvisionOptions.RootFSImageOverride when a specific mirror/version is
// needed. Note: WSL's --import (verified on 3.0.3.0) does not accept URLs —
// the tarball must be downloaded to disk first (see downloadRootfs).
func RootfsURL(base, version string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "", "ubuntu":
		if version == "" {
			version = DefaultVersion("ubuntu")
		}
		codename, ok := ubuntuCodenames[version]
		if !ok {
			return "", fmt.Errorf(
				"no known cloud-images codename for Ubuntu %s; pass ProvisionOptions.RootFSImageOverride with a rootfs archive URL or path",
				version)
		}
		// Verified 2026-11: the numeric /releases/<ver>/release/ tree is dead
		// (302s to a 404) and the old cloudimg-amd64 layout was retired; only
		// the codename trees host rootfs archives. WSL's importer (bsdtar)
		// reads .tar.xz directly, so no re-compression is needed.
		return fmt.Sprintf(
			"https://cloud-images.ubuntu.com/%s/current/%s-server-cloudimg-amd64-root.tar.xz",
			codename, codename), nil
	case "debian":
		// Debian no longer publishes WSL-importable rootfs tarballs — its cloud
		// mirrors (bookworm and trixie, checked 2026-07) only ship full disk
		// images (qcow2/raw/tar.xz), which wsl --import cannot consume.
		return "", fmt.Errorf(
			"Debian no longer publishes WSL-importable rootfs tarballs; pass ProvisionOptions.RootFSImageOverride pointing to a rootfs archive")
	default:
		return "", fmt.Errorf("unsupported base %q (want ubuntu|debian)", base)
	}
}

// DefaultVersion returns a sensible default release for the given base family.
func DefaultVersion(base string) string {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "", "ubuntu":
		return "24.04" // latest LTS — stable on WSL
	default:
		return "12" // Debian stable (bookworm)
	}
}

// ubuntuCodenames maps Ubuntu version numbers to their codenames for
// building cloud-images.ubuntu.com rootfs URLs (verified live 2026-11).
var ubuntuCodenames = map[string]string{
	"20.04": "focal",
	"22.04": "jammy",
	"24.04": "noble",
	"25.10": "questing",
	"26.04": "resolute",
}

// codenameForDebian maps a Debian numeric release to its codename.
func codenameForDebian(version string) string {
	switch version {
	case "11":
		return "bullseye"
	case "12", "13":
		return "bookworm"
	default:
		return "bookworm" // default to current stable codename
	}
}

// ImportMinimalDistro downloads the official rootfs tarball and imports it as a
// new WSL distribution via `wsl --import`, then sets its default user so it does
// not start as root (cloud images ship without password logins). It is
// idempotent: if a distro with opts.Name already exists, it returns nil without
// re-downloading or re-importing.
func ImportMinimalDistro(ctx context.Context, opts ProvisionOptions) error {
	if opts.Name == "" {
		opts.Name = Dist
	}
	base := strings.ToLower(strings.TrimSpace(opts.Base))
	switch base {
	case "", "ubuntu":
		opts.Base = "ubuntu"
	case "debian":
		opts.Base = "debian"
	default:
		return fmt.Errorf("unsupported base %q (want ubuntu|debian)", opts.Base)
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

	// The VHDX container must live on an NTFS drive WSL can write to. `wsl
	// --import` runs on the host and requires Windows paths for both the install
	// location and the archive — a /mnt/c/... path fails at HCS with
	// 0x800704b0 (verified 2026-11) — so use the native temp dir.
	tag := "lapel-import-" + randTag()
	installLoc, err := importTargetDir(tag)
	if err != nil {
		return fmt.Errorf("creating import target %s: %w", installLoc, err)
	}

	// WSL's --import only accepts a local archive path — passing the URL
	// directly fails with Wsl/ERROR_INVALID_NAME (verified on 3.0.3.0) — so
	// download the rootfs first, then import from disk. The archive is removed
	// afterwards; the VHDX container stays in installLoc.
	archive, err := downloadRootfs(ctx, url, installLoc)
	if err != nil {
		// Clean up the import directory on download failure to avoid orphan containers
		os.RemoveAll(installLoc)
		return fmt.Errorf("downloading rootfs %s: %w", url, err)
	}
	defer os.Remove(archive)
	// --import is a GLOBAL flag: it cannot be combined with -d (WSL rejects the
	// combination), so route through runWslGlobal.
	if _, err := runWslGlobal(ctx, "--import", opts.Name, installLoc, archive); err != nil {
		// Clean up the import directory on import failure to avoid orphan containers
		os.RemoveAll(installLoc)
		return fmt.Errorf("importing %s from %s: %w", opts.Name, archive, err)
	}

	// Cloud images have no non-root user and no password logins. Create the
	// default user (with passwordless sudo for later provisioning steps) and a
	// durable /etc/wsl.conf BEFORE switching the default user: until that
	// switch, every `wsl -d <name>` call still runs as root, which is what the
	// setup script needs.
	defaultUser := opts.DefaultUser
	if defaultUser == "" {
		defaultUser = os.Getenv("USER")
	}
	if defaultUser != "" {
		if err := setupFreshDistro(ctx, opts.Name, defaultUser); err != nil {
			return fmt.Errorf("setting up fresh distro %s: %w", opts.Name, err)
		}
		// --set-default-user is a --manage option, NOT a top-level flag (WSL
		// 3.0.3.0 rejects `wsl --set-default-user` as an invalid argument).
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
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(dest)
		return "", err
	}
	return dest, nil
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
func setupFreshDistro(ctx context.Context, distro, user string) error {
	if err := validateUsername(user); err != nil {
		return err
	}
	build := detectWSLBuild(ctx)
	var interopSection string
	if build >= WSLBuildThreshold || build == 0 { // 0 = detection failed, assume modern
		interopSection = "\n[interop]\nappendWindowsPaths=false"
	} else {
		// Pre-17093 builds don't support [interop] appendWindowsPaths; rely on
		// PATH filtering (perl one-liner) or accept the long PATH.
		interopSection = ""
	}
	script := fmt.Sprintf(`set -e
if ! id -u %s >/dev/null 2>&1; then
  getent group sudo >/dev/null || groupadd sudo
  useradd -m -s /bin/bash -G sudo %s
fi
# Passwordless sudo for common provisioning commands (more restrictive than ALL)
echo %s " ALL=(ALL) NOPASSWD: /usr/bin/apt-get, /usr/bin/systemctl, /usr/sbin/useradd, /usr/sbin/usermod, /bin/chmod, /bin/chown, /bin/mkdir, /usr/bin/tee, /bin/bash" > /etc/sudoers.d/90-lapel-%s
chmod 440 /etc/sudoers.d/90-lapel-%s
if [ -f /etc/wsl.conf ]; then cp /etc/wsl.conf /etc/wsl.conf.lapel-bak; fi
cat > /etc/wsl.conf <<'WSLCONF'
[boot]
systemd=true

[automount]
enabled = true

[network]
generateResolvConf = false

[user]
default = %s%s
WSLCONF
# Use Google's public DNS for reliable resolution (WSL's NAT gateway 10.255.255.254
cat > /etc/resolv.conf <<'RESOLV'
nameserver 8.8.8.8
nameserver 8.8.4.4
RESOLV
# Runtime PATH filter: strip /mnt/[a-zA-Z]+ Windows mount paths from PATH.
# This works even when [interop] appendWindowsPaths=false doesn't fully suppress them.
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
`, user, user, shellQuote(user), user, user, user, interopSection)
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

// InstallAgentToolkit installs the agent-toolkit baseline packages into an
// existing distro idempotently. It runs `apt-get update` once, then installs
// only the packages that are not already present (queried via dpkg-query), so
// repeating it without manifest changes does not reinstall everything.
func InstallAgentToolkit(ctx context.Context, dist string, packages []string) error {
	if len(packages) == 0 {
		packages = AgentToolkitPackages
	}
	if _, err := runRoot(ctx, dist, "apt-get -y update"); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}
	missing, pkgErr := missingPackages(ctx, dist, packages)
	// If dpkg-query failed, we conservatively reinstall everything to ensure
	// the toolkit is present. The error is logged but doesn't block installation.
	if pkgErr != nil {
		fmt.Printf("[WARN] package query failed (%v), reinstalling all %d packages\n", pkgErr, len(packages))
	}
	if len(missing) == 0 {
		return nil
	}
	// Quote each package name so apt-get never interprets shell metacharacters
	// in an untrusted manifest entry (spec 8).
	var quoted []string
	for _, p := range missing {
		quoted = append(quoted, shellQuote(p))
	}
	script := "set -e\napt-get -y install " + strings.Join(quoted, " ")
	if _, err := runRoot(ctx, dist, script); err != nil {
		return fmt.Errorf("apt-get install %v: %w", missing, err)
	}
	return nil
}

// installUv ensures uv (Rust-based Python package manager + runner) is installed
// and usable inside the distro. It is idempotent: if /usr/local/uv exists it skips
// the download; otherwise it runs the official installer as root, then pins uv to
// a stable version so `uv` works for agents out of the box.
func installUv(ctx context.Context, dist string) error {
	// Check whether uv is already installed (root-owned binary in /usr/local/uv).
	check := `test -x /usr/local/uv/bin/uv && echo ok`
	out, err := runRoot(ctx, dist, check)
	if err != nil || !strings.Contains(out, "ok") {
		// Official uv installer: downloads the latest release tarball and
		// extracts to /usr/local/uv. We use the root path so any user can
		// pick it up from PATH.
		script := `set -e
if ! command -v curl >/dev/null 2>&1; then apt-get -y install curl; fi
curl -fsSL https://astral.sh/uv/install.sh | bash -s -- --install-dir /usr/local/uv
# uv's installer writes to ~/.bashrc of the calling user, but we want it
# system-wide so every agent session picks up ` + "`export PATH=\"/usr/local/uv/bin:$PATH\"`" + `. The
# per-user init is harmless; we just make sure /usr/local/uv/bin is on PATH.
cat > /etc/profile.d/uv.sh <<'UVSH'
export UV_CACHE_DIR=/tmp/uv-cache
export PATH="/usr/local/uv/bin:$PATH"
UVSH`
		if _, err := runRoot(ctx, dist, script); err != nil {
			return fmt.Errorf("installing uv: %w", err)
		}
	}
	// Pin a stable Python version for agents. `uv python install 3.12` installs
	// CPython 3.12 as a standalone binary (no system Python dependency), so it
	// works even on minimal distros without /usr/bin/python3.
	useScript := `set -e
export PATH="/usr/local/uv/bin:$PATH"
uv python install 3.12`
	if _, err := runRoot(ctx, dist, useScript); err != nil {
		return fmt.Errorf("pinning Python 3.12 via uv: %w", err)
	}
	return nil
}

// installFnmAndNode ensures fnm + a Node LTS runtime are installed and usable
// inside the distro. It is idempotent: if /usr/local/fnm exists it skips the
// download; otherwise it runs the official installer as root, then pins Node LTS
// as the active version so `node` works for agents out of the box.
func installFnmAndNode(ctx context.Context, dist string) error {
	// Check whether fnm is already installed (root-owned binary in /usr/local/fnm).
	check := `test -x /usr/local/fnm/fnm && echo ok`
	out, err := runRoot(ctx, dist, check)
	if err != nil || !strings.Contains(out, "ok") {
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
		if _, err := runRoot(ctx, dist, script); err != nil {
			return fmt.Errorf("installing fnm: %w", err)
		}
	}
	// Pin Node LTS. `fnm install --lts` auto-marks the installed version as
	// default + lts-latest, so a single install call is enough — no separate
	// `use` step needed.
	useScript := `set -e
export FNM_DIR=/usr/local/fnm
export PATH="/usr/local/fnm:$PATH"
eval "$(/usr/local/fnm/fnm env)" 2>/dev/null || true
fnm install --lts`
	if _, err := runRoot(ctx, dist, useScript); err != nil {
		return fmt.Errorf("pinning Node LTS via fnm: %w", err)
	}
	return nil
}

// provisionDistro orchestrates import-if-missing then baseline-install for the
// given options. When the distro already exists it installs packages only when
// opts.EnsurePackages is set (default true), keeping existing-distro calls cheap.
func provisionDistro(ctx context.Context, b *DistBackend, opts ProvisionOptions) error {
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
		if err := ImportMinimalDistro(ctx, opts); err != nil {
			return err
		}
	} else if !opts.EnsurePackages {
		return nil // existing distro; opt out of package maintenance
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
		user = os.Getenv("USER")
	}
	keyInstalled, err := EnsureSSHAccess(ctx, opts.Name, user, opts.SSHPublicKeyPath)
	if err != nil {
		return fmt.Errorf("ensuring ssh access: %w", err)
	}
	if keyInstalled {
		// WSL's localhost forwarding takes a few seconds to settle after boot,
		// so VerifySSH retries for up to 30s before declaring failure.
		if err := VerifySSH(ctx, opts.Name, user, 30*time.Second); err != nil {
			return err
		}
	}
	return nil
}

// missingPackages returns the subset of packages that are not installed in the
// current distro (queried once via dpkg-query). On query failure it assumes
// nothing is present so a full install can proceed.
func missingPackages(ctx context.Context, dist string, packages []string) ([]string, error) {
	// ${db:Status-Status} yields the single-word status ("installed", "removed",
	// ...). The older ${Status} field expands to "install ok installed" and
	// breaks word-based parsing — that mismatch used to mark every package as
	// missing on every run.
	out, err := runWslWith(ctx, []string{"-d", dist, "dpkg-query", "-W", "-f=${Package}\\t${db:Status-Status}\\n"})
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

// importTargetDir returns a writable directory for the VHDX container: on the
// host mount when inside WSL, otherwise a local temp dir.
// importTargetDir creates (and returns) the directory that will hold the
// imported distro's ext4.vhdx. It must be a Windows path: wsl --import runs
// on the host and HCS rejects WSL-style /mnt/... paths.
func importTargetDir(tag string) (string, error) {
	target := filepath.Join(os.TempDir(), tag)
	return target, os.MkdirAll(target, 0o755)
}
