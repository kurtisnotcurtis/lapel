package wsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lapel/internal/domain"
)

// TestE2EFreshImportAndSetup exercises the full provisioning path against a
// real, throwaway WSL distribution: official rootfs import, first-boot user /
// sudo / wsl.conf setup, default-user switch, agent-toolkit install, and SSH
// wiring. It is skipped unless TB_E2E=1 because it downloads an official rootfs
// (~500 MB), creates a real distro on this machine, and unregisters it again at
// the end. Run from the Windows host:
//
//	TB_E2E=1 go test -run TestE2EFreshImportAndSetup -v -timeout 45m ./internal/backend/wsl/
func TestE2EFreshImportAndSetup(t *testing.T) {
	if os.Getenv("TB_E2E") != "1" {
		t.Skip("set TB_E2E=1 to run the live e2e provision (downloads a rootfs and creates a throwaway distro)")
	}

	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	if user == "" {
		user = "tbagent"
	}
	name := fmt.Sprintf("tb-e2e-%d", time.Now().Unix())

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	inWSL := os.Getenv("WSL_DISTRO_NAME") != ""

	// Best-effort cleanup: unregister the throwaway distro and remove any
	// leftover import directories (VHDX containers). TB_E2E_KEEP=1 leaves both
	// in place for post-mortem inspection (the distro's data lives in the
	// import directory, so keep skips that removal too).
	t.Cleanup(func() {
		if os.Getenv("TB_E2E_KEEP") == "1" {
			t.Logf("KEEP: leaving %s and its import directory in place (TB_E2E_KEEP=1)", name)
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer ccancel()
		if out, err := runWslGlobal(cctx, "--unregister", name); err != nil {
			t.Logf("cleanup: unregister %s failed: %v (out: %s)", name, err, strings.TrimSpace(out))
		} else {
			t.Logf("cleanup: unregistered %s", name)
		}
		if home, herr := os.UserHomeDir(); herr == nil {
			for _, dir := range []string{
				filepath.Join(home, "AppData", "Local", "Temp"),
				os.TempDir(),
			} {
				matches, _ := filepath.Glob(filepath.Join(dir, "lapel-import-*"))
				for _, m := range matches {
					if err := os.RemoveAll(m); err == nil {
						t.Logf("cleanup: removed %s", m)
					}
				}
			}
		}
	})

	opts := ProvisionOptions{
		Name:           name,
		Base:           "ubuntu",
		Version:        "24.04",
		Packages:       AgentToolkitPackages,
		EnsurePackages: true,
		DefaultUser:    user,
	}
	if home, err := os.UserHomeDir(); err == nil {
		opts.SSHPublicKeyPath = filepath.Join(home, ".ssh", "id_ed25519.pub")
	}

	t.Logf("provisioning throwaway distro %s (base ubuntu 24.04, user %s)", name, user)
	if err := provisionDistro(ctx, &DistBackend{}, opts); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// 1. The distro exists and is listed by wsl.exe.
	names, err := ListDistros(ctx)
	if err != nil {
		t.Fatalf("listing distros after provision: %v", err)
	}
	found := false
	for _, n := range names {
		if strings.EqualFold(n, name) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("distro %s not in %v after provision", name, names)
	}

	// 2. The default user is the non-root user we asked for: a plain wsl.exe -d
	//    call must now log in as that user, not root. Every check below targets
	//    the throwaway distro explicitly — the package-level Dist points at the
	//    machine's usual distro and must not be consulted here.
	out, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "whoami"})
	if err != nil {
		t.Fatalf("whoami on fresh distro: %v", err)
	}
	if got := strings.TrimSpace(out); got != user {
		t.Fatalf("default user is %q, want %q", got, user)
	}

	// 3. Passwordless sudo works for the default user (runRoot must be able to
	//    escalate without interaction).
	if _, err := runRoot(ctx, name, "true"); err != nil {
		t.Fatalf("passwordless sudo check: %v", err)
	}

	// 4. The durable wsl.conf pins the default user (guards against WSL metadata
	//    resets reverting to root).
	out, err = runWslWith(ctx, []string{"-d", name, "bash", "-c", "cat /etc/wsl.conf"})
	if err != nil {
		t.Fatalf("reading wsl.conf: %v", err)
	}
	if !strings.Contains(out, "default = "+user) {
		t.Errorf("wsl.conf does not pin default user %q:\n%s", user, out)
	}
	if !strings.Contains(out, "systemd=true") {
		t.Errorf("wsl.conf does not enable systemd:\n%s", out)
	}

	// 5. The agent toolkit baseline is installed (spot-check a few packages).
	// Use stdin (heredoc) instead of -c argv to avoid WSL's argv mangling of
	// dpkg-query's ${Package} and ${db:Status-Status} format variables, which
	// bash expands to empty strings when passed via `-c "..."`.
	out, err = runWslWithInput(ctx, []string{"-d", name, "bash"}, `dpkg-query -W -f='${Package}\t${db:Status-Status}\n' openssh-server ripgrep git-filter-repo tmux`)
	if err != nil {
		t.Fatalf("dpkg-query spot check: %v", err)
	}
	for _, pkg := range []string{"openssh-server", "ripgrep", "git-filter-repo", "tmux"} {
		if !parseInstalledPackages(out)[pkg] {
			t.Errorf("expected %s to be installed, got:\n%s", pkg, out)
		}
	}

	// 5b. Command substitutions survive the Exec path (regression: WSL 3.0.3.0
	//     mangles $(...) in argv — even inside quotes — pre-evaluating or
	//     stripping it before bash sees it, verified 2026-11). Scripts must
	//     travel via stdin; this proves a user command with $(...) is intact.
	savedDist := Dist // Exec targets the package-level distro (req.Dist is unused)
	Dist = name
	execRes, err := (&DistBackend{}).Exec(ctx, domain.ExecRequest{
		Cmd: []string{"bash", "-c", "x=$(echo ok); echo sub=$x"},
	})
	Dist = savedDist
	if err != nil {
		t.Fatalf("exec with command substitution: %v", err)
	}
	if got := strings.TrimSpace(execRes.Stdout); got != "sub=ok" {
		t.Errorf("command substitution mangled in Exec: stdout=%q stderr=%q exit=%d", execRes.Stdout, execRes.Stderr, execRes.ExitCode)
	}

	// 6. A full WSL reboot must keep the non-root default user (the regression
	//    observed on Ubuntu-Lean: metadata resets can revert the default to
	//    root). Only possible from the Windows host — inside WSL, --shutdown
	//    would kill this test's own session.
	if inWSL {
		t.Log("running inside WSL; skipping wsl --shutdown reboot verification")
		return
	}
	if _, err := runWslGlobal(ctx, "--shutdown"); err != nil {
		t.Fatalf("wsl --shutdown: %v", err)
	}
	var booted string
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		out, err = runWslWith(ctx, []string{"-d", name, "bash", "-c", "whoami"})
		if err == nil {
			booted = strings.TrimSpace(out)
			break
		}
	}
	if booted == "" {
		t.Fatalf("distro did not come back after wsl --shutdown")
	}
	if booted != user {
		t.Fatalf("after full reboot the default user is %q, want %q", booted, user)
	}
	t.Logf("reboot verification passed: default user still %q", user)
}
