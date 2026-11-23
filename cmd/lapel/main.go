// Command lapel is the control-plane CLI for the Lapel platform.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"lapel/internal/backend/wsl"
	"lapel/internal/config"
	"lapel/internal/domain"
	"lapel/internal/hardware"
	"lapel/internal/provider"
	"lapel/internal/reconcile"
)

// Build-time version metadata injected via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// ExitCodeError signals that a child command exited with a specific non-zero
// status code and lapel should propagate that code without printing an extra
// Cobra error banner.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// resolveDistro is the single distro resolver used across all CLI commands.
// Precedence: explicit CLI --distro flag > manifest environment.distribution > "lapel".
func resolveDistro(flagVal string, m *config.Manifest) string {
	if d := strings.TrimSpace(flagVal); d != "" {
		return d
	}
	if m != nil {
		if d := strings.TrimSpace(m.Environment.Distribution); d != "" {
			return d
		}
	}
	return wsl.DefaultDist
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := root()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	if err := cmd.ExecuteContext(ctx); err != nil {
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	r := &cobra.Command{
		Use:     "lapel",
		Short:   "Get local AI working in WSL — properly.",
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
	}
	r.AddCommand(
		setupCmd(),
		doctorCmd(),
		envCmd(),
		providerCmd(),
		wslConfigCmd(),
		provisionCmd(),
		initCmd(),
		applyCmd(),
		jailControlsCmd(),
		statusCmd(),
		planCmd(),
		runCmd(),
		shellCmd(),
		pluginCmd(),
		benchmarkCmd(),
		communityCmd(),
	)
	return r
}

func doctorCmd() *cobra.Command {
	var distro string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check WSL environment availability and report diagnostics (read-only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			targetDist := resolveDistro(distro, m)
			b := wsl.NewBackend(targetDist)

			status, err := b.Detect(ctx)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}
			fmt.Printf("backend: %s (distro: %s)\n", status.Name, targetDist)
			fmt.Printf("  available: %v\n", status.Available)
			if status.Version != "" {
				fmt.Printf("  wsl version: %s\n", status.Version)
			}
			if status.Note != "" {
				fmt.Printf("  note: %s\n", status.Note)
			}

			// Read-only WSL kernel inspection (never runs `wsl --update`).
			if status.Available {
				if kInfo, err := wsl.InspectKernelInfo(ctx); err == nil {
					if kInfo.KernelVersion != "" {
						fmt.Printf("  kernel version: %s\n", kInfo.KernelVersion)
					}
				}

				// Check GPU/CUDA status inside WSL when the distro is provisioned.
				if status.Note == "" {
					gStatus, err := wsl.CheckGPUStatus(ctx, targetDist)
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
			}

			// Hardware detection and model recommendation.
			fmt.Println("\n=== Hardware & Model Recommendation ===")
			hwInfo, err := hardware.Detect(ctx)
			if err != nil {
				fmt.Printf("  ⚠ Could not detect hardware: %s\n", err)
			} else {
				if hwInfo.GPU.Available && hwInfo.GPU.VRAMGB > 0 {
					fmt.Printf("  ✓ GPU: %s (%.1f GB VRAM)\n", hwInfo.GPU.Name, hwInfo.GPU.VRAMGB)
				} else {
					fmt.Println("  ℹ No dedicated GPU detected")
				}

				fmt.Printf("  ✓ RAM: %.1f GB total, %.1f GB available (%.0f%% used)\n",
					hwInfo.RAM.TotalGB, hwInfo.RAM.AvailableGB, hwInfo.RAM.UsedPercent)

				fmt.Printf("  ✓ CPU: %s (%d cores, %d threads)\n",
					hwInfo.CPU.Name, hwInfo.CPU.Cores, hwInfo.CPU.Threads)

				rec := hardware.NewDefaultRecommender().Recommend(hwInfo)
				fmt.Printf("\n  Recommended model tier: %s (%.1f GB VRAM)\n", rec.Tier.Name, rec.Tier.MinVRAMGB)
				fmt.Printf("  Suggested model: %s\n", rec.ModelName)
				fmt.Printf("  Reasoning:\n")
				for _, line := range splitLines(rec.Reasoning) {
					fmt.Printf("    %s\n", line)
				}
			}

			if !status.Available {
				fmt.Printf("hint: run `lapel setup` or `lapel provision` to create the %q WSL distribution.\n", targetDist)
			} else {
				fmt.Println("WSL environment is available. Proceed to `lapel env status` for runtime details.")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	return cmd
}

func envCmd() *cobra.Command {
	var distro string
	inspectRun := func(cmd *cobra.Command, _ []string) error {
		m, err := config.LoadOptional("")
		if err != nil {
			return err
		}
		targetDist := resolveDistro(distro, m)
		b := wsl.NewBackend(targetDist)
		st, err := b.Inspect(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Printf("exists=%v distribution=%q version=%q running=%v\n", st.Exists, st.Distribution, st.Version, st.Running)
		for k, v := range st.Metadata {
			fmt.Printf("  %s=%s\n", k, v)
		}
		return nil
	}
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Inspect and customize the managed WSL distribution",
		RunE:  inspectRun,
	}
	cmd.PersistentFlags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Inspect the registered distribution runtime state",
			RunE:  inspectRun,
		},
		envStyleCmd(),
		envSystemdCmd(),
	)
	return cmd
}

func envSystemdCmd() *cobra.Command {
	var (
		distro  string
		restart bool
	)
	cmd := &cobra.Command{
		Use:   "systemd [status|on|off]",
		Short: "Inspect or toggle systemd in the managed WSL distribution",
		Long: `Inspect or toggle systemd in /etc/wsl.conf for the managed WSL distribution.

With no argument (or "status"), prints both the /etc/wsl.conf configuration and
whether systemd is currently PID 1. Pass "off" to disable systemd and start sshd
via WSL's lightweight [boot] command hook, or "on" to re-enable systemd.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			flagDist := distro
			if flagDist == "" {
				if f := cmd.Flags().Lookup("distro"); f != nil {
					flagDist = f.Value.String()
				}
			}
			target := resolveDistro(flagDist, m)

			action := "status"
			if len(args) == 1 {
				action = strings.ToLower(strings.TrimSpace(args[0]))
			}

			switch action {
			case "", "status":
				st, err := wsl.InspectSystemd(ctx, target)
				if err != nil {
					return err
				}
				cfgStr := "unset (defaults to false in WSL)"
				if st.Configured != nil {
					cfgStr = fmt.Sprintf("%v", *st.Configured)
				}
				fmt.Printf("distro:     %s\n", st.Distro)
				fmt.Printf("configured: systemd=%s\n", cfgStr)
				if st.BootCommand != "" {
					fmt.Printf("boot cmd:   %s\n", st.BootCommand)
				}
				fmt.Printf("runtime:    systemd active=%v, sshd running=%v\n", st.Active, st.SSHDRunning)
				if st.Configured != nil && *st.Configured != st.Active {
					fmt.Printf("  ℹ Pending restart: run `wsl --terminate %s` for systemd=%v to take effect.\n", st.Distro, *st.Configured)
				}
				return nil

			case "on", "true", "enable", "enabled":
				if err := wsl.ApplySystemdConfig(ctx, target, true); err != nil {
					return err
				}
				fmt.Printf("✓ Enabled systemd in %s (/etc/wsl.conf)\n", target)
				if restart {
					if err := wsl.TerminateDistro(ctx, target); err != nil {
						return err
					}
					fmt.Printf("✓ Terminated %s — systemd will be active on next shell/command\n", target)
				} else {
					fmt.Printf("  ℹ Run `wsl --terminate %s` (or re-run with --restart) to apply.\n", target)
				}
				return nil

			case "off", "false", "disable", "disabled":
				if err := wsl.ApplySystemdConfig(ctx, target, false); err != nil {
					return err
				}
				fmt.Printf("✓ Disabled systemd in %s (/etc/wsl.conf) and installed %s for sshd\n", target, wsl.BootHookPath)
				if restart {
					if err := wsl.TerminateDistro(ctx, target); err != nil {
						return err
					}
					fmt.Printf("✓ Terminated %s — lightweight boot hook will run on next shell/command\n", target)
				} else {
					fmt.Printf("  ℹ Run `wsl --terminate %s` (or re-run with --restart) to apply.\n", target)
				}
				return nil

			default:
				return fmt.Errorf("unknown action %q (want status, on, or off)", action)
			}
		},
	}
	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.Flags().BoolVar(&restart, "restart", false, "terminate the WSL distro immediately so the change takes effect on next launch")
	return cmd
}

func envStyleCmd() *cobra.Command {
	var (
		distro     string
		preset     string
		badge      string
		tabColor   string
		background string
		user       string
		noBanner   bool
		noWT       bool
	)
	cmd := &cobra.Command{
		Use:   "style",
		Short: "Apply shell prompt, welcome banner, and Windows Terminal styling to the WSL distro",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}

			spec := domain.EnvironmentStyle{Preset: wsl.PresetLapelDark}
			if m != nil {
				spec = m.EnvSpec().Style
				if !cmd.Flags().Changed("user") && m.Jail.User != "" {
					user = m.Jail.User
				}
			}

			flagDist := distro
			if flagDist == "" {
				if f := cmd.Flags().Lookup("distro"); f != nil {
					flagDist = f.Value.String()
				}
			}
			targetDist := resolveDistro(flagDist, m)

			if cmd.Flags().Changed("preset") {
				spec.Preset = preset
			}
			if cmd.Flags().Changed("badge") {
				spec.Badge = badge
			}
			if cmd.Flags().Changed("tab-color") {
				spec.TabColor = tabColor
			}
			if cmd.Flags().Changed("background") {
				spec.Background = background
			}
			if cmd.Flags().Changed("no-banner") {
				b := !noBanner
				spec.Banner = &b
			}
			if cmd.Flags().Changed("no-windows-terminal") {
				wt := !noWT
				spec.WindowsTerminal = &wt
			}

			resolved := wsl.ResolveStyle(targetDist, spec)
			if err := wsl.ApplyDistroStyle(ctx, targetDist, user, spec); err != nil {
				return err
			}
			fragPath, err := wsl.ApplyWindowsTerminalFragment(targetDist, spec)
			if err != nil {
				return err
			}

			if !resolved.Enabled {
				fmt.Printf("✓ Removed lapel shell styling and Windows Terminal fragment for %s\n", targetDist)
				return nil
			}
			fmt.Printf("✓ Applied %q style to %s (badge: %q, banner: %v)\n", resolved.Preset, targetDist, resolved.Badge, resolved.Banner)
			if fragPath != "" {
				fmt.Printf("✓ Updated Windows Terminal fragment: %s (scheme: %q, tabColor: %s)\n", fragPath, resolved.SchemeName, resolved.TabColor)
				fmt.Printf("  ℹ Restart Windows Terminal (close all windows) if the %q profile does not appear in the dropdown yet.\n", targetDist)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.Flags().StringVar(&preset, "preset", "", "style preset (lapel-dark, violet, minimal, none)")
	cmd.Flags().StringVar(&badge, "badge", "", "custom prompt badge text (defaults to distro name)")
	cmd.Flags().StringVar(&tabColor, "tab-color", "", "Windows Terminal tab hex color (#RRGGBB)")
	cmd.Flags().StringVar(&background, "background", "", "Windows Terminal background hex color (#RRGGBB)")
	cmd.Flags().StringVar(&user, "user", "", "Linux user to configure ~/.bashrc and ~/.hushlogin for")
	cmd.Flags().BoolVar(&noBanner, "no-banner", false, "disable the one-line welcome banner on login")
	cmd.Flags().BoolVar(&noWT, "no-windows-terminal", false, "skip writing the Windows Terminal JSON fragment")
	return cmd
}

// providerCmd is the parent command for inference provider management.
func providerCmd() *cobra.Command {
	var distro string
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage local inference providers (LM Studio, Ollama)",
	}
	cmd.PersistentFlags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.AddCommand(
		providerDetectCmd(&distro),
		providerStatusCmd(&distro),
		providerModelListCmd(&distro),
		providerReachabilityCmd(&distro),
	)
	return cmd
}

// providerDetectCmd checks which inference providers are installed.
func providerDetectCmd(distroFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Check which inference providers are installed on the Windows host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			reg := provider.NewRegistry(resolveDistro(*distroFlag, m))
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
func providerStatusCmd(distroFlag *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show runtime status for all inference providers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			reg := provider.NewRegistry(resolveDistro(*distroFlag, m))
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
func providerModelListCmd(distroFlag *string) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "model-list",
		Short: "List available models for an inference provider",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			reg := provider.NewRegistry(resolveDistro(*distroFlag, m))
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
			for _, model := range models {
				fmt.Println(model)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "provider", "lm-studio", "provider name (lm-studio or ollama)")
	return cmd
}

// providerReachabilityCmd probes the inference endpoint from inside WSL.
func providerReachabilityCmd(distroFlag *string) *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{
		Use:   "reachability",
		Short: "Probe the inference endpoint from inside WSL and classify errors",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			targetDist := resolveDistro(*distroFlag, m)
			if host == "" {
				resolved, err := provider.HostIP(ctx, targetDist)
				if err != nil {
					return fmt.Errorf("resolving host IP: %w", err)
				}
				host = resolved
			}
			addr := fmt.Sprintf("%s:%d", host, port)
			fmt.Printf("distro=%q host=%q port=%d\n", targetDist, host, port)
			res := provider.CheckEndpoint(ctx, "http://"+addr, 8*time.Second, targetDist)
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

// wslConfigCmd runs a read-only assessment of the host .wslconfig resource caps.
func wslConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Report-only check of host .wslconfig resource caps (WSL2 [wsl2] section)",
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

// provisionCmd installs the agent-toolkit baseline into the distro.
func provisionCmd() *cobra.Command {
	var (
		distro  string
		systemd bool
	)
	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Install the agent-toolkit baseline into the distro (idempotent)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			targetDist := resolveDistro(distro, m)
			spec := domain.Environment{Name: targetDist, Distribution: targetDist}
			if m != nil {
				spec = m.EnvSpec()
				spec.Name = targetDist
				spec.Distribution = targetDist
			}
			if cmd.Flags().Changed("systemd") {
				s := systemd
				spec.Systemd = &s
			}
			fmt.Printf("=== lapel provision (%s) ===\n", spec.Name)
			b := wsl.NewBackend(spec.Name)
			if err := b.Provision(ctx, spec); err != nil {
				return fmt.Errorf("provisioning: %w", err)
			}
			fmt.Printf("✓ Agent-toolkit baseline installed into %s\n", spec.Name)
			fmt.Printf("  • Open a shell now: lapel shell (or lapel shell --wt for a new Windows Terminal tab)\n")
			fmt.Printf("  • Restart Windows Terminal (close all windows) to see the %q profile in the dropdown\n", spec.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.Flags().BoolVar(&systemd, "systemd", true, "enable systemd in /etc/wsl.conf (use --systemd=false for lightweight sshd-only boot)")
	return cmd
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
			m, err := config.LoadEffective(manifestPath)
			if err != nil {
				return err
			}
			res, err := reconcile.Diff(ctx, m, reconcile.NewOptions(ctx, reconcile.EnvName(m)))
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
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "path to lapel.yaml (default: discover in cwd/ancestors)")
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
