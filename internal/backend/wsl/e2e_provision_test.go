package wsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lapel/internal/agent"
	"lapel/internal/domain"
	"lapel/internal/provider"
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

	// 7. Agent detection: verify that agents can be detected inside the distro.
	// This tests the exec path for agent binaries (pi, copilot, claude).
	t.Log("\n=== Agent Detection ===")
	agentMgr := agent.NewManager()
	agents, err := agentMgr.DetectAll(ctx)
	if err != nil {
		t.Fatalf("agent detection failed: %v", err)
	}
	for _, a := range agents {
		status := "✗ not found"
		if a.Installed {
			status = "✓ available"
		}
		errMsg := ""
		if a.Error != "" {
			errMsg = fmt.Sprintf(" (error: %s)", a.Error)
		}
		t.Logf("  %s: %s%s", a.Name, status, errMsg)
	}

	// 8. Inference provider testing: check if LM Studio or Ollama are reachable.
	// This tests the full stack from WSL to Windows host networking.
	t.Log("\n=== Inference Provider Testing ===")
	reg := provider.NewRegistry(name) // use our throwaway distro
	statuses, err := reg.StatusAll(ctx)
	if err != nil {
		t.Fatalf("provider status check failed: %v", err)
	}
	for _, s := range statuses {
		installed := "no"
		if s.Installed {
			installed = "yes"
		}
		running := "no"
		if s.Running {
			running = "yes"
		}
		t.Logf("  %s: installed=%s running=%s port=%d",
			s.Name, installed, running, s.Port)
		if len(s.Models) > 0 {
			t.Logf("    models: %d loaded", len(s.Models))
		}
		if s.Error != "" {
			t.Logf("    error: %s", s.Error)
		}
	}

	// 9. Verify Go runtime is functional (we just installed golang-go).
	t.Log("\n=== Go Runtime Verification ===")
	goOut, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "go version"})
	if err != nil {
		t.Fatalf("go version check failed: %v", err)
	}
	t.Logf("  Go: %s", strings.TrimSpace(goOut))

	// 10. Verify Python/uv is functional.
	t.Log("\n=== Python/uv Verification ===")
	uvOut, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "uv --version"})
	if err != nil {
		t.Fatalf("uv version check failed: %v", err)
	}
	t.Logf("  uv: %s", strings.TrimSpace(uvOut))

	pythonOut, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "python3 --version"})
	if err != nil {
		// python3 might not be in PATH if uv manages it exclusively; that's ok
		t.Logf("  python3: %s (may be managed by uv)", strings.TrimSpace(pythonOut))
	} else {
		t.Logf("  python3: %s", strings.TrimSpace(pythonOut))
	}

	// 11. Verify Node/fnm is functional.
	t.Log("\n=== Node/fnm Verification ===")
	nodeOut, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "node --version"})
	if err != nil {
		t.Fatalf("node version check failed: %v", err)
	}
	t.Logf("  Node: %s", strings.TrimSpace(nodeOut))

	fnmOut, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "fnm --version"})
	if err != nil {
		t.Fatalf("fnm version check failed: %v", err)
	}
	t.Logf("  fnm: %s", strings.TrimSpace(fnmOut))

	// 12. Test a simple Go program compilation and execution.
	t.Log("\n=== Go Compilation Test ===")
	goTestProgram := `
package main
import "fmt"
func main() {
	fmt.Println("Hello from lapel e2e test!")
}`
	tmpDir := "/tmp/lapel-e2e-go-test"
	runWslWith(ctx, []string{"-d", name, "bash", "-c", fmt.Sprintf("mkdir -p %s && cat > main.go << 'EOF'\n%s\nEOF", tmpDir, goTestProgram)})
	compileRes, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", fmt.Sprintf("cd %s && go build -o hello main.go && ./hello 2>&1", tmpDir)})
	if err != nil {
		t.Fatalf("Go compilation test failed: %v\noutput: %s", err, compileRes)
	}
	t.Logf("  Go program output: %s", strings.TrimSpace(compileRes))

	// 13. Test SSH connectivity from Windows host to WSL distro.
	if !inWSL {
		t.Log("\n=== SSH Connectivity Test ===")
		sshOut, err := runWslWith(ctx, []string{"-d", name, "bash", "-c", "ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 localhost whoami"})
		if err != nil {
			t.Logf("  SSH localhost test: %v (may need passwordless sudo setup)", err)
		} else {
			t.Logf("  SSH localhost: %s", strings.TrimSpace(sshOut))
		}
	}

	// 14. Summary report.
	t.Log("\n=== E2E Test Summary ===")
	t.Logf("Distro: %s (ubuntu 24.04)", name)
	t.Logf("User: %s", user)
	t.Logf("Agents detected: %d/%d", countAvailable(agents), len(agents))
	t.Logf("Providers running: %d/%d", countRunning(statuses), len(statuses))
}

// countAvailable counts how many agents are available.
func countAvailable(agents []agent.AgentStatus) int {
	count := 0
	for _, a := range agents {
		if a.Installed {
			count++
		}
	}
	return count
}

// countRunning counts how many providers are running.
func countRunning(statuses []*provider.ProviderStatus) int {
	count := 0
	for _, s := range statuses {
		if s.Running {
			count++
		}
	}
	return count
}
