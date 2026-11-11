package wsl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EnsureSSHAccess makes the distro reachable from the Windows host (via its
// guest IP or `ssh <user>@localhost`): it ensures openssh-server is
// installed, generates any missing SSH host keys, appends the
// given public key (read from a Windows-side file path) to the user's
// authorized_keys inside the distro, and enables/starts sshd.
//
// It returns true when a key was (re)installed — meaning VerifySSH should run
// next — and false when no public key file exists (skipped, not an error).
func EnsureSSHAccess(ctx context.Context, dist, user, pubKeyPath string) (bool, error) {
	if err := validateUsername(user); err != nil {
		return false, err
	}
	if pubKeyPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false, fmt.Errorf("resolving home dir for default key path: %w", err)
		}
		pubKeyPath = filepath.Join(home, ".ssh", "id_ed25519.pub")
	}
	raw, err := os.ReadFile(pubKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // no key material on the host; skip silently
		}
		return false, fmt.Errorf("reading public key %s: %w", pubKeyPath, err)
	}
	key := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(key, "ssh-") {
		return false, fmt.Errorf("%s does not look like a public key (expected leading \"ssh-...\")", pubKeyPath)
	}

	// openssh-server may be missing on pre-existing distros; the toolkit
	// install is idempotent and installs only what is absent.
	if err := InstallAgentToolkit(ctx, dist, []string{"openssh-server"}); err != nil {
		return false, fmt.Errorf("ensuring openssh-server: %w", err)
	}

	// The key goes into the USER's home (not root's): runRoot executes as
	// root, so resolve the user's home explicitly.
	script := fmt.Sprintf(`set -e
home=$(getent passwd %s | cut -d: -f6)
[ -n "$home" ] || { echo "user %s not found in distro" >&2; exit 1; }
mkdir -p "$home/.ssh"
touch "$home/.ssh/authorized_keys"
chmod 700 "$home/.ssh"
grep -qxF %s "$home/.ssh/authorized_keys" || echo %s >> "$home/.ssh/authorized_keys"
chown -R %s:%s "$home/.ssh" 2>/dev/null || true
chmod 600 "$home/.ssh/authorized_keys"
`, user, user, shellQuote(key), shellQuote(key), user, user)
	if _, err := runRoot(ctx, dist, script); err != nil {
		return false, fmt.Errorf("installing ssh key: %w", err)
	}

	// Enable at boot and make sure sshd is running right now. systemctl covers
	// systemd-based WSL distros; the fallbacks cover minimal images without it.
	enable := "systemctl enable ssh 2>/dev/null || update-rc.d ssh defaults 2>/dev/null || true"
	// Ubuntu's openssh-server postinst defers host-key generation to a
	// boot-time unit. On distros that booted before the package was installed
	// (the fresh-import case) no keys exist and sshd refuses to start with
	// "no hostkeys available". ssh-keygen -A is idempotent: it only generates
	// missing keys.
	keys := "ssh-keygen -A 2>/dev/null || true"
	start := "(systemctl restart ssh 2>/dev/null) || (service ssh start 2>/dev/null) || (/usr/sbin/sshd 2>/dev/null) || true"
	if _, err := runRoot(ctx, dist, enable+"\n"+keys+"\n"+start); err != nil {
		return false, fmt.Errorf("enabling ssh service: %w", err)
	}
	return true, nil
}

// distroHostIP returns the first IP address reported by the distro itself
// (`hostname -I`), or "" when it cannot be determined. In WSL NAT mode the
// guest IP is directly reachable from the Windows host and — unlike
// localhost:22 — is unambiguous when several distros run sshd (WSL's
// localhost port mapping hands the port to one of them, arbitrarily).
func distroHostIP(ctx context.Context, dist string) string {
	out, err := runWslWith(ctx, []string{"-d", dist, "hostname", "-I"})
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// VerifySSH retries `ssh -o BatchMode=yes <user>@<host> true` until it
// succeeds or timeout elapses. WSL's localhost forwarding takes a few seconds
// to settle after boot, so a single immediate attempt is not enough.
//
// Each round first tries the distro's own guest IP (unambiguous even when
// other distros run sshd), then falls back to localhost:22.
func VerifySSH(ctx context.Context, dist, user string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var ip string // resolved lazily: the guest IP may not be assigned yet right after first boot
	attempt := 0
	for {
		attempt++
		if ip == "" {
			ip = distroHostIP(ctx, dist)
		}
		hosts := []string{"localhost"}
		if ip != "" {
			hosts = []string{ip, "localhost"}
		}
		roundErrs := make([]string, 0, len(hosts))
		for _, host := range hosts {
			args := []string{
				"-o", "BatchMode=yes",
				"-o", "ConnectTimeout=5",
				// StrictHostKeyChecking=no: E2E provisioning uses throwaway
				// distros that may reuse IPs across runs. WSL's DHCP hands out
				// the same IP to a new distro, but ssh-keygen -A generates
				// fresh host keys each time — accept-new would fail on the key
				// change. For connectivity verification we don't need strict
				// host key validation.
				"-o", "StrictHostKeyChecking=no",
				user + "@" + host,
				"true",
			}
			cmd := exec.Command("ssh", args...)
			out, err := cmd.CombinedOutput()
			if err == nil {
				fmt.Printf("[INFO] SSH verified after %d attempt(s) via %s\n", attempt, host)
				return nil
			}
			errMsg := fmt.Sprintf("ssh %s@%s: %v (%s)", user, host, err, strings.TrimSpace(string(out)))
			roundErrs = append(roundErrs, errMsg)
			fmt.Printf("[DEBUG] Attempt %d/%s failed: %s\n", attempt, host, errMsg)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("SSH verification timed out after %v (%d attempts): %s",
				timeout, attempt-1, strings.Join(roundErrs, "; "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
