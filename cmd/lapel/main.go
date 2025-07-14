// Command lapel is the control-plane CLI for the Lapel platform.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"lapel/internal/backend/wsl"
	"lapel/internal/config"
	"lapel/internal/domain"
	"lapel/internal/hardware"
	"lapel/internal/provider"
	"lapel/internal/reconcile"
)

func main() {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	r := &cobra.Command{
		Use:   "lapel",
		Short: "Get local AI working in WSL — properly.",
	}
	r.AddCommand(
		setupCmd(),
		doctorCmd(),
		envCmd(),
		providerCmd(),
		sessionCmd(),
		wslConfigCmd(),
		provisionCmd(),
		applyCmd(),
		statusCmd(),
		planCmd(),
		checkLocalhostCmd(),
		runCmd(),
		healthCmd(),
	)
	return r
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check WSL environment availability and report diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			b := &wsl.DistBackend{}
			status, err := b.Detect(ctx)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}
			fmt.Printf("backend: %s\n", status.Name)
			fmt.Printf("  available: %v\n", status.Available)
			if status.Version != "" {
				fmt.Printf("  wsl version: %s\n", status.Version)
			}

			fmt.Printf("[DEBUG] status.Available=%v\n", status.Available)

			// Check kernel updates if WSL is available.
			if status.Available {
				kStatus, err := wsl.CheckKernelUpdates(ctx)
				if err != nil {
					fmt.Printf("  kernel update check: error (%s)\n", err)
				} else {
					updateInfo := "up to date"
					if kStatus.Available {
						updateInfo = "UPDATE AVAILABLE"
					}
					fmt.Printf("  kernel: %s\n", updateInfo)
					if kStatus.CurrentVersion != "" {
						fmt.Printf("    current version: %s\n", kStatus.CurrentVersion)
					}
					if kStatus.Message != "" && kStatus.Available {
						fmt.Printf("    message: %s\n", kStatus.Message)
					}
				}

				// Check GPU/CUDA status for ML inference support.
				gStatus, err := wsl.CheckGPUStatus(ctx)
				if err != nil {
					fmt.Printf("  gpu: error (%s)\n", err)
				} else {
					fmt.Printf("  gpu: %s\n", gStatus.Message)
					if gStatus.CUDAVersion != "unknown" && gStatus.CUDAVersion != "" {
						fmt.Printf("    cuda: %s\n", gStatus.CUDAVersion)
					}
					if gStatus.DriverVersion != "unknown" && gStatus.DriverVersion != "" {
						fmt.Printf("    driver: %s\n", gStatus.DriverVersion)
					}
					if len(gStatus.GPUs) > 0 {
						for i, gpu := range gStatus.GPUs {
							fmt.Printf("    gpu %d: %s\n", i+1, gpu)
						}
					}
				}
			}

			// Hardware detection and model recommendation.
			fmt.Println("\n=== Hardware & Model Recommendation ===")
			hwInfo, err := hardware.Detect(ctx)
			if err != nil {
				fmt.Printf("  ⚠ Could not detect hardware: %s\n", err)
			} else {
				// GPU info
				if hwInfo.GPU.Available && hwInfo.GPU.VRAMGB > 0 {
					fmt.Printf("  ✓ GPU: %s (%.1f GB VRAM)\n", hwInfo.GPU.Name, hwInfo.GPU.VRAMGB)
				} else {
					fmt.Println("  ℹ No dedicated GPU detected")
				}

				// RAM info
				fmt.Printf("  ✓ RAM: %.1f GB total, %.1f GB available (%.0f%% used)\n",
					hwInfo.RAM.TotalGB, hwInfo.RAM.AvailableGB, hwInfo.RAM.UsedPercent)

				// CPU info
				fmt.Printf("  ✓ CPU: %s (%d cores, %d threads)\n",
					hwInfo.CPU.Name, hwInfo.CPU.Cores, hwInfo.CPU.Threads)

				// Model recommendation
				rec := hardware.NewDefaultRecommender().Recommend(hwInfo)
				fmt.Printf("\n  Recommended model tier: %s (%.1f GB VRAM)\n", rec.Tier.Name, rec.Tier.MinVRAMGB)
				fmt.Printf("  Suggested model: %s\n", rec.ModelName)
				fmt.Printf("  Reasoning:\n")
				for _, line := range splitLines(rec.Reasoning) {
					fmt.Printf("    %s\n", line)
				}
			}

			if !status.Available {
				fmt.Println("hint: install a distro via `wsl.exe --install` and set DIST=Ubuntu-Lean or run in native Linux.")
			} else {
				fmt.Println("WSL environment is available. Proceed to `lapel env status` for runtime details.")
			}
			return nil
		},
	}
}

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env status",
		Short: "Inspect the registered distribution runtime state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b := &wsl.DistBackend{}
			st, err := b.Inspect(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("exists=%v distribution=%q version=%q running=%v\n", st.Exists, st.Distribution, st.Version, st.Running)
			for k, v := range st.Metadata {
				fmt.Printf("  %s=%s\n", k, v)
			}
			return nil
		},
	}
	return cmd
}

// providerCmd is the parent command for inference provider management.
func providerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage local inference providers (LM Studio, Ollama)",
	}
	cmd.AddCommand(
		providerDetectCmd(),
		providerStatusCmd(),
		providerModelListCmd(),
		providerReachabilityCmd(),
	)
	return cmd
}

// providerDetectCmd checks which inference providers are installed.
func providerDetectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Check which inference providers are installed on the Windows host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			reg := provider.NewRegistry(wsl.Dist)
			statuses, err := reg.DetectAll(ctx)
			if err != nil {
				return fmt.Errorf("detect failed: %w", err)
			}
			for _, s := range statuses {
				installed := "no"
				if s.Installed {
					installed = "yes"
				}
				errMsg := ""
				if s.Error != "" {
					errMsg = fmt.Sprintf(" (%s)", s.Error)
				}
				fmt.Printf("%s: installed=%s%s\n", s.Name, installed, errMsg)
			}
			return nil
		},
	}
}

// providerStatusCmd shows runtime status for all providers.
func providerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show runtime status for all inference providers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			reg := provider.NewRegistry(wsl.Dist)
			statuses, err := reg.StatusAll(ctx)
			if err != nil {
				return fmt.Errorf("status failed: %w", err)
			}
			for _, s := range statuses {
				fmt.Printf("%s:\n", s.Name)
				fmt.Printf("  installed: %v\n", s.Installed)
				fmt.Printf("  running:   %v\n", s.Running)
				if s.Port > 0 {
					fmt.Printf("  port:      %d\n", s.Port)
				}
				if s.BindAddr != "" {
					fmt.Printf("  bind:      %s\n", s.BindAddr)
				}
				if len(s.Models) > 0 {
					fmt.Printf("  models:    %d loaded\n", len(s.Models))
				}
				if s.Error != "" {
					fmt.Printf("  error:     %s\n", s.Error)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

// providerModelListCmd lists available models for a specific provider.
func providerModelListCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "model-list",
		Short: "List available models for an inference provider",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			reg := provider.NewRegistry(wsl.Dist)
			p := reg.FindByName(name)
			if p == nil {
				return fmt.Errorf("provider %q not found (available: lm-studio, ollama)", name)
			}
			models, err := p.ListModels(ctx)
			if err != nil {
				return fmt.Errorf("list models failed: %w", err)
			}
			if len(models) == 0 {
				fmt.Printf("%s: no models loaded\n", name)
				return nil
			}
			for _, m := range models {
				fmt.Println(m)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "provider", "lm-studio", "provider name (lm-studio or ollama)")
	return cmd
}

// providerReachabilityCmd probes the inference endpoint from inside WSL.
func providerReachabilityCmd() *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{
		Use:   "reachability",
		Short: "Probe the inference endpoint from inside WSL and classify errors",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if host == "" {
				resolved, err := provider.HostIP(ctx)
				if err != nil {
					return fmt.Errorf("resolving host IP: %w", err)
				}
				host = resolved
			}
			addr := fmt.Sprintf("%s:%d", host, port)
			fmt.Printf("host=%q port=%d\n", host, port)
			res := provider.CheckEndpoint(ctx, "http://"+addr, 8*time.Second)
			fmt.Printf("status=%s detail=%q\n", res.Status, res.Detail)
			if res.Error != nil {
				return fmt.Errorf("%s: %w", res.Status, res.Error)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "Windows host IP (default: resolved from WSL route)")
	cmd.Flags().IntVar(&port, "port", 1234, "inference endpoint port")
	return cmd
}

func sessionCmd() *cobra.Command {
	var idle = 30 * time.Second
	cmd := &cobra.Command{
		Use:   "session smoke",
		Short: "Launch a session, then stop it and confirm no orphaned processes remain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			b := &wsl.DistBackend{}
			tag := fmt.Sprintf("lapel-smoke-%d", os.Getpid())
			req := domain.ExecRequest{
				Dist:       wsl.Dist,
				WorkingDir: "/tmp",
				Cmd:        []string{"bash", "-c", fmt.Sprintf("exec -a '%s' sleep %d", tag, int(idle.Seconds()))},
			}
			p, err := b.Launch(ctx, req)
			if err != nil {
				return err
			}
			fmt.Printf("launched session pid=%d\n", p.PID())
			time.Sleep(500 * time.Millisecond) // let it boot and spawn the child
			if err := p.Stop(); err != nil {
				return err
			}
			res, _ := p.Wait()
			fmt.Printf("stopped; exit=%d killed=%v\n", res.ExitCode, res.Killed)
			// Verify no orphaned sleeps remain in the guest.
			leftover, err := wsl.CountChildren(ctx, tag)
			if err != nil {
				return fmt.Errorf("orphan check: %w", err)
			}
			fmt.Printf("remaining session children in guest: %d\n", leftover)
			if leftover > 0 {
				return fmt.Errorf("found %d orphaned processes after Stop()", leftover)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&idle, "idle", 30*time.Second, "how long the session's child sleeps")
	return cmd
}

// wslConfigCmd runs a read-only assessment of the host .wslconfig resource caps.
func wslConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "config",
		Short:  "Report-only check of host .wslconfig resource caps (WSL2 [wsl2] section)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			report, err := wsl.InspectConfig(ctx)
			if err != nil {
				return fmt.Errorf("config check: %w", err)
			}
			printConfig(report)
			return nil
		},
	}
}

// provisionCmd installs the agent-toolkit baseline into the distro. It is
// idempotent: it imports a minimal Ubuntu/Debian distro if missing, then installs
// only packages that are not already present.
func provisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "provision",
		Short:  "Install the agent-toolkit baseline into the distro (idempotent)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			b := &wsl.DistBackend{}
			spec := domain.Environment{Name: wsl.Dist}
			if err := b.Provision(ctx, spec); err != nil {
				return fmt.Errorf("provisioning: %w", err)
			}
			fmt.Printf("agent-toolkit baseline installed into %s\n", wsl.Dist)
			return nil
		},
	}
}

// checkLocalhostCmd validates that the distro can reach localhost services on
// the host (mirrored networking), complementing provider reachability which
// checks outbound endpoints. It reports one of the same statuses used by the
// connectivity classifier so results are consistent across commands.
func checkLocalhostCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check-localhost PORT",
		Short: "Probe localhost:<PORT> from inside WSL and classify reachability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid port %q: %w", args[0], err)
			}
			res := provider.CheckLocalhost(ctx, port, 8*time.Second)
			fmt.Printf("status=%s detail=%q\n", res.Status, res.Detail)
			if res.Error != nil {
				return fmt.Errorf("%s: %w", res.Status, res.Error)
			}
			return nil
		},
	}
}

// planCmd computes the desired-state diff for the effective manifest and prints
// whether each managed resource (environment, provider, workspace) matches what
// the manifest declares. It exits non-zero when anything is not as-desired so it
// can gate an apply step.
func planCmd() *cobra.Command {
	var manifestPath string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show whether the environment matches the desired-state manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := os.MkdirAll(".", 0o755); err != nil { // ensure discovery can search cwd
				return fmt.Errorf("creating work dir: %w", err)
			}
			m, err := config.LoadEffective(manifestPath)
			if err != nil {
				return err
			}
			res, err := reconcile.Diff(ctx, m, reconcile.NewOptions(ctx))
			if err != nil {
				return fmt.Errorf("reconciling: %w", err)
			}
			printDiff(res)
			if !res.AsDesired() {
				return ErrNotAsDesired
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "path to LAPeL.yaml (default: discover in cwd/ancestors)")
	return cmd
}

// printConfig renders a read-only .wslconfig resource-cap report.
func printConfig(report wsl.ConfigReport) {
	if report.Path == "" || report.Format == "" {
		fmt.Println("WSL config: none found (reporting default caps)")
		return
	}
	fmt.Printf("WSL config: %s (%s)\n", report.Path, report.Format)
	for _, key := range []string{wsl.CtrlMemory, wsl.CtrlProcessors, wsl.CtrlSwap, wsl.CtrlGuiApps} {
		fmt.Println(report.ControlLine(key))
	}
	for _, note := range report.Notes {
		fmt.Printf("  %s\n", note)
	}
	for _, w := range report.Warnings {
		fmt.Printf("WARNING: %s\n", w)
	}
}

// splitLines splits a string by newlines, returning non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}
